package main

import (
	"strings"
	"testing"
)

func TestFunctionTable(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range functionTable {
		if seen[strings.ToLower(r.name)] {
			t.Errorf("%s twice", r.name)
		}
		seen[strings.ToLower(r.name)] = true
	}
	if len(functionTable) != 777 {
		t.Fatalf("%d functions, vMix 29.0.0.50 has 777", len(functionTable))
	}
	if len(functions) != 777+len(transitionEffects)-2 { // Merge and Fade... are effects only; FadeToBlack etc. are functions
		// Fade, Merge, Cut, Zoom... are TransitionEffect names, not ShortcutFunction members.
		t.Logf("%d names in all", len(functions))
	}
	for _, n := range []string{"Fade", "cut", "Wipe", "SetVolume", "OverlayInput8Off", "ReplayPlayLastEventToOutput", "PTZMoveToVirtualInputPosition", "OMTPreviewOn"} {
		if _, err := lookupFunction(n); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	s, _ := lookupFunction("SetVolume")
	if !s.Input || !s.Value || s.Mix || *s.Min != 0 || *s.Max != 100 || !s.Integer {
		t.Fatalf("%+v", s)
	}
	s, _ = lookupFunction("SetPanX")
	if *s.Min != -2 || *s.Max != 2 || s.Integer || s.checkValue("-1.5") != nil || s.checkValue("3") == nil {
		t.Fatalf("%+v", s)
	}
	s, _ = lookupFunction("SetVolumeFade") // "Volume 0-100,Milliseconds": no single range
	if s.Min != nil || s.checkValue("20,2000") != nil {
		t.Fatalf("%+v", s)
	}
	s, _ = lookupFunction("Stinger1")
	if !s.MixList || !s.Input || s.Duration {
		t.Fatalf("%+v", s)
	}
	s, _ = lookupFunction("ReplayPlayLastEvent")
	if !s.Channel {
		t.Fatalf("%+v", s)
	}
	if _, err := lookupFunction("SetVolum"); err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("%v", err)
	}
	if _, err := lookupFunction("ScriptStartDynamic"); err == nil {
		t.Fatal("ScriptStartDynamic allowed")
	}
	if n := functionNames(); len(n) != len(functions) || n[0] > n[1] {
		t.Fatal("names")
	}
}
