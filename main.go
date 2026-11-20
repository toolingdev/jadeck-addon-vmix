// vMix: a PTZ controller add-on. Cut, fade, preview, tally and live
// status through vMix's Web Controller API.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/toolingdev/ptz-controller/sdk/addon"
)

// Version is set by the release build.
var Version = "1.0.0"

func main() { addon.Serve(spec()) }

var widgetSample = map[string]any{"/api/switcher/status": map[string]any{
	"label": "vMix — office PC", "connected": true, "version": "28", "edition": "Pro",
	"inputs": []map[string]any{
		{"number": 1, "key": "a", "title": "PTZ Wide — Sanctuary", "type": "Capture", "state": "Running"},
		{"number": 2, "key": "b", "title": "PTZ Pulpit — Close", "type": "Capture", "state": "Running"},
		{"number": 3, "key": "c", "title": "Slides", "type": "NDI", "state": "Running"},
		{"number": 4, "key": "d", "title": "Announcements", "type": "Video", "state": "Paused"},
	},
	"preview": 2, "active": 1, "streaming": true, "recording": true,
}}

func spec() addon.Spec {
	return addon.Spec{
		Manifest: addon.Manifest{
			ID: "vmix", Name: "vMix", Version: Version, Publisher: "PTZ Controller", APIVersion: addon.APIVersion, Repo: "toolingdev/ptz-addon-vmix",
			Description: "Cut, fade, preview, tally and live status from vMix.",
			Reaches:     "The vMix PCs you add as connections (vMix Web Controller, usually port 8088)",
			ConnectionTypes: []addon.ConnectionType{{
				ID: "vmix", Label: "vMix", Capabilities: []string{addon.CapSwitcher},
				Description: "In vMix: Settings → Web Controller → enable it (default port 8088).",
				Fields: []addon.Field{
					{Key: "host", Label: "vMix PC address", Type: "text", Default: "127.0.0.1", Placeholder: "127.0.0.1",
						Help: "127.0.0.1 when vMix runs on this same PC; otherwise that PC's IP.", Required: true},
					{Key: "port", Label: "Web Controller port", Type: "number", Default: 8088, Min: addon.Num(1), Max: addon.Num(65535)},
				},
				Sample:     map[string]any{"host": "192.168.1.30", "port": 8088},
				SampleTest: "Connected — 4 inputs, program 1, preview 2",
			}},
			Widgets: []addon.WidgetDef{
				{Kind: "core", Type: "switcher.cutfade", Label: "Cut / Fade", Sample: widgetSample},
				{Kind: "core", Type: "switcher.inputs", Label: "Input bank", Sample: widgetSample},
				{Kind: "core", Type: "stage.live", Label: "Live status", Sample: widgetSample, Settings: map[string]any{"mode": "stream"}},
			},
			Changelog: "First release as its own add-on (was built into the controller).",
		},
		Open: map[string]addon.OpenFunc{"vmix": func(raw json.RawMessage) (any, error) {
			c := struct {
				Host string `json:"host"`
				Port int    `json:"port"`
			}{Host: "127.0.0.1", Port: 8088}
			if err := json.Unmarshal(raw, &c); err != nil {
				return nil, err
			}
			if c.Port == 0 {
				c.Port = 8088
			}
			return New(fmt.Sprintf("http://%s:%d", c.Host, c.Port)), nil
		}},
	}
}
