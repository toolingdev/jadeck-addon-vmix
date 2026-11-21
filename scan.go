package main

// Network scan for "Find on network": the /24 around each of this
// computer's IPv4 addresses, probed with bounded concurrency, short
// timeouts and a total budget. Read-only: probes only GET status pages.

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

const (
	scanDialTimeout = 400 * time.Millisecond // per address: connect
	scanHostTimeout = 900 * time.Millisecond // per address: connect + answer
	scanWorkers     = 64
	scanBudget      = 3800 * time.Millisecond // whole scan
	scanCacheFor    = 30 * time.Second
)

var tailscaleNet = netip.MustParsePrefix("100.64.0.0/10")

// localAddrs is each up interface's IPv4 address with its prefix.
func localAddrs() []netip.Prefix {
	var out []netip.Prefix
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(n.IP.To4())
			if !ok {
				continue
			}
			ones, bits := n.Mask.Size()
			ones -= bits - 32
			if ones < 0 || ones > 32 {
				ones = 32
			}
			out = append(out, netip.PrefixFrom(ip, ones))
		}
	}
	return out
}

// scanHosts lists the hosts to probe: the /24 around each address
// (loopback, link-local and Tailscale's 100.64/10 skipped: a Tailscale
// address is typed, not scanned), in interface order, without repeats.
func scanHosts(addrs []netip.Prefix) []string {
	var out []string
	seen := map[netip.Prefix]bool{}
	for _, p := range addrs {
		ip := p.Addr()
		if !ip.Is4() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || tailscaleNet.Contains(ip) || ip.IsUnspecified() {
			continue
		}
		n := netip.PrefixFrom(ip, 24).Masked()
		if seen[n] {
			continue
		}
		seen[n] = true
		b := n.Addr().As4()
		for i := 1; i <= 254; i++ {
			b[3] = byte(i)
			out = append(out, netip.AddrFrom4(b).String())
		}
	}
	return out
}

// selfHosts is this computer's own IPv4 addresses.
func selfHosts(addrs []netip.Prefix) map[string]bool {
	m := map[string]bool{}
	for _, p := range addrs {
		m[p.Addr().String()] = true
	}
	return m
}

// scan probes every target with at most scanWorkers at a time and
// returns what answered, in target order. It stops early when ctx ends.
func scan[T any](ctx context.Context, targets []T, probe func(ctx context.Context, t T) (addon.Option, bool)) []addon.Option {
	found := make([]*addon.Option, len(targets))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := min(scanWorkers, len(targets))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				hctx, cancel := context.WithTimeout(ctx, scanHostTimeout)
				if o, ok := probe(hctx, targets[i]); ok {
					found[i] = &o
				}
				cancel()
			}
		}()
	}
feed:
	for i := range targets {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	out := []addon.Option{}
	for _, o := range found {
		if o != nil {
			out = append(out, *o)
		}
	}
	return out
}

// dialer is the scan's connect: short, no keep-alive.
var scanDialer = &net.Dialer{Timeout: scanDialTimeout, KeepAlive: -1}

// hostPort is host:port.
func hostPort(h string, p int) string { return net.JoinHostPort(h, strconv.Itoa(p)) }

// optCache keeps the last scan for scanCacheFor; callers at the same
// time share one scan.
type optCache struct {
	mu   sync.Mutex
	at   time.Time
	opts []addon.Option
}

func (c *optCache) get(fn func() []addon.Option) []addon.Option {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.opts != nil && time.Since(c.at) < scanCacheFor {
		return c.opts
	}
	c.opts, c.at = fn(), time.Now()
	return c.opts
}
