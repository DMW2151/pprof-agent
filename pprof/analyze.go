package pprof

import (
	"sort"
	"strings"

	goprof "github.com/google/pprof/profile"
)

// HotspotEntry is one row in a top-N analysis.
type HotspotEntry struct {
	Function   string  `json:"function"`
	File       string  `json:"file"`
	Flat       int64   `json:"flat"`
	FlatPct    float64 `json:"flat_pct"`
	Cumulative int64   `json:"cumulative"`
	CumPct     float64 `json:"cum_pct"`
}

// TopN returns the top N hotspot functions for the given value index.
// sortBy controls ranking: "cumulative" ranks by cumulative cost; anything
// else (including "") ranks by flat cost.
func TopN(p *Profile, idx, n int, sortBy string) []HotspotEntry {
	flat, cum := aggregateBySym(p, idx)
	total := p.TotalValue(idx)
	if total == 0 {
		total = 1
	}

	entries := make([]HotspotEntry, 0, len(flat))
	for fn, fv := range flat {
		entries = append(entries, HotspotEntry{
			Function:   fn,
			File:       fileForFunction(p.raw, fn),
			Flat:       fv,
			FlatPct:    100 * float64(fv) / float64(total),
			Cumulative: cum[fn],
			CumPct:     100 * float64(cum[fn]) / float64(total),
		})
	}

	if sortBy == "cumulative" {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Cumulative != entries[j].Cumulative {
				return entries[i].Cumulative > entries[j].Cumulative
			}
			return entries[i].Function < entries[j].Function
		})
	} else {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Flat != entries[j].Flat {
				return entries[i].Flat > entries[j].Flat
			}
			return entries[i].Function < entries[j].Function
		})
	}

	if n > 0 && n < len(entries) {
		entries = entries[:n]
	}
	return entries
}

func aggregateBySym(p *Profile, idx int) (flat, cum map[string]int64) {
	flat = make(map[string]int64)
	cum = make(map[string]int64)
	for _, s := range p.raw.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := s.Value[idx]
		seen := make(map[string]bool)
		for i, loc := range s.Location {
			for _, line := range loc.Line {
				if line.Function == nil {
					continue
				}
				fn := line.Function.Name
				if i == 0 && !seen[fn] {
					flat[fn] += v
				}
				if !seen[fn] {
					cum[fn] += v
					seen[fn] = true
				}
			}
		}
	}
	return flat, cum
}

func fileForFunction(p *goprof.Profile, name string) string {
	for _, fn := range p.Function {
		if fn.Name == name {
			if fn.Filename == "" {
				return "unknown"
			}
			if _, after, ok := strings.Cut(fn.Filename, "/src/"); ok {
				return after
			}
			return fn.Filename
		}
	}
	return "unknown"
}
