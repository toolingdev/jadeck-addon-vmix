// Package vmix is the vMix switcher driver: vMix's Web Controller HTTP
// API (Settings → Web Controller must be enabled in vMix).
//
//	GET /api/                                state as XML
//	GET /api/?Function=PreviewInput&Input=N
//	GET /api/?Function=Cut
//	GET /api/?Function=Fade&Duration=1000
//
// addon.json:  {"driver": "vmix", "host": "127.0.0.1", "port": 8088}
package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toolingdev/ptz-controller/sdk/addon"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/") + "/api/", http: &http.Client{Timeout: 2500 * time.Millisecond}}
}

func (c *Client) get(ctx context.Context, q url.Values) ([]byte, error) {
	u := c.base
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vMix not reachable: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vMix answered HTTP %d", resp.StatusCode)
	}
	return b, nil
}

func (c *Client) call(ctx context.Context, fn string, kv ...string) error {
	q := url.Values{"Function": {fn}}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	_, err := c.get(ctx, q)
	return err
}

func (c *Client) State(ctx context.Context) (addon.SwitcherState, error) {
	b, err := c.get(ctx, nil)
	if err != nil {
		return addon.SwitcherState{}, err
	}
	return ParseXML(b)
}

func (c *Client) Preview(ctx context.Context, n int) error {
	return c.call(ctx, "PreviewInput", "Input", strconv.Itoa(n))
}
func (c *Client) Cut(ctx context.Context) error { return c.call(ctx, "Cut") }
func (c *Client) Fade(ctx context.Context, ms int) error {
	return c.call(ctx, "Fade", "Duration", strconv.Itoa(ms))
}

type xmlInput struct {
	Number string `xml:"number,attr"`
	Key    string `xml:"key,attr"`
	Title  string `xml:"title,attr"`
	Type   string `xml:"type,attr"`
	State  string `xml:"state,attr"`
	Text   string `xml:",chardata"`
}

type xmlVmix struct {
	XMLName     xml.Name   `xml:"vmix"`
	Version     string     `xml:"version"`
	Edition     string     `xml:"edition"`
	Inputs      []xmlInput `xml:"inputs>input"`
	Preview     string     `xml:"preview"`
	Active      string     `xml:"active"`
	Streaming   string     `xml:"streaming"`
	Recording   string     `xml:"recording"`
	FadeToBlack string     `xml:"fadeToBlack"`
}

// ParseXML turns vMix's /api/ XML into SwitcherState.
func ParseXML(b []byte) (addon.SwitcherState, error) {
	var v xmlVmix
	if err := xml.Unmarshal(b, &v); err != nil {
		return addon.SwitcherState{}, fmt.Errorf("vMix sent back unreadable XML: %w", err)
	}
	num := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	tru := func(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }
	st := addon.SwitcherState{
		Version: strings.TrimSpace(v.Version), Edition: strings.TrimSpace(v.Edition),
		Preview: num(v.Preview), Active: num(v.Active),
		Streaming: tru(v.Streaming), Recording: tru(v.Recording), FadeToBlack: tru(v.FadeToBlack),
		Inputs: []addon.Input{},
	}
	for _, in := range v.Inputs {
		t := in.Title
		if t == "" {
			t = strings.TrimSpace(in.Text)
		}
		st.Inputs = append(st.Inputs, addon.Input{Number: num(in.Number), Key: in.Key, Title: t, Type: in.Type, State: in.State})
	}
	sort.Slice(st.Inputs, func(i, j int) bool { return st.Inputs[i].Number < st.Inputs[j].Number })
	return st, nil
}
