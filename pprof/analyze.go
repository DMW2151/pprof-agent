package pprof

import (
	"sort"
	"strings"

	goprof "github.com/google/pprof/profile"
)

// CallerCallee is one entry in the callers or callees list of a FunctionDetail.
type CallerCallee struct {
	Function string  `json:"function"`
	File     string  `json:"file"`
	Value    int64   `json:"value"`
	Pct      float64 `json:"pct"`
}

// FunctionDetail is the result of InspectFunction for a single matched function.
type FunctionDetail struct {
	Function   string         `json:"function"`
	File       string         `json:"file"`
	Flat       int64          `json:"flat"`
	FlatPct    float64        `json:"flat_pct"`
	Cumulative int64          `json:"cumulative"`
	CumPct     float64        `json:"cum_pct"`
	Callers    []CallerCallee `json:"callers"`
	Callees    []CallerCallee `json:"callees"`
}

// InspectFunction returns per-function detail for every function whose name
// contains substr (case-insensitive). Each result includes flat/cumulative
// cost, direct callers (who called it), and direct callees (what it called),
// all sorted by value descending. Results are sorted by cumulative cost.
func InspectFunction(p *Profile, idx int, substr string) []FunctionDetail {
	substr = strings.ToLower(substr)

	matchedFns := map[string]bool{}
	for _, fn := range p.raw.Function {
		if strings.Contains(strings.ToLower(fn.Name), substr) {
			matchedFns[fn.Name] = true
		}
	}
	if len(matchedFns) == 0 {
		return nil
	}

	total := p.TotalValue(idx)
	if total == 0 {
		total = 1
	}

	type acc struct {
		flat    int64
		cum     int64
		callers map[string]int64
		callees map[string]int64
	}
	accs := make(map[string]*acc, len(matchedFns))
	for fn := range matchedFns {
		accs[fn] = &acc{callers: make(map[string]int64), callees: make(map[string]int64)}
	}

	for _, s := range p.raw.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := s.Value[idx]

		// Flatten sample into leaf-to-root stack.
		// s.Location[0] is the leaf; within a location loc.Line[0] is innermost.
		var stack []string
		for _, loc := range s.Location {
			for _, line := range loc.Line {
				if line.Function != nil {
					stack = append(stack, line.Function.Name)
				}
			}
		}

		seen := make(map[string]bool)
		for i, fn := range stack {
			a, ok := accs[fn]
			if !ok {
				continue
			}
			if i == 0 {
				a.flat += v
			}
			if !seen[fn] {
				a.cum += v
				seen[fn] = true
			}
			if i > 0 {
				a.callees[stack[i-1]] += v
			}
			if i+1 < len(stack) {
				a.callers[stack[i+1]] += v
			}
		}
	}

	results := make([]FunctionDetail, 0, len(accs))
	for fn, a := range accs {
		if a.cum == 0 {
			continue
		}
		results = append(results, FunctionDetail{
			Function:   fn,
			File:       fileForFunction(p.raw, fn),
			Flat:       a.flat,
			FlatPct:    100 * float64(a.flat) / float64(total),
			Cumulative: a.cum,
			CumPct:     100 * float64(a.cum) / float64(total),
			Callers:    topCallerCallees(p.raw, a.callers, total),
			Callees:    topCallerCallees(p.raw, a.callees, total),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Cumulative > results[j].Cumulative
	})
	return results
}

func topCallerCallees(raw *goprof.Profile, m map[string]int64, total int64) []CallerCallee {
	out := make([]CallerCallee, 0, len(m))
	for fn, v := range m {
		out = append(out, CallerCallee{
			Function: fn,
			File:     fileForFunction(raw, fn),
			Value:    v,
			Pct:      100 * float64(v) / float64(total),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

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
