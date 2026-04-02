package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const defaultMaxDepth = 5

var fileManagerSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "tree | stat | find"
		},
		"path": {
			"type": "string",
			"description": "Root path for tree/stat, or search root for find. Defaults to workspace root."
		},
		"pattern": {
			"type": "string",
			"description": "Glob pattern for find (e.g. '*.go', '**/*_test.go')."
		},
		"max_depth": {
			"type": "number",
			"description": "Maximum recursion depth for tree. Default 5."
		}
	}
}`)

func fileManagerTool(box *mcp.Sandbox) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "file_manager",
		Clearance: models.ClearanceLead,
		Description: "Read-only project structure overview. " +
			"Operations: tree (recursive directory listing), stat (file metadata), find (glob search). " +
			"All paths are scoped to the project workspace.",
		Schema:  fileManagerSchema,
		Handler: fileManagerHandler(box),
	}
}

func fileManagerHandler(box *mcp.Sandbox) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)

		// Resolve the root path (defaults to sandbox root).
		pathArg, _ := args["path"].(string)
		if pathArg == "" {
			pathArg = "."
		}
		root, err := box.Resolve(pathArg)
		if err != nil {
			return "", err
		}

		switch op {
		case "tree":
			maxDepth := defaultMaxDepth
			if md := toFloat64(args["max_depth"]); md > 0 {
				maxDepth = int(md)
			}
			return fmTree(root, maxDepth)

		case "stat":
			return fmStat(root)

		case "find":
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return "", fmt.Errorf("file_manager: find requires a pattern argument")
			}
			return fmFind(root, pattern)

		default:
			return "", fmt.Errorf("file_manager: unknown operation %q (must be tree, stat, or find)", op)
		}
	}
}

// fmTree produces an indented directory tree up to maxDepth levels deep.
func fmTree(root string, maxDepth int) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("file_manager: stat %s: %w", root, err)
	}
	var sb strings.Builder
	sb.WriteString(info.Name() + "/\n")
	walkTree(&sb, root, "", 1, maxDepth)
	return sb.String(), nil
}

func walkTree(sb *strings.Builder, dir, prefix string, depth, maxDepth int) {
	if depth > maxDepth {
		sb.WriteString(prefix + "  ...\n")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for i, e := range entries {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(entries)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		name := e.Name()
		if e.IsDir() {
			sb.WriteString(prefix + connector + name + "/\n")
			walkTree(sb, filepath.Join(dir, name), childPrefix, depth+1, maxDepth)
		} else {
			sb.WriteString(prefix + connector + name + "\n")
		}
	}
}

// fmStat returns file/directory metadata.
func fmStat(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file_manager: stat %s: %w", path, err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Name:     %s\n", info.Name()))
	sb.WriteString(fmt.Sprintf("Path:     %s\n", path))
	if info.IsDir() {
		sb.WriteString("Type:     directory\n")
		count := countEntries(path)
		sb.WriteString(fmt.Sprintf("Children: %d\n", count))
	} else {
		sb.WriteString("Type:     file\n")
		sb.WriteString(fmt.Sprintf("Size:     %d bytes\n", info.Size()))
	}
	sb.WriteString(fmt.Sprintf("Modified: %s\n", info.ModTime().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Mode:     %s\n", info.Mode().String()))
	return sb.String(), nil
}

func countEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// fmFind searches for files matching a glob pattern under root.
func fmFind(root, pattern string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		matched, mErr := filepath.Match(pattern, d.Name())
		if mErr != nil {
			return fmt.Errorf("file_manager: invalid pattern %q: %w", pattern, mErr)
		}
		if matched {
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return fmt.Sprintf("no files matching %q found under %s", pattern, root), nil
	}
	return strings.Join(matches, "\n"), nil
}
