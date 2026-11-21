package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toolingdev/jadeck/sdk/addon"
	"github.com/toolingdev/jadeck/sdk/addon/addontest"
)

func TestManifest(t *testing.T) {
	m := spec().Manifest
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.Version != "2.0.0" || len(m.Actions) < 30 {
		t.Fatalf("version %s, %d actions", m.Version, len(m.Actions))
	}
	sc := sampleConn()
	sources := map[string]bool{}
	for _, w := range m.Widgets {
		if w.Kind == addon.KindFrame {
			for _, f := range []string{w.Entry, strings.Replace(w.Entry, ".html", ".js", 1), strings.Replace(w.Entry, ".html", ".css", 1), "ui/kit.js", "ui/kit.css"} {
				if _, err := os.Stat(filepath.FromSlash(f)); err != nil {
					t.Errorf("widget %s: %v", w.Type, err)
				}
			}
		}
		for _, f := range w.Fields {
			if f.OptionsFrom != "" {
				sources[f.OptionsFrom] = true
			}
		}
		if w.Kind != addon.KindCore && w.Sample["connected"] != true {
			t.Errorf("widget %s: sample isn't a connected state", w.Type)
		}
	}
	for _, a := range m.Actions {
		for _, f := range a.Params {
			if f.OptionsFrom != "" {
				sources[f.OptionsFrom] = true
			}
		}
	}
	for name := range sources {
		if o, err := sc.AddonOptions(context.Background(), name); err != nil || len(o) == 0 {
			t.Errorf("options %s: %v %v", name, o, err)
		}
	}
	// Every action is handled and every handler declared.
	declared := map[string]bool{}
	for _, a := range m.Actions {
		declared[a.ID] = true
		if handlers[a.ID] == nil {
			t.Errorf("action %s has no handler", a.ID)
		}
	}
	for id := range handlers {
		if !declared[id] {
			t.Errorf("handler %s isn't declared", id)
		}
	}
	// No personal data in the public manifest.
	b, _ := json.Marshal(m)
	if em := regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`).FindAllString(string(b), -1); len(em) > 0 {
		t.Errorf("manifest has email addresses %v", em)
	}
	if ip := regexp.MustCompile(`\b(100|192\.168|10)\.\d+\.\d+(\.\d+)?\b`).FindAllString(string(b), -1); len(ip) > 1 || (len(ip) == 1 && ip[0] != "192.168.1.30") {
		t.Errorf("manifest has addresses %v", ip)
	}
	// The ui/ folder stays small.
	var total int64
	filepath.Walk("ui", func(_ string, fi os.FileInfo, _ error) error {
		if fi != nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	if total > addon.MaxUISize {
		t.Fatalf("ui/ is %d bytes", total)
	}
}

func TestHostForms(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.5": "http://10.0.0.5:8088", " http://10.0.0.5/ ": "http://10.0.0.5:8088",
		"10.0.0.5:9000": "http://10.0.0.5:8088", "::1": "http://[::1]:8088", "[fe80::1]": "http://[fe80::1]:8088",
		"vmix-pc.local": "http://vmix-pc.local:8088",
	} {
		c, err := open([]byte(`{"host":"` + in + `"}`))
		if err != nil || c.http.base != want || c.tcp.addr != strings.TrimPrefix(strings.Replace(want, "8088", "8099", 1), "http://") {
			t.Errorf("%q: %v %v", in, c, err)
		}
	}
	if _, err := open([]byte(`{"host":" "}`)); err == nil {
		t.Error("blank host accepted")
	}
	if _, err := open([]byte(`{"host":"a","port":70000}`)); err == nil {
		t.Error("bad port accepted")
	}
	c, _ := open([]byte(`{"host":"bad host","link":"http"}`))
	if _, err := c.State(context.Background()); err == nil { // used to panic on a nil request
		t.Error("bad host accepted")
	}
	c, _ = open([]byte(`{"host":"a","link":"http","username":"op","password":"x","readOnly":true}`))
	if c.tcp != nil || c.http.user != "op" || c.http.password != "x" || !c.cfg.ReadOnly {
		t.Errorf("%+v", c.cfg)
	}
}

func TestOverThePipe(t *testing.T) {
	f := newFake(t)
	h := addontest.Start(t, spec())
	if caps := h.Open("v", "vmix", f.config(nil)); len(caps) != 1 || caps[0] != addon.CapSwitcher {
		t.Fatalf("caps %v", caps)
	}
	var st addon.SwitcherState
	h.Call("v", "switcher.state", nil, &st)
	if len(st.Inputs) != 5 || st.Active != 1 {
		t.Fatalf("%+v", st)
	}
	h.Call("v", "switcher.preview", map[string]int{"input": 3}, nil)
	h.Call("v", "switcher.cut", nil, nil)
	var r map[string]any
	h.Call("v", "addon.action", map[string]any{"name": "overlay", "params": map[string]any{"n": 1, "op": "in", "input": "Lower third"}}, &r)
	if r["did"] != "in" {
		t.Fatalf("%v", r)
	}
	if err := h.TryCall("v", "addon.action", map[string]any{"name": "set_tbar", "params": map[string]any{"value": 3}}, nil); err == nil {
		t.Fatal("T-bar 3 accepted")
	}
	got := strings.Join(f.callList(), ",")
	if got != "http PreviewInput?Input="+k3+",http Cut?,http OverlayInput1In?Input="+k4 {
		t.Fatalf("%s", got)
	}
	// Widget settings shape the state.
	s := h.State("v", map[string]any{"inputs": []any{k3, "1"}, "numbers": false, "tap": "preview_take"})
	grid := s["inputGrid"].([]any)
	if len(grid) != 2 || grid[0].(map[string]any)["label"] != "Clip" || grid[0].(map[string]any)["params"].(map[string]any)["take"] != true {
		t.Fatalf("grid %v", grid)
	}
	plain := h.State("v", nil)
	if len(plain["inputGrid"].([]any)) != 5 {
		t.Fatal("plain state")
	}
	// Live options for widget fields.
	if o := h.Options("vmix.titleFields", "v"); len(o) != 2 || o[0].Value != k4+"|Name.Text" || o[0].Label != "4 · Lower third › Name" {
		t.Fatalf("%v", o)
	}
	if o := h.Options("vmix.audio", "v"); len(o) != 7 || o[1].Value != "busA" {
		t.Fatalf("%v", o)
	}
	if o := h.Options("vmix.audio", ""); len(o) != 8 { // no connection: the static list
		t.Fatalf("%v", o)
	}
	// Meters: fresh every time.
	res, err := h.ResourceResult("v", "meters?ids=master,busB,"+k1+",nope")
	if err != nil {
		t.Fatal(err)
	}
	if res.MaxAge == nil || *res.MaxAge != 0 || res.ContentType != "application/json" {
		t.Fatalf("%+v", res)
	}
	var m struct {
		Strips []map[string]any `json:"strips"`
	}
	json.Unmarshal(res.Data, &m)
	if len(m.Strips) != 3 || m.Strips[1]["id"] != "busB" || m.Strips[2]["ref"] != k1 || m.Strips[2]["meter"].([]any)[1] != 0.25 {
		t.Fatalf("%v", m.Strips)
	}
	if _, err := h.ResourceResult("v", "thumbs/1.png"); err == nil {
		t.Fatal("unknown resource served")
	}
	var probe map[string]any
	h.Call("v", "addon.action", map[string]any{"name": "probe"}, &probe)
	if probe["ok"] != true || probe["tcp"] != "ok" || probe["lanOnlyRejected"] != false {
		t.Fatalf("%v", probe)
	}
}
