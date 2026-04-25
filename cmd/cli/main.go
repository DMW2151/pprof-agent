// pprofctl: Upload Go pprof profiles to an MCP server
//
// Usage:
//
//	pprofctl upload --path ./profile.pb.gz --name my-profile
//	pprofctl list
//	pprofctl profile --profile-id ID info
//	pprofctl profile --profile-id ID analyze --metric alloc_space
//	pprofctl compare --baseline ID --candidate ID
//
// Environment variables:
//
//	MCP_SERVER_URL: Default server URL (default: http://localhost:8082/mcp)
//	MCP_TIMEOUT:   Default request timeout (default: 10s)
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/dmw2151/pprof-mcp/tools"
	"github.com/google/pprof/profile"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// global
var (
	serverURL        string
	timeout          time.Duration
	profileID        string
	metric           string
	topN             int
	uploadPath       string
	uploadName       string
	analyzeSortBy    string
	flameThreshold   float64
	flameDepth       int
	querySymbol      string
	queryMaxStacks   int
	compareBaseline  string
	compareCandidate string
)

const (
	DefaultAddr     = ":8082"
	DefaultLogPath  = "/tmp/pprof-mcp.log"
	DefaultTTL      = 30 * time.Minute
	DefaultCapacity = 256
)

const rootExample = `# Upload a local profile
pprofctl upload --path ./heap.pb.gz --name heap-before-opt

# Upload from a running service
pprofctl upload --path http://localhost:6060/debug/pprof/heap --name live-heap

# List all profiles
pprofctl list

# Profile-scoped commands
pprofctl profile --profile-id <id> info
pprofctl profile --profile-id <id> analyze --metric alloc_space --sort-by flat --top-n 20
pprofctl profile --profile-id <id> flame --metric cpu --threshold 0.5 --depth 8
pprofctl profile --profile-id <id> query --symbol runtime.mallocgc
pprofctl profile --profile-id <id> delete

# Compare two profiles
pprofctl compare --baseline <id> --candidate <id> --metric alloc_space`

func main() {

	logger := slog.New(slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	))

	slog.SetDefault(logger)

	rootCmd := &cobra.Command{
		Use:     "pprof-ctl",
		Short:   "Upload and analyze Go pprof profiles",
		Example: rootExample,
	}

	mcpSrv := envOrDefault("MCP_SERVER_URL", "http://localhost:8082/mcp")

	rootCmd.PersistentFlags().StringVar(&serverURL, "url", mcpSrv, "analysis server URL [$MCP_SERVER_URL]")
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", 10*time.Second, "request timeout [$MCP_TIMEOUT]")

	// Upload Profile
	uploadCmd := &cobra.Command{
		Use:   "upload --path FILE|URL",
		Short: "Upload a pprof profile",
		RunE:  runUpload,
	}
	uploadCmd.Flags().StringVar(&uploadPath, "path", "", "profile file path or URL (required)")
	uploadCmd.Flags().StringVar(&uploadName, "name", "", "optional profile name")
	_ = uploadCmd.MarkFlagRequired("path")

	// List Profiles
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all uploaded profiles",
		RunE:  runList,
	}

	// Profile Group ...
	profileCmd := &cobra.Command{
		Use:   "profile --profile-id ID <subcommand>",
		Short: "Operate on a specific profile",
	}
	profileCmd.PersistentFlags().StringVar(&profileID, "profile-id", "", "profile ID (required)")
	_ = profileCmd.MarkPersistentFlagRequired("profile-id")

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show profile metadata",
		RunE:  runInfo,
	}

	analyzeCmd := &cobra.Command{
		Use:   "analyze",
		Short: "Show hotspot analysis",
		RunE:  runAnalyze,
	}
	analyzeCmd.Flags().StringVar(&metric, "metric", "", "sample type (e.g. alloc_space, cpu)")
	analyzeCmd.Flags().StringVar(&analyzeSortBy, "sort-by", "flat", "sort field: flat or cumulative")
	analyzeCmd.Flags().IntVar(&topN, "top-n", 10, "number of entries to return")

	flameCmd := &cobra.Command{
		Use:   "flame",
		Short: "Show flame tree",
		RunE:  runFlame,
	}
	flameCmd.Flags().StringVar(&metric, "metric", "", "sample type")
	flameCmd.Flags().Float64Var(&flameThreshold, "threshold", 0.0, "minimum % of total to include a node")
	flameCmd.Flags().IntVar(&flameDepth, "depth", 0, "maximum call depth (0 = unlimited)")

	queryCmd := &cobra.Command{
		Use:   "query --symbol SYMBOL",
		Short: "Query stacks containing a symbol",
		RunE:  runQuery,
	}
	queryCmd.Flags().StringVar(&querySymbol, "symbol", "", "symbol/function name to search (required)")
	queryCmd.Flags().StringVar(&metric, "metric", "", "sample type")
	queryCmd.Flags().IntVar(&queryMaxStacks, "max-stacks", 10, "maximum stacks to return")
	_ = queryCmd.MarkFlagRequired("symbol")

	// Delete Profile
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a profile",
		RunE:  runDelete,
	}
	profileCmd.AddCommand(infoCmd, analyzeCmd, flameCmd, queryCmd, deleteCmd)

	// Compare Profiles
	compareCmd := &cobra.Command{
		Use:   "compare --baseline ID --candidate ID",
		Short: "Diff two profiles",
		RunE:  runCompare,
	}
	compareCmd.Flags().StringVar(&compareBaseline, "baseline", "", "baseline profile ID (required)")
	compareCmd.Flags().StringVar(&compareCandidate, "candidate", "", "candidate profile ID (required)")
	compareCmd.Flags().StringVar(&metric, "metric", "", "sample type")
	compareCmd.Flags().IntVar(&topN, "top-n", 20, "number of diff entries to return")
	_ = compareCmd.MarkFlagRequired("baseline")
	_ = compareCmd.MarkFlagRequired("candidate")

	rootCmd.AddCommand(uploadCmd, listCmd, profileCmd, compareCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("error: %v", err)
	}
}

// --- session / transport ---

func connect(ctx context.Context) (*mcp.ClientSession, error) {
	transport := &mcp.StreamableClientTransport{Endpoint: serverURL}
	client := mcp.NewClient(&mcp.Implementation{Name: "pprofctl", Version: "0.1.0"}, nil)
	return client.Connect(ctx, transport, &mcp.ClientSessionOptions{})
}

func callTool(ctx context.Context, name string, args any) error {
	session, err := connect(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return fmt.Errorf("call tool: %w", err)
	}
	fmt.Printf("%+v\n", res.Content[0]) // ...
	return nil
}

func runUpload(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw, err := os.ReadFile(uploadPath)
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}
	prof, err := parseProfile(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("parse profile: %w", err)
	}
	prof = prof.Compact()

	// slog.Debug("fetching user record", fmt.Sprintf("%+v\n", prof))

	var buf bytes.Buffer
	if err := prof.Write(&buf); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	return callTool(ctx, tools.UploadProfile, tools.UploadProfileInput{
		Name: uploadName,
		Data: base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}

func runList(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.ListProfiles, tools.ListProfilesInput{})
}

func runInfo(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.ProfileInfo, tools.ProfileInfoInput{ProfileID: profileID})
}

func runAnalyze(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.AnalyzeProfile, tools.AnalyzeProfileInput{
		ProfileID: profileID,
		Metric:    metric,
		SortBy:    analyzeSortBy,
		TopN:      topN,
	})
}

func runFlame(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.FlameTree, tools.FlameTreeInput{
		ProfileID:    profileID,
		Metric:       metric,
		ThresholdPct: flameThreshold,
		Depth:        flameDepth,
	})
}

func runQuery(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.QuerySymbol, tools.QuerySymbolInput{
		ProfileID: profileID,
		Symbol:    querySymbol,
		Metric:    metric,
		MaxStacks: queryMaxStacks,
	})
}

func runCompare(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.CompareProfiles, tools.CompareProfilesInput{
		BaselineID:  compareBaseline,
		CandidateID: compareCandidate,
		Metric:      metric,
		TopN:        topN,
	})
}

func runDelete(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return callTool(ctx, tools.DeleteProfile, tools.DeleteProfileInput{ProfileID: profileID})
}

func parseProfile(raw io.Reader) (*profile.Profile, error) {
	// N.B: Parse checks validity; input may be gzip-compressed protobuf, fallback to uncompressed.
	if gr, err := gzip.NewReader(raw); err == nil {
		defer gr.Close()
		return profile.Parse(gr)
	}
	return profile.Parse(raw)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
