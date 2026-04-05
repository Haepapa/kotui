package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/haepapa/kotui/pkg/models"
)

const maxAttempts = 3

// Executor validates tool call arguments, enforces permissions, and runs
// the handler with automatic retries.
type Executor struct {
	reg  *Registry
	gate *PermissionGate
	box  *Sandbox
}

func newExecutor(r *Registry, g *PermissionGate, s *Sandbox) *Executor {
	return &Executor{reg: r, gate: g, box: s}
}

// execute is the hot path:
//  1. Look up the tool definition
//  2. Permission gate — reject immediately if clearance insufficient
//  3. Validate args against the tool's JSON schema
//  4. Invoke the handler up to maxAttempts times
//  5. On exhaustion → EscalationError
func (e *Executor) execute(ctx context.Context, clearance models.Clearance, call models.ToolCall) (models.ToolResult, error) {
	def, ok := e.reg.lookup(call.ToolName)
	if !ok {
		return models.ToolResult{}, fmt.Errorf("mcp: unknown tool %q", call.ToolName)
	}

	// Permission gate — this must be checked before any other work.
	if err := e.gate.check(clearance, def.Clearance, def.Name); err != nil {
		return models.ToolResult{}, err
	}

	// Schema validation.
	if err := validateArgs(def.Schema, call.Args); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.ToolName, IsError: true, Output: err.Error(), Attempts: 1}, err
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return models.ToolResult{}, ctx.Err()
		default:
		}

		output, err := def.Handler(ctx, call.Args)
		if err == nil {
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.ToolName,
				Output:   output,
				Attempts: attempt,
			}, nil
		}
		lastErr = err

		// For MCPErrors: include the suggestion in the result so the agent
		// can read it and self-correct.  Non-recoverable errors skip remaining
		// retries immediately — there is nothing the executor can do.
		var mcpErr *MCPError
		if errors.As(err, &mcpErr) && !mcpErr.IsRecoverable {
			escErr := &EscalationError{ToolName: call.ToolName, Attempts: attempt, Last: lastErr}
			return models.ToolResult{
				CallID:   call.ID,
				ToolName: call.ToolName,
				Output:   escErr.Error(),
				IsError:  true,
				Attempts: attempt,
			}, escErr
		}
	}

	// All attempts exhausted.
	escErr := &EscalationError{ToolName: call.ToolName, Attempts: maxAttempts, Last: lastErr}
	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.ToolName,
		Output:   escErr.Error(),
		IsError:  true,
		Attempts: maxAttempts,
	}, escErr
}

// validateArgs checks that the provided args satisfy the tool's JSON Schema.
// Handles the common MVP subset: object type, required fields, property types.
// Unrecognised schema keywords are silently skipped (forward-compatible).
func validateArgs(schema json.RawMessage, args map[string]any) error {
	if len(schema) == 0 {
		return nil // no schema == no validation
	}

	var s schemaNode
	if err := jsonUnmarshal(schema, &s); err != nil {
		return fmt.Errorf("mcp: malformed tool schema: %w", err)
	}

	// Only validate object-type schemas.
	if s.Type != "" && s.Type != "object" {
		return nil
	}

	// Check required fields.
	for _, req := range s.Required {
		if _, ok := args[req]; !ok {
			return fmt.Errorf("mcp: missing required argument %q", req)
		}
	}

	// Check property types where declared.
	for name, prop := range s.Properties {
		val, present := args[name]
		if !present {
			continue
		}
		if err := checkType(name, prop.Type, val); err != nil {
			return err
		}
	}

	return nil
}

type schemaNode struct {
	Type       string                `json:"type"`
	Required   []string              `json:"required"`
	Properties map[string]schemaNode `json:"properties"`
}

func checkType(name, wantType string, val any) error {
	if wantType == "" {
		return nil
	}
	ok := false
	switch wantType {
	case "string":
		_, ok = val.(string)
	case "number", "integer":
		switch val.(type) {
		case float64, int, int64, float32:
			ok = true
		}
	case "boolean":
		_, ok = val.(bool)
	case "array":
		_, ok = val.([]any)
	case "object":
		_, ok = val.(map[string]any)
	default:
		ok = true // unknown type — skip
	}
	if !ok {
		return fmt.Errorf("mcp: argument %q: expected type %q, got %T", name, wantType, val)
	}
	return nil
}

// jsonUnmarshal is a thin wrapper so schema.go and registry.go can share it
// without importing encoding/json in multiple files unnecessarily.
func jsonUnmarshal(data json.RawMessage, v any) error {
	return json.Unmarshal(data, v)
}
