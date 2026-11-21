package main

// Conn is one vMix connection. The TCP API (8099) is the live link:
// SUBSCRIBE TALLY + SUBSCRIBE ACTS push changes, and the state XML is
// read over the same socket when something changed (debounced) or when
// a widget asks and what we have is older than a second. Functions go
// over HTTP (8088), which reports vMix's errors ("No suitable Function
// could be found."); when HTTP can't connect they go over TCP instead.
// Without a TCP link (TCP off, blocked, or "HTTP only") the XML is read
// over HTTP on demand. Nothing that changes vMix is sent on connect.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

type config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TCPPort  int    `json:"tcpPort"`
	Link     string `json:"link"` // tcp (default) | http
	User     string `json:"username"`
	Password string `json:"password"`
	ReadOnly bool   `json:"readOnly"`
}

type Conn struct {
	cfg   config
	host  string // normalised host
	http  *httpAPI
	tcp   *tcpLink // nil: HTTP only
	now   func() time.Time
	delay time.Duration // debounce before re-reading the XML after a push

	nmu    sync.Mutex
	notify addon.Notifier

	mu       sync.Mutex
	snap     *Snapshot
	snapAt   time.Time
	snapErr  error
	fetching chan struct{}
	fetchSeq int
	tally    string
	since    map[string]time.Time // record / stream / external / multicorder: when it started
	pvwAt    map[string]time.Time // "mix|key": when this add-on put it in preview
	tbar     float64
	refresh  *time.Timer
	closed   bool
}

func newConn(cfg config, host string) *Conn {
	c := &Conn{cfg: cfg, host: host, now: time.Now, delay: 150 * time.Millisecond,
		since: map[string]time.Time{}, pvwAt: map[string]time.Time{}}
	c.http = newHTTP("http://"+joinHostPort(host, cfg.Port), cfg.User, cfg.Password, 4*time.Second)
	if cfg.Link != "http" {
		c.tcp = newTCP(joinHostPort(host, cfg.TCPPort))
		c.tcp.onPush = c.onPush
		c.tcp.onUp = func(string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c.fetch(ctx)
			c.changed()
		}
		c.tcp.onDown = func(error) { c.changed() }
	}
	return c
}

func (c *Conn) start() {
	if c.tcp != nil {
		c.tcp.start()
	}
}

// SetNotify is called by the SDK: n tells the controller to refresh.
func (c *Conn) SetNotify(n addon.Notifier) {
	c.nmu.Lock()
	c.notify = n
	c.nmu.Unlock()
}

func (c *Conn) changed() {
	c.nmu.Lock()
	n := c.notify
	c.nmu.Unlock()
	if n != nil {
		n()
	}
}

func (c *Conn) Close() error {
	c.mu.Lock()
	c.closed = true
	if c.refresh != nil {
		c.refresh.Stop()
	}
	c.mu.Unlock()
	if c.tcp != nil {
		c.tcp.close()
	}
	c.http.close()
	return nil
}

func (c *Conn) tcpUp() bool {
	if c.tcp == nil {
		return false
	}
	up, _, _ := c.tcp.status()
	return up
}

// onPush handles TALLY / ACTS lines from the TCP API.
func (c *Conn) onPush(r tcpReply) {
	switch r.cmd {
	case "TALLY":
		c.mu.Lock()
		c.tally = strings.TrimSpace(r.text)
		c.mu.Unlock()
		c.changed()
	case "ACTS":
		f := strings.Fields(r.text)
		if len(f) >= 2 {
			c.noteActivator(f[0], f[len(f)-1])
		}
	}
	c.scheduleRefresh()
}

// noteActivator keeps start times for the outputs as they change.
func (c *Conn) noteActivator(event, value string) {
	key := map[string]string{"Recording": "record", "Streaming": "stream", "External": "external", "MultiCorder": "multicorder"}[event]
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if value == "1" || strings.EqualFold(value, "true") {
		if _, ok := c.since[key]; !ok {
			c.since[key] = c.now()
		}
	} else {
		delete(c.since, key)
	}
}

// scheduleRefresh re-reads the XML shortly (pushes come in bursts).
func (c *Conn) scheduleRefresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.refresh != nil {
		return
	}
	c.refresh = time.AfterFunc(c.delay, func() {
		c.mu.Lock()
		c.refresh = nil
		c.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := c.fetch(ctx); err == nil {
			c.changed()
		}
	})
}

// readXML reads the state XML over TCP when the link is up, else HTTP.
func (c *Conn) readXML(ctx context.Context) ([]byte, error) {
	var tcpErr error
	if c.tcpUp() {
		tctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		r, err := c.tcp.request(tctx, "XML")
		cancel()
		if err == nil {
			return r.payload, nil
		}
		tcpErr = err
	}
	b, err := c.http.get(ctx, nil)
	if err != nil {
		if tcpErr == nil && c.tcp != nil {
			_, _, tcpErr = c.tcp.status()
		}
		if tcpErr != nil && !errors.Is(err, ErrAuth) {
			return nil, fmt.Errorf("%w; TCP API: %v", err, tcpErr)
		}
		return nil, err
	}
	return b, nil
}

// fetch reads the XML now: the answer comes from a read that started
// after the call (a read already under way when fetch is called may
// predate a change, so it is waited out and another started). Callers
// at the same moment share one read.
func (c *Conn) fetch(ctx context.Context) (*Snapshot, error) {
	c.mu.Lock()
	mine := c.fetchSeq // reads numbered above this started after this call
	for c.fetching != nil {
		ch, seq := c.fetching, c.fetchSeq
		c.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		if seq > mine {
			defer c.mu.Unlock()
			if c.snapErr != nil {
				return nil, c.snapErr
			}
			return c.snap, nil
		}
	}
	ch := make(chan struct{})
	c.fetching = ch
	c.fetchSeq++
	c.mu.Unlock()

	b, err := c.readXML(ctx)
	var s *Snapshot
	if err == nil {
		s, err = ParseSnapshot(b)
	}
	c.mu.Lock()
	if err == nil {
		c.observe(s)
		c.snap, c.snapAt = s, c.now()
	}
	c.snapErr = err
	c.fetching = nil
	close(ch)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s, nil
}

// snapshot returns state no older than maxAge.
func (c *Conn) snapshot(ctx context.Context, maxAge time.Duration) (*Snapshot, error) {
	c.mu.Lock()
	s, at, err := c.snap, c.snapAt, c.snapErr
	c.mu.Unlock()
	if s != nil && err == nil && c.now().Sub(at) < maxAge {
		return s, nil
	}
	return c.fetch(ctx)
}

// observe updates output start times from a new snapshot (c.mu held).
func (c *Conn) observe(s *Snapshot) {
	set := func(key string, on bool, started time.Time) {
		if !on {
			delete(c.since, key)
			return
		}
		if _, ok := c.since[key]; !ok || key == "record" {
			c.since[key] = started
		}
	}
	now := c.now()
	rec := now
	if s.RecordingSeconds > 0 {
		rec = now.Add(-time.Duration(s.RecordingSeconds) * time.Second)
	} else if t, ok := c.since["record"]; ok {
		rec = t
	}
	set("record", s.Recording, rec)
	set("stream", s.Streaming, now)
	set("external", s.External, now)
	set("multicorder", s.MultiCorder, now)
}

// fn runs a vMix function: HTTP first (it reports vMix's errors), TCP
// when HTTP can't connect.
func (c *Conn) fn(ctx context.Context, name string, q url.Values) error {
	if c.cfg.ReadOnly {
		return ErrReadOnly
	}
	err := c.http.function(ctx, name, q)
	if err != nil && !errors.Is(err, ErrVmix) && !errors.Is(err, ErrAuth) && c.tcpUp() {
		line := "FUNCTION " + name
		if e := q.Encode(); e != "" {
			line += " " + e
		}
		tctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, terr := c.tcp.request(tctx, line)
		cancel()
		if terr == nil {
			err = nil
		} else {
			err = fmt.Errorf("%w; over the TCP API too: %v", err, terr)
		}
	}
	if err != nil {
		return err
	}
	c.scheduleRefresh() // vMix runs most functions a moment later on its UI thread
	return nil
}

// ------------------------------------------------------------ switcher

func (c *Conn) State(ctx context.Context) (addon.SwitcherState, error) {
	s, err := c.snapshot(ctx, 500*time.Millisecond)
	if err != nil {
		return addon.SwitcherState{}, c.friendly(err)
	}
	m, _ := s.Mix(1)
	st := addon.SwitcherState{Version: s.Version, Edition: s.Edition, Preview: m.Preview, Active: m.Program,
		Streaming: s.Streaming, Recording: s.Recording, FadeToBlack: s.FadeToBlack, Inputs: []addon.Input{}}
	for _, in := range s.Inputs {
		st.Inputs = append(st.Inputs, addon.Input{Number: in.Number, Key: in.Key, Title: in.Title, Type: in.Type, State: in.State})
	}
	return st, nil
}

// Preview puts input number n in preview, addressed by its GUID.
func (c *Conn) Preview(ctx context.Context, n int) error {
	if c.cfg.ReadOnly {
		return ErrReadOnly
	}
	in := strconv.Itoa(n)
	if s, err := c.snapshot(ctx, 2*time.Second); err == nil {
		x := s.ByNumber(n)
		if x == nil {
			return fmt.Errorf("vMix has no input %d (it has %d)", n, len(s.Inputs))
		}
		if x.Key != "" {
			in = x.Key
		}
	}
	return c.fn(ctx, "PreviewInput", url.Values{"Input": {in}})
}

func (c *Conn) Cut(ctx context.Context) error { return c.fn(ctx, "Cut", nil) }

func (c *Conn) Fade(ctx context.Context, ms int) error {
	q := url.Values{}
	if ms > 0 {
		q.Set("Duration", strconv.Itoa(ms))
	}
	return c.fn(ctx, "Fade", q)
}

// tallyFor returns the main mix's tally digit per input number when the
// TCP link has one that matches the inputs (0 off, 1 program, 2 preview).
func (c *Conn) tallyFor(s *Snapshot) map[int]byte {
	c.mu.Lock()
	t := c.tally
	c.mu.Unlock()
	if !c.tcpUp() || len(t) != len(s.Inputs) {
		return nil
	}
	out := map[int]byte{}
	for i := range t {
		out[i+1] = t[i]
	}
	return out
}
