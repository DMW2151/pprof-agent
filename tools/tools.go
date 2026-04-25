package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dmw2151/pprof-mcp/pprof"
	"github.com/dmw2151/pprof-mcp/registry"
)

const (
	ListFiles       = "list_files"
	UploadFile      = "upload_file"
	UploadProfile   = "upload_profile"
	ListProfiles    = "list_profiles"
	ProfileInfo     = "profile_info"
	AnalyzeProfile  = "analyze_profile"
	FlameTree       = "flame_tree"
	QuerySymbol     = "query_symbol"
	CompareProfiles = "compare_profiles"
	DeleteProfile   = "delete_profile"
	InspectFunction = "inspect_function"
)

func jsonStr(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// Call dispatches a tool by name and returns its JSON result as a string.
func Call(reg *registry.Registry, ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case ListFiles:
		var in ListFilesInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return ListFilesHandler()(ctx, in)
	case UploadFile:
		var in UploadFileInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return UploadFileHandler(reg)(ctx, in)
	case UploadProfile:
		var in UploadProfileInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return UploadProfileHandler(reg)(ctx, in)
	case ListProfiles:
		return ListProfilesHandler(reg)(ctx, ListProfilesInput{})
	case ProfileInfo:
		var in ProfileInfoInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return ProfileInfoHandler(reg)(ctx, in)
	case AnalyzeProfile:
		var in AnalyzeProfileInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return AnalyzeProfileHandler(reg)(ctx, in)
	case FlameTree:
		var in FlameTreeInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return FlameTreeHandler(reg)(ctx, in)
	case QuerySymbol:
		var in QuerySymbolInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return QuerySymbolHandler(reg)(ctx, in)
	case CompareProfiles:
		var in CompareProfilesInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return CompareProfilesHandler(reg)(ctx, in)
	case DeleteProfile:
		var in DeleteProfileInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return DeleteProfileHandler(reg)(ctx, in)
	case InspectFunction:
		var in InspectFunctionInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return InspectFunctionHandler(reg)(ctx, in)
	default:
		return "", fmt.Errorf("unknown tool: %q", name)
	}
}

type ListFilesInput struct {
	Dir string `json:"dir,omitempty"`
}

func ListFilesHandler() func(context.Context, ListFilesInput) (string, error) {
	return func(_ context.Context, in ListFilesInput) (string, error) {
		dir := in.Dir
		if dir == "" {
			dir = "."
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("list_files: %w", err)
		}
		files := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, map[string]any{
				"name":   e.Name(),
				"path":   filepath.Join(dir, e.Name()),
				"is_dir": e.IsDir(),
				"size":   info.Size(),
			})
		}
		return jsonStr(map[string]any{"dir": dir, "files": files})
	}
}

type UploadFileInput struct {
	FilePath string `json:"file_path"`
	Name     string `json:"name,omitempty"`
}

func UploadFileHandler(reg *registry.Registry) func(context.Context, UploadFileInput) (string, error) {
	return func(ctx context.Context, in UploadFileInput) (string, error) {
		data, err := os.ReadFile(in.FilePath)
		if err != nil {
			return "", fmt.Errorf("upload_file: %w", err)
		}
		name := in.Name
		if name == "" {
			name = filepath.Base(in.FilePath)
		}
		id, err := reg.LoadBytes(data, name)
		if err != nil {
			slog.ErrorContext(ctx, "upload_file: parse failed", "path", in.FilePath, "err", err)
			return "", err
		}
		e, _ := reg.Get(id)
		slog.InfoContext(ctx, "upload_file: registered", "id", id, "path", in.FilePath)
		return jsonStr(map[string]any{
			"profile_id":   id,
			"sample_types": e.Profile.SampleTypes,
			"sample_count": e.Profile.SampleCount(),
			"expires_at":   e.ExpiresAt.String(),
		})
	}
}

type UploadProfileInput struct {
	Data string `json:"data"`
	Name string `json:"name,omitempty"`
}

func UploadProfileHandler(reg *registry.Registry) func(context.Context, UploadProfileInput) (string, error) {
	return func(ctx context.Context, in UploadProfileInput) (string, error) {
		raw, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			slog.ErrorContext(ctx, "upload_profile: base64 decode failed", "err", err)
			return "", fmt.Errorf("upload_profile: data must be standard base64: %w", err)
		}
		id, err := reg.LoadBytes(raw, in.Name)
		if err != nil {
			slog.ErrorContext(ctx, "upload_profile: parse failed", "source", in.Name, "err", err)
			return "", err
		}
		e, _ := reg.Get(id)
		slog.InfoContext(ctx, "upload_profile: registered", "id", id, "source", in.Name)
		return jsonStr(map[string]any{
			"profile_id":   id,
			"sample_types": e.Profile.SampleTypes,
			"sample_count": e.Profile.SampleCount(),
			"expires_at":   e.ExpiresAt.String(),
		})
	}
}

type ListProfilesInput struct{}

func ListProfilesHandler(reg *registry.Registry) func(context.Context, ListProfilesInput) (string, error) {
	return func(_ context.Context, _ ListProfilesInput) (string, error) {
		entries := reg.List()
		if len(entries) == 0 {
			return jsonStr(map[string]any{"profiles": []any{}})
		}
		rows := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, map[string]any{
				"id":           e.ID,
				"sample_types": e.Profile.SampleTypes,
				"sample_count": e.Profile.SampleCount(),
				"expires_at":   e.ExpiresAt.String(),
			})
		}
		return jsonStr(map[string]any{"profiles": rows})
	}
}

type ProfileInfoInput struct {
	ProfileID string `json:"profile_id"`
}

func ProfileInfoHandler(reg *registry.Registry) func(context.Context, ProfileInfoInput) (string, error) {
	return func(_ context.Context, in ProfileInfoInput) (string, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return "", err
		}
		p := e.Profile
		return jsonStr(map[string]any{
			"id":           e.ID,
			"duration":     p.Duration.String(),
			"sample_count": p.SampleCount(),
			"meta":         p.Metadata(),
		})
	}
}

type AnalyzeProfileInput struct {
	ProfileID string `json:"profile_id"`
	Metric    string `json:"metric,omitempty"`
	SortBy    string `json:"sort_by,omitempty"`
	TopN      int    `json:"top_n,omitempty"`
}

func AnalyzeProfileHandler(reg *registry.Registry) func(context.Context, AnalyzeProfileInput) (string, error) {
	return func(_ context.Context, in AnalyzeProfileInput) (string, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return "", err
		}
		n := in.TopN
		if n <= 0 {
			n = 20
		}
		p := e.Profile
		idx := p.ResolveMetric(in.Metric)
		return jsonStr(map[string]any{
			"profile_id":   in.ProfileID,
			"metric":       p.SampleTypes[idx],
			"sort_by":      in.SortBy,
			"sample_count": p.SampleCount(),
			"total_value":  p.TotalValue(idx),
			"hotspots":     pprof.TopN(p, idx, n, in.SortBy),
		})
	}
}

type FlameTreeInput struct {
	ProfileID    string  `json:"profile_id"`
	Metric       string  `json:"metric,omitempty"`
	ThresholdPct float64 `json:"threshold_pct,omitempty"`
	Depth        int     `json:"depth,omitempty"`
}

func FlameTreeHandler(reg *registry.Registry) func(context.Context, FlameTreeInput) (string, error) {
	return func(_ context.Context, in FlameTreeInput) (string, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return "", err
		}
		threshold := in.ThresholdPct
		if threshold <= 0 {
			threshold = 1.0
		}
		depth := in.Depth
		if depth <= 0 {
			depth = 6
		}
		p := e.Profile
		idx := p.ResolveMetric(in.Metric)
		tree := pprof.FlameTree(p, idx, threshold)
		pruneDepth(tree, depth, 0)
		return jsonStr(map[string]any{
			"profile_id":    in.ProfileID,
			"metric":        p.SampleTypes[idx],
			"threshold_pct": threshold,
			"depth_limit":   depth,
			"root":          tree,
		})
	}
}

func pruneDepth(n *pprof.FlameNode, maxDepth, cur int) {
	if n == nil {
		return
	}
	if cur >= maxDepth {
		n.Children = nil
		return
	}
	for _, c := range n.Children {
		pruneDepth(c, maxDepth, cur+1)
	}
}

type QuerySymbolInput struct {
	ProfileID string `json:"profile_id"`
	Symbol    string `json:"symbol"`
	Metric    string `json:"metric,omitempty"`
	MaxStacks int    `json:"max_stacks,omitempty"`
}

func QuerySymbolHandler(reg *registry.Registry) func(context.Context, QuerySymbolInput) (string, error) {
	return func(_ context.Context, in QuerySymbolInput) (string, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return "", err
		}
		max := in.MaxStacks
		if max <= 0 {
			max = 10
		}
		p := e.Profile
		idx := p.ResolveMetric(in.Metric)
		all := pprof.QuerySymbol(p, idx, in.Symbol)
		total := len(all)
		if max < len(all) {
			all = all[:max]
		}
		return jsonStr(map[string]any{
			"profile_id":    in.ProfileID,
			"symbol":        in.Symbol,
			"metric":        p.SampleTypes[idx],
			"total_matches": total,
			"stacks":        all,
		})
	}
}

type CompareProfilesInput struct {
	BaselineID  string `json:"baseline_id"`
	CandidateID string `json:"candidate_id"`
	Metric      string `json:"metric,omitempty"`
	TopN        int    `json:"top_n,omitempty"`
}

func CompareProfilesHandler(reg *registry.Registry) func(context.Context, CompareProfilesInput) (string, error) {
	return func(_ context.Context, in CompareProfilesInput) (string, error) {
		base, err := reg.Get(in.BaselineID)
		if err != nil {
			return "", fmt.Errorf("baseline: %w", err)
		}
		cand, err := reg.Get(in.CandidateID)
		if err != nil {
			return "", fmt.Errorf("candidate: %w", err)
		}
		n := in.TopN
		if n <= 0 {
			n = 20
		}
		idx := base.Profile.ResolveMetric(in.Metric)
		diffs := pprof.Compare(base.Profile, cand.Profile, idx)
		if n < len(diffs) {
			diffs = diffs[:n]
		}
		nReg, nImp := 0, 0
		for _, d := range diffs {
			if d.DeltaFlat > 0 {
				nReg++
			} else if d.DeltaFlat < 0 {
				nImp++
			}
		}
		return jsonStr(map[string]any{
			"baseline_id":  in.BaselineID,
			"candidate_id": in.CandidateID,
			"metric":       base.Profile.SampleTypes[idx],
			"base_total":   base.Profile.TotalValue(idx),
			"cand_total":   cand.Profile.TotalValue(idx),
			"regressions":  nReg,
			"improvements": nImp,
			"diffs":        diffs,
		})
	}
}

type DeleteProfileInput struct {
	ProfileID string `json:"profile_id"`
}

func DeleteProfileHandler(reg *registry.Registry) func(context.Context, DeleteProfileInput) (string, error) {
	return func(ctx context.Context, in DeleteProfileInput) (string, error) {
		if err := reg.Delete(in.ProfileID); err != nil {
			slog.WarnContext(ctx, "delete_profile: not found", "id", in.ProfileID)
			return "", err
		}
		slog.InfoContext(ctx, "delete_profile: removed", "id", in.ProfileID)
		return jsonStr(map[string]any{
			"profile_id": in.ProfileID,
			"status":     "deleted",
		})
	}
}

type InspectFunctionInput struct {
	ProfileID string `json:"profile_id"`
	Function  string `json:"function"`
	Metric    string `json:"metric,omitempty"`
}

func InspectFunctionHandler(reg *registry.Registry) func(context.Context, InspectFunctionInput) (string, error) {
	return func(_ context.Context, in InspectFunctionInput) (string, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return "", err
		}
		p := e.Profile
		idx := p.ResolveMetric(in.Metric)
		matches := pprof.InspectFunction(p, idx, in.Function)
		return jsonStr(map[string]any{
			"profile_id": in.ProfileID,
			"metric":     p.SampleTypes[idx],
			"query":      in.Function,
			"matches":    len(matches),
			"functions":  matches,
		})
	}
}
