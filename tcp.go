package main

// TCP: vMix's TCP API (default port 8099), the live link. Lines end in
// CRLF; the first word is the command. Replies are "CMD OK text",
// "CMD ER message", or "CMD <bytes>[ text]" followed by exactly that many
// bytes (XML). vMix says "VERSION OK x" on connect. After SUBSCRIBE TALLY
// / SUBSCRIBE ACTS it pushes "TALLY OK 0120..." and "ACTS OK Event [n] v"
// lines in between replies. No authentication; "Restrict access to LAN
// only" (on by default) closes the socket right after accepting it.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errLinkDown = errors.New("the TCP API link to vMix is down")

type tcpReply struct {
	cmd     string
	status  string // OK | ER | "" (payload)
	text    string
	payload []byte
	err     error // link went down before the reply came
}

type tcpLink struct {
	addr string
	// Callbacks (never called with l.mu held).
	onPush func(tcpReply)       // TALLY / ACTS pushes
	onUp   func(version string) // after the subscriptions are in place
	onDown func(err error)

	pingEvery, pingTimeout, hello time.Duration
	backoffMin, backoffMax        time.Duration

	wmu     sync.Mutex // one request written (and queued) at a time
	mu      sync.Mutex
	conn    net.Conn
	gone    chan struct{} // closed when the current session ends
	pending map[string][]chan tcpReply
	up      bool
	version string
	lastErr error
	closed  bool
	stop    chan struct{}
	done    chan struct{}
}

func newTCP(addr string) *tcpLink {
	return &tcpLink{
		addr: addr, pingEvery: 10 * time.Second, pingTimeout: 5 * time.Second, hello: 4 * time.Second,
		backoffMin: 500 * time.Millisecond, backoffMax: 30 * time.Second,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (l *tcpLink) start() { go l.run() }

func (l *tcpLink) close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	close(l.stop)
	if l.conn != nil {
		l.conn.Close()
	}
	l.mu.Unlock()
	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
	}
}

// status: whether the link is up, vMix's version, the last error.
func (l *tcpLink) status() (bool, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.up, l.version, l.lastErr
}

func jitter(d time.Duration) time.Duration {
	return d - d/5 + time.Duration(rand.Int64N(int64(d/5)*2+1))
}

func (l *tcpLink) run() {
	defer close(l.done)
	backoff := l.backoffMin
	for {
		start := time.Now()
		err := l.session()
		l.mu.Lock()
		closed := l.closed
		wasUp := l.up
		l.up = false
		if err != nil {
			l.lastErr = err
		}
		l.mu.Unlock()
		if closed {
			return
		}
		if l.onDown != nil && (wasUp || err != nil) {
			l.onDown(err)
		}
		if time.Since(start) > 15*time.Second {
			backoff = l.backoffMin
		}
		select {
		case <-l.stop:
			return
		case <-time.After(jitter(backoff)):
		}
		backoff = min(backoff*2, l.backoffMax)
	}
}

// session is one connection, from dial to drop.
func (l *tcpLink) session() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-l.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	d := net.Dialer{Timeout: 3 * time.Second, KeepAlive: 15 * time.Second}
	c, err := d.DialContext(ctx, "tcp", l.addr)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return netError("TCP API", l.addr, "", err)
	}
	local := c.LocalAddr().String()
	br := bufio.NewReaderSize(c, 64<<10)
	c.SetReadDeadline(time.Now().Add(l.hello))
	first, err := br.ReadString('\n')
	if err != nil {
		c.Close()
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return fmt.Errorf("vMix's TCP API at %s accepted the connection but said nothing (is that port really vMix's TCP API?)", l.addr)
		}
		return netError("TCP API", l.addr, local, err)
	}
	c.SetReadDeadline(time.Time{})
	version := ""
	if cmd, rest, _ := strings.Cut(strings.TrimRight(first, "\r\n"), " "); cmd == "VERSION" {
		version = strings.TrimSpace(strings.TrimPrefix(rest, "OK"))
	} else {
		c.Close()
		return fmt.Errorf("%s doesn't talk like vMix's TCP API (it said %q)", l.addr, clip(strings.TrimSpace(first), 60))
	}

	gone := make(chan struct{})
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		c.Close()
		return nil
	}
	l.conn, l.gone, l.pending, l.version = c, gone, map[string][]chan tcpReply{}, version
	l.mu.Unlock()

	readErr := make(chan error, 1)
	go func() { readErr <- l.readLoop(br) }()
	teardown := func() {
		c.Close()
		l.mu.Lock()
		pend := l.pending
		l.conn, l.pending = nil, nil
		close(gone)
		l.mu.Unlock()
		for _, q := range pend {
			for _, ch := range q {
				ch <- tcpReply{err: errLinkDown}
			}
		}
	}

	for _, sub := range []string{"SUBSCRIBE TALLY", "SUBSCRIBE ACTS"} {
		sctx, scancel := context.WithTimeout(ctx, l.pingTimeout)
		_, err := l.request(sctx, sub)
		scancel()
		if err != nil {
			teardown()
			<-readErr
			return fmt.Errorf("vMix's TCP API refused %s: %w", sub, err)
		}
	}
	l.mu.Lock()
	l.up, l.lastErr = true, nil
	l.mu.Unlock()
	if l.onUp != nil {
		go l.onUp(version)
	}

	t := time.NewTicker(l.pingEvery)
	defer t.Stop()
	for {
		select {
		case err := <-readErr:
			teardown()
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("vMix closed the TCP API connection (%s)", l.addr)
			}
			return fmt.Errorf("TCP API link to %s broke: %w", l.addr, err)
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, l.pingTimeout)
			_, err := l.request(pctx, "PING")
			pcancel()
			if err != nil && ctx.Err() == nil {
				teardown()
				<-readErr
				return fmt.Errorf("vMix stopped answering PING on the TCP API (%s)", l.addr)
			}
		case <-ctx.Done():
			teardown()
			<-readErr
			return nil
		}
	}
}

const maxPayload = 64 << 20

func (l *tcpLink) readLoop(br *bufio.Reader) error {
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		r, err := parseReplyLine(line)
		if err != nil {
			return err
		}
		if r.status == "" && r.payload != nil {
			if _, err := io.ReadFull(br, r.payload); err != nil {
				return err
			}
		}
		l.deliver(r)
	}
}

// parseReplyLine reads one reply line. A length-prefixed reply comes back
// with payload allocated (to be filled from the stream).
func parseReplyLine(line string) (tcpReply, error) {
	cmd, rest, _ := strings.Cut(line, " ")
	r := tcpReply{cmd: cmd}
	st, txt, _ := strings.Cut(rest, " ")
	switch st {
	case "OK", "ER":
		r.status, r.text = st, txt
	default:
		n, err := strconv.Atoi(st)
		if err != nil || n < 0 {
			r.text = rest
			return r, nil
		}
		if n > maxPayload {
			return r, fmt.Errorf("vMix announced a %d-byte reply; that's more than this add-on reads", n)
		}
		r.payload, r.text = make([]byte, n), txt
	}
	return r, nil
}

func (l *tcpLink) deliver(r tcpReply) {
	l.mu.Lock()
	if q := l.pending[r.cmd]; len(q) > 0 {
		ch := q[0]
		l.pending[r.cmd] = q[1:]
		l.mu.Unlock()
		ch <- r
		return
	}
	l.mu.Unlock()
	if (r.cmd == "TALLY" || r.cmd == "ACTS") && l.onPush != nil {
		l.onPush(r)
	}
}

// request sends one command and waits for its reply. An ER reply is an
// error carrying vMix's text.
func (l *tcpLink) request(ctx context.Context, line string) (tcpReply, error) {
	cmd, _, _ := strings.Cut(line, " ")
	ch := make(chan tcpReply, 1)
	l.wmu.Lock()
	l.mu.Lock()
	c, gone := l.conn, l.gone
	if c == nil {
		l.mu.Unlock()
		l.wmu.Unlock()
		return tcpReply{}, errLinkDown
	}
	l.pending[cmd] = append(l.pending[cmd], ch)
	l.mu.Unlock()
	c.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := io.WriteString(c, line+"\r\n")
	l.wmu.Unlock()
	if err != nil {
		c.Close() // the read loop ends and the session tears down
		return tcpReply{}, fmt.Errorf("writing to vMix's TCP API: %w", err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r, r.err
		}
		if r.status == "ER" {
			return r, fmt.Errorf("%w %q (TCP API %s)", ErrVmix, r.text, cmd)
		}
		return r, nil
	case <-gone:
		return tcpReply{}, errLinkDown
	case <-ctx.Done():
		// The reply, if it comes, lands in ch (buffered) and is dropped;
		// the queue order stays right for later requests.
		return tcpReply{}, ctx.Err()
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
