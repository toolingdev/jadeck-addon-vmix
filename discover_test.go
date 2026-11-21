package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

func portOf(t *testing.T, a net.Addr) int { t.Helper(); return a.(*net.TCPAddr).Port }

func target(t *testing.T, f *fakeVmix) vmixTarget {
	return vmixTarget{"127.0.0.1", portOf(t, f.httpLn.Addr()), portOf(t, f.tcpLn.Addr())}
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	return portOf(t, ln.Addr())
}

func TestScanHosts(t *testing.T) {
	hs := scanHosts([]netip.Prefix{
		netip.MustParsePrefix("192.168.1.37/24"),
		netip.MustParsePrefix("100.101.102.103/32"),
		netip.MustParsePrefix("169.254.10.2/16"),
	})
	if len(hs) != 254 || hs[0] != "192.168.1.1" || hs[253] != "192.168.1.254" {
		t.Fatalf("%d hosts", len(hs))
	}
}

func TestFindVmix(t *testing.T) {
	plain := newFake(t)
	locked := newFake(t)
	locked.set(func(f *fakeVmix) { f.password = "s3cret" })
	lanOnly := newFake(t)
	lanOnly.set(func(f *fakeVmix) { f.reject = true })
	webOff := newFake(t)
	webOff.httpSrv.Close() // TCP API still answers
	noTCP := newFake(t)
	noTCP.tcpLn.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>printer</html>")) }))
	defer other.Close()
	otherPort := portOf(t, other.Listener.Addr())

	targets := []vmixTarget{
		target(t, plain), target(t, locked), target(t, lanOnly), target(t, webOff), target(t, noTCP),
		{"127.0.0.1", otherPort, freePort(t)},
		{"127.0.0.1", freePort(t), freePort(t)}, // nothing there
	}
	start := time.Now()
	opts := findVmix(context.Background(), targets)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("slow: %v", time.Since(start))
	}
	want := []string{"vMix 29 4K", "vMix 29 (password set)", lanOnlyLabel, "vMix 29 (Web Controller off", "vMix 29 4K · TCP API off"}
	if len(opts) != len(want) {
		t.Fatalf("%+v", opts)
	}
	for i, w := range want {
		if !strings.HasPrefix(opts[i].Label, opts[i].Value+" — "+w) {
			t.Fatalf("%d: %q, want %q", i, opts[i].Label, w)
		}
	}
	if opts[0].Value != joinHostPort("127.0.0.1", targets[0].http) {
		t.Fatalf("value %q", opts[0].Value)
	}
}

func TestFindVmixCancelled(t *testing.T) {
	var targets []vmixTarget
	for i := 1; i <= 254; i++ {
		targets = append(targets, vmixTarget{netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}).String(), 8088, 8099})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if opts := findVmix(ctx, targets); len(opts) != 0 {
		t.Fatalf("%+v", opts)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("cancelled scan took %v", time.Since(start))
	}
}

func TestDropSelfAndCache(t *testing.T) {
	opts := dropSelf([]addon.Option{{Value: "127.0.0.1"}, {Value: "192.168.1.37"}, {Value: "192.168.1.30"}}, map[string]bool{"192.168.1.37": true})
	if len(opts) != 2 || opts[1].Value != "192.168.1.30" {
		t.Fatalf("%+v", opts)
	}
	var c optCache
	n := 0
	fn := func() []addon.Option { n++; return []addon.Option{} }
	c.get(fn)
	c.get(fn)
	if n != 1 {
		t.Fatalf("scanned %d times", n)
	}
}

func TestFriendlyTestMessages(t *testing.T) {
	// The detailed explanation stays underneath the one-liner.
	f := newFake(t)
	f.set(func(f *fakeVmix) { f.password = "s3cret" })
	c := connect(t, f, map[string]any{"link": "http"})
	_, err := c.State(context.Background())
	if !strings.HasPrefix(err.Error(), "vMix has a Web Controller password") || !strings.Contains(errors.Unwrap(err).Error(), "Authentication required") {
		t.Fatalf("%v / %v", err, errors.Unwrap(err))
	}
	// Something else on the port.
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	o, _ := open([]byte(`{"host":"127.0.0.1","link":"http","port":` + strconv.Itoa(portOf(t, other.Listener.Addr())) + `}`))
	o.start()
	defer o.Close()
	if _, err := o.State(context.Background()); err == nil || !strings.Contains(err.Error(), "isn't vMix's Web Controller") {
		t.Fatalf("other: %v", err)
	}
	// LAN only: one line.
	l := newFake(t)
	l.set(func(f *fakeVmix) { f.reject = true })
	lc := connect(t, l, map[string]any{"link": "http"})
	if _, err := lc.State(context.Background()); err == nil || strings.Count(err.Error(), ".") > 2 || !strings.Contains(err.Error(), "only allows its own network") {
		t.Fatalf("lan only: %v", err)
	}
	// Unknown name.
	d, _ := open([]byte(`{"host":"no-such-host.invalid","link":"http"}`))
	d.start()
	defer d.Close()
	if _, err := d.State(context.Background()); err == nil || !strings.Contains(err.Error(), "check the vMix PC's address") {
		t.Fatalf("dns: %v", err)
	}
}
