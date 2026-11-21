package main

// Probe: a read-only check of both of vMix's doors, reported in plain
// words. It reads the state XML over HTTP and says hello on the TCP API;
// it never runs a function.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

func (c *Conn) Probe(ctx context.Context) map[string]any {
	out := map[string]any{"ok": false, "readOnly": c.cfg.ReadOnly, "authNeeded": false, "lanOnlyRejected": false}
	var lines []string

	// HTTP (Web Controller)
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	b, err := c.http.get(hctx, nil)
	cancel()
	if err == nil {
		s, perr := ParseSnapshot(b)
		if perr != nil {
			err = perr
		} else {
			out["ok"] = true
			out["http"] = "ok"
			out["version"], out["edition"], out["inputs"], out["mixes"] = s.Version, s.Edition, len(s.Inputs), len(s.Mixes)
			if c.cfg.Password != "" {
				out["authOK"] = true
			}
			lines = append(lines, fmt.Sprintf("vMix %s %s, %d inputs, %d mix%s.", s.Edition, s.Version, len(s.Inputs), len(s.Mixes), plural(len(s.Mixes), "", "es")))
		}
	}
	if err != nil {
		out["http"] = err.Error()
		out["authNeeded"] = errors.Is(err, ErrAuth)
		if errors.Is(err, ErrDropped) {
			out["lanOnlyRejected"] = true
		}
		lines = append(lines, "Web Controller: "+err.Error())
	}
	if l := c.http.localAddr(); l != "" {
		out["clientAddress"] = hostOf(l)
	}

	// TCP API
	if c.tcp == nil {
		out["tcp"] = "not used (this connection is set to HTTP only)"
	} else {
		v, local, err := tcpHello(ctx, c.tcp.addr)
		if local != "" {
			out["clientAddress"] = hostOf(local)
		}
		if err != nil {
			out["tcp"] = err.Error()
			if errors.Is(err, ErrDropped) {
				out["lanOnlyRejected"] = true
			}
			lines = append(lines, "TCP API: "+err.Error())
		} else {
			out["tcp"] = "ok"
			if out["version"] == nil {
				out["version"] = v
			}
			if out["ok"] == false {
				lines = append(lines, "TCP API answers (vMix "+v+"), so live tally works, but functions and full state need the Web Controller.")
			}
		}
	}
	if c.cfg.ReadOnly {
		lines = append(lines, "Read-only: this connection won't change anything in vMix.")
	}
	out["summary"] = strings.Join(lines, " ")
	return out
}

// tcpHello connects to the TCP API, reads VERSION and says QUIT.
func tcpHello(ctx context.Context, addr string) (version, local string, err error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", "", netError("TCP API", addr, "", err)
	}
	defer conn.Close()
	local = conn.LocalAddr().String()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return "", local, fmt.Errorf("%s accepted the connection but said nothing (is it vMix's TCP API port?)", addr)
		}
		return "", local, netError("TCP API", addr, local, err)
	}
	cmd, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
	if cmd != "VERSION" {
		return "", local, fmt.Errorf("%s doesn't talk like vMix's TCP API (it said %q)", addr, clip(strings.TrimSpace(line), 60))
	}
	conn.Write([]byte("QUIT\r\n"))
	return strings.TrimSpace(strings.TrimPrefix(rest, "OK")), local, nil
}
