Pprof profile analysis tools for Go applications.

## Workflow

Always upload a profile before analyzing it. Use `upload_profile` with the profile's standard base64-encoded bytes and an optional human-readable name. The returned `profile_id` and `sample_types` are required for all subsequent tool calls — check `sample_types` to know which metrics are available.

**Analysis tools**
- `analyze_profile`: Top-N hotspot functions by flat or cumulative cost. Start here.
- `flame_tree`: Weighted call tree showing how cost flows through call paths.
- `query_symbol`: All call stacks containing a given function or package name.
- `profile_info`: Profile metadata — timestamp, period, binary mappings.

**Comparison**
Upload two profiles, then call `compare_profiles` with their IDs. Results are sorted by absolute cost delta; regressions have positive delta, improvements have negative delta.

**Metric resolution**
Omit `metric` to use the primary metric automatically (prefers nanoseconds for CPU, bytes for memory). Pass a name substring like `"cpu"` or `"alloc_space"` to be explicit.
