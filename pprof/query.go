package pprof

import "strings"

// StackEntry is one sample containing the queried symbol.
type StackEntry struct {
	Value int64    `json:"value"`
	Stack []string `json:"stack"` // root → leaf
}

// QuerySymbol returns all samples containing a function whose name contains
// substr (case-insensitive), sorted by value descending.
func QuerySymbol(p *Profile, idx int, substr string) []StackEntry {
	substr = strings.ToLower(substr)
	var results []StackEntry

	for _, s := range p.raw.Sample {
		if idx >= len(s.Value) {
			continue
		}
		v := s.Value[idx]

		stack := make([]string, 0, len(s.Location))
		matched := false

		for i := len(s.Location) - 1; i >= 0; i-- {
			loc := s.Location[i]
			for j := len(loc.Line) - 1; j >= 0; j-- {
				line := loc.Line[j]
				if line.Function == nil {
					continue
				}
				fn := line.Function.Name
				stack = append(stack, fn)
				if strings.Contains(strings.ToLower(fn), substr) {
					matched = true
				}
			}
		}

		if matched {
			results = append(results, StackEntry{Value: v, Stack: stack})
		}
	}

	// sort by value desc (simple insertion sort — profiles are small)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Value > results[i].Value {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results
}
