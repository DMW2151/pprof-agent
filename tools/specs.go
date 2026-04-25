package tools

// ToolSpec describes a tool's interface. The agent loop derives its Anthropic
// tool registrations from Properties and Required.
type ToolSpec struct {
	Name        string
	Title       string
	Description string
	Properties  map[string]any
	Required    []string
}

// Specs lists every tool in registration order.
var Specs = []ToolSpec{
	ListFilesSpec,
	UploadFileSpec,
	UploadProfileSpec,
	ListProfilesSpec,
	ProfileInfoSpec,
	AnalyzeProfileSpec,
	InspectFunctionSpec,
	FlameTreeSpec,
	QuerySymbolSpec,
	CompareProfilesSpec,
	DeleteProfileSpec,
}

var ListFilesSpec = ToolSpec{
	Name:        ListFiles,
	Title:       "List Files",
	Description: "List files in a directory. Use this to discover profile files when the user gives a vague location (e.g. 'the heap profile I just wrote', 'profiles in ./foo/bar'). Defaults to the current working directory if no dir is specified.",
	Properties: map[string]any{
		"dir": map[string]any{
			"type":        "string",
			"description": "Directory to list (defaults to cwd)",
		},
	},
}

var UploadFileSpec = ToolSpec{
	Name:        UploadFile,
	Title:       "Upload File",
	Description: "Read a local pprof profile from the filesystem and register it for analysis. Use this whenever the user provides a file path — it handles reading and uploading in one step. Returns profile_id and sample_types.",
	Properties: map[string]any{
		"file_path": map[string]any{
			"type":        "string",
			"description": "Path to the .pprof file (absolute or relative to cwd)",
		},
		"name": map[string]any{
			"type":        "string",
			"description": "Optional human-readable label (defaults to the filename)",
		},
	},
	Required: []string{"file_path"},
}

var UploadProfileSpec = ToolSpec{
	Name:        UploadProfile,
	Title:       "Upload Profile",
	Description: "Register a pprof profile from standard base64-encoded bytes. Optionally supply a human-readable name (e.g. 'prod-cpu-2024-01-15') for easy identification in list_profiles. Returns profile_id and sample_types — read sample_types to know which metric values are valid before calling other tools.",
	Properties: map[string]any{
		"data": map[string]any{
			"type":        "string",
			"description": "Standard base64-encoded pprof profile bytes (from read_file output)",
		},
		"name": map[string]any{
			"type":        "string",
			"description": "Human-readable name, e.g. 'cpu-before.pprof'",
		},
	},
	Required: []string{"data"},
}

var ListProfilesSpec = ToolSpec{
	Name:        ListProfiles,
	Title:       "List Profiles",
	Description: "List all currently loaded profiles with their IDs, sample_types, and source names. Use to discover available profile_ids or check what metrics each profile exposes.",
	Properties:  map[string]any{},
}

var ProfileInfoSpec = ToolSpec{
	Name:        ProfileInfo,
	Title:       "Profile Info",
	Description: "Return detailed metadata for a loaded profile: collection timestamp, sampling period and unit, binary mappings, and all sample value dimensions. Use when you need to validate or describe the profile before analysis.",
	Properties: map[string]any{
		"profile_id": map[string]any{"type": "string"},
	},
	Required: []string{"profile_id"},
}

var AnalyzeProfileSpec = ToolSpec{
	Name:        AnalyzeProfile,
	Title:       "Analyze Profile",
	Description: "Return the top-N hotspot functions for the given metric. sort_by='flat' (default) ranks by cost in the function itself; sort_by='cumulative' ranks by total cost including callees — use cumulative to find bottleneck call-path roots. Start every analysis session here. Follow up with inspect_function to drill into a specific hotspot.",
	Properties: map[string]any{
		"profile_id": map[string]any{"type": "string"},
		"metric":     map[string]any{"type": "string", "description": "Metric name substring, e.g. 'cpu' or 'alloc_space'. Omit to use primary metric."},
		"sort_by":    map[string]any{"type": "string", "description": "'flat' (default) or 'cumulative'"},
		"top_n":      map[string]any{"type": "integer", "description": "Number of results (default 20)"},
	},
	Required: []string{"profile_id"},
}

var InspectFunctionSpec = ToolSpec{
	Name:        InspectFunction,
	Title:       "Inspect Function",
	Description: "Return detailed cost breakdown for a specific function: flat and cumulative cost, direct callers (who calls it), and direct callees (what it calls). Accepts a case-insensitive substring — if multiple functions match, all are returned sorted by cumulative cost. Use after analyze_profile to drill into a hotspot before reading the full flame tree.",
	Properties: map[string]any{
		"profile_id": map[string]any{"type": "string"},
		"function":   map[string]any{"type": "string", "description": "Function name substring to inspect (case-insensitive)"},
		"metric":     map[string]any{"type": "string", "description": "Metric name substring. Omit to use primary metric."},
	},
	Required: []string{"profile_id", "function"},
}

var FlameTreeSpec = ToolSpec{
	Name:        FlameTree,
	Title:       "Flame Tree",
	Description: "Return the weighted call tree as JSON. Each node carries value and pct. Prune noise with threshold_pct (default 1%); limit depth to keep output focused. Use after analyze_profile to trace how cost flows through call paths.",
	Properties: map[string]any{
		"profile_id":    map[string]any{"type": "string"},
		"metric":        map[string]any{"type": "string"},
		"threshold_pct": map[string]any{"type": "number", "description": "Prune nodes below this % of total (default 1.0)"},
		"depth":         map[string]any{"type": "integer", "description": "Max tree depth (default 6)"},
	},
	Required: []string{"profile_id"},
}

var QuerySymbolSpec = ToolSpec{
	Name:        QuerySymbol,
	Title:       "Query Symbol",
	Description: "Find all sample stacks containing a function or package name (case-insensitive substring). Returns total_matches and up to max_stacks stacks with their values. Use to see every call path that reaches a specific function.",
	Properties: map[string]any{
		"profile_id": map[string]any{"type": "string"},
		"symbol":     map[string]any{"type": "string", "description": "Function or package name substring to search for"},
		"metric":     map[string]any{"type": "string"},
		"max_stacks": map[string]any{"type": "integer", "description": "Max stacks to return (default 10)"},
	},
	Required: []string{"profile_id", "symbol"},
}

var CompareProfilesSpec = ToolSpec{
	Name:        CompareProfiles,
	Title:       "Compare Profiles",
	Description: "Diff two profiles by flat cost per function for the given metric. Returns regressions (positive delta) and improvements (negative delta) sorted by absolute change. Both profiles must have compatible sample_types.",
	Properties: map[string]any{
		"baseline_id":  map[string]any{"type": "string"},
		"candidate_id": map[string]any{"type": "string"},
		"metric":       map[string]any{"type": "string"},
		"top_n":        map[string]any{"type": "integer"},
	},
	Required: []string{"baseline_id", "candidate_id"},
}

var DeleteProfileSpec = ToolSpec{
	Name:        DeleteProfile,
	Title:       "Delete Profile",
	Description: "Remove a profile from the registry. Profiles also expire automatically after the configured TTL.",
	Properties: map[string]any{
		"profile_id": map[string]any{"type": "string"},
	},
	Required: []string{"profile_id"},
}
