package main

import (
	"testing"

	"github.com/toolingdev/jadeck/sdk/addon"
	"github.com/toolingdev/jadeck/sdk/addon/addontest"
)

func TestOverThePipe(t *testing.T) {
	h := addontest.Start(t, spec())
	if caps := h.Open("v", "vmix", map[string]any{"host": "127.0.0.1", "port": 1}); len(caps) != 1 || caps[0] != addon.CapSwitcher {
		t.Fatalf("caps %v", caps)
	}
	if err := h.TryCall("v", "switcher.state", nil, nil); err == nil {
		t.Fatal("nothing listens on port 1, yet state answered")
	}
}
