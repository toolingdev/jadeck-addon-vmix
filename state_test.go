package main

import (
	"context"
	"strings"
	"testing"
)

const (
	s1 = "1b6c0d2e-0000-4000-8000-000000000001"
	s4 = "1b6c0d2e-0000-4000-8000-000000000004"
	s5 = "1b6c0d2e-0000-4000-8000-000000000005"
	s6 = "1b6c0d2e-0000-4000-8000-000000000006"
)

func st(t *testing.T, settings map[string]any) map[string]any {
	t.Helper()
	s, err := sampleConn().AddonStateFor(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func items(v any) []map[string]any { return v.([]map[string]any) }

func labels(v any) string {
	var out []string
	for _, b := range items(v) {
		out = append(out, b["label"].(string))
	}
	return strings.Join(out, ",")
}

func TestStateBasics(t *testing.T) {
	s := st(t, nil)
	if s["connected"] != true || s["program"] != 1 || s["preview"] != 2 || s["programTitle"] != "Camera Wide" || s["version"] != "29.0.0.50" {
		t.Fatalf("%v", s)
	}
	if len(s["mixes"].([]map[string]any)) != 2 || s["mixes"].([]map[string]any)[1]["programTitle"] != "Slides" {
		t.Fatalf("mixes %v", s["mixes"])
	}
	ov := s["overlays"].([]map[string]any)
	if ov[0]["title"] != "Lower third" || ov[0]["onAir"] != true || ov[1]["preview"] != true {
		t.Fatalf("overlays %v", ov)
	}
	out := s["outputs"].(map[string]any)
	if r := out["recording"].(map[string]any); r["seconds"] != 754 || r["since"] != "2026-11-21T18:17:26Z" {
		t.Fatalf("recording %v", r)
	}
	if !strings.Contains(s["outputStatus"].(string), "Recording 12:34") || !strings.Contains(s["outputStatus"].(string), "Streaming (1) 41:00") {
		t.Fatalf("%q", s["outputStatus"])
	}
	if s["clockLabel"] != "Recording" || s["clockAt"] != "2026-11-21T18:17:26Z" {
		t.Fatalf("clock %v %v", s["clockAt"], s["clockLabel"])
	}
	tally := s["tallyText"].(string)
	if !strings.Contains(tally, "Program: 1 · Camera Wide") || !strings.Contains(tally, "Overlay 1 (on air): 5 · Lower third") || !strings.Contains(tally, "REC 12:34 · LIVE") {
		t.Fatalf("%q", tally)
	}
	if st := st(t, map[string]any{"mix": "2", "clock": "stream"}); !strings.Contains(st["tallyText"].(string), "Mix 2 Program: 3 · Slides") || st["clockLabel"] != "Streaming" {
		t.Fatalf("%v", st["tallyText"])
	}
}

func TestInputGridSettings(t *testing.T) {
	g := items(st(t, nil)["inputGrid"])
	if len(g) != 7 || g[0]["tone"] != "pgm" || g[1]["tone"] != "pvw" || g[0]["label"] != "1 · Camera Wide" || g[0]["action"] != "preview_input" {
		t.Fatalf("%v", g)
	}
	if g[3]["remaining"] != int64(157000) || g[3]["number"] != 4 {
		t.Fatalf("%v", g[3])
	}
	g = items(st(t, map[string]any{"inputs": []any{s4, "1", "gone"}, "numbers": false, "tap": "program", "mix": "2"})["inputGrid"])
	if len(g) != 2 || g[0]["label"] != "Announcements" || g[0]["action"] != "program_input" || g[0]["tone"] != "pvw" || g[1]["tone"] != nil {
		t.Fatalf("%v", g)
	}
	if p := g[0]["params"].(map[string]any); p["mix"] != 2 || p["input"] != s4 {
		t.Fatalf("%v", p)
	}
	g = items(st(t, map[string]any{"tap": "program_hold", "holdSeconds": 4.0})["inputGrid"])
	if g[0]["hold"] != 4 || g[0]["action"] != "program_input" {
		t.Fatal("hold")
	}
}

func TestTransitionsAndOverlays(t *testing.T) {
	s := st(t, map[string]any{"buttons": []any{"cut", "t2", "ftb", "bogus"}, "duration": 750.0, "stinger": 3.0})
	if labels(s["transitionBar"]) != "Cut,Transition 2 · Merge,FTB" {
		t.Fatalf("%v", labels(s["transitionBar"]))
	}
	if b := items(s["transitionBar"])[1]; b["sub"] != "1.0 s" {
		t.Fatalf("%v", b)
	}
	s = st(t, map[string]any{"duration": 750.0, "stinger": 3.0})
	bar := items(s["transitionBar"])
	if labels(bar) != "Cut,Fade,Merge,Stinger 3,FTB" || bar[1]["sub"] != "0.8 s" || bar[1]["params"].(map[string]any)["duration"] != 750 {
		t.Fatalf("%v", bar)
	}
	o := items(st(t, map[string]any{"channels": []any{"1", "2", "3"}, "overlay3": s6})["overlayButtons"])
	if labels(o) != "1 · Lower third,2 · Slides,3 · Walk-in music" || o[0]["tone"] != "pgm" || o[0]["sub"] != "ON AIR" || o[1]["tone"] != "pvw" || o[2]["tone"] != nil {
		t.Fatalf("%v", o)
	}
	if o[2]["params"].(map[string]any)["input"] != s6 {
		t.Fatalf("%v", o[2])
	}
}

func TestOutputWidgets(t *testing.T) {
	s := st(t, map[string]any{"outputs": []any{"record", "stream", "external"}})
	if labels(s["outputButtons"]) != "Stop recording,Stop stream,Start external" || items(s["outputButtons"])[0]["sub"] != "LIVE 12:34" || items(s["outputButtons"])[2]["sub"] != "OFF AIR" {
		t.Fatalf("%v", labels(s["outputButtons"]))
	}
	rows := s["outputRows"].([]map[string]any)
	if len(rows) != 3 || rows[0]["seconds"] != 754 || rows[1]["name"] != "Stream 1" || rows[1]["since"] != "2026-11-21T17:49:00Z" || rows[2]["on"] != false {
		t.Fatalf("%v", rows)
	}
	rows = st(t, map[string]any{"outputs": []any{"stream"}, "stream": 2.0})["outputRows"].([]map[string]any)
	if rows[0]["name"] != "Stream 2" || rows[0]["on"] != false || rows[0]["params"].(map[string]any)["stream"] != 2 {
		t.Fatalf("%v", rows)
	}
}

func TestTitleMediaListReplay(t *testing.T) {
	s := st(t, nil)
	ch := items(s["titleChoices"])
	if len(ch) != 2 || ch[0]["value"] != s5+"|Name.Text" || ch[0]["label"] != "Name" || s["titlePlaceholder"] != "Name now: Jane Doe" {
		t.Fatalf("%v %v", ch, s["titlePlaceholder"])
	}
	s = st(t, map[string]any{"title": s5, "fields": []any{s5 + "|Role.Text"}})
	if ch := items(s["titleChoices"]); len(ch) != 1 || ch[0]["label"] != "Role" {
		t.Fatalf("%v", ch)
	}
	if labels(s["titlePresetButtons"]) != "Previous preset,Next preset" {
		t.Fatal(labels(s["titlePresetButtons"]))
	}
	s = st(t, map[string]any{"title": s1})
	if s["titlePlaceholder"] != "No title input in vMix" && !strings.Contains(s["titlePlaceholder"].(string), "no text fields") {
		t.Fatalf("%v", s["titlePlaceholder"])
	}
	s = st(t, nil) // first media input: Announcements
	if labels(s["mediaButtons"]) != "Restart,Play,Previous,Next,Loop" || s["mediaStatus"] != "4 · Announcements — Paused 1:23 / 4:00 · loop" {
		t.Fatalf("%v %v", labels(s["mediaButtons"]), s["mediaStatus"])
	}
	if m := s["media"].(map[string]any); m["key"] != s4 || m["duration"] != int64(240000) || m["list"] != 0 {
		t.Fatalf("%v", m)
	}
	s = st(t, map[string]any{"input": s6})
	mb := items(s["mediaButtons"])
	if mb[1]["label"] != "Pause" || mb[1]["active"] != true || mb[3]["disabled"] != nil || s["media"].(map[string]any)["item"] != "song two.mp4" {
		t.Fatalf("%v", mb)
	}
	sec := s["listSections"].([]map[string]any)
	li := sec[0]["items"].([]map[string]any)
	if len(li) != 3 || li[1]["active"] != true || s["listCurrent"] != s6+"|2" || li[2]["sub"] != "skipped" {
		t.Fatalf("%v", sec)
	}
	rb := items(s["replayButtons"])
	if labels(rb) != "Mark in,Mark out,Play last,Pause,Live" || rb[4]["active"] != true || rb[0]["disabled"] != nil {
		t.Fatalf("%v", rb)
	}
}

func TestMixerStrips(t *testing.T) {
	ms := st(t, nil)["mixerStrips"].([]map[string]any)
	if len(ms) != 3 || ms[0]["id"] != "master" || ms[1]["label"] != "Stream" || ms[2]["muted"] != true {
		t.Fatalf("%v", ms)
	}
	ms = st(t, map[string]any{"strips": []any{s1, "busA", "busF", "3"}})["mixerStrips"].([]map[string]any)
	if len(ms) != 2 || ms[0]["label"] != "1 · Camera Wide" || ms[0]["kind"] != "Input 1" || ms[1]["kind"] != "Bus A" {
		t.Fatalf("%v", ms) // busF missing, input 3 has no audio
	}
}

func TestButtonPad(t *testing.T) {
	acts := []any{
		"cut",
		padEncode("overlay", "n", "1", "op", "toggle", "input", s5),
		padEncode("output", "output", "record", "op", "toggle", "confirmed", "true"),
		padEncode("audio_mute", "target", "busB"),
		padEncode("preview_input", "input", "1b6c0d2e-0000-4000-8000-0000000000ff"),
		"probe",
	}
	s := st(t, map[string]any{"actions": acts, "labels": "Take\n| green\n\n Bus B | none",
		"functions": "Music down | SetVolumeFade?Input=6&Value=20,2000 | amber\nBad | Teleport\n | "})
	b := items(s["padButtons"])
	if len(b) != 8 {
		t.Fatalf("%d %v", len(b), b)
	}
	if b[0]["label"] != "Take" || b[0]["action"] != "cut" || b[0]["tone"] != nil {
		t.Fatalf("0 %v", b[0])
	}
	if b[1]["label"] != "Overlay 1 toggle: 5 · Lower third" || b[1]["tone"] != "pvw" { // colour green from the labels
		t.Fatalf("1 %v", b[1])
	}
	if p := b[1]["params"].(map[string]any); p["n"] != 1.0 || p["op"] != "toggle" || p["input"] != s5 {
		t.Fatalf("params %v", p)
	}
	if b[2]["tone"] != "pgm" || b[2]["label"] != "Start / stop recording" || b[2]["hold"] != 5 || b[2]["params"].(map[string]any)["confirmed"] != true { // auto: it is recording
		t.Fatalf("2 %v", b[2])
	}
	if b[3]["label"] != "Bus B" || b[3]["active"] != nil { // muted, but colour none
		t.Fatalf("3 %v", b[3])
	}
	if b[4]["disabled"] != true || b[4]["label"] != "Input gone" {
		t.Fatalf("4 %v", b[4])
	}
	if b[5]["disabled"] != true {
		t.Fatalf("5 %v", b[5])
	}
	if b[6]["action"] != "run_function" || b[6]["active"] != true || b[6]["params"].(map[string]any)["value"] != "20,2000" || b[6]["params"].(map[string]any)["input"] != "6" {
		t.Fatalf("6 %v", b[6])
	}
	if b[7]["disabled"] != true {
		t.Fatalf("7 %v", b[7])
	}
	// Auto colours follow state.
	s = st(t, map[string]any{"actions": []any{padEncode("audio_mute", "target", "busB"), padEncode("preview_input", "input", "2"), "fade_to_black"}})
	b = items(s["padButtons"])
	if b[0]["active"] != true || b[1]["tone"] != "pvw" || b[2]["active"] != nil {
		t.Fatalf("%v", b)
	}
	// Every offered action decodes to a declared action with valid params.
	for _, o := range padOptions(sampleConn().snap) {
		a, p, err := padDecode(o.value)
		if err != nil || handlers[a] == nil {
			t.Errorf("%s: %v", o.value, err)
		}
		_ = p
	}
}

func TestPadActionsRunAgainstFake(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	s, _ := c.fetch(context.Background())
	for _, o := range padOptions(s) {
		if strings.Contains(o.value, "op=stop") || strings.Contains(o.value, "op=toggle") && strings.HasPrefix(o.value, "output") {
			continue
		}
		a, p, _ := padDecode(o.value)
		f.clearCalls()
		if _, err := act(t, c, a, p); err != nil && !strings.Contains(err.Error(), "No suitable") {
			t.Errorf("%s (%s): %v", o.label, o.value, err)
		}
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"NDI DESKTOP-1 (Proclaim - Livestream)": "Proclaim - Livestream",
		"NDI CAM (Chan1, 10.0.0.5)":             "Chan1, 10.0.0.5",
		"Wing Audio":                            "Wing Audio",
		"NDI ()":                                "NDI ()",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}
