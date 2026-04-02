package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Sandbox scopes all file-system operations to a project workspace root.
// It prevents path traversal attacks (../../etc/passwd) and ensures
// every file operation stays within the active project directory.
type Sandbox struct {
	root string // absolute, cleaned path
}

func newSandbox(root string) *Sandbox {
	if root == "" {
		return &Sandbox{root: ""}
	}
	return &Sandbox{root: filepath.Clean(root)}
}

// NewSandboxForTest creates a Sandbox directly — exported for use in package tests.
func NewSandboxForTest(root string) *Sandbox { return newSandbox(root) }


// Root returns the sandbox root directory.
func (s *Sandbox) Root() string { return s.root }

// Disabled reports whether sandbox enforcement is turned off (root is "").
// Used in tests and headless relay mode.
func (s *Sandbox) Disabled() bool { return s.root == "" }

// Resolve takes a path (absolute or relative) and returns the absolute path
// within the sandbox. Returns a SandboxError if the resolved path would
// escape the sandbox root.
//
// If the sandbox is disabled (root == ""), the path is returned as-is after
// cleaning.
func (s *Sandbox) Resolve(path string) (string, error) {
	if s.root == "" {
		return filepath.Clean(path), nil
	}

	// Make relative paths relative to the sandbox root.
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.root, path)
	}

	clean := filepath.Clean(path)

	// Verify the cleaned path is inside the sandbox root.
	// We require a trailing separator to avoid "root2" matching "root".
	rootSlash := s.root + string(filepath.Separator)
	if clean != s.root && !strings.HasPrefix(clean, rootSlash) {
		return "", &SandboxError{Path: path, Root: s.root}
	}
	return clean, nil
}

// BackupPath returns the path where a file should be backed up before deletion.
// Format: <original_path>.kotui_bak
func (s *Sandbox) BackupPath(path string) string {
	return path + ".kotui_bak"
}

// ValidateSudo returns an error if the args map contains a "sudo" key set to true.
// MCP tools must call this before constructing any shell command.
func ValidateSudo(args map[string]any) error {
	if v, ok := args["sudo"]; ok {
		if b, ok := v.(bool); ok && b {
			return fmt.Errorf("mcp: sudo is not permitted")
		}
		if s, ok := v.(string); ok && s == "true" {
			return fmt.Errorf("mcp: sudo is not permitted")
		}
	}
	return nil
}
