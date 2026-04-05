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
var ExportedParseConfidenceSignal = parseConfidenceSignal

// ExportedLowConfidenceError allows tests to inspect the error type.
type ExportedLowConfidenceError = LowConfidenceError

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

// NewCogQueueForTest creates a CogQueue with an optional state callback.
// The caller must call Start with an appropriate context before submitting.
// sysmon is nil (pressure throttling disabled) in tests.
func NewCogQueueForTest(onState func(QueueState)) *CogQueue {
	return NewCogQueue(onState, nil)
}

// ExportedCogPriorities exposes the priority constants for black-box tests.
var (
	ExportedP0Emergency   = P0Emergency
	ExportedP1Lead        = P1Lead
	ExportedP2Interactive = P2Interactive
	ExportedP3Background  = P3Background
)
