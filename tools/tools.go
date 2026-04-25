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
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
)

func jsonResult(v any) (*mcp.CallToolResult, struct{}, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, struct{}{}, nil
}

type ListProfilesInput struct{}

type ListProfilesOutput struct {
	Profiles []ListProfilesEntry `json:"profiles"`
}

type ListProfilesEntry struct {
	ID          string   `json:"id"`
	SampleTypes []string `json:"sample_types"`
	SampleCount int      `json:"sample_count"`
	ExpiresAt   string   `json:"expires_at"`
}

func ListProfilesHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, ListProfilesInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ ListProfilesInput) (*mcp.CallToolResult, struct{}, error) {
		entries := reg.List()
		if len(entries) == 0 {
			return jsonResult(map[string]any{"profiles": []any{}})
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
		return jsonResult(map[string]any{"profiles": rows})
	}
}

type ProfileInfoInput struct {
	ProfileID string `json:"profile_id"`
}

type ProfileInfoOutput struct {
	ID          string `json:"id"`
	Duration    string `json:"duration"`
	SampleCount int    `json:"sample_count"`
	Meta        any    `json:"meta"`
}

func ProfileInfoHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, ProfileInfoInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in ProfileInfoInput) (*mcp.CallToolResult, struct{}, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return nil, struct{}{}, err
		}
		p := e.Profile
		return jsonResult(map[string]any{
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

type AnalyzeProfileOutput struct {
	ProfileID   string               `json:"profile_id"`
	Metric      string               `json:"metric"`
	SortBy      string               `json:"sort_by"`
	SampleCount int                  `json:"sample_count"`
	TotalValue  int64                `json:"total_value"`
	Hotspots    []pprof.HotspotEntry `json:"hotspots"`
}

func AnalyzeProfileHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, AnalyzeProfileInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in AnalyzeProfileInput) (*mcp.CallToolResult, struct{}, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return nil, struct{}{}, err
		}
		n := in.TopN
		if n <= 0 {
			n = 20
		}
		p := e.Profile
		idx := p.ResolveMetric(in.Metric)
		return jsonResult(map[string]any{
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

type FlameTreeOutput struct {
	ProfileID    string           `json:"profile_id"`
	Metric       string           `json:"metric"`
	ThresholdPct float64          `json:"threshold_pct"`
	DepthLimit   int              `json:"depth_limit"`
	Root         *pprof.FlameNode `json:"root"`
}

func FlameTreeHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, FlameTreeInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in FlameTreeInput) (*mcp.CallToolResult, struct{}, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return nil, struct{}{}, err
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
		return jsonResult(map[string]any{
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

type QuerySymbolOutput struct {
	ProfileID    string             `json:"profile_id"`
	Symbol       string             `json:"symbol"`
	Metric       string             `json:"metric"`
	TotalMatches int                `json:"total_matches"`
	Stacks       []pprof.StackEntry `json:"stacks"`
}

func QuerySymbolHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, QuerySymbolInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in QuerySymbolInput) (*mcp.CallToolResult, struct{}, error) {
		e, err := reg.Get(in.ProfileID)
		if err != nil {
			return nil, struct{}{}, err
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
		return jsonResult(map[string]any{
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

type CompareProfilesOutput struct {
	BaselineID   string            `json:"baseline_id"`
	CandidateID  string            `json:"candidate_id"`
	Metric       string            `json:"metric"`
	BaseTotal    int64             `json:"base_total"`
	CandTotal    int64             `json:"cand_total"`
	Regressions  int               `json:"regressions"`
	Improvements int               `json:"improvements"`
	Diffs        []pprof.DiffEntry `json:"diffs"`
}

func CompareProfilesHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, CompareProfilesInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in CompareProfilesInput) (*mcp.CallToolResult, struct{}, error) {
		base, err := reg.Get(in.BaselineID)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("baseline: %w", err)
		}
		cand, err := reg.Get(in.CandidateID)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("candidate: %w", err)
		}
		n := in.TopN
		if n <= 0 {
			n = 20
		}
		// resolve metric against baseline; candidate must have same dimensions
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
		return jsonResult(map[string]any{
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

type DeleteProfileOutput struct {
	ProfileID string `json:"profile_id"`
	Status    string `json:"status"`
}

func DeleteProfileHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, DeleteProfileInput) (*mcp.CallToolResult, struct{}, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in DeleteProfileInput) (*mcp.CallToolResult, struct{}, error) {
		if err := reg.Delete(in.ProfileID); err != nil {
			slog.WarnContext(ctx, "Delete_profile: not found", "id", in.ProfileID)
			return nil, struct{}{}, err
		}
		slog.InfoContext(ctx, "Delete_profile: removed", "id", in.ProfileID)
		return jsonResult(map[string]any{
			"profile_id": in.ProfileID,
			"status":     "Deleteed",
		})
	}
}

type UploadProfileInput struct {
	Data string `json:"data"`
	Name string `json:"name,omitempty"`
}

type UploadProfileOutput struct {
	ProfileID   string   `json:"profile_id"`
	SampleTypes []string `json:"sample_types"`
	SampleCount int      `json:"sample_count"`
	ExpiresAt   string   `json:"expires_at"`
}

type ListFilesInput struct {
	Dir string `json:"dir,omitempty"`
}

func ListFilesHandler() func(context.Context, *mcp.CallToolRequest, ListFilesInput) (*mcp.CallToolResult, struct{}, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in ListFilesInput) (*mcp.CallToolResult, struct{}, error) {
		dir := in.Dir
		if dir == "" {
			dir = "."
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("list_files: %w", err)
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
		return jsonResult(map[string]any{"dir": dir, "files": files})
	}
}

type UploadFileInput struct {
	FilePath string `json:"file_path"`
	Name     string `json:"name,omitempty"`
}

func UploadFileHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, UploadFileInput) (*mcp.CallToolResult, struct{}, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UploadFileInput) (*mcp.CallToolResult, struct{}, error) {
		data, err := os.ReadFile(in.FilePath)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("upload_file: %w", err)
		}
		name := in.Name
		if name == "" {
			name = filepath.Base(in.FilePath)
		}
		id, err := reg.LoadBytes(data, name)
		if err != nil {
			slog.ErrorContext(ctx, "upload_file: parse failed", "path", in.FilePath, "err", err)
			return nil, struct{}{}, err
		}
		e, _ := reg.Get(id)
		slog.InfoContext(ctx, "upload_file: registered", "id", id, "path", in.FilePath)
		return jsonResult(map[string]any{
			"profile_id":   id,
			"sample_types": e.Profile.SampleTypes,
			"sample_count": e.Profile.SampleCount(),
			"expires_at":   e.ExpiresAt.String(),
		})
	}
}

// Call dispatches a tool by name, unmarshals the JSON input, runs the handler,
// and returns the text content of the result. It is the primary entry point for
// callers that don't need to deal with MCP protocol types directly (e.g. the
// local agent loop).
func Call(reg *registry.Registry, ctx context.Context, name string, input json.RawMessage) (string, error) {
	var (
		result *mcp.CallToolResult
		err    error
	)
	switch name {
	case ListFiles:
		var in ListFilesInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = ListFilesHandler()(ctx, nil, in)
	case UploadFile:
		var in UploadFileInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = UploadFileHandler(reg)(ctx, nil, in)
	case UploadProfile:
		var in UploadProfileInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = UploadProfileHandler(reg)(ctx, nil, in)
	case ListProfiles:
		result, _, err = ListProfilesHandler(reg)(ctx, nil, ListProfilesInput{})
	case ProfileInfo:
		var in ProfileInfoInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = ProfileInfoHandler(reg)(ctx, nil, in)
	case AnalyzeProfile:
		var in AnalyzeProfileInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = AnalyzeProfileHandler(reg)(ctx, nil, in)
	case FlameTree:
		var in FlameTreeInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = FlameTreeHandler(reg)(ctx, nil, in)
	case QuerySymbol:
		var in QuerySymbolInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = QuerySymbolHandler(reg)(ctx, nil, in)
	case CompareProfiles:
		var in CompareProfilesInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = CompareProfilesHandler(reg)(ctx, nil, in)
	case DeleteProfile:
		var in DeleteProfileInput
		if err = json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		result, _, err = DeleteProfileHandler(reg)(ctx, nil, in)
	default:
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	if err != nil {
		return "", err
	}
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return "", nil
}

func UploadProfileHandler(reg *registry.Registry) func(context.Context, *mcp.CallToolRequest, UploadProfileInput) (*mcp.CallToolResult, struct{}, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UploadProfileInput) (*mcp.CallToolResult, struct{}, error) {
		raw, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			slog.ErrorContext(ctx, "upload_profile: base64 decode failed", "err", err)
			return nil, struct{}{}, fmt.Errorf("upload_profile: data must be standard base64 (encoding/base64 StdEncoding): %w", err)
		}

		id, err := reg.LoadBytes(raw, in.Name)
		if err != nil {
			slog.ErrorContext(ctx, "upload_profile: parse failed", "source", in.Name, "err", err)
			return nil, struct{}{}, err
		}
		e, _ := reg.Get(id)
		slog.InfoContext(ctx,
			"upload_profile: registered",
			"id", id,
			"source", in.Name,
			"sample_types", e.Profile.SampleTypes,
			"samples", e.Profile.SampleCount(),
		)
		return jsonResult(map[string]any{
			"profile_id":   id,
			"sample_types": e.Profile.SampleTypes,
			"sample_count": e.Profile.SampleCount(),
			"expires_at":   e.ExpiresAt.String(),
		})
	}
}
