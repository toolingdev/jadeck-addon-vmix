package main

// HTTP: vMix's Web Controller (default port 8088). GET /api is the state
// XML, GET /api?Function=...&Input=... runs a function. Optional Basic
// auth (vMix: Settings → Web Controller → username / password; an empty
// password there means no auth at all).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Errors a caller can tell apart (wrapped with vMix's own words).
var (
	// ErrDropped: vMix accepted the connection and closed it at once,
	// which is what "Restrict access to LAN only" does to clients outside
	// 10/8, 172.16/12 and 192.168/16 (and what the TCP API does past 25
	// clients).
	ErrDropped = errors.New("vMix dropped the connection")
	// ErrAuth: the Web Controller wants a (different) username/password.
	ErrAuth = errors.New("vMix wants a Web Controller password")
	// ErrVmix: vMix answered with an error of its own (the text follows).
	ErrVmix = errors.New("vMix said")
	// ErrRefused: nothing listens on that port (vMix closed, or that
	// door is off in its settings).
	ErrRefused = errors.New("vMix isn't listening")
	// ErrNoAnswer: no answer at all (wrong address, firewall, PC off).
	ErrNoAnswer = errors.New("no answer from vMix")
	// ErrReadOnly: the connection is set to read-only.
	ErrReadOnly = errors.New("this vMix connection is read-only (its settings say so)")
)

const lanOnlyHelp = "vMix accepted the connection and closed it straight away. That is vMix's " +
	"\"Restrict access to LAN only\" setting: it only lets in 10.x, 172.16-31.x and 192.168.x addresses, " +
	"so a Tailscale (100.x) or other routed address is dropped. In vMix: Settings → Web Controller → untick " +
	"\"Restrict access to LAN only\" (there is one box for the Web Controller and one for the TCP API), then OK"

type httpAPI struct {
	base     string // http://host:port
	user     string
	password string
	client   *http.Client

	mu    sync.Mutex
	local string // our side of the last connection (to explain LAN-only drops)
}

func newHTTP(base, user, password string, timeout time.Duration) *httpAPI {
	h := &httpAPI{base: strings.TrimRight(base, "/"), user: user, password: password}
	d := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 15 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := d.DialContext(ctx, network, addr)
			if err == nil {
				h.mu.Lock()
				h.local = c.LocalAddr().String()
				h.mu.Unlock()
			}
			return c, err
		},
		MaxIdleConnsPerHost: 2,
		// vMix closes an idle keep-alive socket after 30 s.
		IdleConnTimeout:       20 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableCompression:    true,
	}
	h.client = &http.Client{Timeout: timeout, Transport: tr}
	return h
}

func (h *httpAPI) localAddr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.local
}

func (h *httpAPI) close() { h.client.CloseIdleConnections() }

// get fetches /api with q; the body on 200, or an error carrying vMix's text.
func (h *httpAPI) get(ctx context.Context, q url.Values) ([]byte, error) {
	u := h.base + "/api/"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("bad vMix address: %w", err)
	}
	if h.password != "" {
		user := h.user
		if user == "" {
			user = "admin"
		}
		req.SetBasicAuth(user, h.password)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, netError("Web Controller", h.base, h.localAddr(), err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, netError("Web Controller", h.base, h.localAddr(), err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return b, nil
	case resp.StatusCode == http.StatusUnauthorized:
		if h.password == "" {
			return nil, fmt.Errorf("%w: vMix answered %q. Fill in the username (usually admin) and password from vMix Settings → Web Controller", ErrAuth, vmixText(b, resp.Status))
		}
		return nil, fmt.Errorf("%w: vMix refused the username/password (%q). Check them against vMix Settings → Web Controller", ErrAuth, vmixText(b, resp.Status))
	default:
		return nil, fmt.Errorf("%w %q (HTTP %d)", ErrVmix, vmixText(b, resp.Status), resp.StatusCode)
	}
}

// function runs one vMix function over HTTP.
func (h *httpAPI) function(ctx context.Context, name string, q url.Values) error {
	v := url.Values{"Function": {name}}
	for k, vs := range q {
		v[k] = vs
	}
	_, err := h.get(ctx, v)
	return err
}

// vmixText is vMix's error body as one short line.
func vmixText(b []byte, status string) string {
	t := strings.Join(strings.Fields(string(b)), " ")
	if t == "" {
		return status
	}
	if r := []rune(t); len(r) > 300 {
		t = string(r[:300]) + "…"
	}
	return t
}

// netError explains a failed connection in an operator's words.
func netError(what, addr, local string, err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.ECONNABORTED),
		strings.Contains(err.Error(), "connection reset"), strings.Contains(err.Error(), "forcibly closed"),
		strings.Contains(err.Error(), "server closed idle connection"):
		msg := lanOnlyHelp + "."
		if isTailscale(local) {
			msg += fmt.Sprintf(" (This computer reached vMix from %s, a Tailscale address.)", hostOf(local))
		}
		if what == "TCP API" {
			msg += " It also happens when vMix already has 25 TCP API clients."
		}
		return fmt.Errorf("%w (%s at %s): %s", ErrDropped, what, strings.TrimPrefix(addr, "http://"), msg)
	case errors.Is(err, syscall.ECONNREFUSED), strings.Contains(err.Error(), "connection refused"), strings.Contains(err.Error(), "actively refused"):
		hint := "is vMix running, and is Settings → Web Controller enabled?"
		if what == "TCP API" {
			hint = "is vMix running, and is Settings → Web Controller → TCP API enabled?"
		}
		return fmt.Errorf("%w at %s (%s): %s", ErrRefused, strings.TrimPrefix(addr, "http://"), what, hint)
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w at %s (%s): wrong address, a firewall, or the PC is off", ErrNoAnswer, strings.TrimPrefix(addr, "http://"), what)
	}
	return fmt.Errorf("vMix not reachable at %s (%s): %w", strings.TrimPrefix(addr, "http://"), what, err)
}

func hostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// isTailscale reports whether addr (host or host:port) is in 100.64.0.0/10.
func isTailscale(addr string) bool {
	ip := net.ParseIP(hostOf(addr))
	if ip == nil {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}
