package main

// The connection test's one-liner: what's wrong and what to change. The
// full explanation stays underneath (errors.Is / Unwrap, and the probe).

import (
	"context"
	"errors"
	"net"
	"strings"
)

// friendlyError shows a short line and keeps the detailed error.
type friendlyError struct {
	msg string
	err error
}

func (e *friendlyError) Error() string { return e.msg }
func (e *friendlyError) Unwrap() error { return e.err }

// friendly turns a failed state read into one line saying what to fix.
func (c *Conn) friendly(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	web := joinHostPort(c.host, c.cfg.Port)
	var dns *net.DNSError
	var msg string
	switch {
	case errors.Is(err, ErrAuth) && c.cfg.Password == "":
		msg = "vMix has a Web Controller password — fill in the username (usually admin) and password from vMix: Settings → Web Controller."
	case errors.Is(err, ErrAuth):
		msg = "vMix refused the username/password — check them against vMix: Settings → Web Controller."
	case errors.Is(err, ErrDropped):
		msg = "vMix only allows its own network — in vMix: Settings → Web Controller, untick \"Restrict access to LAN only\" (Web Controller and TCP API)."
		if isTailscale(c.http.localAddr()) {
			msg += " This computer reaches it over Tailscale."
		}
	case errors.As(err, &dns):
		msg = "No computer called \"" + c.host + "\" was found — check the vMix PC's address."
	case errors.Is(err, ErrRefused):
		msg = "vMix isn't listening on " + web + " — open vMix and tick Settings → Web Controller → Enabled (and check the port)."
	case errors.Is(err, ErrNoAnswer), errors.Is(err, context.DeadlineExceeded):
		msg = "No answer from " + c.host + " — check the address, that the PC is on, and that its firewall lets in vMix (ports 8088 and 8099)."
	case errors.Is(err, ErrVmix), strings.Contains(err.Error(), "unreadable XML"):
		msg = web + " answers, but it isn't vMix's Web Controller — check the address and port."
	default:
		return err
	}
	return &friendlyError{msg: msg, err: err}
}
