package main

// The add-on's actions (Manifest.Actions): each is a dashboard button,
// a macro step and an agent tool. Parameters are checked here before
// anything reaches vMix. Inputs are given as a number, a GUID (key) or
// a title and always sent to vMix as the GUID. Mixes are 1-16 (1 = the
// main mix), sent to vMix as Mix=0-15.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

const (
	takeGuard = 600 * time.Millisecond // a second tap sooner than this is a double tap, not a take
)

// ------------------------------------------------------------- params

type params map[string]any

func parseParams(raw json.RawMessage) (params, error) {
	p := params{}
	if len(raw) == 0 || string(raw) == "null" {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parameters must be a JSON object: %w", err)
	}
	return p, nil
}

func (p params) has(k string) bool {
	v, ok := p[k]
	if !ok || v == nil {
		return false
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return false
	}
	return true
}

func (p params) str(k string) string {
	switch v := p[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func (p params) num(k string, def, lo, hi float64) (float64, error) {
	if !p.has(k) {
		return def, nil
	}
	var f float64
	switch v := p[k].(type) {
	case float64:
		f = v
	case string:
		x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number, not %q", k, v)
		}
		f = x
	default:
		return 0, fmt.Errorf("%s must be a number", k)
	}
	if math.IsNaN(f) || f < lo || f > hi {
		return 0, fmt.Errorf("%s must be between %g and %g (got %g)", k, lo, hi, f)
	}
	return f, nil
}

func (p params) int(k string, def, lo, hi int) (int, error) {
	f, err := p.num(k, float64(def), float64(lo), float64(hi))
	if err != nil {
		return 0, err
	}
	if f != math.Trunc(f) {
		return 0, fmt.Errorf("%s must be a whole number (got %g)", k, f)
	}
	return int(f), nil
}

// tri reads true / false / "toggle" (missing = def).
func (p params) tri(k, def string) (string, error) {
	if !p.has(k) {
		return def, nil
	}
	switch v := p[k].(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "on", "yes", "1":
			return "true", nil
		case "false", "off", "no", "0":
			return "false", nil
		case "toggle":
			return "toggle", nil
		}
	}
	return "", fmt.Errorf("%s must be true, false or \"toggle\"", k)
}

func (p params) oneOf(k, def string, choices ...string) (string, error) {
	if !p.has(k) {
		if def == "" {
			return "", fmt.Errorf("%s is required (one of %s)", k, strings.Join(choices, ", "))
		}
		return def, nil
	}
	v := strings.TrimSpace(p.str(k))
	for _, c := range choices {
		if strings.EqualFold(c, v) {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s must be one of %s (got %q)", k, strings.Join(choices, ", "), v)
}

// --------------------------------------------------------- resolution

var guidRe = func(s string) bool {
	s = strings.Trim(s, "{}")
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// input resolves an input reference (number, GUID or title) against the
// current state. When vMix can't be read a number or GUID is still sent.
func (c *Conn) input(ctx context.Context, v any) (*Input, string, error) {
	ref := strings.TrimSpace(params{"x": v}.str("x"))
	if ref == "" {
		return nil, "", errors.New("input is required (its number, title or key)")
	}
	s, err := c.snapshot(ctx, 2*time.Second)
	if err != nil {
		if _, nerr := strconv.Atoi(ref); nerr == nil || guidRe(ref) {
			return nil, ref, nil
		}
		return nil, "", err
	}
	return s.find(ref)
}

func (s *Snapshot) find(ref string) (*Input, string, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		if in := s.ByNumber(n); in != nil {
			return in, keyOr(in), nil
		}
		return nil, "", fmt.Errorf("vMix has no input %d (it has %d)", n, len(s.Inputs))
	}
	if in := s.ByKey(strings.Trim(ref, "{}")); in != nil {
		return in, keyOr(in), nil
	}
	for _, exact := range []bool{true, false} {
		for i := range s.Inputs {
			in := &s.Inputs[i]
			if (exact && (in.Title == ref || in.ShortTitle == ref)) || (!exact && (strings.EqualFold(in.Title, ref) || strings.EqualFold(in.ShortTitle, ref))) {
				return in, keyOr(in), nil
			}
		}
	}
	return nil, "", fmt.Errorf("vMix has no input %q", ref)
}

func keyOr(in *Input) string {
	if in.Key != "" {
		return in.Key
	}
	return strconv.Itoa(in.Number)
}

// mix reads the mix param (1-16) and checks vMix has it.
func (c *Conn) mix(ctx context.Context, p params, q url.Values) (int, error) {
	n, err := p.int("mix", 1, 1, 16)
	if err != nil {
		return 0, err
	}
	if n > 1 {
		if s, err := c.snapshot(ctx, 2*time.Second); err == nil {
			if _, ok := s.Mix(n); !ok {
				return 0, fmt.Errorf("vMix has no Mix %d (this vMix has %d mix%s)", n, len(s.Mixes), plural(len(s.Mixes), "", "es"))
			}
		}
		q.Set("Mix", strconv.Itoa(n-1))
	}
	return n, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// field finds a title field by name ("Headline", "Headline.Text").
func (in *Input) field(name, kind string) (string, error) {
	name = strings.TrimSpace(name)
	if len(in.Fields) == 0 {
		return "", fmt.Errorf("input %s has no title fields", in.Label())
	}
	suffix := map[string]string{"text": ".Text", "image": ".Source"}[kind]
	var names []string
	for _, pass := range []func(f Field) bool{
		func(f Field) bool { return f.Name == name },
		func(f Field) bool { return f.Name == name+suffix },
		func(f Field) bool { return strings.EqualFold(f.Name, name) || strings.EqualFold(f.Name, name+suffix) },
	} {
		for _, f := range in.Fields {
			if f.Kind == kind && pass(f) {
				return f.Name, nil
			}
		}
	}
	for _, f := range in.Fields {
		if f.Kind == kind {
			names = append(names, f.Name)
		}
	}
	if name == "" && len(names) == 1 {
		return names[0], nil
	}
	if len(names) == 0 {
		return "", fmt.Errorf("input %s has no %s fields", in.Label(), kind)
	}
	return "", fmt.Errorf("input %s has no %s field %q (it has %s)", in.Label(), kind, name, strings.Join(names, ", "))
}

// ------------------------------------------------------------- defs

var (
	pInput  = addon.Field{Key: "input", Label: "Input", Type: "text", Required: true, OptionsFrom: "vmix.inputs", Help: "Number, title or key (GUID). Sent to vMix as the key, so it survives inputs being moved."}
	pInputO = addon.Field{Key: "input", Label: "Input", Type: "text", OptionsFrom: "vmix.inputs", Help: "Number, title or key. Blank: the input in preview."}
	pMix    = addon.Field{Key: "mix", Label: "Mix", Type: "number", Default: 1, Min: addon.Num(1), Max: addon.Num(16), Help: "1 = the main mix; 2-16 = vMix's extra mixes."}
	pDur    = addon.Field{Key: "duration", Label: "Duration (ms)", Type: "number", Min: addon.Num(0), Max: addon.Num(60000), Help: "Blank: vMix's own duration."}
	pTarget = addon.Field{Key: "target", Label: "Strip", Type: "text", Required: true, OptionsFrom: "vmix.audio", Help: "master, headphones, busA … busG, or an input (number, title or key)."}
)

func opts(vals ...string) []addon.Option {
	out := make([]addon.Option, 0, len(vals)/2)
	for i := 0; i+1 < len(vals); i += 2 {
		out = append(out, addon.Option{Value: vals[i], Label: vals[i+1]})
	}
	return out
}

var effectOptions = func() []addon.Option {
	var o []addon.Option
	for _, e := range transitionEffects {
		o = append(o, addon.Option{Value: e, Label: e})
	}
	for i := 1; i <= 8; i++ {
		o = append(o, addon.Option{Value: fmt.Sprintf("Stinger%d", i), Label: fmt.Sprintf("Stinger %d", i)})
	}
	return o
}()

func actionDefs() []addon.ActionDef {
	return []addon.ActionDef{
		{ID: "preview_input", Label: "Preview input…", MacroStep: true, Description: "Put an input in preview. With take on, tapping the input that is already in preview cuts it to program (a second tap within 0.6 s counts as a double tap and is ignored).",
			Params: []addon.Field{pInput, pMix, {Key: "take", Label: "Tap again to take", Type: "bool"}}},
		{ID: "program_input", Label: "Cut input to program…", MacroStep: true, Description: "Cut an input straight to program.", Params: []addon.Field{pInput, pMix}},
		{ID: "cut", Label: "Cut", MacroStep: true, Description: "Cut preview to program (or straight to the input given).", Params: []addon.Field{pMix, pInputO}},
		{ID: "fade", Label: "Fade", MacroStep: true, Params: []addon.Field{pMix, pDur, pInputO}},
		{ID: "merge", Label: "Merge", MacroStep: true, Params: []addon.Field{pMix, pDur, pInputO}},
		{ID: "stinger", Label: "Stinger…", MacroStep: true, Params: []addon.Field{{Key: "n", Label: "Stinger", Type: "number", Default: 1, Min: addon.Num(1), Max: addon.Num(8)}, pMix, pInputO}},
		{ID: "transition", Label: "Transition…", MacroStep: true, Description: "Any of vMix's transitions by name (Wipe, Zoom, Stinger3…).",
			Params: []addon.Field{{Key: "effect", Label: "Effect", Type: "select", Required: true, Default: "Fade", Options: effectOptions}, pDur, pMix, pInputO}},
		{ID: "transition_button", Label: "Transition 1-4…", MacroStep: true, Description: "Press one of vMix's four transition buttons (main mix), with the effect and duration set there.",
			Params: []addon.Field{{Key: "n", Label: "Button", Type: "number", Required: true, Default: 1, Min: addon.Num(1), Max: addon.Num(4)}}},
		{ID: "set_tbar", Label: "T-bar…", Description: "Move the T-bar (main mix): 0 = start, 1 = end.",
			Params: []addon.Field{{Key: "value", Label: "Position", Type: "number", Required: true, Min: addon.Num(0), Max: addon.Num(1)}}},
		{ID: "fade_to_black", Label: "Fade to black", MacroStep: true, Description: "Toggle vMix's fade to black."},
		{ID: "overlay", Label: "Overlay…", MacroStep: true, Description: "Overlay channel 1-8: toggle (on air → out, else in), in, out, off (instant) or preview. Blank input: the input in preview.",
			Params: []addon.Field{{Key: "n", Label: "Overlay", Type: "number", Required: true, Default: 1, Min: addon.Num(1), Max: addon.Num(8)},
				{Key: "op", Label: "Do", Type: "select", Default: "toggle", Options: opts("toggle", "Toggle", "in", "In", "out", "Out", "off", "Off (instant)", "preview", "Preview overlay")},
				pInputO, pMix}},
		{ID: "overlay_all_off", Label: "All overlays off", MacroStep: true},
		{ID: "output", Label: "Record / stream…", MacroStep: true, Description: "Start, stop or toggle recording, streaming (stream 1-5, 0 = all), external, MultiCorder, fullscreen or the playlist. Recording, streaming, external and MultiCorder only start or stop with confirmed set: dashboard buttons send it after a press-and-hold.",
			Params: []addon.Field{{Key: "output", Label: "What", Type: "select", Required: true, Default: "record",
				Options: opts("record", "Recording", "stream", "Streaming", "external", "External", "multicorder", "MultiCorder", "fullscreen", "Fullscreen", "playlist", "Playlist")},
				{Key: "op", Label: "Do", Type: "select", Default: "toggle", Options: opts("toggle", "Toggle", "start", "Start", "stop", "Stop")},
				{Key: "stream", Label: "Stream", Type: "number", Default: 0, Min: addon.Num(0), Max: addon.Num(5), Help: "0 = all streams."},
				{Key: "confirmed", Label: "Confirmed", Type: "bool", Help: "Required for recording, streaming, external and MultiCorder (a safety: dashboard buttons send it only after the hold)."}}},
		{ID: "snapshot", Label: "Snapshot", MacroStep: true, Description: "Save a still of the output (or of one input) on the vMix PC.",
			Params: []addon.Field{{Key: "input", Label: "Input", Type: "text", OptionsFrom: "vmix.inputs", Help: "Blank: the output."}, {Key: "file", Label: "File on the vMix PC", Type: "text", Help: "Blank: vMix's snapshot folder."}}},
		{ID: "audio_volume", Label: "Volume…", MacroStep: true, Params: []addon.Field{pTarget,
			{Key: "value", Label: "Volume (0-100)", Type: "number", Required: true, Min: addon.Num(0), Max: addon.Num(100)},
			{Key: "fade", Label: "Fade (ms)", Type: "number", Min: addon.Num(0), Max: addon.Num(60000)}}},
		{ID: "audio_mute", Label: "Mute…", MacroStep: true, Params: []addon.Field{pTarget, {Key: "muted", Label: "Muted", Type: "select", Default: "toggle", Options: opts("toggle", "Toggle", "true", "Mute", "false", "Unmute")}}},
		{ID: "audio_solo", Label: "Solo…", MacroStep: true, Params: []addon.Field{pTarget, {Key: "on", Label: "Solo", Type: "select", Default: "toggle", Options: opts("toggle", "Toggle", "true", "On", "false", "Off")}}},
		{ID: "audio_bus", Label: "Send input to bus…", MacroStep: true, Params: []addon.Field{pInput,
			{Key: "bus", Label: "Bus", Type: "select", Required: true, Options: opts("M", "Master", "A", "A", "B", "B", "C", "C", "D", "D", "E", "E", "F", "F", "G", "G")},
			{Key: "on", Label: "On", Type: "select", Default: "toggle", Options: opts("toggle", "Toggle", "true", "On", "false", "Off")}}},
		{ID: "title_text", Label: "Set title text…", MacroStep: true, Description: "Set a text field of a title (SetText). Field: e.g. Headline or Headline.Text.",
			Params: []addon.Field{{Key: "input", Label: "Title", Type: "text", OptionsFrom: "vmix.titles"}, {Key: "field", Label: "Field", Type: "text", OptionsFrom: "vmix.titleFields"},
				{Key: "text", Label: "Text", Type: "textarea"}, {Key: "target", Label: "Title and field", Type: "text", Advanced: true, Help: "\"<input key>|<field>\" instead of input + field (the title editor widget sends this)."}}},
		{ID: "title_image", Label: "Set title image…", MacroStep: true, Description: "Set an image field of a title (SetImage) to a file on the vMix PC or a URL.",
			Params: []addon.Field{{Key: "input", Label: "Title", Type: "text", Required: true, OptionsFrom: "vmix.titles"}, {Key: "field", Label: "Field", Type: "text"}, {Key: "path", Label: "File or URL", Type: "text", Required: true}}},
		{ID: "title_preset", Label: "Title preset…", MacroStep: true, Params: []addon.Field{{Key: "input", Label: "Title", Type: "text", Required: true, OptionsFrom: "vmix.titles"},
			{Key: "op", Label: "Do", Type: "select", Default: "next", Options: opts("next", "Next", "prev", "Previous", "select", "Select")},
			{Key: "index", Label: "Preset (select)", Type: "number", Min: addon.Num(0), Max: addon.Num(1000)}}},
		{ID: "media", Label: "Media transport…", MacroStep: true, Description: "Play, pause, restart, next / previous item, loop or seek a video, audio file or list input.",
			Params: []addon.Field{{Key: "input", Label: "Input", Type: "text", Required: true, OptionsFrom: "vmix.media"},
				{Key: "op", Label: "Do", Type: "select", Required: true, Default: "play_pause", Options: opts("play_pause", "Play / pause", "play", "Play", "pause", "Pause", "restart", "Restart", "next", "Next item", "prev", "Previous item", "loop", "Loop on / off", "loop_on", "Loop on", "loop_off", "Loop off", "seek", "Seek")},
				{Key: "position", Label: "Position (ms, seek)", Type: "number", Min: addon.Num(0)}}},
		{ID: "list_select", Label: "Select list item…", MacroStep: true, Params: []addon.Field{{Key: "input", Label: "List input", Type: "text", OptionsFrom: "vmix.lists"},
			{Key: "index", Label: "Item (1 = first)", Type: "number", Min: addon.Num(1)}, {Key: "id", Label: "Item id", Type: "text", Advanced: true, Help: "\"<input key>|<index>\" (the list widget sends this)."}}},
		{ID: "replay", Label: "Replay…", MacroStep: true, Params: []addon.Field{
			{Key: "op", Label: "Do", Type: "select", Required: true, Default: "mark_in", Options: opts("mark_in", "Mark in", "mark_out", "Mark out", "mark_in_out", "Mark last N seconds", "cancel", "Cancel mark",
				"play_last", "Play last event", "play_last_to_output", "Play last event to output", "play_selected", "Play selected event", "play", "Play", "pause", "Pause", "stop", "Stop events",
				"live", "Live", "recorded", "Recorded", "jump_to_now", "Jump to now", "start_recording", "Start recording", "stop_recording", "Stop recording")},
			{Key: "seconds", Label: "Seconds (mark last N)", Type: "number", Min: addon.Num(1), Max: addon.Num(600)},
			{Key: "channel", Label: "Channel", Type: "select", Options: opts("", "Current", "A", "A", "B", "B")}}},
		{ID: "script", Label: "Script…", MacroStep: true, Description: "Start or stop one of the vMix preset's named scripts.",
			Params: []addon.Field{{Key: "name", Label: "Script name", Type: "text", Required: true}, {Key: "op", Label: "Do", Type: "select", Default: "start", Options: opts("start", "Start", "stop", "Stop")}}},
		{ID: "script_stop_all", Label: "Stop all scripts", MacroStep: true},
		{ID: "ptz_preset", Label: "PTZ preset (vMix)…", MacroStep: true, Description: "Move a camera vMix controls to the position stored in one of its PTZ virtual inputs.",
			Params: []addon.Field{{Key: "input", Label: "PTZ virtual input", Type: "text", Required: true, OptionsFrom: "vmix.inputs"}}},
		{ID: "ptz_home", Label: "PTZ home (vMix)…", MacroStep: true, Params: []addon.Field{{Key: "input", Label: "PTZ input", Type: "text", Required: true, OptionsFrom: "vmix.inputs"}}},
		{ID: "run_function", Label: "Run vMix function…", MacroStep: true, Description: "Advanced: any of vMix 29's 777 functions by name, with only the parameters that function takes (checked before sending).",
			Params: []addon.Field{{Key: "function", Label: "Function", Type: "text", Required: true, Placeholder: "SetVolumeFade"},
				{Key: "input", Label: "Input", Type: "text", OptionsFrom: "vmix.inputs"}, {Key: "value", Label: "Value", Type: "text"},
				{Key: "duration", Label: "Duration (ms)", Type: "number", Min: addon.Num(0)}, pMix,
				{Key: "selectedName", Label: "SelectedName", Type: "text", Advanced: true}, {Key: "selectedIndex", Label: "SelectedIndex", Type: "number", Advanced: true},
				{Key: "channel", Label: "Replay channel", Type: "text", Advanced: true}}},
		{ID: "probe", Label: "Check connection", Description: "Read-only: what vMix this is, how many inputs, and whether a password or the LAN-only setting is in the way."},
		{ID: "refresh", Label: "Refresh state", Description: "Read vMix's state again now (read-only)."},
	}
}

var readOnlyOK = map[string]bool{"probe": true, "refresh": true}

// AddonAction runs one action.
func (c *Conn) AddonAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	p, err := parseParams(raw)
	if err != nil {
		return nil, err
	}
	h, ok := handlers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", addon.ErrUnknownAction, name)
	}
	if c.cfg.ReadOnly && !readOnlyOK[name] {
		return nil, ErrReadOnly
	}
	return h(c, ctx, p)
}

type handler func(c *Conn, ctx context.Context, p params) (any, error)

var done = map[string]any{"ok": true}

var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"preview_input":     (*Conn).aPreview,
		"program_input":     (*Conn).aProgram,
		"cut":               func(c *Conn, ctx context.Context, p params) (any, error) { return c.transition(ctx, "Cut", p, false) },
		"fade":              func(c *Conn, ctx context.Context, p params) (any, error) { return c.transition(ctx, "Fade", p, true) },
		"merge":             func(c *Conn, ctx context.Context, p params) (any, error) { return c.transition(ctx, "Merge", p, true) },
		"stinger":           (*Conn).aStinger,
		"transition":        (*Conn).aTransition,
		"transition_button": (*Conn).aTransitionButton,
		"set_tbar":          (*Conn).aTbar,
		"fade_to_black":     func(c *Conn, ctx context.Context, p params) (any, error) { return done, c.fn(ctx, "FadeToBlack", nil) },
		"overlay":           (*Conn).aOverlay,
		"overlay_all_off": func(c *Conn, ctx context.Context, p params) (any, error) {
			return done, c.fn(ctx, "OverlayInputAllOff", nil)
		},
		"output":       (*Conn).aOutput,
		"snapshot":     (*Conn).aSnapshot,
		"audio_volume": (*Conn).aVolume,
		"audio_mute":   (*Conn).aMute,
		"audio_solo":   (*Conn).aSolo,
		"audio_bus":    (*Conn).aBus,
		"title_text":   func(c *Conn, ctx context.Context, p params) (any, error) { return c.aTitleField(ctx, p, "text") },
		"title_image":  func(c *Conn, ctx context.Context, p params) (any, error) { return c.aTitleField(ctx, p, "image") },
		"title_preset": (*Conn).aTitlePreset,
		"media":        (*Conn).aMedia,
		"list_select":  (*Conn).aListSelect,
		"replay":       (*Conn).aReplay,
		"script":       (*Conn).aScript,
		"script_stop_all": func(c *Conn, ctx context.Context, p params) (any, error) {
			return done, c.fn(ctx, "ScriptStopAll", nil)
		},
		"ptz_preset": func(c *Conn, ctx context.Context, p params) (any, error) {
			return c.inputFn(ctx, p, "PTZMoveToVirtualInputPosition")
		},
		"ptz_home":     func(c *Conn, ctx context.Context, p params) (any, error) { return c.inputFn(ctx, p, "PTZHome") },
		"run_function": (*Conn).aRunFunction,
		"probe":        func(c *Conn, ctx context.Context, p params) (any, error) { return c.Probe(ctx), nil },
		"refresh": func(c *Conn, ctx context.Context, p params) (any, error) {
			s, err := c.fetch(ctx)
			if err != nil {
				return nil, err
			}
			c.changed()
			return map[string]any{"ok": true, "inputs": len(s.Inputs)}, nil
		},
	}
}

func (c *Conn) inputFn(ctx context.Context, p params, fn string) (any, error) {
	_, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	return done, c.fn(ctx, fn, url.Values{"Input": {key}})
}

func (c *Conn) aPreview(ctx context.Context, p params) (any, error) {
	in, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	q := url.Values{"Input": {key}}
	mix, err := c.mix(ctx, p, q)
	if err != nil {
		return nil, err
	}
	take, err := p.tri("take", "false")
	if err != nil {
		return nil, err
	}
	gk := fmt.Sprintf("%d|%s", mix, key)
	if take == "true" && in != nil {
		if s, err := c.snapshot(ctx, time.Second); err == nil {
			if m, _ := s.Mix(mix); m.Preview == in.Number && m.Program != in.Number {
				c.mu.Lock()
				at, ok := c.pvwAt[gk]
				c.mu.Unlock()
				if ok && c.now().Sub(at) < takeGuard {
					return map[string]any{"ok": true, "ignored": "double tap: tap the input again to take it"}, nil
				}
				if err := c.fn(ctx, "Cut", q); err != nil {
					return nil, err
				}
				c.mu.Lock()
				delete(c.pvwAt, gk)
				c.mu.Unlock()
				return map[string]any{"ok": true, "took": in.Label()}, nil
			}
		}
	}
	if err := c.fn(ctx, "PreviewInput", q); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.pvwAt[gk] = c.now()
	c.mu.Unlock()
	return done, nil
}

func (c *Conn) aProgram(ctx context.Context, p params) (any, error) {
	_, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	q := url.Values{"Input": {key}}
	if _, err := c.mix(ctx, p, q); err != nil {
		return nil, err
	}
	return done, c.fn(ctx, "Cut", q)
}

// transition runs a transition effect: preview → program on the mix, or
// straight to the input given.
func (c *Conn) transition(ctx context.Context, effect string, p params, duration bool) (any, error) {
	q := url.Values{}
	if _, err := c.mix(ctx, p, q); err != nil {
		return nil, err
	}
	if duration {
		d, err := p.int("duration", 0, 0, 60000)
		if err != nil {
			return nil, err
		}
		if d > 0 {
			q.Set("Duration", strconv.Itoa(d))
		}
	} else if p.has("duration") {
		return nil, fmt.Errorf("%s takes no duration", effect)
	}
	if p.has("input") {
		_, key, err := c.input(ctx, p["input"])
		if err != nil {
			return nil, err
		}
		q.Set("Input", key)
	}
	return done, c.fn(ctx, effect, q)
}

func (c *Conn) aStinger(ctx context.Context, p params) (any, error) {
	n, err := p.int("n", 1, 1, 8)
	if err != nil {
		return nil, err
	}
	return c.transition(ctx, fmt.Sprintf("Stinger%d", n), p, false)
}

func (c *Conn) aTransition(ctx context.Context, p params) (any, error) {
	e := strings.TrimSpace(p.str("effect"))
	for _, o := range effectOptions {
		if strings.EqualFold(o.Value, e) {
			return c.transition(ctx, o.Value, p, !strings.HasPrefix(o.Value, "Stinger") && o.Value != "Cut")
		}
	}
	return nil, fmt.Errorf("effect must be one of vMix's transitions (%s, Stinger1-8), not %q", strings.Join(transitionEffects, ", "), e)
}

func (c *Conn) aTransitionButton(ctx context.Context, p params) (any, error) {
	if !p.has("n") {
		return nil, errors.New("n is required (1-4)")
	}
	n, err := p.int("n", 1, 1, 4)
	if err != nil {
		return nil, err
	}
	return done, c.fn(ctx, fmt.Sprintf("Transition%d", n), nil)
}

func (c *Conn) aTbar(ctx context.Context, p params) (any, error) {
	if !p.has("value") {
		return nil, errors.New("value is required (0-1)")
	}
	v, err := p.num("value", 0, 0, 1)
	if err != nil {
		return nil, err
	}
	if err := c.fn(ctx, "SetFader", url.Values{"Value": {strconv.Itoa(int(math.Round(v * 255)))}}); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.tbar = v
	if v >= 1 { // vMix finishes the transition at 255; the bar then starts over from 0
		c.tbar = 0
	}
	c.mu.Unlock()
	return done, nil
}

func (c *Conn) aOverlay(ctx context.Context, p params) (any, error) {
	if !p.has("n") {
		return nil, errors.New("n is required (overlay 1-8)")
	}
	n, err := p.int("n", 1, 1, 8)
	if err != nil {
		return nil, err
	}
	op, err := p.oneOf("op", "toggle", "toggle", "in", "out", "off", "preview")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	key := ""
	if p.has("input") {
		if _, key, err = c.input(ctx, p["input"]); err != nil {
			return nil, err
		}
	}
	if op == "toggle" {
		s, err := c.snapshot(ctx, time.Second)
		if err != nil {
			return nil, err
		}
		if o := s.Overlay(n); o.Input != 0 && !o.Preview {
			op = "out"
		} else {
			op = "in"
		}
	}
	fn := fmt.Sprintf("OverlayInput%d", n)
	switch op {
	case "in":
		fn += "In"
		if _, err := c.mix(ctx, p, q); err != nil {
			return nil, err
		}
		if key != "" {
			q.Set("Input", key)
		}
	case "out":
		fn += "Out"
	case "off":
		fn += "Off"
	case "preview":
		fn = fmt.Sprintf("PreviewOverlayInput%d", n)
		if key != "" {
			q.Set("Input", key)
		}
	}
	if err := c.fn(ctx, fn, q); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "did": op}, nil
}

var outputNames = map[string]string{"record": "recording", "stream": "streaming", "external": "external output", "multicorder": "MultiCorder", "fullscreen": "fullscreen", "playlist": "playlist"}

func (c *Conn) aOutput(ctx context.Context, p params) (any, error) {
	out, err := p.oneOf("output", "", "record", "stream", "external", "multicorder", "fullscreen", "playlist")
	if err != nil {
		return nil, err
	}
	op, err := p.oneOf("op", "toggle", "toggle", "start", "stop")
	if err != nil {
		return nil, err
	}
	stream, err := p.int("stream", 0, 0, 5)
	if err != nil {
		return nil, err
	}
	if stream != 0 && out != "stream" {
		return nil, errors.New("stream is only for output stream")
	}
	if risky[out] && p.str("confirmed") != "true" {
		return nil, fmt.Errorf("starting or stopping %s needs confirmed: true (on a dashboard: press and hold the button)", outputNames[out])
	}
	if op == "toggle" {
		s, err := c.snapshot(ctx, time.Second)
		if err != nil {
			return nil, err
		}
		running := map[string]bool{"record": s.Recording, "stream": s.Streaming, "external": s.External,
			"multicorder": s.MultiCorder, "fullscreen": s.Fullscreen, "playlist": s.PlayList}[out]
		if out == "stream" && stream > 0 {
			running = containsInt(s.StreamChannels, stream)
		}
		op = map[bool]string{true: "stop", false: "start"}[running]
	}
	fns := map[string][2]string{"record": {"StartRecording", "StopRecording"}, "stream": {"StartStreaming", "StopStreaming"},
		"external": {"StartExternal", "StopExternal"}, "multicorder": {"StartMultiCorder", "StopMultiCorder"},
		"fullscreen": {"FullscreenOn", "FullscreenOff"}, "playlist": {"StartPlayList", "StopPlayList"}}[out]
	fn := fns[0]
	if op == "stop" {
		fn = fns[1]
	}
	q := url.Values{}
	if stream > 0 {
		q.Set("Value", strconv.Itoa(stream-1)) // vMix numbers streams from 0
	}
	if err := c.fn(ctx, fn, q); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "did": op}, nil
}

// risky outputs start or stop only with confirmed: true.
var risky = map[string]bool{"record": true, "stream": true, "external": true, "multicorder": true}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (c *Conn) aSnapshot(ctx context.Context, p params) (any, error) {
	q := url.Values{}
	if p.has("file") {
		q.Set("Value", p.str("file"))
	}
	if p.has("input") {
		_, key, err := c.input(ctx, p["input"])
		if err != nil {
			return nil, err
		}
		q.Set("Input", key)
		return done, c.fn(ctx, "SnapshotInput", q)
	}
	return done, c.fn(ctx, "Snapshot", q)
}

// strip is an audio target: the master, headphones, a bus or an input.
type strip struct {
	kind string // master | headphones | bus | input
	bus  string // A..G
	key  string
	in   *Input
}

func (c *Conn) strip(ctx context.Context, v any) (strip, error) {
	t := strings.TrimSpace(params{"x": v}.str("x"))
	l := strings.ToLower(strings.ReplaceAll(t, " ", ""))
	switch {
	case t == "":
		return strip{}, errors.New("target is required (master, headphones, busA-busG or an input)")
	case l == "master" || l == "m":
		return strip{kind: "master"}, nil
	case l == "headphones" || l == "phones":
		return strip{kind: "headphones"}, nil
	case len(l) == 4 && strings.HasPrefix(l, "bus") && l[3] >= 'a' && l[3] <= 'g':
		return strip{kind: "bus", bus: strings.ToUpper(l[3:])}, nil
	case len(l) == 1 && l[0] >= 'a' && l[0] <= 'g':
		return strip{kind: "bus", bus: strings.ToUpper(l)}, nil
	}
	in, key, err := c.input(ctx, t)
	if err != nil {
		return strip{}, err
	}
	return strip{kind: "input", key: key, in: in}, nil
}

func (c *Conn) aVolume(ctx context.Context, p params) (any, error) {
	st, err := c.strip(ctx, p["target"])
	if err != nil {
		return nil, err
	}
	if !p.has("value") {
		return nil, errors.New("value is required (0-100)")
	}
	v, err := p.num("value", 0, 0, 100)
	if err != nil {
		return nil, err
	}
	fade, err := p.int("fade", 0, 0, 60000)
	if err != nil {
		return nil, err
	}
	vol := strconv.Itoa(int(math.Round(v)))
	val := vol
	suffix := ""
	if fade > 0 {
		val, suffix = vol+","+strconv.Itoa(fade), "Fade"
	}
	q := url.Values{"Value": {val}}
	var fn string
	switch st.kind {
	case "master":
		fn = "SetMasterVolume" + suffix
	case "headphones":
		if fade > 0 {
			return nil, errors.New("vMix can't fade the headphones volume")
		}
		fn = "SetHeadphonesVolume"
	case "bus":
		fn = "SetBus" + st.bus + "Volume" + suffix
	default:
		fn = "SetVolume" + suffix
		q.Set("Input", st.key)
	}
	return done, c.fn(ctx, fn, q)
}

func (c *Conn) aMute(ctx context.Context, p params) (any, error) {
	st, err := c.strip(ctx, p["target"])
	if err != nil {
		return nil, err
	}
	m, err := p.tri("muted", "toggle")
	if err != nil {
		return nil, err
	}
	// vMix's words are "audio on" (unmuted) / "audio off" (muted).
	suffix := map[string]string{"toggle": "", "true": "Off", "false": "On"}[m]
	switch st.kind {
	case "master":
		return done, c.fn(ctx, "MasterAudio"+suffix, nil)
	case "headphones":
		return nil, errors.New("vMix has no mute for the headphones")
	case "bus":
		return done, c.fn(ctx, "BusXAudio"+suffix, url.Values{"Value": {st.bus}})
	}
	return done, c.fn(ctx, "Audio"+suffix, url.Values{"Input": {st.key}})
}

func (c *Conn) aSolo(ctx context.Context, p params) (any, error) {
	st, err := c.strip(ctx, p["target"])
	if err != nil {
		return nil, err
	}
	on, err := p.tri("on", "toggle")
	if err != nil {
		return nil, err
	}
	suffix := map[string]string{"toggle": "", "true": "On", "false": "Off"}[on]
	switch st.kind {
	case "bus":
		return done, c.fn(ctx, "BusXSolo"+suffix, url.Values{"Value": {st.bus}})
	case "input":
		return done, c.fn(ctx, "Solo"+suffix, url.Values{"Input": {st.key}})
	}
	return nil, fmt.Errorf("vMix has no solo for the %s", st.kind)
}

func (c *Conn) aBus(ctx context.Context, p params) (any, error) {
	_, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	b, err := p.oneOf("bus", "", "M", "A", "B", "C", "D", "E", "F", "G")
	if err != nil {
		return nil, err
	}
	on, err := p.tri("on", "toggle")
	if err != nil {
		return nil, err
	}
	suffix := map[string]string{"toggle": "", "true": "On", "false": "Off"}[on]
	return done, c.fn(ctx, "AudioBus"+suffix, url.Values{"Input": {key}, "Value": {b}})
}

// titleTarget reads input + field, or target "<key>|<field>".
func (c *Conn) titleTarget(ctx context.Context, p params, kind string) (string, string, error) {
	ref, field := p["input"], p.str("field")
	if t := p.str("target"); t != "" {
		k, f, ok := strings.Cut(t, "|")
		if !ok {
			return "", "", fmt.Errorf("target must be \"<input key>|<field>\", not %q", t)
		}
		ref, field = k, f
	}
	in, key, err := c.input(ctx, ref)
	if err != nil {
		return "", "", err
	}
	if in == nil { // vMix unreadable: send what was given
		if field == "" {
			return "", "", errors.New("field is required")
		}
		return key, field, nil
	}
	name, err := in.field(field, kind)
	return key, name, err
}

func (c *Conn) aTitleField(ctx context.Context, p params, kind string) (any, error) {
	key, field, err := c.titleTarget(ctx, p, kind)
	if err != nil {
		return nil, err
	}
	val, fn := p.str("text"), "SetText"
	if kind == "image" {
		if !p.has("path") {
			return nil, errors.New("path is required (a file on the vMix PC or a URL)")
		}
		val, fn = p.str("path"), "SetImage"
	}
	if len(val) > 4000 {
		return nil, errors.New("text is too long (4000 characters at most)")
	}
	if err := c.fn(ctx, fn, url.Values{"Input": {key}, "SelectedName": {field}, "Value": {val}}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "field": field}, nil
}

func (c *Conn) aTitlePreset(ctx context.Context, p params) (any, error) {
	_, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	op, err := p.oneOf("op", "next", "next", "prev", "select")
	if err != nil {
		return nil, err
	}
	q := url.Values{"Input": {key}}
	switch op {
	case "next":
		return done, c.fn(ctx, "NextTitlePreset", q)
	case "prev":
		return done, c.fn(ctx, "PreviousTitlePreset", q)
	}
	if !p.has("index") {
		return nil, errors.New("index is required to select a preset")
	}
	i, err := p.int("index", 0, 0, 1000)
	if err != nil {
		return nil, err
	}
	q.Set("Value", strconv.Itoa(i))
	return done, c.fn(ctx, "SelectTitlePreset", q)
}

func (c *Conn) aMedia(ctx context.Context, p params) (any, error) {
	in, key, err := c.input(ctx, p["input"])
	if err != nil {
		return nil, err
	}
	op, err := p.oneOf("op", "", "play_pause", "play", "pause", "restart", "next", "prev", "loop", "loop_on", "loop_off", "seek")
	if err != nil {
		return nil, err
	}
	q := url.Values{"Input": {key}}
	fn := map[string]string{"play_pause": "PlayPause", "play": "Play", "pause": "Pause", "restart": "Restart", "next": "NextItem",
		"prev": "PreviousItem", "loop": "Loop", "loop_on": "LoopOn", "loop_off": "LoopOff", "seek": "SetPosition"}[op]
	if op == "seek" {
		if !p.has("position") {
			return nil, errors.New("position (ms) is required to seek")
		}
		max := 24 * 3600 * 1000
		if in != nil && in.Duration > 0 {
			max = int(in.Duration)
		}
		pos, err := p.int("position", 0, 0, max)
		if err != nil {
			return nil, err
		}
		q.Set("Value", strconv.Itoa(pos))
	}
	return done, c.fn(ctx, fn, q)
}

func (c *Conn) aListSelect(ctx context.Context, p params) (any, error) {
	ref, idx := p["input"], p
	if id := p.str("id"); id != "" {
		k, i, ok := strings.Cut(id, "|")
		if !ok {
			return nil, fmt.Errorf("id must be \"<input key>|<index>\", not %q", id)
		}
		ref, idx = k, params{"index": i}
	}
	in, key, err := c.input(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !idx.has("index") {
		return nil, errors.New("index is required (1 = first item)")
	}
	max := 100000
	if in != nil && len(in.List) > 0 {
		max = len(in.List)
	}
	i, err := idx.int("index", 1, 1, max)
	if err != nil {
		return nil, err
	}
	return done, c.fn(ctx, "SelectIndex", url.Values{"Input": {key}, "Value": {strconv.Itoa(i)}})
}

func (c *Conn) aReplay(ctx context.Context, p params) (any, error) {
	ops := map[string]string{"mark_in": "ReplayMarkIn", "mark_out": "ReplayMarkOut", "mark_in_out": "ReplayMarkInOut", "cancel": "ReplayMarkCancel",
		"play_last": "ReplayPlayLastEvent", "play_last_to_output": "ReplayPlayLastEventToOutput", "play_selected": "ReplayPlaySelectedEvent",
		"play": "ReplayPlay", "pause": "ReplayPause", "stop": "ReplayStopEvents", "live": "ReplayLive", "recorded": "ReplayRecorded",
		"jump_to_now": "ReplayJumpToNow", "start_recording": "ReplayStartRecording", "stop_recording": "ReplayStopRecording"}
	names := make([]string, 0, len(ops))
	for k := range ops {
		names = append(names, k)
	}
	op, err := p.oneOf("op", "", sortedStrings(names)...)
	if err != nil {
		return nil, err
	}
	fn := ops[op]
	spec, _ := lookupFunction(fn)
	q := url.Values{}
	if op == "mark_in_out" {
		sec, err := p.int("seconds", 10, 1, 600)
		if err != nil {
			return nil, err
		}
		q.Set("Value", strconv.Itoa(sec))
	}
	if p.has("channel") {
		ch, err := p.oneOf("channel", "", "A", "B")
		if err != nil {
			return nil, err
		}
		if !spec.Channel {
			return nil, fmt.Errorf("replay %s has no channel", op)
		}
		q.Set("Channel", ch)
	}
	return done, c.fn(ctx, fn, q)
}

func (c *Conn) aScript(ctx context.Context, p params) (any, error) {
	name := strings.TrimSpace(p.str("name"))
	if name == "" {
		return nil, errors.New("name is required (the script's name in vMix)")
	}
	op, err := p.oneOf("op", "start", "start", "stop")
	if err != nil {
		return nil, err
	}
	fn := map[string]string{"start": "ScriptStart", "stop": "ScriptStop"}[op]
	return done, c.fn(ctx, fn, url.Values{"Value": {name}})
}

// aRunFunction runs any function after checking it against the list.
func (c *Conn) aRunFunction(ctx context.Context, p params) (any, error) {
	name := strings.TrimSpace(p.str("function"))
	if name == "" {
		return nil, errors.New("function is required (e.g. SetVolumeFade)")
	}
	spec, err := lookupFunction(name)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	refuse := func(k string) error { return fmt.Errorf("%s doesn't take %s", spec.Name, k) }
	if p.has("input") {
		if !spec.Input {
			return nil, refuse("an input")
		}
		_, key, err := c.input(ctx, p["input"])
		if err != nil {
			return nil, err
		}
		q.Set("Input", key)
	}
	if p.has("value") {
		if !spec.Value {
			return nil, refuse("a value")
		}
		v := p.str("value")
		if err := spec.checkValue(v); err != nil {
			return nil, err
		}
		q.Set("Value", v)
	}
	if p.has("duration") {
		if !spec.Duration {
			return nil, refuse("a duration")
		}
		d, err := p.int("duration", 0, 0, 600000)
		if err != nil {
			return nil, err
		}
		q.Set("Duration", strconv.Itoa(d))
	}
	if p.has("mix") {
		if !spec.Mix {
			return nil, refuse("a mix")
		}
		if _, err := c.mix(ctx, p, q); err != nil {
			return nil, err
		}
	}
	if p.has("channel") {
		if !spec.Channel {
			return nil, refuse("a replay channel")
		}
		ch, err := p.oneOf("channel", "", "A", "B")
		if err != nil {
			return nil, err
		}
		q.Set("Channel", ch)
	}
	// vMix reads SelectedName / SelectedIndex for any function (titles, lists).
	if p.has("selectedName") {
		q.Set("SelectedName", p.str("selectedName"))
	}
	if p.has("selectedIndex") {
		i, err := p.int("selectedIndex", 0, 0, 100000)
		if err != nil {
			return nil, err
		}
		q.Set("SelectedIndex", strconv.Itoa(i))
	}
	if err := c.fn(ctx, spec.Name, q); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "function": spec.Name, "sent": q.Encode()}, nil
}
