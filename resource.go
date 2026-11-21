package main

// Resources: "meters?ids=master,busA,<input key>" is the audio mixer
// frame's fast data (levels, mutes, solos and meters as JSON). vMix only
// reports meters in its state XML, so a meter read re-reads the XML when
// what we have is older than 100 ms (one read at a time, shared by every
// frame asking; the controller also folds identical requests within
// 50 ms into one). MaxAge 0: never cached.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/toolingdev/jadeck/sdk/addon"
)

const meterEvery = 100 * time.Millisecond

func (c *Conn) ResourceResult(ctx context.Context, path string) (addon.ResourceResult, error) {
	name, query, _ := strings.Cut(path, "?")
	if name != "meters" {
		return addon.ResourceResult{}, fmt.Errorf("%w: %s", addon.ErrNotFound, path)
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return addon.ResourceResult{}, fmt.Errorf("bad meters query: %w", err)
	}
	s, err := c.snapshot(ctx, meterEvery)
	if err != nil {
		return addon.ResourceResult{}, err
	}
	ids := []string{}
	for _, part := range strings.Split(q.Get("ids"), ",") {
		if part = strings.TrimSpace(part); part != "" && len(ids) < 64 {
			ids = append(ids, part)
		}
	}
	if len(ids) == 0 {
		ids = append(ids, "master")
		for _, b := range s.Buses {
			ids = append(ids, "bus"+b.ID)
		}
	}
	strips := []map[string]any{}
	for _, id := range ids {
		if st := stripInfo(s, id); st != nil {
			st["ref"] = id
			strips = append(strips, st)
		}
	}
	b, _ := json.Marshal(map[string]any{"strips": strips, "at": c.now().UnixMilli()})
	return addon.ResourceResult{ContentType: "application/json", Data: b, MaxAge: addon.Age(0)}, nil
}
