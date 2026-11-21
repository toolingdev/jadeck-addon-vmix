package main

// vMix's state XML (GET /api on 8088, XML on 8099), parsed into a
// Snapshot. Layout from vMix 29.0.0.50 (VMIX-API.md §1.3): booleans are
// .NET True/False, numbers use the invariant culture, mixes in XML are
// numbered from 2 (<mix number="2"> is Mix 2, addressed as Mix=1).

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Field is one data field of a title input (GT or XAML).
type Field struct {
	Name  string `json:"name"`  // "Headline.Text", "Logo.Source"
	Kind  string `json:"kind"`  // text | image | color
	Value string `json:"value"` // text, file path or #AARRGGBB
}

// ListItem is one entry of a List / VideoList / Photos input (1-based).
type ListItem struct {
	Index    int    `json:"index"`
	Path     string `json:"path"`
	Title    string `json:"title"` // file name without the folder
	Selected bool   `json:"selected"`
	Enabled  bool   `json:"enabled"`
}

// Replay is the replay engine's state (on the Replay input).
type Replay struct {
	Live      bool    `json:"live"`
	Recording bool    `json:"recording"`
	Channel   string  `json:"channel"` // channelMode
	Events    int     `json:"events"`
	Speed     float64 `json:"speed"`
}

// Input is one vMix input. Key is vMix's GUID: actions address inputs by
// it because numbers shift when inputs are moved or removed.
type Input struct {
	Number     int        `json:"number"`
	Key        string     `json:"key"`
	Title      string     `json:"title"`
	ShortTitle string     `json:"shortTitle,omitempty"`
	Type       string     `json:"type"`
	State      string     `json:"state"`    // Running | Paused | Completed
	Position   int64      `json:"position"` // ms
	Duration   int64      `json:"duration"` // ms (0 for live sources)
	Loop       bool       `json:"loop"`
	HasAudio   bool       `json:"hasAudio"` // the input has an audio strip
	Muted      bool       `json:"muted"`
	Volume     float64    `json:"volume"` // 0-100
	Balance    float64    `json:"balance"`
	Solo       bool       `json:"solo"`
	Buses      []string   `json:"buses"` // "M", "A".."G"
	Meter      [2]float64 `json:"meter"` // amplitude 0..1, left/right
	GainDb     float64    `json:"gainDb"`
	Selected   int        `json:"selectedIndex,omitempty"` // list inputs, 1-based
	Fields     []Field    `json:"fields,omitempty"`
	List       []ListItem `json:"list,omitempty"`
	Replay     *Replay    `json:"replay,omitempty"`
}

// Mix is one mix's program and preview (input numbers; 0 = none).
type Mix struct {
	Number  int `json:"number"` // 1 = main
	Program int `json:"program"`
	Preview int `json:"preview"`
}

// Overlay is one overlay channel (1-16; functions address 1-8).
type Overlay struct {
	Number  int  `json:"number"`
	Input   int  `json:"input"` // 0 = off
	Preview bool `json:"preview"`
}

// Transition is one of the four transition buttons.
type Transition struct {
	Number   int    `json:"number"`
	Effect   string `json:"effect"`
	Duration int    `json:"duration"` // ms
}

// Bus is the master or one of buses A-G.
type Bus struct {
	ID           string     `json:"id"` // Master, A..G
	Name         string     `json:"name"`
	Volume       float64    `json:"volume"`
	Muted        bool       `json:"muted"`
	Meter        [2]float64 `json:"meter"`
	Solo         bool       `json:"solo"`
	SendToMaster bool       `json:"sendToMaster"`
	Headphones   float64    `json:"headphones,omitempty"` // master only
}

// Snapshot is everything vMix's XML says.
type Snapshot struct {
	Version          string
	Edition          string
	Preset           string
	Inputs           []Input
	Mixes            []Mix // [0] is the main mix
	Overlays         []Overlay
	Transitions      []Transition
	FadeToBlack      bool
	Recording        bool
	RecordingSeconds int
	RecordingFile    string
	Streaming        bool
	StreamChannels   []int // which streams (1-5) are live
	External         bool
	PlayList         bool
	MultiCorder      bool
	Fullscreen       bool
	Master           Bus
	Buses            []Bus // the buses vMix lists (A-G)
}

type xAttrInput struct {
	Number        string `xml:"number,attr"`
	Key           string `xml:"key,attr"`
	Title         string `xml:"title,attr"`
	ShortTitle    string `xml:"shortTitle,attr"`
	Type          string `xml:"type,attr"`
	State         string `xml:"state,attr"`
	Position      string `xml:"position,attr"`
	Duration      string `xml:"duration,attr"`
	Loop          string `xml:"loop,attr"`
	Muted         string `xml:"muted,attr"`
	Volume        string `xml:"volume,attr"`
	Balance       string `xml:"balance,attr"`
	Solo          string `xml:"solo,attr"`
	AudioBusses   string `xml:"audiobusses,attr"`
	MeterF1       string `xml:"meterF1,attr"`
	MeterF2       string `xml:"meterF2,attr"`
	GainDb        string `xml:"gainDb,attr"`
	SelectedIndex string `xml:"selectedIndex,attr"`
	Text          string `xml:",chardata"`
	Texts         []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"text"`
	Images []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"image"`
	Colors []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"color"`
	List []struct {
		Enabled  string `xml:"enabled,attr"`
		Selected string `xml:"selected,attr"`
		Value    string `xml:",chardata"`
	} `xml:"list>item"`
	Replay *struct {
		Live        string `xml:"live,attr"`
		Recording   string `xml:"recording,attr"`
		ChannelMode string `xml:"channelMode,attr"`
		Events      string `xml:"events,attr"`
		Speed       string `xml:"speed,attr"`
	} `xml:"replay"`
}

type xBus struct {
	Volume       string `xml:"volume,attr"`
	Muted        string `xml:"muted,attr"`
	MeterF1      string `xml:"meterF1,attr"`
	MeterF2      string `xml:"meterF2,attr"`
	Headphones   string `xml:"headphonesVolume,attr"`
	Name         string `xml:"name,attr"`
	Solo         string `xml:"solo,attr"`
	SendToMaster string `xml:"sendToMaster,attr"`
}

type xVmix struct {
	XMLName  xml.Name     `xml:"vmix"`
	Version  string       `xml:"version"`
	Edition  string       `xml:"edition"`
	Preset   string       `xml:"preset"`
	Inputs   []xAttrInput `xml:"inputs>input"`
	Overlays []struct {
		Number  string `xml:"number,attr"`
		Preview string `xml:"preview,attr"`
		Value   string `xml:",chardata"`
	} `xml:"overlays>overlay"`
	Preview     string `xml:"preview"`
	Active      string `xml:"active"`
	FadeToBlack string `xml:"fadeToBlack"`
	Transitions []struct {
		Number   string `xml:"number,attr"`
		Effect   string `xml:"effect,attr"`
		Duration string `xml:"duration,attr"`
	} `xml:"transitions>transition"`
	Recording struct {
		Duration  string `xml:"duration,attr"`
		Filename1 string `xml:"filename1,attr"`
		Value     string `xml:",chardata"`
	} `xml:"recording"`
	External  string `xml:"external"`
	Streaming struct {
		Attrs []xml.Attr `xml:",any,attr"`
		Value string     `xml:",chardata"`
	} `xml:"streaming"`
	PlayList    string `xml:"playList"`
	MultiCorder string `xml:"multiCorder"`
	Fullscreen  string `xml:"fullscreen"`
	Mixes       []struct {
		Number  string `xml:"number,attr"`
		Preview string `xml:"preview"`
		Active  string `xml:"active"`
	} `xml:"mix"`
	Audio struct {
		Master *xBus `xml:"master"`
		BusA   *xBus `xml:"busA"`
		BusB   *xBus `xml:"busB"`
		BusC   *xBus `xml:"busC"`
		BusD   *xBus `xml:"busD"`
		BusE   *xBus `xml:"busE"`
		BusF   *xBus `xml:"busF"`
		BusG   *xBus `xml:"busG"`
	} `xml:"audio"`
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
func atoi64(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }
func truthy(s string) bool  { return strings.EqualFold(strings.TrimSpace(s), "true") }

func fileTitle(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	return p
}

func bus(id string, b *xBus) Bus {
	return Bus{ID: id, Name: strings.TrimSpace(b.Name), Volume: atof(b.Volume), Muted: truthy(b.Muted),
		Meter: [2]float64{atof(b.MeterF1), atof(b.MeterF2)}, Solo: truthy(b.Solo), SendToMaster: truthy(b.SendToMaster),
		Headphones: atof(b.Headphones)}
}

// ParseSnapshot reads vMix's state XML.
func ParseSnapshot(b []byte) (*Snapshot, error) {
	var v xVmix
	if err := xml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("vMix sent back unreadable XML: %w", err)
	}
	s := &Snapshot{
		Version: strings.TrimSpace(v.Version), Edition: strings.TrimSpace(v.Edition), Preset: strings.TrimSpace(v.Preset),
		Inputs: []Input{}, FadeToBlack: truthy(v.FadeToBlack),
		Recording: truthy(v.Recording.Value), RecordingSeconds: atoi(v.Recording.Duration), RecordingFile: v.Recording.Filename1,
		Streaming: truthy(v.Streaming.Value), External: truthy(v.External), PlayList: truthy(v.PlayList),
		MultiCorder: truthy(v.MultiCorder), Fullscreen: truthy(v.Fullscreen),
	}
	for _, x := range v.Inputs {
		in := Input{
			Number: atoi(x.Number), Key: strings.TrimSpace(x.Key), Title: x.Title, ShortTitle: x.ShortTitle, Type: x.Type, State: x.State,
			Position: atoi64(x.Position), Duration: atoi64(x.Duration), Loop: truthy(x.Loop),
			HasAudio: x.Volume != "" || x.Muted != "", Muted: truthy(x.Muted), Volume: atof(x.Volume), Balance: atof(x.Balance),
			Solo: truthy(x.Solo), Buses: []string{}, Meter: [2]float64{atof(x.MeterF1), atof(x.MeterF2)}, GainDb: atof(x.GainDb),
			Selected: atoi(x.SelectedIndex),
		}
		if in.Title == "" {
			in.Title = strings.TrimSpace(x.Text)
		}
		for _, b := range strings.Split(x.AudioBusses, ",") {
			if b = strings.TrimSpace(b); b != "" {
				in.Buses = append(in.Buses, b)
			}
		}
		for _, t := range x.Texts {
			in.Fields = append(in.Fields, Field{Name: t.Name, Kind: "text", Value: t.Value})
		}
		for _, t := range x.Images {
			in.Fields = append(in.Fields, Field{Name: t.Name, Kind: "image", Value: t.Value})
		}
		for _, t := range x.Colors {
			in.Fields = append(in.Fields, Field{Name: t.Name, Kind: "color", Value: t.Value})
		}
		for i, it := range x.List {
			li := ListItem{Index: i + 1, Path: strings.TrimSpace(it.Value), Selected: truthy(it.Selected), Enabled: !strings.EqualFold(it.Enabled, "false")}
			li.Title = fileTitle(li.Path)
			if li.Selected && in.Selected == 0 {
				in.Selected = li.Index
			}
			in.List = append(in.List, li)
		}
		if r := x.Replay; r != nil {
			in.Replay = &Replay{Live: truthy(r.Live), Recording: truthy(r.Recording), Channel: r.ChannelMode, Events: atoi(r.Events), Speed: atof(r.Speed)}
		}
		s.Inputs = append(s.Inputs, in)
	}
	sort.SliceStable(s.Inputs, func(i, j int) bool { return s.Inputs[i].Number < s.Inputs[j].Number })

	s.Mixes = append(s.Mixes, Mix{Number: 1, Program: atoi(v.Active), Preview: atoi(v.Preview)})
	for _, m := range v.Mixes {
		if n := atoi(m.Number); n >= 2 && n <= 16 {
			s.Mixes = append(s.Mixes, Mix{Number: n, Program: atoi(m.Active), Preview: atoi(m.Preview)})
		}
	}
	sort.SliceStable(s.Mixes, func(i, j int) bool { return s.Mixes[i].Number < s.Mixes[j].Number })

	for _, o := range v.Overlays {
		if n := atoi(o.Number); n >= 1 && n <= 16 {
			s.Overlays = append(s.Overlays, Overlay{Number: n, Input: atoi(o.Value), Preview: truthy(o.Preview)})
		}
	}
	sort.SliceStable(s.Overlays, func(i, j int) bool { return s.Overlays[i].Number < s.Overlays[j].Number })
	for _, t := range v.Transitions {
		if n := atoi(t.Number); n >= 1 && n <= 4 {
			s.Transitions = append(s.Transitions, Transition{Number: n, Effect: t.Effect, Duration: atoi(t.Duration)})
		}
	}
	sort.SliceStable(s.Transitions, func(i, j int) bool { return s.Transitions[i].Number < s.Transitions[j].Number })
	for _, a := range v.Streaming.Attrs {
		if n, ok := strings.CutPrefix(a.Name.Local, "channel"); ok && truthy(a.Value) {
			if c := atoi(n); c >= 1 && c <= 5 {
				s.StreamChannels = append(s.StreamChannels, c)
			}
		}
	}
	sort.Ints(s.StreamChannels)
	if m := v.Audio.Master; m != nil {
		s.Master = bus("Master", m)
	} else {
		s.Master = Bus{ID: "Master"}
	}
	for _, b := range []struct {
		id string
		x  *xBus
	}{{"A", v.Audio.BusA}, {"B", v.Audio.BusB}, {"C", v.Audio.BusC}, {"D", v.Audio.BusD}, {"E", v.Audio.BusE}, {"F", v.Audio.BusF}, {"G", v.Audio.BusG}} {
		if b.x != nil {
			s.Buses = append(s.Buses, bus(b.id, b.x))
		}
	}
	return s, nil
}

// Mix returns mix n (1 = main) and whether vMix has it.
func (s *Snapshot) Mix(n int) (Mix, bool) {
	for _, m := range s.Mixes {
		if m.Number == n {
			return m, true
		}
	}
	return Mix{Number: n}, false
}

// ByNumber returns the input with that number.
func (s *Snapshot) ByNumber(n int) *Input {
	for i := range s.Inputs {
		if s.Inputs[i].Number == n {
			return &s.Inputs[i]
		}
	}
	return nil
}

// ByKey returns the input with that GUID (case-insensitive).
func (s *Snapshot) ByKey(k string) *Input {
	for i := range s.Inputs {
		if strings.EqualFold(s.Inputs[i].Key, k) {
			return &s.Inputs[i]
		}
	}
	return nil
}

// Overlay returns overlay channel n.
func (s *Snapshot) Overlay(n int) Overlay {
	for _, o := range s.Overlays {
		if o.Number == n {
			return o
		}
	}
	return Overlay{Number: n}
}

// Bus returns the master ("Master"/"M") or bus A-G.
func (s *Snapshot) Bus(id string) (Bus, bool) {
	if strings.EqualFold(id, "master") || strings.EqualFold(id, "m") {
		return s.Master, true
	}
	for _, b := range s.Buses {
		if strings.EqualFold(b.ID, id) {
			return b, true
		}
	}
	return Bus{ID: strings.ToUpper(id)}, false
}

// IsTitle reports whether an input has data fields (GT / XAML titles).
func (in *Input) IsTitle() bool { return len(in.Fields) > 0 }

// IsMedia reports whether an input has a transport (play / pause).
func (in *Input) IsMedia() bool {
	switch in.Type {
	case "Video", "AudioFile", "VideoList", "Photos", "ImageSequence", "PowerPoint", "Audio", "VideoLoop", "DVD":
		if in.Type == "Audio" { // live audio capture: no transport
			return false
		}
		return true
	}
	return in.Duration > 0 || len(in.List) > 0
}

// Label is "3 · Camera Wide".
func (in *Input) Label() string { return fmt.Sprintf("%d · %s", in.Number, in.Title) }
