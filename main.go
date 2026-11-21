// vMix: a jadeck add-on. Switching on every mix, transitions, T-bar,
// overlays, recording / streaming, audio, titles, media, lists, replay,
// scripts and any of vMix's 777 functions, with live tally over vMix's
// TCP API and functions over its Web Controller.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// Version is set by the release build.
var Version = "2.0.0"

func main() { addon.Serve(spec()) }

const changelog = `2.0.0: a full vMix control surface.
- Live link over the TCP API (8099): tally and activators are pushed, state is read when something changes; PING keep-alive and reconnect with backoff. Functions over the Web Controller (8088), with vMix's own error text.
- Optional Web Controller username / password. A clear message when vMix's "Restrict access to LAN only" drops the connection (Tailscale 100.x addresses are dropped by default).
- Inputs are addressed by their key, so moving inputs in vMix doesn't send the wrong one.
- State: every mix, overlays, transitions 1-4, fade to black, recording / streaming / external / MultiCorder with times, audio master and buses A-G, per-input audio, title fields, lists, replay.
- Risky buttons (start / stop streaming and recording, a held cut) are pressed and held (3-10 s, default 5); the add-on also requires confirmed: true for those outputs.
- Actions: preview / cut on any mix, all transitions and stingers, T-bar, FTB, overlays, outputs, snapshots, volume / mute / solo / buses, title text and images, title presets, media transport, list items, replay, scripts, PTZ through vMix, run any function (checked against vMix 29's list), probe.
- Widgets: input grid, transitions, T-bar, overlays, record & stream, output status and clock, audio mixer with live meters, title editor, title presets, media transport, list items, replay, tally, and a button pad of any actions. Each has its own settings.
- Read-only connections; nothing is changed in vMix on connect.`

// joinHostPort is host:port, IPv6-safe.
func joinHostPort(h string, p int) string { return net.JoinHostPort(h, strconv.Itoa(p)) }

// normHost turns what people type into the address field into a host.
func normHost(s string) string {
	h := strings.TrimSpace(s)
	h = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(h, "http://"), "https://"), "/")
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host // "10.0.0.5:8088" typed into the address field
	}
	return strings.Trim(h, "[]")
}

func open(raw json.RawMessage) (*Conn, error) {
	cfg := config{Host: "127.0.0.1", Port: 8088, TCPPort: 8099, Link: "tcp", User: "admin"}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8088
	}
	if cfg.TCPPort == 0 {
		cfg.TCPPort = 8099
	}
	if cfg.Port < 1 || cfg.Port > 65535 || cfg.TCPPort < 1 || cfg.TCPPort > 65535 {
		return nil, fmt.Errorf("ports must be 1-65535")
	}
	if cfg.Link != "http" {
		cfg.Link = "tcp"
	}
	h := normHost(cfg.Host)
	if h == "" {
		return nil, fmt.Errorf("enter the vMix PC's address: %w", addon.ErrNotConfigured)
	}
	return newConn(cfg, h), nil
}

// ----------------------------------------------------------- samples

// sampleXML is a made-up vMix for the Settings previews (and the tests).
const sampleXML = `<vmix><version>29.0.0.50</version><edition>Pro</edition>
<inputs>
<input key="1b6c0d2e-0000-4000-8000-000000000001" number="1" type="Capture" title="Camera Wide" shortTitle="Wide" state="Running" position="0" duration="0" loop="False" muted="False" volume="100" balance="0" solo="False" audiobusses="M" meterF1="0.31" meterF2="0.29">Camera Wide</input>
<input key="1b6c0d2e-0000-4000-8000-000000000002" number="2" type="Capture" title="Camera Close" state="Running" position="0" duration="0" loop="False" muted="True" volume="80" balance="0" solo="False" audiobusses="M,A" meterF1="0" meterF2="0">Camera Close</input>
<input key="1b6c0d2e-0000-4000-8000-000000000003" number="3" type="NDI" title="Slides" state="Running" position="0" duration="0" loop="False">Slides</input>
<input key="1b6c0d2e-0000-4000-8000-000000000004" number="4" type="Video" title="Announcements" state="Paused" position="83000" duration="240000" loop="True" muted="False" volume="90" balance="0" solo="False" audiobusses="M" meterF1="0.12" meterF2="0.11">Announcements</input>
<input key="1b6c0d2e-0000-4000-8000-000000000005" number="5" type="GT" title="Lower third" state="Running" position="0" duration="0" loop="False">Lower third
<text index="0" name="Name.Text">Jane Doe</text><text index="1" name="Role.Text">Guest speaker</text><image index="0" name="Logo.Source">C:\gfx\logo.png</image></input>
<input key="1b6c0d2e-0000-4000-8000-000000000006" number="6" type="VideoList" title="Walk-in music" state="Running" position="61000" duration="200000" loop="False" muted="False" volume="70" balance="0" solo="False" audiobusses="M,B" meterF1="0.22" meterF2="0.24" selectedIndex="2">Walk-in music
<list><item>C:\media\intro.mp4</item><item selected="true">C:\media\song two.mp4</item><item enabled="false">C:\media\outro.mp4</item></list></input>
<input key="1b6c0d2e-0000-4000-8000-000000000007" number="7" type="Replay" title="Replay" state="Paused" position="0" duration="0" loop="False">Replay<replay live="True" recording="True" channelMode="AB" events="1" speed="1"><timecode>2026-11-21T10:00:00.000</timecode></replay></input>
</inputs>
<outputs><output type="output" number="2" source="Preview"/></outputs>
<overlays><overlay number="1">5</overlay><overlay number="2" preview="True">3</overlay><overlay number="3"/><overlay number="4"/><overlay number="5"/><overlay number="6"/><overlay number="7"/><overlay number="8"/><overlay number="9"/><overlay number="10"/><overlay number="11"/><overlay number="12"/><overlay number="13"/><overlay number="14"/><overlay number="15"/><overlay number="16"/></overlays>
<preview>2</preview><active>1</active><fadeToBlack>False</fadeToBlack>
<transitions><transition number="1" effect="Fade" duration="500"/><transition number="2" effect="Merge" duration="1000"/><transition number="3" effect="Wipe" duration="1000"/><transition number="4" effect="Stinger1" duration="2000"/></transitions>
<recording duration="754" filename1="C:\rec\service.mp4">True</recording>
<external>False</external>
<streaming channel1="True">True</streaming>
<playList>False</playList><multiCorder>False</multiCorder><fullscreen>False</fullscreen>
<mix number="2"><preview>4</preview><active>3</active></mix>
<audio><master volume="100" muted="False" meterF1="0.42" meterF2="0.4" headphonesVolume="75"/><busA volume="80" muted="False" meterF1="0.2" meterF2="0.2" name="Stream" solo="False" sendToMaster="False"/><busB volume="60" muted="True" meterF1="0" meterF2="0" name="" solo="False" sendToMaster="True"/></audio>
<dynamic><input1></input1><value1></value1></dynamic>
</vmix>`

var sampleAt = time.Date(2026, 11, 21, 18, 30, 0, 0, time.UTC)

// sampleConn is a Conn holding the sample state (never connects).
func sampleConn() *Conn {
	c := newConn(config{Port: 8088, TCPPort: 8099, Link: "http"}, "192.0.2.1")
	s, err := ParseSnapshot([]byte(sampleXML))
	if err != nil {
		panic(err)
	}
	c.now = func() time.Time { return sampleAt }
	c.observe(s)
	c.since["stream"] = sampleAt.Add(-41 * time.Minute)
	c.snap, c.snapAt = s, sampleAt
	return c
}

func sampleState(settings map[string]any) map[string]any {
	st, _ := sampleConn().AddonStateFor(context.Background(), settings)
	st["label"] = "vMix — main PC"
	return st
}

var coreSample = map[string]any{"/api/switcher/status": map[string]any{
	"label": "vMix — main PC", "connected": true, "version": "29.0.0.50", "edition": "Pro",
	"inputs": []map[string]any{
		{"number": 1, "key": "a", "title": "Camera Wide", "type": "Capture", "state": "Running"},
		{"number": 2, "key": "b", "title": "Camera Close", "type": "Capture", "state": "Running"},
		{"number": 3, "key": "c", "title": "Slides", "type": "NDI", "state": "Running"},
		{"number": 4, "key": "d", "title": "Announcements", "type": "Video", "state": "Paused"},
	},
	"preview": 2, "active": 1, "streaming": true, "recording": true,
}}

// ----------------------------------------------------------- widgets

var (
	fMix  = addon.Field{Key: "mix", Label: "Mix", Type: "select", Default: "1", OptionsFrom: "vmix.mixes", Help: "Main mix, or one of vMix's extra mixes."}
	fHold = addon.Field{Key: "holdSeconds", Label: "Hold to confirm (seconds)", Type: "number", Default: 5, Min: addon.Num(3), Max: addon.Num(10),
		Help: "Starting or stopping a stream or recording (and a held cut) needs the button held this long."}
	sz = func(w, h int) *addon.Size { return &addon.Size{W: w, H: h} }
)

func overlayInputFields() []addon.Field {
	var f []addon.Field
	for n := 1; n <= 8; n++ {
		f = append(f, addon.Field{Key: fmt.Sprintf("overlay%d", n), Label: fmt.Sprintf("Overlay %d input", n), Type: "select", OptionsFrom: "vmix.inputs",
			Advanced: n > 4, Help: "What the button puts on. Blank: the input in preview (like vMix's own overlay buttons)."})
	}
	return f
}

func widgets() []addon.WidgetDef {
	padSample := []any{"cut", padEncode("overlay", "n", "1", "op", "toggle", "input", "1b6c0d2e-0000-4000-8000-000000000005"),
		padEncode("output", "output", "record", "op", "toggle", "confirmed", "true"), padEncode("audio_mute", "target", "master"),
		padEncode("media", "input", "1b6c0d2e-0000-4000-8000-000000000004", "op", "play_pause"), "fade_to_black"}
	return []addon.WidgetDef{
		{Kind: "core", Type: "switcher.cutfade", Label: "Cut / Fade", Sample: coreSample},
		{Kind: "core", Type: "switcher.inputs", Label: "Input bank", Sample: coreSample},
		{Kind: "core", Type: "stage.live", Label: "Live status", Sample: coreSample, Settings: map[string]any{"mode": "stream"}},

		{Kind: addon.KindFrame, Type: "input_grid", Label: "Inputs", Description: "vMix's inputs as tiles, like a multiview: red border is program, green preview. Tap to preview; pick how a take works in the widget's settings.",
			Entry: "ui/inputs.html", Size: sz(8, 6), MinPx: &addon.Size{W: 160, H: 120},
			Fields: []addon.Field{
				{Key: "inputs", Label: "Inputs", Type: addon.FieldOrder, OptionsFrom: "vmix.inputs", Help: "Pick and order them. None picked: all of vMix's inputs, in vMix's order."},
				{Key: "tap", Label: "Tap", Type: "select", Default: "preview", Options: []addon.Option{
					{Value: "preview", Label: "Preview (take with the Transitions widget)"},
					{Value: "preview_take", Label: "Preview; tap the green one again to cut"},
					{Value: "program_hold", Label: "Cut, after a press-and-hold"},
					{Value: "program", Label: "Cut at once (no confirm)"}}},
				{Key: "numbers", Label: "Show input numbers", Type: "bool", Default: true},
				fHold,
				fMix,
			},
			Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "input_buttons", Label: "Input buttons", Description: "vMix's inputs as compact buttons: red is program, green preview. Tap to preview; pick how a take works in the widget's settings.",
			Bind: map[string]any{"items": "inputGrid"}, Size: sz(6, 4), MinPx: &addon.Size{W: 200, H: 100},
			Fields: []addon.Field{
				{Key: "inputs", Label: "Inputs", Type: addon.FieldOrder, OptionsFrom: "vmix.inputs", Help: "Pick and order them. None picked: all of vMix's inputs, in vMix's order."},
				{Key: "tap", Label: "Tap", Type: "select", Default: "preview", Options: []addon.Option{
					{Value: "preview", Label: "Preview (take with the Transitions widget)"},
					{Value: "preview_take", Label: "Preview; tap the green one again to cut"},
					{Value: "program_hold", Label: "Cut, after a press-and-hold"},
					{Value: "program", Label: "Cut at once (no confirm)"}}},
				{Key: "numbers", Label: "Show input numbers", Type: "bool", Default: true},
				fHold,
				fMix,
			},
			Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "transitions", Label: "Transitions", Description: "Cut, Fade, Merge, Stinger, fade to black and vMix's four transition buttons; pick which.",
			Bind: map[string]any{"items": "transitionBar"}, Size: sz(6, 2), MinPx: &addon.Size{W: 200, H: 64},
			Fields: []addon.Field{
				{Key: "buttons", Label: "Buttons", Type: addon.FieldOrder, Default: []any{"cut", "fade", "merge", "stinger", "ftb"}, Options: []addon.Option{
					{Value: "cut", Label: "Cut"}, {Value: "fade", Label: "Fade"}, {Value: "merge", Label: "Merge"}, {Value: "stinger", Label: "Stinger"},
					{Value: "ftb", Label: "Fade to black"}, {Value: "t1", Label: "Transition 1"}, {Value: "t2", Label: "Transition 2"},
					{Value: "t3", Label: "Transition 3"}, {Value: "t4", Label: "Transition 4"}}},
				fMix,
				{Key: "duration", Label: "Fade / Merge duration (ms)", Type: "number", Min: addon.Num(0), Max: addon.Num(60000), Help: "Blank: vMix's own."},
				{Key: "stinger", Label: "Stinger", Type: "number", Default: 1, Min: addon.Num(1), Max: addon.Num(8)},
			},
			Sample: sampleState(map[string]any{"buttons": []any{"cut", "fade", "merge", "stinger", "ftb"}})},
		{Kind: addon.KindFrame, Type: "tbar", Label: "T-bar", Description: "vMix's T-bar as a fader: drag from preview to program (main mix).",
			Entry: "ui/tbar.html", Size: sz(2, 6), MinPx: &addon.Size{W: 90, H: 140}, Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "overlays", Label: "Overlays", Description: "Overlay channels: red is on air, green is in the preview overlay. Tap to take one in or out.",
			Bind: map[string]any{"items": "overlayButtons"}, Size: sz(4, 2), MinPx: &addon.Size{W: 160, H: 64},
			Fields: append([]addon.Field{
				{Key: "channels", Label: "Channels", Type: "select", Multiple: true, Default: []any{"1", "2", "3", "4"},
					Options: []addon.Option{{Value: "1", Label: "1"}, {Value: "2", Label: "2"}, {Value: "3", Label: "3"}, {Value: "4", Label: "4"}, {Value: "5", Label: "5"}, {Value: "6", Label: "6"}, {Value: "7", Label: "7"}, {Value: "8", Label: "8"}}},
				fMix}, overlayInputFields()...),
			Sample: sampleState(nil)},
		{Kind: addon.KindFrame, Type: "outputs", Label: "Outputs", Description: "Streaming, recording, external and MultiCorder: a status dot, how long each has run, and Start / Stop (press and hold).",
			Entry: "ui/outputs.html", Size: sz(4, 4), MinPx: &addon.Size{W: 200, H: 120},
			Fields: []addon.Field{
				{Key: "outputs", Label: "Outputs", Type: addon.FieldOrder, Default: []any{"stream", "record", "external", "multicorder"}, Options: []addon.Option{
					{Value: "record", Label: "Record"}, {Value: "stream", Label: "Stream"}, {Value: "external", Label: "External"},
					{Value: "multicorder", Label: "MultiCorder"}, {Value: "fullscreen", Label: "Fullscreen"}, {Value: "playlist", Label: "Playlist"}}},
				{Key: "stream", Label: "Stream", Type: "number", Default: 0, Min: addon.Num(0), Max: addon.Num(5), Help: "0 = all streams; 1-5 = one of them."},
				fHold,
			},
			Sample: sampleState(nil)},
		{Kind: addon.KindText, Type: "output_status", Label: "Output status", Description: "Recording, streaming, external and MultiCorder, with times.",
			Bind: map[string]any{"body": "outputStatus"}, Size: sz(3, 3), MinPx: &addon.Size{W: 160, H: 80}, Sample: sampleState(nil)},
		{Kind: addon.KindClock, Type: "output_clock", Label: "Recording clock", Description: "The time, and how long vMix has been recording (or streaming).",
			Bind: map[string]any{"at": "clockAt", "label": "clockLabel"}, Size: sz(2, 3), MinPx: &addon.Size{W: 140, H: 100},
			Fields: []addon.Field{{Key: "clock", Label: "Count", Type: "select", Default: "record", Options: []addon.Option{{Value: "record", Label: "Recording"}, {Value: "stream", Label: "Streaming"}}}},
			Sample: sampleState(nil)},
		{Kind: addon.KindFrame, Type: "audio_mixer", Label: "Audio mixer", Description: "Faders with live meters and a mute on every strip: master, buses and the inputs you pick.",
			Entry: "ui/mixer.html", Size: sz(6, 8), MinPx: &addon.Size{W: 120, H: 200},
			Fields: []addon.Field{
				{Key: "strips", Label: "Strips", Type: addon.FieldOrder, OptionsFrom: "vmix.audio", Help: "Pick and order them. None picked: master and every bus."},
				{Key: "meters", Label: "Meters", Type: "bool", Default: true},
				{Key: "db", Label: "dB readout", Type: "bool", Default: true},
				{Key: "mute", Label: "Mute buttons", Type: "bool", Default: true},
				{Key: "solo", Label: "Solo buttons", Type: "bool", Default: false},
				{Key: "faders", Label: "Faders", Type: "bool", Default: true, Help: "Off: meters and mutes only (nothing to nudge by accident)."},
			},
			Sample: sampleState(map[string]any{"strips": []any{"master", "busA", "busB", "1b6c0d2e-0000-4000-8000-000000000001", "1b6c0d2e-0000-4000-8000-000000000002"}})},
		{Kind: addon.KindInput, Type: "title_editor", Label: "Title editor", Description: "Type new text into a title's field and send it to vMix. The chips pick the field.",
			Bind: map[string]any{"action": "title_text", "param": "text", "choices": "titleChoices", "choiceParam": "target", "placeholder": "titlePlaceholder", "button": "Update"},
			Size: sz(4, 3), MinPx: &addon.Size{W: 200, H: 100},
			Fields: []addon.Field{
				{Key: "title", Label: "Title input", Type: "select", OptionsFrom: "vmix.titles", Help: "Blank: the first title in vMix."},
				{Key: "fields", Label: "Fields", Type: "select", Multiple: true, OptionsFrom: "vmix.titleFields", Help: "None picked: every text field of that title."},
			},
			Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "title_presets", Label: "Title presets", Description: "Previous / next preset of a title (its saved sets of text).",
			Bind: map[string]any{"items": "titlePresetButtons", "columns": 2}, Size: sz(3, 2), MinPx: &addon.Size{W: 160, H: 64},
			Fields: []addon.Field{{Key: "title", Label: "Title input", Type: "select", OptionsFrom: "vmix.titles"}}, Sample: sampleState(nil)},
		{Kind: addon.KindFrame, Type: "media_transport", Label: "Media", Description: "A video, audio or list input: progress with position and length, restart, play, pause, next and loop.",
			Entry: "ui/media.html", Size: sz(5, 3), MinPx: &addon.Size{W: 220, H: 120},
			Fields: []addon.Field{{Key: "input", Label: "Input", Type: "select", OptionsFrom: "vmix.media", Help: "Blank: the first media input."}}, Sample: sampleState(nil)},
		{Kind: addon.KindSections, Type: "list_items", Label: "List", Description: "The items of a List input; tap one to select it.",
			Bind: map[string]any{"sections": "listSections", "current": "listCurrent", "action": "list_select", "param": "id"}, Size: sz(3, 5), MinPx: &addon.Size{W: 180, H: 120},
			Fields: []addon.Field{{Key: "list", Label: "List input", Type: "select", OptionsFrom: "vmix.lists"}}, Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "replay", Label: "Replay", Description: "Mark in, mark out, play the last event, pause and live.",
			Bind: map[string]any{"items": "replayButtons"}, Size: sz(5, 2), MinPx: &addon.Size{W: 200, H: 64}, Sample: sampleState(nil)},
		{Kind: addon.KindText, Type: "tally", Label: "Tally", Description: "What's on program and preview, the overlays on air, and whether vMix is recording or live.",
			Bind: map[string]any{"body": "tallyText"}, Size: sz(3, 3), MinPx: &addon.Size{W: 160, H: 80},
			Fields: []addon.Field{fMix}, Sample: sampleState(nil)},
		{Kind: addon.KindButtons, Type: "button_pad", Label: "Button pad", Description: "Your own buttons: pick any vMix actions (overlay 1 in with input 6, start recording, cut…), name and colour them.",
			Bind: map[string]any{"items": "padButtons"}, Size: sz(4, 3), MinPx: &addon.Size{W: 140, H: 64},
			Fields: []addon.Field{
				{Key: "actions", Label: "Buttons", Type: addon.FieldOrder, OptionsFrom: "vmix.padActions", Help: "Pick the actions and put them in order. Recording, streaming and the \"(hold)\" cuts are pressed and held."},
				fHold,
				{Key: "labels", Label: "Names and colours", Type: "textarea", Placeholder: "Lower third | red\nRecord\n | amber",
					Help: "One line per button, in order: name | colour. Colour: auto (follows vMix: red on air, green preview, amber muted / active), red, green, amber or none. Blank keeps the default."},
				{Key: "functions", Label: "More buttons (any vMix function)", Type: "textarea", Advanced: true, Placeholder: "Music down | SetVolumeFade?Input=6&Value=20,2000 | amber",
					Help: "One per line: name | Function?Input=…&Value=…&Duration=…&Mix=… | colour. Checked against vMix 29's function list."},
			},
			Sample: sampleState(map[string]any{"actions": padSample, "labels": "\nLower third\n\nMaster mute"})},
	}
}

func spec() addon.Spec {
	return addon.Spec{
		Manifest: addon.Manifest{
			ID: "vmix", Name: "vMix", Version: Version, Publisher: "jadeck", APIVersion: addon.APIVersion, Repo: "toolingdev/jadeck-addon-vmix",
			Description: "Switch, transitions, overlays, record / stream, audio, titles, media and replay in vMix.",
			Reaches:     "The vMix PCs you add as connections (vMix Web Controller, port 8088, and TCP API, port 8099)",
			ConnectionTypes: []addon.ConnectionType{{
				ID: "vmix", Label: "vMix", Capabilities: []string{addon.CapSwitcher},
				Description: "In vMix: Settings → Web Controller → tick Enabled (port 8088) and TCP API Enabled (port 8099). " +
					"\"Restrict access to LAN only\" (on by default, one box for each) lets in only 10.x, 172.16-31.x and 192.168.x addresses: untick both when jadeck reaches vMix over Tailscale (100.x) or another routed network.",
				Fields: []addon.Field{
					{Key: "host", Label: "vMix PC address", Type: "text", Default: "127.0.0.1", Placeholder: "127.0.0.1", OptionsFrom: optHosts,
						Help: "vMix PCs on this network are listed. 127.0.0.1 when vMix runs on this same PC; otherwise that PC's IP address or name (over Tailscale or a VPN, type it).", Required: true},
					{Key: "port", Label: "Web Controller port", Type: "number", Default: 8088, Min: addon.Num(1), Max: addon.Num(65535)},
					{Key: "tcpPort", Label: "TCP API port", Type: "number", Default: 8099, Min: addon.Num(1), Max: addon.Num(65535), Advanced: true},
					{Key: "link", Label: "Live updates", Type: "select", Default: "tcp", Advanced: true, Options: []addon.Option{
						{Value: "tcp", Label: "TCP API (instant tally; recommended)"}, {Value: "http", Label: "Web Controller only (reads state when asked)"}}},
					{Key: "username", Label: "Web Controller username", Type: "text", Default: "admin", Advanced: true,
						Help: "Only used with a password. vMix: Settings → Web Controller."},
					{Key: "password", Label: "Web Controller password", Type: "password",
						Help: "Only if one is set in vMix: Settings → Web Controller. (The TCP API has no password.)"},
					{Key: "readOnly", Label: "Read-only", Type: "bool", Default: false,
						Help: "Show tally and state, but never change anything in vMix (for a second screen or a tally-only PC)."},
				},
				Sample:     map[string]any{"host": "192.168.1.30", "port": 8088, "password": ""},
				SampleTest: "Connected — vMix Pro 29.0.0.50, 7 inputs, program 1, preview 2",
			}},
			Actions:   actionDefs(),
			Widgets:   widgets(),
			Changelog: changelog,
		},
		Open: map[string]addon.OpenFunc{"vmix": func(raw json.RawMessage) (any, error) {
			c, err := open(raw)
			if err != nil {
				return nil, err
			}
			c.start()
			return c, nil
		}},
		Options: options(),
	}
}

// options is the static widget lists plus "Find on network".
func options() map[string]func() []addon.Option {
	o := staticOptions()
	o[optHosts] = hostOptions
	return o
}
