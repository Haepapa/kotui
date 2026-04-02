package mcp

import "github.com/haepapa/kotui/pkg/models"

// PermissionGate enforces the clearance hierarchy.
//
// Immutable Law: an agent may only invoke a tool if its clearance level is
// greater than or equal to the tool's required clearance.
//
//	Lead (2)       ≥ Lead, Specialist, Trial tools
//	Specialist (1) ≥ Specialist, Trial tools
//	Trial (0)      ≥ Trial tools only
type PermissionGate struct{}

func newPermissionGate() *PermissionGate { return &PermissionGate{} }

// Check returns a PermissionError if agentClearance < requiredClearance,
// or nil if the call is permitted.
func (g *PermissionGate) check(agentClearance, requiredClearance models.Clearance, toolName string) error {
	if agentClearance < requiredClearance {
		return &PermissionError{
			AgentClearance:    agentClearance,
			RequiredClearance: requiredClearance,
			ToolName:          toolName,
		}
	}
	return nil
}
