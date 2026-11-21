package main

// The add-on's state: what the dashboard widgets bind to (main.go's
// manifest). AddonStateFor gets the asking widget's settings (v2.1) and
// fills that widget's keys for it (which mix, which inputs, which title
// …); without settings the defaults apply (main mix, all inputs, the
// first title / media / list input).

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

// ------------------------------------------------------------ settings

type wset map[string]any

func (w wset) str(k, def string) string {
	switch v := w[k].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return def
}

func (w wset) bool(k string, def bool) bool {
	switch v := w[k].(type) {
	case bool:
		return v
	case string:
		return map[string]bool{"true": true, "false": false}[v] || (v == "" && def)
	}
	return def
}

func (w wset) int(k string, def, lo, hi int) int {
	n := def
	switch v := w[k].(type) {
	case float64:
		n = int(v)
	case string:
		if x, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = x
		}
	}
	if n < lo || n > hi {
		return def
	}
	return n
}

func (w wset) list(k string) []string {
	var out []string
	switch v := w[k].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = append(out, v...)
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func sortedStrings(xs []string) []string { sort.Strings(xs); return xs }

// clock is h:mm:ss (or m:ss under an hour).
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// --------------------------------------------------------------- state

func (c *Conn) AddonState(ctx context.Context) (map[string]any, error) {
	return c.AddonStateFor(ctx, nil)
}

// AddonStateFor is the state, with the widget keys filled in for the
// asking widget's settings.
func (c *Conn) AddonStateFor(ctx context.Context, settings map[string]any) (map[string]any, error) {
	w := wset(settings)
	out := map[string]any{
		"connected": false, "readOnly": c.cfg.ReadOnly, "link": "http", "error": "",
		"inputs": []any{}, "mixes": []any{}, "overlays": []any{}, "transitions": []any{},
		"inputGrid": []any{}, "transitionBar": []any{}, "transitionButtons": []any{}, "overlayButtons": []any{},
		"outputButtons": []any{}, "outputStatus": "", "clockAt": "", "clockLabel": "",
		"titleChoices": []any{}, "titlePlaceholder": "", "titlePresetButtons": []any{},
		"mediaButtons": []any{}, "mediaStatus": "", "media": nil, "outputRows": []any{}, "listSections": []any{}, "listCurrent": "",
		"replayButtons": []any{}, "padButtons": []any{}, "tallyText": "", "mixerStrips": []any{}, "tbar": 0.0,
	}
	if c.tcp != nil {
		up, _, terr := c.tcp.status()
		t := map[string]any{"up": up, "error": ""}
		if terr != nil && !up {
			t["error"] = terr.Error()
		}
		out["tcp"] = t
		if up {
			out["link"] = "tcp"
		}
	}
	s, err := c.snapshot(ctx, time.Second)
	if err != nil {
		out["error"] = err.Error()
		out["tallyText"] = "vMix isn't answering: " + err.Error()
		out["outputStatus"] = "vMix isn't answering"
		return out, nil
	}
	c.mu.Lock()
	since := map[string]time.Time{}
	for k, v := range c.since {
		since[k] = v
	}
	out["tbar"] = c.tbar
	c.mu.Unlock()
	now := c.now()
	tally := c.tallyFor(s)

	out["connected"] = true
	out["version"], out["edition"], out["preset"] = s.Version, s.Edition, s.Preset

	// inputs, with tally for the main mix
	main, _ := s.Mix(1)
	inputs := make([]map[string]any, 0, len(s.Inputs))
	for i := range s.Inputs {
		in := &s.Inputs[i]
		m := map[string]any{"number": in.Number, "key": in.Key, "title": in.Title, "shortTitle": in.ShortTitle, "type": in.Type,
			"state": in.State, "loop": in.Loop, "position": in.Position, "duration": in.Duration,
			"hasAudio": in.HasAudio, "muted": in.Muted, "volume": in.Volume, "solo": in.Solo, "buses": in.Buses, "meter": in.Meter,
			"tally": tone(in.Number, main, tally)}
		if len(in.Fields) > 0 {
			m["fields"] = in.Fields
		}
		if len(in.List) > 0 {
			m["list"], m["selectedIndex"] = in.List, in.Selected
		}
		if in.Replay != nil {
			m["replay"] = in.Replay
			out["replay"] = in.Replay
		}
		inputs = append(inputs, m)
	}
	out["inputs"] = inputs
	title := func(n int) (string, string) {
		if in := s.ByNumber(n); in != nil {
			return in.Title, in.Key
		}
		return "", ""
	}
	mixes := []map[string]any{}
	for _, m := range s.Mixes {
		pt, pk := title(m.Program)
		vt, vk := title(m.Preview)
		mixes = append(mixes, map[string]any{"number": m.Number, "program": m.Program, "preview": m.Preview,
			"programTitle": pt, "programKey": pk, "previewTitle": vt, "previewKey": vk})
	}
	out["mixes"] = mixes
	out["program"], out["preview"] = main.Program, main.Preview
	out["programTitle"], _ = title(main.Program)
	out["previewTitle"], _ = title(main.Preview)
	ovs := []map[string]any{}
	for _, o := range s.Overlays {
		t, k := title(o.Input)
		ovs = append(ovs, map[string]any{"number": o.Number, "input": o.Input, "key": k, "title": t, "preview": o.Preview, "onAir": o.Input != 0 && !o.Preview})
	}
	out["overlays"] = ovs
	out["transitions"] = s.Transitions
	out["fadeToBlack"] = s.FadeToBlack
	iso := func(k string) string {
		if t, ok := since[k]; ok {
			return t.UTC().Format(time.RFC3339)
		}
		return ""
	}
	recSecs := s.RecordingSeconds
	if s.Recording && recSecs == 0 {
		if t, ok := since["record"]; ok {
			recSecs = int(now.Sub(t).Seconds())
		}
	}
	out["outputs"] = map[string]any{
		"recording":   map[string]any{"on": s.Recording, "seconds": recSecs, "since": iso("record"), "file": s.RecordingFile},
		"streaming":   map[string]any{"on": s.Streaming, "channels": orEmpty(s.StreamChannels), "since": iso("stream")},
		"external":    map[string]any{"on": s.External, "since": iso("external")},
		"multiCorder": map[string]any{"on": s.MultiCorder, "since": iso("multicorder")},
		"fullscreen":  s.Fullscreen, "playList": s.PlayList,
	}
	out["audio"] = map[string]any{"master": s.Master, "buses": orEmptyB(s.Buses)}

	mix := w.int("mix", 1, 1, 16)
	if _, ok := s.Mix(mix); !ok {
		mix = 1
	}
	out["inputGrid"] = c.inputGrid(s, w, mix, tally)
	out["transitionBar"] = transitionBar(s, w, mix)
	out["transitionButtons"] = transitionButtons(s)
	out["overlayButtons"] = overlayButtons(s, w)
	out["outputButtons"], out["outputStatus"] = outputs(s, w, since, recSecs, now)
	which := w.str("clock", "record")
	if which == "stream" {
		out["clockAt"], out["clockLabel"] = iso("stream"), map[bool]string{true: "Streaming", false: "Not streaming"}[s.Streaming]
	} else {
		out["clockAt"], out["clockLabel"] = iso("record"), map[bool]string{true: "Recording", false: "Not recording"}[s.Recording]
	}
	out["titleChoices"], out["titlePlaceholder"], out["titlePresetButtons"] = titleEditor(s, w)
	out["mediaButtons"], out["mediaStatus"] = media(s, w)
	out["media"] = mediaInfo(s, w)
	out["outputRows"] = outputRows(s, w, since, recSecs)
	out["listSections"], out["listCurrent"] = lists(s, w)
	out["replayButtons"] = replayButtons(s)
	out["padButtons"] = c.padButtons(s, w, tally)
	out["tallyText"] = tallyText(s, mix, recSecs)
	out["mixerStrips"] = mixerStrips(s, w)
	return out, nil
}

func orEmpty(x []int) []int {
	if x == nil {
		return []int{}
	}
	return x
}

func orEmptyB(x []Bus) []Bus {
	if x == nil {
		return []Bus{}
	}
	return x
}

// tone is "pgm", "pvw" or "" for an input on the main mix: the TCP tally
// when there is one (it counts overlays and layers on air), else the XML.
func tone(n int, m Mix, tally map[int]byte) string {
	if tally != nil {
		switch tally[n] {
		case '1':
			return "pgm"
		case '2':
			return "pvw"
		}
		return ""
	}
	switch n {
	case m.Program:
		return "pgm"
	case m.Preview:
		return "pvw"
	}
	return ""
}

func btn(id, label, action string, p map[string]any) map[string]any {
	b := map[string]any{"id": id, "label": label, "action": action}
	if p != nil {
		b["params"] = p
	}
	return b
}

func (c *Conn) inputGrid(s *Snapshot, w wset, mix int, tally map[int]byte) []map[string]any {
	m, _ := s.Mix(mix)
	if mix != 1 {
		tally = nil
	}
	pick := s.Inputs
	if want := w.list("inputs"); len(want) > 0 {
		pick = nil
		for _, ref := range want {
			if in, _, err := s.find(ref); err == nil {
				pick = append(pick, *in)
			}
		}
	}
	mode := w.str("tap", "preview")
	numbers := w.bool("numbers", true)
	hold := holdSeconds(w)
	out := []map[string]any{}
	for i := range pick {
		in := &pick[i]
		p := map[string]any{"input": keyOr(in), "mix": mix}
		action := "preview_input"
		switch mode {
		case "preview_take":
			p["take"] = true
		case "program_hold", "program_confirm": // program_confirm: 2.0 drafts
			action = "program_input"
		case "program":
			action = "program_input"
		}
		label := in.Title
		if numbers {
			label = in.Label()
		}
		b := btn(keyOr(in), label, action, p)
		if t := tone(in.Number, m, tally); t != "" {
			b["tone"] = t
		}
		// For the tile grid frame: the parts of the label, and a media clock.
		b["number"], b["title"], b["type"], b["state"] = in.Number, in.Title, in.Type, in.State
		if mode == "program_hold" || mode == "program_confirm" {
			b["hold"], b["holdLabel"] = hold, "Keep holding…"
		}
		if in.Duration > 0 {
			b["remaining"] = in.Duration - in.Position
		}
		out = append(out, b)
	}
	return out
}

func transitionBar(s *Snapshot, w wset, mix int) []map[string]any {
	mp := func(extra map[string]any) map[string]any {
		p := map[string]any{"mix": mix}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	dur := map[string]any{}
	if d := w.int("duration", 0, 0, 60000); d > 0 {
		dur["duration"] = d
	}
	n := w.int("stinger", 1, 1, 8)
	ftb := btn("ftb", "FTB", "fade_to_black", nil)
	ftb["active"] = s.FadeToBlack
	fade, merge := btn("fade", "Fade", "fade", mp(dur)), btn("merge", "Merge", "merge", mp(dur))
	if d, ok := dur["duration"].(int); ok {
		fade["sub"] = fmt.Sprintf("%.1f s", float64(d)/1000)
		merge["sub"] = fade["sub"]
	}
	all := map[string]map[string]any{
		"cut":     btn("cut", "Cut", "cut", mp(nil)),
		"fade":    fade,
		"merge":   merge,
		"stinger": btn("stinger", fmt.Sprintf("Stinger %d", n), "stinger", mp(map[string]any{"n": n})),
		"ftb":     ftb,
	}
	for _, t := range transitionButtons(s) {
		all[t["id"].(string)] = t
	}
	which := w.list("buttons")
	if len(which) == 0 {
		which = []string{"cut", "fade", "merge", "stinger", "ftb"}
	}
	out := []map[string]any{}
	for _, k := range which {
		if b, ok := all[k]; ok {
			out = append(out, b)
		}
	}
	return out
}

func transitionButtons(s *Snapshot) []map[string]any {
	out := []map[string]any{}
	for n := 1; n <= 4; n++ {
		b := btn(fmt.Sprintf("t%d", n), fmt.Sprintf("Transition %d", n), "transition_button", map[string]any{"n": n})
		for _, t := range s.Transitions {
			if t.Number == n {
				b["label"] = fmt.Sprintf("Transition %d · %s", n, t.Effect)
				b["sub"] = fmt.Sprintf("%.1f s", float64(t.Duration)/1000)
			}
		}
		out = append(out, b)
	}
	return out
}

func overlayButtons(s *Snapshot, w wset) []map[string]any {
	out := []map[string]any{}
	mix := w.int("mix", 1, 1, 16)
	chans := w.list("channels")
	if len(chans) == 0 {
		chans = []string{"1", "2", "3", "4"}
	}
	for _, ch := range chans {
		n, err := strconv.Atoi(ch)
		if err != nil || n < 1 || n > 8 {
			continue
		}
		o := s.Overlay(n)
		p := map[string]any{"n": n, "op": "toggle"}
		if mix > 1 {
			p["mix"] = mix
		}
		label := fmt.Sprintf("%d · off", n)
		if ref := w.str(fmt.Sprintf("overlay%d", n), ""); ref != "" {
			if in, key, err := s.find(ref); err == nil {
				p["input"] = key
				label = fmt.Sprintf("%d · %s", n, in.Title)
			}
		}
		b := btn(fmt.Sprintf("o%d", n), label, "overlay", p)
		if in := s.ByNumber(o.Input); in != nil {
			b["label"] = fmt.Sprintf("%d · %s", n, in.Title)
			if o.Preview {
				b["tone"], b["sub"] = "pvw", "PREVIEW"
			} else {
				b["tone"], b["sub"] = "pgm", "ON AIR"
			}
		}
		out = append(out, b)
	}
	return out
}

func outputs(s *Snapshot, w wset, since map[string]time.Time, recSecs int, now time.Time) ([]map[string]any, string) {
	which := w.list("outputs")
	if len(which) == 0 {
		which = []string{"record", "stream"}
	}
	stream := w.int("stream", 0, 0, 5)
	elapsed := func(k string) string {
		if t, ok := since[k]; ok {
			return " " + clock(now.Sub(t))
		}
		return ""
	}
	type o struct {
		label string
		on    bool
		time  string
	}
	streamOn := s.Streaming
	if stream > 0 {
		streamOn = containsInt(s.StreamChannels, stream)
	}
	info := map[string]o{
		"record":      {"Record", s.Recording, " " + clock(time.Duration(recSecs)*time.Second)},
		"stream":      {"Stream", streamOn, elapsed("stream")},
		"external":    {"External", s.External, elapsed("external")},
		"multicorder": {"MultiCorder", s.MultiCorder, elapsed("multicorder")},
		"fullscreen":  {"Fullscreen", s.Fullscreen, ""},
		"playlist":    {"Playlist", s.PlayList, ""},
	}
	if stream > 0 {
		info["stream"] = o{fmt.Sprintf("Stream %d", stream), streamOn, info["stream"].time}
	}
	var btns []map[string]any
	for _, k := range which {
		x, ok := info[k]
		if !ok {
			continue
		}
		p := map[string]any{"output": k, "op": "toggle"}
		if k == "stream" && stream > 0 {
			p["stream"] = stream
		}
		noun := map[string]string{"record": "recording", "multicorder": "MultiCorder"}[k]
		if noun == "" {
			noun = strings.ToLower(x.label)
		}
		b := btn(k, map[bool]string{true: "Stop ", false: "Start "}[x.on]+noun, "output", p)
		if x.on {
			b["tone"], b["sub"] = "pgm", "LIVE"+x.time
			if k == "fullscreen" || k == "playlist" {
				b["sub"] = "ON"
			}
		} else {
			b["sub"] = "OFF AIR"
		}
		if risky[k] {
			p["confirmed"] = true
			b["hold"], b["holdLabel"] = holdSeconds(w), "Keep holding…"
		}
		btns = append(btns, b)
	}
	if btns == nil {
		btns = []map[string]any{}
	}
	var st []string
	if s.Recording {
		st = append(st, "Recording "+clock(time.Duration(recSecs)*time.Second))
	} else {
		st = append(st, "Not recording")
	}
	if s.Streaming {
		ch := ""
		if len(s.StreamChannels) > 0 {
			var n []string
			for _, c := range s.StreamChannels {
				n = append(n, strconv.Itoa(c))
			}
			ch = " (" + strings.Join(n, ", ") + ")"
		}
		st = append(st, "Streaming"+ch+elapsed("stream"))
	} else {
		st = append(st, "Not streaming")
	}
	if s.External {
		st = append(st, "External on"+elapsed("external"))
	}
	if s.MultiCorder {
		st = append(st, "MultiCorder on"+elapsed("multicorder"))
	}
	if s.Fullscreen {
		st = append(st, "Fullscreen on")
	}
	return btns, strings.Join(st, "\n")
}

// pickInput is the input a widget setting names, or the first that fits.
func pickInput(s *Snapshot, ref string, fits func(*Input) bool) *Input {
	if ref != "" {
		if in, _, err := s.find(ref); err == nil {
			return in
		}
		return nil
	}
	for i := range s.Inputs {
		if fits(&s.Inputs[i]) {
			return &s.Inputs[i]
		}
	}
	return nil
}

func shortField(name string) string {
	return strings.TrimSuffix(strings.TrimSuffix(name, ".Text"), ".Source")
}

func titleEditor(s *Snapshot, w wset) ([]map[string]any, string, []map[string]any) {
	choices, presets := []map[string]any{}, []map[string]any{}
	in := pickInput(s, w.str("title", ""), (*Input).IsTitle)
	if in == nil {
		return choices, "No title input in vMix", presets
	}
	want := map[string]bool{}
	for _, f := range w.list("fields") {
		k, name, ok := strings.Cut(f, "|")
		if !ok {
			name = k
		} else if !strings.EqualFold(k, in.Key) {
			continue
		}
		want[name] = true
	}
	placeholder := ""
	for _, f := range in.Fields {
		if f.Kind != "text" || (len(want) > 0 && !want[f.Name]) {
			continue
		}
		choices = append(choices, map[string]any{"value": keyOr(in) + "|" + f.Name, "label": shortField(f.Name)})
		if placeholder == "" {
			placeholder = fmt.Sprintf("%s now: %s", shortField(f.Name), clip(strings.Join(strings.Fields(f.Value), " "), 60))
		}
	}
	if placeholder == "" {
		placeholder = in.Title + " has no text fields"
	}
	presets = append(presets,
		btn("prev", "Previous preset", "title_preset", map[string]any{"input": keyOr(in), "op": "prev"}),
		btn("next", "Next preset", "title_preset", map[string]any{"input": keyOr(in), "op": "next"}))
	return choices, placeholder, presets
}

func media(s *Snapshot, w wset) ([]map[string]any, string) {
	in := pickInput(s, w.str("input", ""), (*Input).IsMedia)
	if in == nil {
		return []map[string]any{}, "No media input in vMix"
	}
	k := keyOr(in)
	p := func(op string) map[string]any { return map[string]any{"input": k, "op": op} }
	playing := in.State == "Running"
	play := btn("play_pause", map[bool]string{true: "Pause", false: "Play"}[playing], "media", p("play_pause"))
	play["active"] = playing
	loop := btn("loop", "Loop", "media", p("loop"))
	loop["active"] = in.Loop
	prev, next := btn("prev", "Previous", "media", p("prev")), btn("next", "Next", "media", p("next"))
	if len(in.List) == 0 {
		prev["disabled"], next["disabled"] = true, true
	}
	st := map[string]string{"Running": "Playing", "Paused": "Paused", "Completed": "Ended"}[in.State]
	if st == "" {
		st = in.State
	}
	status := fmt.Sprintf("%s — %s", in.Label(), st)
	if in.Duration > 0 {
		status += fmt.Sprintf(" %s / %s", clock(time.Duration(in.Position)*time.Millisecond), clock(time.Duration(in.Duration)*time.Millisecond))
	}
	if in.Loop {
		status += " · loop"
	}
	return []map[string]any{btn("restart", "Restart", "media", p("restart")), play, prev, next, loop}, status
}

func lists(s *Snapshot, w wset) ([]map[string]any, string) {
	in := pickInput(s, w.str("list", ""), func(in *Input) bool { return len(in.List) > 0 })
	if in == nil {
		return []map[string]any{}, ""
	}
	items := []map[string]any{}
	cur := ""
	for _, it := range in.List {
		id := fmt.Sprintf("%s|%d", keyOr(in), it.Index)
		x := map[string]any{"id": id, "title": it.Title, "icon": "video", "active": it.Selected}
		if it.Selected {
			cur = id
			if in.Duration > 0 {
				x["sub"] = fmt.Sprintf("%s / %s", clock(time.Duration(in.Position)*time.Millisecond), clock(time.Duration(in.Duration)*time.Millisecond))
			}
		}
		if !it.Enabled {
			x["sub"] = "skipped"
		}
		items = append(items, x)
	}
	return []map[string]any{{"id": keyOr(in), "title": in.Label(), "meta": fmt.Sprintf("%d", len(in.List)), "items": items}}, cur
}

func replayButtons(s *Snapshot) []map[string]any {
	var r *Replay
	for i := range s.Inputs {
		if s.Inputs[i].Replay != nil {
			r = s.Inputs[i].Replay
		}
	}
	p := func(op string) map[string]any { return map[string]any{"op": op} }
	live := btn("live", "Live", "replay", p("live"))
	b := []map[string]any{btn("mark_in", "Mark in", "replay", p("mark_in")), btn("mark_out", "Mark out", "replay", p("mark_out")),
		btn("play_last", "Play last", "replay", p("play_last")), btn("pause", "Pause", "replay", p("pause")), live}
	if r == nil {
		for _, x := range b {
			x["disabled"] = true
		}
	} else {
		live["active"] = r.Live
	}
	return b
}

func tallyText(s *Snapshot, mix int, recSecs int) string {
	m, _ := s.Mix(mix)
	name := func(n int) string {
		if in := s.ByNumber(n); in != nil {
			return in.Label()
		}
		return "—"
	}
	prefix := ""
	if mix > 1 {
		prefix = fmt.Sprintf("Mix %d ", mix)
	}
	lines := []string{prefix + "Program: " + name(m.Program), prefix + "Preview: " + name(m.Preview)}
	for _, o := range s.Overlays {
		if o.Input != 0 {
			st := "on air"
			if o.Preview {
				st = "preview"
			}
			lines = append(lines, fmt.Sprintf("Overlay %d (%s): %s", o.Number, st, name(o.Input)))
		}
	}
	var flags []string
	if s.FadeToBlack {
		flags = append(flags, "FADE TO BLACK")
	}
	if s.Recording {
		flags = append(flags, "REC "+clock(time.Duration(recSecs)*time.Second))
	}
	if s.Streaming {
		flags = append(flags, "LIVE")
	}
	if len(flags) > 0 {
		lines = append(lines, strings.Join(flags, " · "))
	}
	return strings.Join(lines, "\n")
}

// mixerStrips: what the audio mixer frame draws, in the chosen order.
func mixerStrips(s *Snapshot, w wset) []map[string]any {
	ids := w.list("strips")
	if len(ids) == 0 {
		ids = []string{"master"}
		for _, b := range s.Buses {
			ids = append(ids, "bus"+b.ID)
		}
	}
	out := []map[string]any{}
	for _, id := range ids {
		if st := stripInfo(s, id); st != nil {
			out = append(out, st)
		}
	}
	return out
}

// stripInfo is one strip's levels for the mixer (nil if vMix lacks it).
func stripInfo(s *Snapshot, id string) map[string]any {
	l := strings.ToLower(id)
	if l == "master" {
		return map[string]any{"id": "master", "label": "Master", "kind": "Master", "name": "Master", "volume": s.Master.Volume, "muted": s.Master.Muted, "meter": s.Master.Meter, "solo": false, "canSolo": false}
	}
	if len(l) == 4 && strings.HasPrefix(l, "bus") {
		if b, ok := s.Bus(l[3:]); ok {
			label, name := "Bus "+b.ID, "Bus "+b.ID
			if b.Name != "" {
				label, name = b.Name, b.Name
			}
			return map[string]any{"id": "bus" + b.ID, "label": label, "kind": "Bus " + b.ID, "name": name, "volume": b.Volume, "muted": b.Muted, "meter": b.Meter, "solo": b.Solo, "canSolo": true}
		}
		return nil
	}
	in, _, err := s.find(id)
	if err != nil || !in.HasAudio {
		return nil
	}
	return map[string]any{"id": keyOr(in), "label": in.Label(), "kind": fmt.Sprintf("Input %d", in.Number), "name": in.Title, "volume": in.Volume, "muted": in.Muted, "meter": in.Meter, "solo": in.Solo, "canSolo": true}
}

// ------------------------------------------------------------- options

// AddonOptions answers a widget field's live list for this connection.
func (c *Conn) AddonOptions(ctx context.Context, name string) ([]addon.Option, error) {
	s, err := c.snapshot(ctx, 2*time.Second)
	if err != nil {
		return nil, err
	}
	o := []addon.Option{}
	add := func(v, l string) { o = append(o, addon.Option{Value: v, Label: l}) }
	each := func(fits func(*Input) bool) {
		for i := range s.Inputs {
			if in := &s.Inputs[i]; fits(in) {
				add(keyOr(in), in.Label())
			}
		}
	}
	switch name {
	case "vmix.inputs":
		each(func(*Input) bool { return true })
	case "vmix.titles":
		each((*Input).IsTitle)
	case "vmix.titleFields":
		for i := range s.Inputs {
			in := &s.Inputs[i]
			for _, f := range in.Fields {
				if f.Kind == "text" {
					add(keyOr(in)+"|"+f.Name, in.Label()+" › "+shortField(f.Name))
				}
			}
		}
	case "vmix.media":
		each((*Input).IsMedia)
	case "vmix.lists":
		each(func(in *Input) bool { return len(in.List) > 0 })
	case "vmix.audio":
		add("master", "Master")
		for _, b := range s.Buses {
			l := "Bus " + b.ID
			if b.Name != "" {
				l += " · " + b.Name
			}
			add("bus"+b.ID, l)
		}
		each(func(in *Input) bool { return in.HasAudio })
	case "vmix.padActions":
		for _, x := range padOptions(s) {
			add(x.value, x.label)
		}
	case "vmix.mixes":
		for _, m := range s.Mixes {
			add(strconv.Itoa(m.Number), map[bool]string{true: "Main mix", false: fmt.Sprintf("Mix %d", m.Number)}[m.Number == 1])
		}
	default:
		return nil, nil
	}
	return o, nil
}

// staticOptions answer when there is no connection to ask.
func staticOptions() map[string]func() []addon.Option {
	return map[string]func() []addon.Option{
		"vmix.mixes": func() []addon.Option {
			o := []addon.Option{{Value: "1", Label: "Main mix"}}
			for n := 2; n <= 4; n++ {
				o = append(o, addon.Option{Value: strconv.Itoa(n), Label: fmt.Sprintf("Mix %d", n)})
			}
			return o
		},
		"vmix.padActions": func() []addon.Option {
			var o []addon.Option
			for _, x := range padOptions(&Snapshot{}) {
				o = append(o, addon.Option{Value: x.value, Label: x.label})
			}
			return o
		},
		"vmix.audio": func() []addon.Option {
			o := []addon.Option{{Value: "master", Label: "Master"}}
			for _, b := range "ABCDEFG" {
				o = append(o, addon.Option{Value: "bus" + string(b), Label: "Bus " + string(b)})
			}
			return o
		},
	}
}

// mediaInfo is the media frame's input (nil when vMix has none).
func mediaInfo(s *Snapshot, w wset) map[string]any {
	in := pickInput(s, w.str("input", ""), (*Input).IsMedia)
	if in == nil {
		return nil
	}
	item := ""
	for _, it := range in.List {
		if it.Selected {
			item = it.Title
		}
	}
	return map[string]any{"key": keyOr(in), "number": in.Number, "title": in.Title, "state": in.State, "position": in.Position,
		"duration": in.Duration, "loop": in.Loop, "list": len(in.List), "item": item}
}

// outputRows is the outputs frame's list, in the chosen order.
func outputRows(s *Snapshot, w wset, since map[string]time.Time, recSecs int) []map[string]any {
	which := w.list("outputs")
	if len(which) == 0 {
		which = []string{"stream", "record", "external", "multicorder"}
	}
	stream := w.int("stream", 0, 0, 5)
	out := []map[string]any{}
	for _, k := range which {
		row := map[string]any{"id": k, "params": map[string]any{"output": k, "op": "toggle"}}
		if risky[k] {
			row["params"].(map[string]any)["confirmed"] = true
			row["hold"] = holdSeconds(w)
		}
		switch k {
		case "record":
			row["name"], row["on"], row["seconds"] = "Record", s.Recording, recSecs
		case "stream":
			on, name := s.Streaming, "Stream"
			if stream > 0 {
				on, name = containsInt(s.StreamChannels, stream), fmt.Sprintf("Stream %d", stream)
				row["params"].(map[string]any)["stream"] = stream
			} else if len(s.StreamChannels) > 0 {
				var n []string
				for _, c := range s.StreamChannels {
					n = append(n, strconv.Itoa(c))
				}
				name += " " + strings.Join(n, ", ")
			}
			row["name"], row["on"] = name, on
		case "external":
			row["name"], row["on"] = "External", s.External
		case "multicorder":
			row["name"], row["on"] = "MultiCorder", s.MultiCorder
		case "fullscreen":
			row["name"], row["on"] = "Fullscreen", s.Fullscreen
		case "playlist":
			row["name"], row["on"] = "Playlist", s.PlayList
		default:
			continue
		}
		if t, ok := since[k]; ok && row["on"] == true && k != "record" {
			row["since"] = t.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out
}

// holdSeconds is how long risky buttons are held (widget setting, 3-10 s).
func holdSeconds(w wset) int { return w.int("holdSeconds", 5, 3, 10) }
