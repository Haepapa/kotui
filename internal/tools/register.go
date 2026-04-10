// Package tools registers all core MCP tools into the Engine.
//
// Call RegisterAll once at application startup, after the MCP Engine and
// config have been initialised.
package tools

import (
	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/memory"
)

// RegisterAll registers the core MCP tool set into eng.
// cfg is required for tools that need access to application configuration
// (e.g. iot_gateway reads Senior Consultant SSH settings).
func RegisterAll(eng *mcp.Engine, cfg config.Config) error {
	return RegisterAllWithHooks(eng, cfg, nil, nil, nil, nil)
}

// RegisterAllWithHooks is like RegisterAll but also wires optional hooks and
// services used by individual tools:
//   - onBrainUpdate: called by update_self when an agent writes a brain file
//   - onFileWrite:   called by filesystem when a file is written
//   - mem:           pointer to a *memory.Store (may be nil pointer or nil **); the
//                    double-pointer allows the orchestrator to set the store after
//                    tool registration (same pattern as dispatchFileWrite).
//   - sudoGate:      gate for sudo approval workflow (nil = hard-block sudo)
func RegisterAllWithHooks(
	eng *mcp.Engine,
	cfg config.Config,
	onBrainUpdate func(agentID, file, summary string),
	onFileWrite func(string),
	mem **memory.Store,
	sudoGate *SudoGate,
) error {
	box := eng.Sandbox()

	// Build a lazy getter that reads through the double-pointer at call time.
	getStore := func() *memory.Store {
		if mem == nil {
			return nil
		}
		return *mem
	}

	defs := []mcp.ToolDef{
		filesystemTool(box, onFileWrite),
		shellExecutorTool(box, sudoGate),
		fileManagerTool(box),
		iotGatewayTool(cfg),
		webSearchTool(),
		projectCriticTool(box),
		SelfUpdateTool(cfg.App.DataDir, onBrainUpdate),
		WriteJournalTool(cfg.App.DataDir),
		knowledgeBaseTool(box, getStore),
	}

	for _, d := range defs {
		if err := eng.Register(d); err != nil {
			return err
		}
	}
	return nil
}

