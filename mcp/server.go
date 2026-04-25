package mcpserver

import (
	_ "embed"
	"log/slog"

	"github.com/dmw2151/pprof-mcp/registry"
	"github.com/dmw2151/pprof-mcp/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed INSTRUCTIONS.md
var serverInstructions string

func NewServer(reg *registry.Registry, logger *slog.Logger) *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:       "pprof-mcp",
			Title:      "PPROF MCP",
			Version:    "v0.0.1a",
			WebsiteURL: "https://mcp.dmw2151.com",
		},
		&mcp.ServerOptions{
			Instructions: serverInstructions,
			Logger:       logger,
			// Advertise only the capabilities we actually implement. Empty
			// prompts/resources blocks cause the client to call prompts/list
			// and resources/list; when those return nothing despite being
			// advertised, some clients flag the server as misconfigured and
			// suppress tool exposure.
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{ListChanged: true},
			},
		},
	)

	mcp.AddTool(s, tool(tools.UploadProfileSpec), tools.UploadProfileHandler(reg))
	mcp.AddTool(s, tool(tools.ListProfilesSpec), tools.ListProfilesHandler(reg))
	mcp.AddTool(s, tool(tools.ProfileInfoSpec), tools.ProfileInfoHandler(reg))
	mcp.AddTool(s, tool(tools.AnalyzeProfileSpec), tools.AnalyzeProfileHandler(reg))
	mcp.AddTool(s, tool(tools.FlameTreeSpec), tools.FlameTreeHandler(reg))
	mcp.AddTool(s, tool(tools.QuerySymbolSpec), tools.QuerySymbolHandler(reg))
	mcp.AddTool(s, tool(tools.CompareProfilesSpec), tools.CompareProfilesHandler(reg))
	mcp.AddTool(s, tool(tools.DeleteProfileSpec), tools.DeleteProfileHandler(reg))

	return s
}

func tool(spec tools.ToolSpec) *mcp.Tool {
	return &mcp.Tool{
		Name:        spec.Name,
		Title:       spec.Title,
		Description: spec.Description,
		InputSchema: spec.InputSchema(),
	}
}
