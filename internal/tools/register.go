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
	return RegisterAllWithHooks(eng, cfg, nil, nil)
}

// RegisterAllWithHooks is like RegisterAll but also wires the optional
// onBrainUpdate and onFileWrite hooks used by the update_self and filesystem tools.
func RegisterAllWithHooks(eng *mcp.Engine, cfg config.Config, onBrainUpdate func(agentID, file, summary string), onFileWrite func(string)) error {
	box := eng.Sandbox()

	defs := []mcp.ToolDef{
		filesystemTool(box, onFileWrite),
		shellExecutorTool(box),
		fileManagerTool(box),
		iotGatewayTool(cfg),
		webSearchTool(),
		projectCriticTool(box),
		SelfUpdateTool(cfg.App.DataDir, onBrainUpdate),
	}

	for _, d := range defs {
		if err := eng.Register(d); err != nil {
			return err
		}
	}
	return nil
}
