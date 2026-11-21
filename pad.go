package main

// The button pad: a widget whose buttons the operator picks from a live
// list of vMix actions ("Overlay 1 in: 6 · Lower third", "Start
// recording", "Cut"), in their own order, with their own labels and
// colours. Settings:
//
//	buttons   order of "action?param=value&..." (options: vmix.padActions)
//	labels    one line per picked button: "Label | colour" (colour: auto,
//	          red, green, amber, none; blank keeps the default)
//	functions extra buttons, one per line: "Label | Function?Input=3&Value=50 | colour"
//	          (run through run_function, so checked against vMix's list)
//
// Colour "auto" follows state: red while it's on air / recording, green
// in preview, amber while muted or active. A picked action whose input
// is gone shows disabled.

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type padOption struct {
	value, label string
}

// padEncode is the option value for an action with params.
func padEncode(action string, kv ...string) string {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	if len(q) == 0 {
		return action
	}
	return action + "?" + q.Encode()
}

// padDecode turns an option value back into an action and its params.
func padDecode(v string) (string, map[string]any, error) {
	action, query, _ := strings.Cut(strings.TrimSpace(v), "?")
	if _, ok := handlers[action]; !ok || action == "probe" || action == "refresh" {
		return "", nil, fmt.Errorf("%q is not a vMix action", action)
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", nil, err
	}
	p := map[string]any{}
	for k, vs := range q {
		if len(vs) == 0 {
			continue
		}
		x := vs[0]
		if k != "input" && k != "target" && k != "name" {
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				p[k] = f
				continue
			}
			if x == "true" || x == "false" {
				p[k] = x == "true"
				continue
			}
		}
		p[k] = x
	}
	return action, p, nil
}

// padOptions lists the actions the pad offers for this vMix.
func padOptions(s *Snapshot) []padOption {
	var o []padOption
	add := func(label, action string, kv ...string) { o = append(o, padOption{padEncode(action, kv...), label}) }
	add("Cut", "cut")
	add("Fade", "fade")
	add("Merge", "merge")
	for n := 1; n <= 4; n++ {
		add(fmt.Sprintf("Stinger %d", n), "stinger", "n", strconv.Itoa(n))
	}
	for n := 1; n <= 4; n++ {
		add(fmt.Sprintf("Transition %d", n), "transition_button", "n", strconv.Itoa(n))
	}
	add("Fade to black", "fade_to_black")
	for _, out := range []struct{ id, label string }{{"record", "recording"}, {"stream", "streaming"}, {"external", "external"}, {"multicorder", "MultiCorder"}} {
		add("Start "+out.label+" (hold)", "output", "output", out.id, "op", "start", "confirmed", "true")
		add("Stop "+out.label+" (hold)", "output", "output", out.id, "op", "stop", "confirmed", "true")
		add("Start / stop "+out.label+" (hold)", "output", "output", out.id, "op", "toggle", "confirmed", "true")
	}
	add("Snapshot", "snapshot")
	add("All overlays off", "overlay_all_off")
	for n := 1; n <= 4; n++ {
		ns := strconv.Itoa(n)
		add(fmt.Sprintf("Overlay %d toggle (preview input)", n), "overlay", "n", ns, "op", "toggle")
		add(fmt.Sprintf("Overlay %d out", n), "overlay", "n", ns, "op", "out")
	}
	for i := range s.Inputs {
		in := &s.Inputs[i]
		k := keyOr(in)
		add("Preview: "+in.Label(), "preview_input", "input", k)
		add("Cut to: "+in.Label(), "program_input", "input", k)
		add("Cut to (hold): "+in.Label(), "program_input", "input", k, "hold", "true")
		for n := 1; n <= 4; n++ {
			add(fmt.Sprintf("Overlay %d in: %s", n, in.Label()), "overlay", "n", strconv.Itoa(n), "op", "in", "input", k)
			add(fmt.Sprintf("Overlay %d toggle: %s", n, in.Label()), "overlay", "n", strconv.Itoa(n), "op", "toggle", "input", k)
		}
		if in.HasAudio {
			add("Mute on/off: "+in.Label(), "audio_mute", "target", k)
		}
		if in.IsMedia() {
			add("Play / pause: "+in.Label(), "media", "input", k, "op", "play_pause")
			add("Restart: "+in.Label(), "media", "input", k, "op", "restart")
		}
		if len(in.List) > 0 {
			add("Next item: "+in.Label(), "media", "input", k, "op", "next")
		}
		if in.IsTitle() {
			add("Next title preset: "+in.Label(), "title_preset", "input", k, "op", "next")
			add("Previous title preset: "+in.Label(), "title_preset", "input", k, "op", "prev")
		}
	}
	add("Mute on/off: Master", "audio_mute", "target", "master")
	for _, b := range s.Buses {
		add("Mute on/off: Bus "+b.ID, "audio_mute", "target", "bus"+b.ID)
	}
	for _, r := range []struct{ op, label string }{{"mark_in", "Mark in"}, {"mark_out", "Mark out"}, {"play_last", "Play last"}, {"play_last_to_output", "Play last to output"}, {"live", "Live"}} {
		add("Replay: "+r.label, "replay", "op", r.op)
	}
	add("Stop all scripts", "script_stop_all")
	return o
}

// padLabel is the default button label for an option label.
func padLabel(l string) string {
	l = strings.TrimSuffix(l, " (hold)")
	l = strings.Replace(l, " (hold):", ":", 1)
	l = strings.TrimSuffix(l, " (preview input)")
	return l
}

func (c *Conn) padButtons(s *Snapshot, w wset, tally map[int]byte) []map[string]any {
	opts := map[string]string{}
	for _, o := range padOptions(s) {
		opts[o.value] = o.label
	}
	type custom struct{ label, colour string }
	var labels []custom
	for _, line := range strings.Split(w.str("labels", ""), "\n") {
		l, col, _ := strings.Cut(line, "|")
		labels = append(labels, custom{strings.TrimSpace(l), strings.ToLower(strings.TrimSpace(col))})
	}
	main, _ := s.Mix(1)
	out := []map[string]any{}
	for i, v := range w.list("actions") {
		action, p, err := padDecode(v)
		id := fmt.Sprintf("b%d", i+1)
		if err != nil {
			b := btn(id, "Not a vMix action", "", nil)
			b["disabled"] = true
			out = append(out, b)
			continue
		}
		label, known := opts[v]
		label = padLabel(label)
		b := btn(id, label, action, p)
		if !known { // e.g. its input was removed
			if ref, ok := p["input"].(string); ok {
				if _, _, err := s.find(ref); err != nil {
					b["label"], b["disabled"] = "Input gone", true
				}
			}
			if b["label"] == "" {
				b["label"] = action
			}
		}
		colour := "auto"
		if i < len(labels) {
			if labels[i].label != "" {
				b["label"] = labels[i].label
			}
			if labels[i].colour != "" {
				colour = labels[i].colour
			}
		}
		if p["confirmed"] == true || p["hold"] == true { // risky: the dashboard sends it after a press-and-hold
			delete(p, "hold")
			b["hold"], b["holdLabel"] = holdSeconds(w), "Keep holding…"
		}
		applyColour(b, colour, padState(s, action, p, main, tally))
		out = append(out, b)
	}
	// Extra buttons running any vMix function.
	n := len(out)
	for _, line := range strings.Split(w.str("functions", ""), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		label, call := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		fn, query, _ := strings.Cut(call, "?")
		p := map[string]any{"function": strings.TrimSpace(fn)}
		if q, err := url.ParseQuery(query); err == nil {
			for k, vs := range q {
				if len(vs) > 0 {
					p[map[string]string{"input": "input", "value": "value", "duration": "duration", "mix": "mix", "selectedname": "selectedName", "selectedindex": "selectedIndex", "channel": "channel"}[strings.ToLower(k)]] = vs[0]
				}
			}
			delete(p, "")
		}
		if label == "" {
			label = fn
		}
		n++
		b := btn(fmt.Sprintf("f%d", n), label, "run_function", p)
		if _, err := lookupFunction(fn); err != nil {
			b["disabled"] = true
		}
		if len(parts) > 2 {
			applyColour(b, strings.ToLower(strings.TrimSpace(parts[2])), "")
		}
		out = append(out, b)
	}
	return out
}

// padState is the automatic colour for an action: pgm, pvw, active or "".
func padState(s *Snapshot, action string, p map[string]any, main Mix, tally map[int]byte) string {
	in := func() *Input {
		if ref, ok := p["input"].(string); ok {
			if x, _, err := s.find(ref); err == nil {
				return x
			}
		}
		return nil
	}
	switch action {
	case "preview_input", "program_input":
		if x := in(); x != nil {
			return tone(x.Number, main, tally)
		}
	case "overlay":
		n, _ := p["n"].(float64)
		o := s.Overlay(int(n))
		if o.Input == 0 {
			return ""
		}
		if x := in(); x != nil && x.Number != o.Input {
			return ""
		}
		if o.Preview {
			return "pvw"
		}
		return "pgm"
	case "output":
		on := map[string]bool{"record": s.Recording, "stream": s.Streaming, "external": s.External, "multicorder": s.MultiCorder}[fmt.Sprint(p["output"])]
		if on {
			return "pgm"
		}
	case "fade_to_black":
		if s.FadeToBlack {
			return "active"
		}
	case "audio_mute":
		t := strings.ToLower(fmt.Sprint(p["target"]))
		if t == "master" {
			if s.Master.Muted {
				return "active"
			}
		} else if strings.HasPrefix(t, "bus") {
			if b, ok := s.Bus(t[3:]); ok && b.Muted {
				return "active"
			}
		} else if x := in(); x == nil {
			if y, _, err := s.find(t); err == nil && y.Muted {
				return "active"
			}
		}
	case "media":
		if x := in(); x != nil && fmt.Sprint(p["op"]) == "play_pause" && x.State == "Running" {
			return "active"
		}
	}
	return ""
}

func applyColour(b map[string]any, colour, auto string) {
	switch colour {
	case "red":
		b["tone"] = "pgm"
	case "green":
		b["tone"] = "pvw"
	case "amber":
		b["active"] = true
	case "none":
	default: // auto
		switch auto {
		case "pgm", "pvw":
			b["tone"] = auto
		case "active":
			b["active"] = true
		}
	}
}
