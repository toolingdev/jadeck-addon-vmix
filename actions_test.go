package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Every action with its parameters, and the exact vMix call it makes.
func TestActionsSendTheRightFunction(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	cases := []struct {
		name string
		p    map[string]any
		want string // "Name?query" (url.Values.Encode order)
	}{
		{"preview_input", map[string]any{"input": 3}, "PreviewInput?Input=" + k3},
		{"preview_input", map[string]any{"input": "Camera 2"}, "PreviewInput?Input=" + k2},
		{"preview_input", map[string]any{"input": k4, "mix": 2}, "PreviewInput?Input=" + k4 + "&Mix=1"},
		{"program_input", map[string]any{"input": "1"}, "Cut?Input=" + k1},
		{"program_input", map[string]any{"input": 2, "mix": 2}, "Cut?Input=" + k2 + "&Mix=1"},
		{"cut", nil, "Cut?"},
		{"cut", map[string]any{"mix": 2}, "Cut?Mix=1"},
		{"cut", map[string]any{"input": 4}, "Cut?Input=" + k4},
		{"fade", map[string]any{"duration": 750}, "Fade?Duration=750"},
		{"fade", map[string]any{"mix": 2, "duration": 0}, "Fade?Mix=1"},
		{"merge", map[string]any{"duration": 1000}, "Merge?Duration=1000"},
		{"stinger", map[string]any{"n": 3, "mix": 2}, "Stinger3?Mix=1"},
		{"transition", map[string]any{"effect": "wipe", "duration": 400}, "Wipe?Duration=400"},
		{"transition", map[string]any{"effect": "Stinger2"}, "Stinger2?"},
		{"transition_button", map[string]any{"n": 4}, "Transition4?"},
		{"set_tbar", map[string]any{"value": 0.5}, "SetFader?Value=128"},
		{"set_tbar", map[string]any{"value": 1}, "SetFader?Value=255"},
		{"fade_to_black", nil, "FadeToBlack?"},
		{"overlay", map[string]any{"n": 1, "op": "in", "input": 4}, "OverlayInput1In?Input=" + k4},
		{"overlay", map[string]any{"n": 1, "op": "in", "input": 4, "mix": 2}, "OverlayInput1In?Input=" + k4 + "&Mix=1"},
		{"overlay", map[string]any{"n": 2, "op": "out"}, "OverlayInput2Out?"},
		{"overlay", map[string]any{"n": 3, "op": "off"}, "OverlayInput3Off?"},
		{"overlay", map[string]any{"n": 4, "op": "preview", "input": 5}, "PreviewOverlayInput4?Input=" + k5},
		{"overlay", map[string]any{"n": 5}, "OverlayInput5In?"}, // toggle, off now → in (preview input)
		{"overlay_all_off", nil, "OverlayInputAllOff?"},
		{"output", map[string]any{"output": "record", "op": "start", "confirmed": true}, "StartRecording?"},
		{"output", map[string]any{"output": "record", "confirmed": true}, "StopRecording?"}, // toggle: it is recording now
		{"output", map[string]any{"output": "stream", "op": "start", "stream": 2, "confirmed": true}, "StartStreaming?Value=1"},
		{"output", map[string]any{"output": "stream", "op": "stop", "confirmed": true}, "StopStreaming?"},
		{"output", map[string]any{"output": "external", "op": "start", "confirmed": true}, "StartExternal?"},
		{"output", map[string]any{"output": "multicorder", "op": "start", "confirmed": true}, "StartMultiCorder?"},
		{"output", map[string]any{"output": "fullscreen", "op": "stop"}, "FullscreenOff?"},
		{"output", map[string]any{"output": "playlist", "op": "start"}, "StartPlayList?"},
		{"snapshot", nil, "Snapshot?"},
		{"snapshot", map[string]any{"input": 1, "file": `C:\s\a.png`}, "SnapshotInput?Input=" + k1 + "&Value=C%3A%5Cs%5Ca.png"},
		{"audio_volume", map[string]any{"target": "master", "value": 80}, "SetMasterVolume?Value=80"},
		{"audio_volume", map[string]any{"target": "master", "value": 50, "fade": 2000}, "SetMasterVolumeFade?Value=50%2C2000"},
		{"audio_volume", map[string]any{"target": "headphones", "value": 60}, "SetHeadphonesVolume?Value=60"},
		{"audio_volume", map[string]any{"target": "busC", "value": 30}, "SetBusCVolume?Value=30"},
		{"audio_volume", map[string]any{"target": "Bus B", "value": 30, "fade": 500}, "SetBusBVolumeFade?Value=30%2C500"},
		{"audio_volume", map[string]any{"target": 3, "value": 42.4}, "SetVolume?Input=" + k3 + "&Value=42"},
		{"audio_volume", map[string]any{"target": "Clip", "value": 10, "fade": 100}, "SetVolumeFade?Input=" + k3 + "&Value=10%2C100"},
		{"audio_mute", map[string]any{"target": "master", "muted": true}, "MasterAudioOff?"},
		{"audio_mute", map[string]any{"target": "master", "muted": false}, "MasterAudioOn?"},
		{"audio_mute", map[string]any{"target": "master"}, "MasterAudio?"},
		{"audio_mute", map[string]any{"target": "busA", "muted": "true"}, "BusXAudioOff?Value=A"},
		{"audio_mute", map[string]any{"target": 1, "muted": true}, "AudioOff?Input=" + k1},
		{"audio_solo", map[string]any{"target": 2, "on": true}, "SoloOn?Input=" + k2},
		{"audio_solo", map[string]any{"target": "busG", "on": false}, "BusXSoloOff?Value=G"},
		{"audio_bus", map[string]any{"input": 1, "bus": "a", "on": true}, "AudioBusOn?Input=" + k1 + "&Value=A"},
		{"audio_bus", map[string]any{"input": 1, "bus": "M"}, "AudioBus?Input=" + k1 + "&Value=M"},
		{"title_text", map[string]any{"input": 4, "field": "Name", "text": "John Smith"}, "SetText?Input=" + k4 + "&SelectedName=Name.Text&Value=John+Smith"},
		{"title_text", map[string]any{"target": k4 + "|Role.Text", "text": "Host"}, "SetText?Input=" + k4 + "&SelectedName=Role.Text&Value=Host"},
		{"title_text", map[string]any{"input": "Lower third", "field": "role", "text": ""}, "SetText?Input=" + k4 + "&SelectedName=Role.Text&Value="},
		{"title_preset", map[string]any{"input": 4}, "NextTitlePreset?Input=" + k4},
		{"title_preset", map[string]any{"input": 4, "op": "prev"}, "PreviousTitlePreset?Input=" + k4},
		{"title_preset", map[string]any{"input": 4, "op": "select", "index": 2}, "SelectTitlePreset?Input=" + k4 + "&Value=2"},
		{"media", map[string]any{"input": 3, "op": "play"}, "Play?Input=" + k3},
		{"media", map[string]any{"input": 3, "op": "pause"}, "Pause?Input=" + k3},
		{"media", map[string]any{"input": 3, "op": "play_pause"}, "PlayPause?Input=" + k3},
		{"media", map[string]any{"input": 3, "op": "restart"}, "Restart?Input=" + k3},
		{"media", map[string]any{"input": 5, "op": "next"}, "NextItem?Input=" + k5},
		{"media", map[string]any{"input": 5, "op": "prev"}, "PreviousItem?Input=" + k5},
		{"media", map[string]any{"input": 3, "op": "loop_on"}, "LoopOn?Input=" + k3},
		{"media", map[string]any{"input": 3, "op": "seek", "position": 30000}, "SetPosition?Input=" + k3 + "&Value=30000"},
		{"list_select", map[string]any{"input": 5, "index": 2}, "SelectIndex?Input=" + k5 + "&Value=2"},
		{"list_select", map[string]any{"id": k5 + "|1"}, "SelectIndex?Input=" + k5 + "&Value=1"},
		{"replay", map[string]any{"op": "mark_in"}, "ReplayMarkIn?"},
		{"replay", map[string]any{"op": "mark_out"}, "ReplayMarkOut?"},
		{"replay", map[string]any{"op": "mark_in_out", "seconds": 8}, "ReplayMarkInOut?Value=8"},
		{"replay", map[string]any{"op": "play_last", "channel": "B"}, "ReplayPlayLastEvent?Channel=B"},
		{"replay", map[string]any{"op": "play_last_to_output"}, "ReplayPlayLastEventToOutput?"},
		{"replay", map[string]any{"op": "live"}, "ReplayLive?"},
		{"script", map[string]any{"name": "Walk in"}, "ScriptStart?Value=Walk+in"},
		{"script", map[string]any{"name": "Walk in", "op": "stop"}, "ScriptStop?Value=Walk+in"},
		{"script_stop_all", nil, "ScriptStopAll?"},
		{"ptz_preset", map[string]any{"input": 2}, "PTZMoveToVirtualInputPosition?Input=" + k2},
		{"ptz_home", map[string]any{"input": 2}, "PTZHome?Input=" + k2},
		{"run_function", map[string]any{"function": "setvolumefade", "input": 3, "value": "20,2000"}, "SetVolumeFade?Input=" + k3 + "&Value=20%2C2000"},
		{"run_function", map[string]any{"function": "Wipe", "duration": 300, "mix": 2}, "Wipe?Duration=300&Mix=1"},
		{"run_function", map[string]any{"function": "SetText", "input": 4, "value": "x", "selectedName": "Name.Text"}, "SetText?Input=" + k4 + "&SelectedName=Name.Text&Value=x"},
		{"run_function", map[string]any{"function": "ReplayPlay", "channel": "a"}, "ReplayPlay?Channel=A"},
	}
	f.set(func(f *fakeVmix) { f.recording, f.streaming = true, true })
	c.fetch(context.Background())
	for _, tc := range cases {
		f.clearCalls()
		if _, err := act(t, c, tc.name, tc.p); err != nil {
			t.Errorf("%s %v: %v", tc.name, tc.p, err)
			continue
		}
		calls := f.callList()
		if len(calls) != 1 || calls[0] != "http "+tc.want {
			t.Errorf("%s %v: sent %v, want %q", tc.name, tc.p, calls, tc.want)
		}
		if tc.name == "output" { // keep the toggle cases deterministic
			f.set(func(f *fakeVmix) { f.recording, f.streaming = true, true })
			c.fetch(context.Background())
		}
	}
	// set_tbar to 1 completes; the bar starts over.
	c.mu.Lock()
	tb := c.tbar
	c.mu.Unlock()
	if tb != 0 {
		t.Fatalf("tbar %v", tb)
	}
}

func TestActionValidation(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	cases := []struct {
		name string
		p    map[string]any
		want string
	}{
		{"preview_input", nil, "input is required"},
		{"preview_input", map[string]any{"input": 9}, "no input 9"},
		{"preview_input", map[string]any{"input": "Nope"}, `no input "Nope"`},
		{"preview_input", map[string]any{"input": 1, "mix": 3}, "no Mix 3"},
		{"preview_input", map[string]any{"input": 1, "mix": 17}, "between 1 and 16"},
		{"preview_input", map[string]any{"input": 1, "mix": 1.5}, "whole number"},
		{"cut", map[string]any{"duration": 5}, "takes no duration"},
		{"fade", map[string]any{"duration": -1}, "between 0 and 60000"},
		{"stinger", map[string]any{"n": 9}, "between 1 and 8"},
		{"transition", map[string]any{"effect": "Sparkle"}, "transitions"},
		{"transition_button", nil, "n is required"},
		{"set_tbar", map[string]any{"value": 2}, "between 0 and 1"},
		{"set_tbar", nil, "value is required"},
		{"overlay", map[string]any{"n": 9}, "between 1 and 8"},
		{"overlay", map[string]any{"n": 1, "op": "flip"}, "op must be one of"},
		{"output", map[string]any{"output": "tape"}, "output must be one of"},
		{"output", map[string]any{"output": "record", "stream": 2}, "only for output stream"},
		{"output", map[string]any{"output": "record", "op": "start"}, "needs confirmed: true"},
		{"output", map[string]any{"output": "stream", "confirmed": false}, "press and hold"},
		{"output", map[string]any{"output": "stream", "stream": 6}, "between 0 and 5"},
		{"audio_volume", map[string]any{"target": "master", "value": 101}, "between 0 and 100"},
		{"audio_volume", map[string]any{"target": "master"}, "value is required"},
		{"audio_volume", map[string]any{"value": 10}, "target is required"},
		{"audio_volume", map[string]any{"target": "headphones", "value": 10, "fade": 100}, "can't fade"},
		{"audio_mute", map[string]any{"target": "headphones"}, "no mute"},
		{"audio_mute", map[string]any{"target": "master", "muted": "maybe"}, "true, false"},
		{"audio_solo", map[string]any{"target": "master"}, "no solo"},
		{"audio_bus", map[string]any{"input": 1, "bus": "H"}, "bus must be one of"},
		{"title_text", map[string]any{"input": 4, "field": "Nope", "text": "x"}, "Name.Text, Role.Text"},
		{"title_text", map[string]any{"input": 1, "field": "Name", "text": "x"}, "no title fields"},
		{"title_text", map[string]any{"target": "nobar", "text": "x"}, "target must be"},
		{"title_text", map[string]any{"input": 4, "field": "Name", "text": strings.Repeat("x", 4001)}, "too long"},
		{"title_image", map[string]any{"input": 4, "field": "Logo"}, "no image fields"},
		{"title_preset", map[string]any{"input": 4, "op": "select"}, "index is required"},
		{"media", map[string]any{"input": 3}, "op is required"},
		{"media", map[string]any{"input": 3, "op": "seek"}, "position"},
		{"media", map[string]any{"input": 3, "op": "seek", "position": 70000}, "between 0 and 60000"},
		{"list_select", map[string]any{"input": 5, "index": 3}, "between 1 and 2"},
		{"list_select", map[string]any{"id": "x"}, "id must be"},
		{"replay", map[string]any{"op": "fly"}, "op must be one of"},
		{"replay", map[string]any{"op": "mark_in", "channel": "A"}, "has no channel"},
		{"script", nil, "name is required"},
		{"run_function", nil, "function is required"},
		{"run_function", map[string]any{"function": "Teleport"}, "no function"},
		{"run_function", map[string]any{"function": "ScriptStartDynamic", "value": "x"}, "blocks ScriptStartDynamic"},
		{"run_function", map[string]any{"function": "Cut", "value": "1"}, "doesn't take a value"},
		{"run_function", map[string]any{"function": "FadeToBlack", "input": 1}, "doesn't take an input"},
		{"run_function", map[string]any{"function": "Transition1", "mix": 2}, "doesn't take a mix"},
		{"run_function", map[string]any{"function": "SetVolume", "input": 1, "value": "150"}, "out of range"},
		{"run_function", map[string]any{"function": "SetVolume", "input": 1, "value": "loud"}, "needs a number"},
		{"run_function", map[string]any{"function": "SetFader", "value": "1.5"}, "whole number"},
		{"run_function", map[string]any{"function": "Cut", "channel": "A"}, "doesn't take a replay channel"},
		{"nope", nil, "unknown action"},
	}
	for _, tc := range cases {
		f.clearCalls()
		_, err := act(t, c, tc.name, tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %v: got %v, want %q", tc.name, tc.p, err, tc.want)
		}
		if calls := f.callList(); len(calls) != 0 {
			t.Errorf("%s %v: still sent %v", tc.name, tc.p, calls)
		}
	}
}

func TestReadOnly(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, map[string]any{"readOnly": true})
	for _, a := range []string{"cut", "preview_input", "output", "run_function", "audio_mute"} {
		if _, err := act(t, c, a, map[string]any{"input": 1, "output": "record", "function": "Cut", "target": "master"}); !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: %v", a, err)
		}
	}
	if err := c.Preview(context.Background(), 1); !errors.Is(err, ErrReadOnly) {
		t.Error("preview")
	}
	if err := c.Cut(context.Background()); !errors.Is(err, ErrReadOnly) {
		t.Error("cut")
	}
	if err := c.Fade(context.Background(), 500); !errors.Is(err, ErrReadOnly) {
		t.Error("fade")
	}
	p, err := act(t, c, "probe", nil)
	if err != nil || p["ok"] != true || p["readOnly"] != true || p["tcp"] != "ok" || p["version"] != "29.0.0.50" || p["inputs"] != 5 {
		t.Fatalf("probe %v %v", p, err)
	}
	if r, err := act(t, c, "refresh", nil); err != nil || r["inputs"] != 5 {
		t.Fatalf("refresh %v %v", r, err)
	}
	if calls := f.callList(); len(calls) != 0 {
		t.Fatalf("read-only sent %v", calls)
	}
	st, _ := c.AddonState(context.Background())
	if st["readOnly"] != true || st["connected"] != true {
		t.Fatalf("%v", st)
	}
}

func TestSwitcherAddressesByKey(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	ctx := context.Background()
	if err := c.Preview(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err := c.Cut(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Fade(ctx, 750); err != nil {
		t.Fatal(err)
	}
	if err := c.Fade(ctx, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{"http PreviewInput?Input=" + k2, "http Cut?", "http Fade?Duration=750", "http Fade?"}
	if got := f.callList(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", got)
	}
	// The operator swaps inputs 1 and 2 in vMix: number 2 is now the other camera.
	f.set(func(f *fakeVmix) { f.inputs[0], f.inputs[1] = f.inputs[1], f.inputs[0] })
	c.fetch(ctx)
	f.clearCalls()
	c.Preview(ctx, 2)
	if got := f.callList(); len(got) != 1 || got[0] != "http PreviewInput?Input="+k1 {
		t.Fatalf("%v", got)
	}
	if err := c.Preview(ctx, 9); err == nil {
		t.Fatal("input 9 accepted")
	}
}

func TestTapAgainToTake(t *testing.T) {
	f := newFake(t)
	clk := &testClock{t: time.Unix(1_800_000_000, 0)}
	c := connect(t, f, nil, clk)
	c.fetch(context.Background())
	p := map[string]any{"input": 3, "take": true}
	if _, err := act(t, c, "preview_input", p); err != nil {
		t.Fatal(err)
	}
	c.fetch(context.Background())
	r, _ := act(t, c, "preview_input", p) // a double tap
	if r["ignored"] == nil {
		t.Fatalf("double tap took: %v", r)
	}
	clk.Add(time.Second)
	r, _ = act(t, c, "preview_input", p)
	if r["took"] != "3 · Clip" {
		t.Fatalf("%v", r)
	}
	want := []string{"http PreviewInput?Input=" + k3, "http Cut?Input=" + k3}
	if got := f.callList(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", got)
	}
	// Preview set in vMix itself (not by us): one tap takes it.
	f.clearCalls()
	f.run("vmix", "PreviewInput", url.Values{"Input": {"2"}})
	c.fetch(context.Background())
	f.clearCalls()
	if r, _ := act(t, c, "preview_input", map[string]any{"input": 2, "take": true}); r["took"] == nil {
		t.Fatalf("%v", r)
	}
}

func TestHoldToConfirm(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	f.set(func(f *fakeVmix) { f.recording = true })
	c.fetch(context.Background())
	// Risky outputs need confirmed: true (the dashboard sends it after a hold).
	for _, out := range []string{"record", "stream", "external", "multicorder"} {
		if _, err := act(t, c, "output", map[string]any{"output": out}); err == nil || !strings.Contains(err.Error(), "confirmed") {
			t.Errorf("%s without confirmed: %v", out, err)
		}
	}
	if len(f.callList()) != 0 {
		t.Fatalf("%v", f.callList())
	}
	// Fullscreen and the playlist are a plain tap.
	if _, err := act(t, c, "output", map[string]any{"output": "fullscreen", "op": "start"}); err != nil {
		t.Fatal(err)
	}
	f.clearCalls()
	if r, _ := act(t, c, "output", map[string]any{"output": "record", "confirmed": true}); r["did"] != "stop" {
		t.Fatalf("%v", r)
	}
	if got := f.callList(); len(got) != 1 || got[0] != "http StopRecording?" {
		t.Fatalf("%v", got)
	}
	// The widgets carry the hold time and send confirmed.
	st, _ := c.AddonStateFor(context.Background(), map[string]any{"outputs": []any{"record", "fullscreen"}, "holdSeconds": 7.0, "tap": "program_hold"})
	ob := st["outputButtons"].([]map[string]any)
	if ob[0]["hold"] != 7 || ob[0]["params"].(map[string]any)["confirmed"] != true || ob[1]["hold"] != nil {
		t.Fatalf("%v", ob)
	}
	rows := st["outputRows"].([]map[string]any)
	if rows[0]["hold"] != 7 || rows[1]["hold"] != nil {
		t.Fatalf("%v", rows)
	}
	g := st["inputGrid"].([]map[string]any)
	if g[0]["hold"] != 7 || g[0]["action"] != "program_input" || g[0]["holdLabel"] != "Keep holding…" {
		t.Fatalf("%v", g[0])
	}
	st, _ = c.AddonStateFor(context.Background(), map[string]any{"holdSeconds": 30.0}) // out of range: default
	if st["outputButtons"].([]map[string]any)[0]["hold"] != 5 {
		t.Fatal("hold default")
	}
	if g := st["inputGrid"].([]map[string]any); g[0]["hold"] != nil {
		t.Fatal("plain preview tiles don't hold")
	}
}

func TestOverlayToggle(t *testing.T) {
	f := newFake(t)
	c := connect(t, f, nil)
	f.set(func(f *fakeVmix) { f.overlays[1] = 4 })
	c.fetch(context.Background())
	if r, _ := act(t, c, "overlay", map[string]any{"n": 1, "input": 4}); r["did"] != "out" {
		t.Fatalf("%v", r)
	}
	if got := f.callList(); got[0] != "http OverlayInput1Out?" {
		t.Fatalf("%v", got)
	}
}
