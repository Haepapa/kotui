// Package tools registers all core MCP tools into the Engine.
//
// Call RegisterAll once at application startup, after the MCP Engine and
// config have been initialised.
package tools

import (
	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/mcp"
)

// RegisterAll registers the core MCP tool set into eng.
// cfg is required for tools that need access to application configuration
// (e.g. iot_gateway reads Senior Consultant SSH settings).
func RegisterAll(eng *mcp.Engine, cfg config.Config) error {
	box := eng.Sandbox()

	defs := []mcp.ToolDef{
		filesystemTool(box),
		shellExecutorTool(box),
		fileManagerTool(box),
		iotGatewayTool(cfg),
		webSearchTool(),
		projectCriticTool(box),
	}

	for _, d := range defs {
		if err := eng.Register(d); err != nil {
			return err
		}
	}
	return nil
}
