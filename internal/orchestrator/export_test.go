// export_test.go exposes internal functions and constructors for black-box
// testing in the orchestrator_test package. This file is only compiled during
// tests (the _test.go suffix is on the package declaration's file, not the name).
package orchestrator

import (
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

// Parse helpers — exported for tests.
var ExportedParseToolCall = parseToolCall
var ExportedParseEscalation = parseEscalation
var ExportedParseTaskList = parseTaskList
var ExportedStripToolCallLines = stripToolCallLines

// NewRunningAgentForTest creates a RunningAgent directly for unit testing,
// bypassing the need for a real agent.Agent and filesystem.
func NewRunningAgentForTest(id, name, model string, clearance models.Clearance, inf Inferrer, eng *mcp.Engine) *RunningAgent {
	if eng == nil {
		eng = mcp.New("") // disabled sandbox for tests
	}
	return newRunningAgent(id, name, model, clearance, nil, "", inf, eng)
}

// NewVRAMCoordinatorForTest creates a VRAMCoordinator for unit testing.
func NewVRAMCoordinatorForTest(profile models.VRAMProfile, inf Inferrer, leadModel string) *VRAMCoordinator {
	return newVRAMCoordinator(profile, inf, leadModel)
}
