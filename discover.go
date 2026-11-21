package main

// "Find on network" for the host field (option source vmix.hosts): vMix
// PCs on this computer's networks, found by GET /api on the Web
// Controller (8088) and the TCP API's VERSION greeting (8099), plus this
// computer. Read-only: no function is ever run.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"

	"github.com/toolingdev/jadeck/sdk/addon"
)

const optHosts = "vmix.hosts"

type vmixTarget struct {
	host      string
	http, tcp int
}

var hostCache optCache

// hostOptions is the vmix.hosts option source.
func hostOptions() []addon.Option {
	return hostCache.get(func() []addon.Option {
		ctx, cancel := context.WithTimeout(context.Background(), scanBudget)
		defer cancel()
		addrs := localAddrs()
		targets := []vmixTarget{{"127.0.0.1", 8088, 8099}}
		for _, h := range scanHosts(addrs) {
			targets = append(targets, vmixTarget{h, 8088, 8099})
		}
		return dropSelf(findVmix(ctx, targets), selfHosts(addrs))
	})
}

// dropSelf leaves out this computer's own LAN address when 127.0.0.1
// already lists it.
func dropSelf(opts []addon.Option, self map[string]bool) []addon.Option {
	local := false
	for _, o := range opts {
		if strings.HasPrefix(o.Value, "127.0.0.1") {
			local = true
		}
	}
	if !local {
		return opts
	}
	out := []addon.Option{}
	for _, o := range opts {
		if h, _, err := net.SplitHostPort(o.Value); (err == nil && self[h]) || self[o.Value] {
			continue
		}
		out = append(out, o)
	}
	return out
}

var scanClient = &http.Client{Transport: &http.Transport{
	DialContext: scanDialer.DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: scanHostTimeout,
}}

func findVmix(ctx context.Context, targets []vmixTarget) []addon.Option {
	return scan(ctx, targets, probeVmix)
}

// isDrop is "accepted, then closed at once" (vMix's LAN-only setting).
func isDrop(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, ErrDropped) || errors.Is(err, syscall.EPIPE) || strings.Contains(err.Error(), "connection reset") ||
		strings.Contains(err.Error(), "forcibly closed") ||
		// net/http's name for a connection closed before any reply (seen under load)
		strings.Contains(err.Error(), "server closed idle connection")
}

func isRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "actively refused")
}

func probeVmix(ctx context.Context, t vmixTarget) (addon.Option, bool) {
	value := t.host
	if t.http != 8088 {
		value = joinHostPort(t.host, t.http)
	}
	opt := func(what string) (addon.Option, bool) {
		return addon.Option{Value: value, Label: value + " — " + what}, true
	}
	code, body, err := apiGet(ctx, "http://"+joinHostPort(t.host, t.http)+"/api/")
	if err != nil {
		switch {
		case isDrop(err):
			return opt(lanOnlyLabel)
		case !isRefused(err):
			return addon.Option{}, false // nothing there (or too slow)
		}
		// Web Controller off: is the TCP API on?
		v, _, terr := tcpHello(ctx, joinHostPort(t.host, t.tcp))
		switch {
		case terr == nil:
			return opt(strings.TrimSpace("vMix "+major(v)) + " (Web Controller off: tick Settings → Web Controller → Enabled)")
		case isDrop(terr):
			return opt(lanOnlyLabel)
		}
		return addon.Option{}, false
	}
	switch {
	case code == http.StatusUnauthorized:
		v, _, _ := tcpHello(ctx, joinHostPort(t.host, t.tcp))
		name := "vMix"
		if v != "" {
			name += " " + major(v)
		}
		return opt(name + " (password set)")
	case code == http.StatusOK && strings.Contains(body, "<vmix"):
		name := strings.TrimSpace("vMix " + major(tag(body, "version")) + " " + tag(body, "edition"))
		if _, _, terr := tcpHello(ctx, joinHostPort(t.host, t.tcp)); terr != nil && !isDrop(terr) {
			name += " · TCP API off"
		}
		return opt(name)
	}
	return addon.Option{}, false
}

const lanOnlyLabel = "vMix found but it only allows its own network (untick LAN only)"

// apiGet is one short GET; only the start of the body is read.
func apiGet(ctx context.Context, u string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := scanClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, string(b), nil
}

// tag is the text of the first <name>…</name> in s.
func tag(s, name string) string {
	_, rest, ok := strings.Cut(s, "<"+name+">")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, "</"+name+">")
	return strings.TrimSpace(v)
}

// major is "29" from "29.0.0.50".
func major(v string) string {
	m, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	if _, err := strconv.Atoi(m); err != nil {
		return ""
	}
	return m
}
