package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// connect opens a Conn on the fake with fast timings and waits for the
// TCP link (unless link is http).
func connect(t *testing.T, f *fakeVmix, extra map[string]any, clk ...*testClock) *Conn {
	t.Helper()
	raw, _ := json.Marshal(f.config(extra))
	c, err := open(raw)
	if err != nil {
		t.Fatal(err)
	}
	c.delay = 10 * time.Millisecond
	if len(clk) > 0 {
		c.now = clk[0].Now
	}
	if c.tcp != nil {
		c.tcp.backoffMin, c.tcp.backoffMax = 20*time.Millisecond, 100*time.Millisecond
		c.tcp.hello = time.Second
	}
	c.start()
	t.Cleanup(func() { c.Close() })
	f.mu.Lock()
	rej := f.reject
	f.mu.Unlock()
	if c.tcp != nil && !rej {
		waitFor(t, "TCP link up", c.tcpUp)
	}
	return c
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func act(t *testing.T, c *Conn, name string, p map[string]any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(p)
	r, err := c.AddonAction(context.Background(), name, raw)
	if err != nil {
		return nil, err
	}
	m, _ := r.(map[string]any)
	return m, nil
}

const k1, k2, k3, k4, k5 = "aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002",
	"aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004", "aaaaaaaa-0000-4000-8000-000000000005"

// ------------------------------------------------------------- parsing

func TestParseSnapshot(t *testing.T) {
	s, err := ParseSnapshot([]byte(sampleXML))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "29.0.0.50" || s.Edition != "Pro" || len(s.Inputs) != 7 {
		t.Fatalf("%+v", s)
	}
	in1 := s.ByNumber(1)
	if in1.Key != "1b6c0d2e-0000-4000-8000-000000000001" || in1.ShortTitle != "Wide" || !in1.HasAudio || in1.Volume != 100 || in1.Meter[0] != 0.31 || len(in1.Buses) != 1 {
		t.Fatalf("input 1 %+v", in1)
	}
	if s.ByNumber(2).Muted != true || s.ByNumber(3).HasAudio {
		t.Fatal("audio attrs")
	}
	clip := s.ByNumber(4)
	if clip.Position != 83000 || clip.Duration != 240000 || !clip.Loop || clip.State != "Paused" || !clip.IsMedia() {
		t.Fatalf("clip %+v", clip)
	}
	gt := s.ByNumber(5)
	if len(gt.Fields) != 3 || gt.Fields[0] != (Field{"Name.Text", "text", "Jane Doe"}) || gt.Fields[2].Kind != "image" || !gt.IsTitle() {
		t.Fatalf("title %+v", gt.Fields)
	}
	list := s.ByNumber(6)
	if len(list.List) != 3 || list.Selected != 2 || list.List[1].Title != "song two.mp4" || list.List[2].Enabled {
		t.Fatalf("list %+v", list.List)
	}
	if r := s.ByNumber(7).Replay; r == nil || !r.Live || !r.Recording || r.Events != 1 {
		t.Fatalf("replay %+v", r)
	}
	if len(s.Mixes) != 2 || s.Mixes[0] != (Mix{1, 1, 2}) || s.Mixes[1] != (Mix{2, 3, 4}) {
		t.Fatalf("mixes %+v", s.Mixes)
	}
	if len(s.Overlays) != 16 || s.Overlay(1).Input != 5 || !s.Overlay(2).Preview || s.Overlay(3).Input != 0 {
		t.Fatalf("overlays %+v", s.Overlays)
	}
	if len(s.Transitions) != 4 || s.Transitions[3].Effect != "Stinger1" || s.Transitions[1].Duration != 1000 {
		t.Fatalf("transitions %+v", s.Transitions)
	}
	if !s.Recording || s.RecordingSeconds != 754 || !s.Streaming || len(s.StreamChannels) != 1 || s.StreamChannels[0] != 1 || s.External {
		t.Fatalf("outputs %+v", s)
	}
	if s.Master.Volume != 100 || s.Master.Headphones != 75 || len(s.Buses) != 2 || s.Buses[0].Name != "Stream" || !s.Buses[1].Muted || !s.Buses[1].SendToMaster {
		t.Fatalf("audio %+v %+v", s.Master, s.Buses)
	}
	if _, err := ParseSnapshot([]byte("<html/>")); err == nil {
		t.Fatal("non-vMix XML accepted")
	}
}

func TestParseReplyLine(t *testing.T) {
	for line, want := range map[string]tcpReply{
		"VERSION OK 29.0.0.50":               {cmd: "VERSION", status: "OK", text: "29.0.0.50"},
		"FUNCTION ER Function not supported": {cmd: "FUNCTION", status: "ER", text: "Function not supported"},
		"ACTS OK Input 2 1":                  {cmd: "ACTS", status: "OK", text: "Input 2 1"},
		"FOO ER Unknown Command":             {cmd: "FOO", status: "ER", text: "Unknown Command"},
	} {
		r, err := parseReplyLine(line)
		if err != nil || r.cmd != want.cmd || r.status != want.status || r.text != want.text {
			t.Errorf("%q: %+v %v", line, r, err)
		}
	}
	r, _ := parseReplyLine("XML 1234")
	if len(r.payload) != 1234 || r.status != "" {
		t.Fatalf("%+v", r)
	}
	if _, err := parseReplyLine("XML 999999999"); err == nil {
		t.Fatal("huge payload accepted")
	}
}

// ----------------------------------------------------------- transport

func TestConnectChangesNothing(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	st, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Inputs) != 5 || st.Active != 1 || st.Preview != 2 || st.Edition != "4K" || st.Inputs[0].Key != k1 {
		t.Fatalf("%+v", st)
	}
	if _, err := c.AddonState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := f.callList(); len(calls) != 0 {
		t.Fatalf("connect ran functions: %v", calls)
	}
	f.mu.Lock()
	reads := f.xmlReads
	f.mu.Unlock()
	if reads == 0 {
		t.Fatal("no XML read")
	}
}

func TestTCPXMLWithUTF8AndPushes(t *testing.T) {
	f := newFake(t)
	f.set(func(f *fakeVmix) { f.inputs[0].title = "Cámara ✝ 1" })
	c := connect(t, f, map[string]any{"port": 1}) // HTTP closed: XML must come over TCP
	s, err := c.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.ByNumber(1).Title != "Cámara ✝ 1" {
		t.Fatalf("title %q", s.ByNumber(1).Title)
	}
	// Many XML requests interleaved with pushes stay in order.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); f.run("vmix", "PreviewInput", map[string][]string{"Input": {"3"}}) }()
		go func() {
			defer wg.Done()
			r, err := c.tcp.request(context.Background(), "XML")
			if err != nil || !strings.HasPrefix(string(r.payload), "<vmix>") {
				t.Errorf("XML reply %v %q", err, clip(string(r.payload), 20))
			}
		}()
	}
	wg.Wait()
}

func TestTallyPushRefreshes(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	var n atomic.Int32
	c.SetNotify(func() { n.Add(1) })
	c.fetch(context.Background())
	// The operator changes preview in vMix itself.
	f.run("vmix", "PreviewInput", map[string][]string{"Input": {"4"}})
	waitFor(t, "notify", func() bool { return n.Load() > 0 })
	waitFor(t, "preview 4", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.snap == nil {
			return false
		}
		m, _ := c.snap.Mix(1)
		return m.Preview == 4 && c.tally == "10020"
	})
	st, _ := c.AddonState(context.Background())
	grid := st["inputGrid"].([]map[string]any)
	if grid[0]["tone"] != "pgm" || grid[3]["tone"] != "pvw" || grid[1]["tone"] != nil {
		t.Fatalf("grid %v", grid)
	}
	if st["link"] != "tcp" {
		t.Fatalf("link %v", st["link"])
	}
}

func TestReconnect(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	f.dropTCP()
	waitFor(t, "link down", func() bool { return !c.tcpUp() })
	waitFor(t, "link back", c.tcpUp)
	if _, err := c.tcp.request(context.Background(), "XML"); err != nil {
		t.Fatal(err)
	}
}

func TestPingKeepalive(t *testing.T) {
	f := newFake(t)
	raw, _ := json.Marshal(f.config(nil))
	c, _ := open(raw)
	c.tcp.pingEvery, c.tcp.pingTimeout = 30*time.Millisecond, 30*time.Millisecond
	c.tcp.backoffMin, c.tcp.backoffMax = 20*time.Millisecond, 50*time.Millisecond
	c.start()
	defer c.Close()
	waitFor(t, "up", c.tcpUp)
	f.set(func(f *fakeVmix) { f.noPong = true })
	waitFor(t, "PING failure noticed", func() bool {
		_, _, err := c.tcp.status()
		return err != nil && strings.Contains(err.Error(), "PING")
	})
	f.set(func(f *fakeVmix) { f.noPong = false })
	waitFor(t, "up again", c.tcpUp)
}

func TestLANOnlyRejection(t *testing.T) {
	f := newFake(t)
	f.set(func(f *fakeVmix) { f.reject = true })
	c := connect(t, f, nil)
	_, err := c.State(context.Background())
	if !errors.Is(err, ErrDropped) || !strings.Contains(err.Error(), "Restrict access to LAN only") {
		t.Fatalf("state error: %v", err)
	}
	waitFor(t, "TCP error", func() bool {
		_, _, e := c.tcp.status()
		return e != nil && errors.Is(e, ErrDropped) && strings.Contains(e.Error(), "25 TCP API clients")
	})
	p := c.Probe(context.Background())
	if p["lanOnlyRejected"] != true || p["ok"] != false || !strings.Contains(p["summary"].(string), "LAN only") {
		t.Fatalf("probe %v", p)
	}
	st, _ := c.AddonState(context.Background())
	if st["connected"] != false || !strings.Contains(st["error"].(string), "LAN only") {
		t.Fatalf("state %v", st)
	}
	if !isTailscale("100.101.102.103:5000") || isTailscale("192.168.1.2") || isTailscale("100.128.0.1") {
		t.Fatal("isTailscale")
	}
}

func TestBasicAuth(t *testing.T) {
	f := newFake(t)
	f.set(func(f *fakeVmix) { f.password = "s3cret" })
	c := connect(t, f, map[string]any{"link": "http"})
	_, err := c.State(context.Background())
	if !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "username") || !strings.Contains(err.Error(), "Settings → Web Controller") {
		t.Fatalf("no password: %v", err)
	}
	if p := c.Probe(context.Background()); p["authNeeded"] != true {
		t.Fatalf("probe %v", p)
	}
	bad := connect(t, f, map[string]any{"link": "http", "password": "nope"})
	if _, err := bad.State(context.Background()); !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("wrong password: %v", err)
	}
	good := connect(t, f, map[string]any{"link": "http", "password": "s3cret"})
	if _, err := good.State(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := act(t, good, "cut", nil); err != nil {
		t.Fatal(err)
	}
	if p := good.Probe(context.Background()); p["ok"] != true || p["authOK"] != true || p["inputs"] != 5 {
		t.Fatalf("probe %v", p)
	}
	// With TCP up, a function refused for auth is not quietly re-sent over TCP.
	tc := connect(t, f, nil)
	f.clearCalls()
	if _, err := act(t, tc, "cut", nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("cut without password: %v", err)
	}
	if calls := f.callList(); len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
}

func TestVmixErrorText(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	_, err := act(t, c, "run_function", map[string]any{"function": "OMTPreviewOn", "input": 1})
	if !errors.Is(err, ErrVmix) || !strings.Contains(err.Error(), "No suitable Function could be found.") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%v", err)
	}
}

func TestFunctionsOverTCPWhenHTTPIsDown(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	c.fetch(context.Background())
	f.httpSrv.Close()
	f.clearCalls()
	if _, err := act(t, c, "preview_input", map[string]any{"input": 3}); err != nil {
		t.Fatal(err)
	}
	if calls := f.callList(); len(calls) != 1 || calls[0] != "tcp PreviewInput?Input="+k3 {
		t.Fatalf("calls %v", calls)
	}
}

func TestHTTPOnlyLink(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, map[string]any{"link": "http"})
	if c.tcp != nil {
		t.Fatal("TCP link made")
	}
	st, _ := c.AddonState(context.Background())
	if st["connected"] != true || st["link"] != "http" {
		t.Fatalf("%v", st)
	}
}

func TestUnreachable(t *testing.T) {
	c, _ := open([]byte(`{"host":"127.0.0.1","port":1,"tcpPort":1}`))
	c.start()
	defer c.Close()
	_, err := c.State(context.Background())
	if err == nil || !strings.Contains(err.Error(), "isn't listening") {
		t.Fatalf("%v", err)
	}
}
