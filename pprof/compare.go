package pprof

import "sort"

// DiffEntry is the per-function delta between a baseline and candidate profile.
type DiffEntry struct {
	Function  string  `json:"function"`
	BaseFlat  int64   `json:"base_flat"`
	CandFlat  int64   `json:"cand_flat"`
	DeltaFlat int64   `json:"delta_flat"`
	DeltaPct  float64 `json:"delta_pct"`
	IsNew     bool    `json:"is_new,omitempty"`
	IsRemoved bool    `json:"is_removed,omitempty"`
}

// Compare diffs baseline vs candidate by flat cost for the given value index.
// Results are sorted by absolute delta descending.
func Compare(base, cand *Profile, idx int) []DiffEntry {
	baseFlat, _ := aggregateBySym(base, idx)
	candFlat, _ := aggregateBySym(cand, idx)

	seen := make(map[string]bool)
	var entries []DiffEntry

	for fn, bv := range baseFlat {
		seen[fn] = true
		cv := candFlat[fn]
		delta := cv - bv
		var pct float64
		if bv != 0 {
			pct = 100 * float64(delta) / float64(bv)
		}
		entries = append(entries, DiffEntry{
			Function:  fn,
			BaseFlat:  bv,
			CandFlat:  cv,
			DeltaFlat: delta,
			DeltaPct:  pct,
			IsRemoved: cv == 0,
		})
	}
	for fn, cv := range candFlat {
		if seen[fn] {
			continue
		}
		entries = append(entries, DiffEntry{
			Function:  fn,
			CandFlat:  cv,
			DeltaFlat: cv,
			DeltaPct:  100,
			IsNew:     true,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		ai, aj := entries[i].DeltaFlat, entries[j].DeltaFlat
		if ai < 0 {
			ai = -ai
		}
		if aj < 0 {
			aj = -aj
		}
		return ai > aj
	})
	return entries
}
