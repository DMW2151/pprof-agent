package mcpserver_test

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mcpserver "github.com/dmw2151/pprof-mcp/mcp"
	"github.com/dmw2151/pprof-mcp/registry"
	"github.com/dmw2151/pprof-mcp/tools"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const testTTL = 5 * time.Minute
const testCapacity = 64

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := registry.New(testTTL, testCapacity)
	t.Cleanup(reg.Close)

	s := mcpserver.NewServer(reg, slog.Default())
	handler := sdkmcp.NewStreamableHTTPHandler(func(_ *http.Request) *sdkmcp.Server {
		return s
	}, nil)

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

// mcpClient is a minimal stateful MCP client over HTTP for testing.
type mcpClient struct {
	t         *testing.T
	url       string
	sessionID string
	nextID    int
}

func newMCPClient(t *testing.T, ts *httptest.Server) *mcpClient {
	t.Helper()
	c := &mcpClient{t: t, url: ts.URL, nextID: 1}

	// 1. initialize
	resp := c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})
	if resp["error"] != nil {
		t.Fatalf("initialize error: %v", resp["error"])
	}

	// 2. notifications/initialized (notification — no id, expect 202)
	c.notify("notifications/initialized", map[string]any{})

	return c
}

// call sends a JSON-RPC request (with id) and returns the decoded response.
func (c *mcpClient) call(method string, params any) map[string]any {
	c.t.Helper()
	id := c.nextID
	c.nextID++

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})

	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("POST %s: %v", method, err)
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	return c.decodeResponse(method, resp)
}

// notify sends a JSON-RPC notification (no id) and discards the response.
func (c *mcpClient) notify(method string, params any) {
	c.t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("notify %s: %v", method, err)
	}
	resp.Body.Close()
}

func (c *mcpClient) decodeResponse(method string, resp *http.Response) map[string]any {
	c.t.Helper()
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &result); err != nil {
				c.t.Fatalf("decode SSE data for %s: %v", method, err)
			}
			return result
		}
		c.t.Fatalf("no data event in SSE response for %s", method)
		return nil
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.t.Fatalf("decode response for %s: %v", method, err)
	}
	return result
}

func fixtureB64(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "fixtures", "prof", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

// --- tests ---

// toolResult extracts text content from a tools/call response.
func toolResult(t *testing.T, r map[string]any) string {
	t.Helper()
	if r["error"] != nil {
		t.Fatalf("tool call error: %v", r["error"])
	}
	content := r["result"].(map[string]any)["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

func TestServerStartup(t *testing.T) {
	newMCPClient(t, newTestServer(t))
}

func TestServerToolsList(t *testing.T) {
	c := newMCPClient(t, newTestServer(t))

	r := c.call("tools/list", map[string]any{})
	if r["error"] != nil {
		t.Fatalf("tools/list error: %v", r["error"])
	}

	rawTools := r["result"].(map[string]any)["tools"].([]any)
	got := make(map[string]bool, len(rawTools))
	for _, rt := range rawTools {
		got[rt.(map[string]any)["name"].(string)] = true
	}

	for _, name := range []string{
		tools.UploadProfile,
		tools.ListProfiles,
		tools.ProfileInfo,
		tools.AnalyzeProfile,
		tools.FlameTree,
		tools.QuerySymbol,
		tools.CompareProfiles,
		tools.DeleteProfile,
	} {
		if !got[name] {
			t.Errorf("tool %q not registered; got %v", name, got)
		}
	}
}

func TestServerUploadAndAnalyze(t *testing.T) {
	c := newMCPClient(t, newTestServer(t))

	uploadR := c.call("tools/call", map[string]any{
		"name": "upload_profile",
		"arguments": map[string]any{
			"data": fixtureB64(t, "inv.build.cpu.pprof"),
			"name": "inv.build.cpu.pprof",
		},
	})
	text := toolResult(t, uploadR)

	var uploaded struct {
		ProfileID   string `json:"profile_id"`
		SampleCount int    `json:"sample_count"`
	}
	if err := json.Unmarshal([]byte(text), &uploaded); err != nil || uploaded.ProfileID == "" {
		t.Fatalf("parse upload_profile response: %v\nraw: %s", err, text)
	}
	if uploaded.SampleCount == 0 {
		t.Error("expected non-zero sample count")
	}

	analyzeR := c.call("tools/call", map[string]any{
		"name":      "analyze_profile",
		"arguments": map[string]any{"profile_id": uploaded.ProfileID, "top_n": 5},
	})
	aText := toolResult(t, analyzeR)

	var analyzed struct {
		Hotspots []any `json:"hotspots"`
	}
	if err := json.Unmarshal([]byte(aText), &analyzed); err != nil {
		t.Fatalf("parse analyze_profile response: %v", err)
	}
	if len(analyzed.Hotspots) == 0 {
		t.Error("expected hotspots")
	}
}

func TestServerUploadBadBase64(t *testing.T) {
	c := newMCPClient(t, newTestServer(t))

	r := c.call("tools/call", map[string]any{
		"name":      "upload_profile",
		"arguments": map[string]any{"data": "not-valid-base64!!!"},
	})
	if r["error"] != nil {
		t.Fatalf("unexpected transport error: %v", r["error"])
	}
	result := r["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("expected isError=true for bad base64, got: %v", result)
	}
}

func TestServerListProfilesEmpty(t *testing.T) {
	c := newMCPClient(t, newTestServer(t))

	r := c.call("tools/call", map[string]any{
		"name":      "list_profiles",
		"arguments": map[string]any{},
	})
	if r["error"] != nil {
		t.Fatalf("list_profiles error: %v", r["error"])
	}
}
