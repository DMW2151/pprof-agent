package pprof

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	goprof "github.com/google/pprof/profile"
)

// Profile wraps a parsed google/pprof profile with lightweight metadata.
type Profile struct {
	raw         *goprof.Profile
	SampleTypes []string // e.g. ["samples/count", "cpu/nanoseconds"]
	Duration    time.Duration
}

// Load parses a profile from raw bytes (gzip proto or legacy format).
func Load(data []byte) (*Profile, error) {
	raw, err := goprof.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	p := &Profile{
		raw:      raw,
		Duration: time.Duration(raw.DurationNanos),
	}
	p.SampleTypes = make([]string, len(raw.SampleType))
	for i, st := range raw.SampleType {
		p.SampleTypes[i] = st.Type + "/" + st.Unit
	}
	return p, nil
}

// Raw returns the underlying google/pprof profile.
func (p *Profile) Raw() *goprof.Profile { return p.raw }

// SampleCount returns the number of samples in the profile.
func (p *Profile) SampleCount() int { return len(p.raw.Sample) }

// TotalValue returns the sum of the given value index across all samples.
func (p *Profile) TotalValue(idx int) int64 {
	var total int64
	for _, s := range p.raw.Sample {
		if idx < len(s.Value) {
			total += s.Value[idx]
		}
	}
	return total
}

// MetricIndex returns the index of the first SampleType whose type or
// type/unit string matches name (case-insensitive prefix match on type part).
// Returns -1 if not found.
func (p *Profile) MetricIndex(name string) int {
	name = strings.ToLower(name)
	for i, st := range p.SampleTypes {
		lower := strings.ToLower(st)
		if lower == name || strings.HasPrefix(lower, name+"/") {
			return i
		}
	}
	return -1
}

// PrimaryMetricIndex returns the best default value index heuristically:
// nanoseconds > space/bytes > last index.
func (p *Profile) PrimaryMetricIndex() int {
	for i, st := range p.SampleTypes {
		if strings.Contains(st, "nanoseconds") {
			return i
		}
	}
	for i, st := range p.SampleTypes {
		if strings.Contains(st, "space") || strings.Contains(st, "bytes") {
			return i
		}
	}
	if len(p.SampleTypes) > 0 {
		return len(p.SampleTypes) - 1
	}
	return 0
}

// ResolveMetric returns the value index for the given metric name, falling
// back to PrimaryMetricIndex if name is empty or not found.
func (p *Profile) ResolveMetric(name string) int {
	if name != "" {
		if idx := p.MetricIndex(name); idx >= 0 {
			return idx
		}
	}
	return p.PrimaryMetricIndex()
}

// Metadata surfaces supplementary fields from the underlying pprof proto.
type ProfileMeta struct {
	CollectedAt string   `json:"collected_at,omitempty"`
	Period      int64    `json:"period,omitempty"`
	PeriodType  string   `json:"period_type,omitempty"`
	PeriodUnit  string   `json:"period_unit,omitempty"`
	SampleTypes []string `json:"sample_types"`
	Mappings    []string `json:"mappings,omitempty"`
	Comments    []string `json:"comments,omitempty"`
}

// Metadata extracts supplementary metadata from the underlying pprof proto.
func (p *Profile) Metadata() ProfileMeta {
	m := ProfileMeta{SampleTypes: p.SampleTypes}
	if p.raw.TimeNanos != 0 {
		m.CollectedAt = time.Unix(0, p.raw.TimeNanos).UTC().Format(time.RFC3339)
	}
	m.Period = p.raw.Period
	if p.raw.PeriodType != nil {
		m.PeriodType = p.raw.PeriodType.Type
		m.PeriodUnit = p.raw.PeriodType.Unit
	}
	seen := make(map[string]bool)
	for _, mp := range p.raw.Mapping {
		if mp.File != "" && !seen[mp.File] {
			m.Mappings = append(m.Mappings, mp.File)
			seen[mp.File] = true
		}
	}
	m.Comments = p.raw.Comments
	return m
}
