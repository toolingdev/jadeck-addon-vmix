package main

// The function list (functions_table.go, all 777 of vMix 29.0.0.50 plus
// the 20 transition names) and the checks run_function makes before
// anything is sent: the name exists, and only the parameters that
// function reads are given, with values in range.

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type fnRow struct{ cat, name, params, value string }

type fnSpec struct {
	Name     string
	Category string
	Input    bool
	Value    bool
	Duration bool
	Mix      bool
	MixList  bool
	Channel  bool
	Hint     string   // value hint, e.g. "Volume 0-100 int"
	Min, Max *float64 // numeric range when the hint has one
	Integer  bool
}

// transitionEffects are functions too (Input, Duration, Mix).
var transitionEffects = []string{"Fade", "Zoom", "Wipe", "Slide", "Fly", "CrossZoom", "FlyRotate", "Cube", "CubeZoom",
	"VerticalWipe", "VerticalSlide", "Merge", "WipeReverse", "SlideReverse", "VerticalWipeReverse", "VerticalSlideReverse",
	"BarnDoor", "RollerDoor", "AlphaFade", "Cut"}

var (
	functions   = map[string]*fnSpec{} // lower-case name → spec
	rangeRe     = regexp.MustCompile(`(-?\d+(?:\.\d+)?)-(-?\d+(?:\.\d+)?)( int)?$`)
	notAllowedF = map[string]string{
		"scriptstartdynamic": "vMix blocks ScriptStartDynamic over its API (Settings → Web Controller → enhanced security); start a named script with ScriptStart instead",
	}
)

func init() {
	for _, r := range functionTable {
		s := &fnSpec{Name: r.name, Category: r.cat, Hint: r.value,
			Input: strings.Contains(r.params, "I"), Value: strings.Contains(r.params, "V"), Duration: strings.Contains(r.params, "D"),
			Mix: strings.ContainsAny(r.params, "ML"), MixList: strings.Contains(r.params, "L"), Channel: strings.Contains(r.params, "C")}
		if m := rangeRe.FindStringSubmatch(r.value); m != nil && !strings.Contains(r.value, ",") {
			lo, _ := strconv.ParseFloat(m[1], 64)
			hi, _ := strconv.ParseFloat(m[2], 64)
			s.Min, s.Max, s.Integer = &lo, &hi, m[3] != ""
		}
		functions[strings.ToLower(r.name)] = s
	}
	for _, t := range transitionEffects {
		if _, ok := functions[strings.ToLower(t)]; !ok {
			functions[strings.ToLower(t)] = &fnSpec{Name: t, Category: "Transition", Input: true, Duration: true, Mix: true, MixList: true}
		}
	}
}

// lookupFunction finds a function by name (vMix matches case-insensitively).
func lookupFunction(name string) (*fnSpec, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if why, ok := notAllowedF[n]; ok {
		return nil, fmt.Errorf("%s", why)
	}
	if s, ok := functions[n]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("vMix 29 has no function %q%s", name, suggest(n))
}

// suggest names up to three functions that start like n.
func suggest(n string) string {
	if len(n) < 3 {
		return ""
	}
	var out []string
	for k, s := range functions {
		if strings.HasPrefix(k, n[:3]) && (strings.Contains(k, n) || strings.Contains(n, k)) {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	if len(out) == 0 {
		return ""
	}
	return " (did you mean " + strings.Join(out, ", ") + "?)"
}

// checkValue checks a Value against the function's range, if it has one.
func (s *fnSpec) checkValue(v string) error {
	if s.Min == nil || v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fmt.Errorf("%s needs a number for Value (%s), not %q", s.Name, s.Hint, v)
	}
	if f < *s.Min || f > *s.Max {
		return fmt.Errorf("%s: Value %s is out of range (%s)", s.Name, v, s.Hint)
	}
	if s.Integer && f != float64(int64(f)) {
		return fmt.Errorf("%s: Value must be a whole number (%s)", s.Name, s.Hint)
	}
	return nil
}

// functionNames lists every function name, sorted (for the action's help).
func functionNames() []string {
	out := make([]string, 0, len(functions))
	for _, s := range functions {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}
