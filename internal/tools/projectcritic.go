// Package tools/projectcritic implements the project_critic MCP tool.
//
// Phase 10 scope (self-built, Lead clearance):
//   - lint:   Run available linters in the sandbox (go vet, staticcheck if present, eslint if present)
//   - review: Structural analysis — file count, line count, language breakdown, large-file warnings
//   - verify: Syntax verification for Go and JSON files in the sandbox
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const (
	criticLintTimeout   = 60 * time.Second
	criticMaxFileReport = 50
	criticLargeFileKB   = 500
)

var projectCriticSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "lint | review | verify"
		},
		"path": {
			"type": "string",
			"description": "Subdirectory or file within the project workspace to analyse (default: . for root)"
		}
	}
}`)

func projectCriticTool(box *mcp.Sandbox) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "project_critic",
		Clearance: models.ClearanceLead,
		Description: "Static analysis and code review for the project workspace. " +
			"lint: runs available linters (go vet, staticcheck, eslint). " +
			"review: structural analysis — file count, size breakdown, language stats. " +
			"verify: syntax check all Go and JSON files in the target path.",
		Schema:  projectCriticSchema,
		Handler: projectCriticHandler(box),
	}
}

func projectCriticHandler(box *mcp.Sandbox) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}

		resolved, err := box.Resolve(path)
		if err != nil {
			return "", err
		}

		switch op {
		case "lint":
			return criticLint(ctx, resolved)
		case "review":
			return criticReview(resolved)
		case "verify":
			return criticVerify(ctx, resolved)
		default:
			return "", fmt.Errorf("project_critic: unknown operation %q — supported: lint, review, verify", op)
		}
	}
}

// criticLint runs available linters in the target directory and accumulates results.
func criticLint(ctx context.Context, dir string) (string, error) {
	lintCtx, cancel := context.WithTimeout(ctx, criticLintTimeout)
	defer cancel()

	var results []string
	anyFound := false

	// go vet — always available in a Go environment
	if isGoProject(dir) {
		out, err := runLinter(lintCtx, dir, "go", "vet", "./...")
		results = append(results, "## go vet\n"+formatLinterOutput(out, err))
		anyFound = true
	}

	// staticcheck — optional, skip gracefully if not installed
	if isGoProject(dir) && commandExists("staticcheck") {
		out, err := runLinter(lintCtx, dir, "staticcheck", "./...")
		results = append(results, "## staticcheck\n"+formatLinterOutput(out, err))
	}

	// eslint — optional, skip gracefully if not installed
	if isJSProject(dir) && commandExists("eslint") {
		out, err := runLinter(lintCtx, dir, "eslint", "--format=compact", ".")
		results = append(results, "## eslint\n"+formatLinterOutput(out, err))
		anyFound = true
	}

	if !anyFound {
		return "project_critic: no supported linters applicable to this directory " +
			"(no Go or JS project detected).", nil
	}

	return strings.Join(results, "\n\n"), nil
}

// criticReview produces a structural analysis of the project tree.
func criticReview(dir string) (string, error) {
	type fileStat struct {
		path  string
		lines int
		bytes int64
		lang  string
	}

	langExts := map[string]string{
		".go":   "Go",
		".ts":   "TypeScript",
		".js":   "JavaScript",
		".svelte": "Svelte",
		".py":   "Python",
		".sh":   "Shell",
		".md":   "Markdown",
		".toml": "TOML",
		".json": "JSON",
		".yaml": "YAML",
		".yml":  "YAML",
		".sql":  "SQL",
	}

	var stats []fileStat
	var warnings []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		lang := langExts[ext]
		if lang == "" {
			lang = "Other"
		}

		rel, _ := filepath.Rel(dir, path)
		content, err := os.ReadFile(path)
		lines := 0
		if err == nil {
			lines = bytes.Count(content, []byte("\n"))
		}

		fs := fileStat{path: rel, lines: lines, bytes: info.Size(), lang: lang}
		stats = append(stats, fs)

		if info.Size() > criticLargeFileKB*1024 {
			warnings = append(warnings, fmt.Sprintf("LARGE FILE: %s (%.1f KB)", rel, float64(info.Size())/1024))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("project_critic: review walk error: %w", err)
	}

	// Aggregate by language
	langLines := make(map[string]int)
	langFiles := make(map[string]int)
	totalLines := 0
	totalFiles := len(stats)

	for _, fs := range stats {
		langLines[fs.lang] += fs.lines
		langFiles[fs.lang]++
		totalLines += fs.lines
	}

	// Sort languages by line count descending
	type langStat struct {
		name  string
		files int
		lines int
	}
	var langs []langStat
	for lang, lines := range langLines {
		langs = append(langs, langStat{lang, langFiles[lang], lines})
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i].lines > langs[j].lines })

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Project Review\n\nTotal files: %d | Total lines: %d\n\n", totalFiles, totalLines))
	sb.WriteString("### Language Breakdown\n\n")
	sb.WriteString("| Language | Files | Lines |\n|----------|-------|-------|\n")
	for _, l := range langs {
		sb.WriteString(fmt.Sprintf("| %s | %d | %d |\n", l.name, l.files, l.lines))
	}

	// Top 10 largest files
	sort.Slice(stats, func(i, j int) bool { return stats[i].lines > stats[j].lines })
	limit := 10
	if len(stats) < limit {
		limit = len(stats)
	}
	if limit > 0 {
		sb.WriteString("\n### Top Files by Line Count\n\n")
		for _, fs := range stats[:limit] {
			sb.WriteString(fmt.Sprintf("- %s (%d lines)\n", fs.path, fs.lines))
		}
	}

	if len(warnings) > 0 {
		sb.WriteString("\n### Warnings\n\n")
		for _, w := range warnings {
			sb.WriteString("- " + w + "\n")
		}
	}

	return sb.String(), nil
}

// criticVerify performs syntax checks on Go and JSON files.
func criticVerify(ctx context.Context, dir string) (string, error) {
	var issues []string
	checked := 0

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		rel, _ := filepath.Rel(dir, path)

		switch ext {
		case ".json":
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			var probe any
			if jsonErr := json.Unmarshal(data, &probe); jsonErr != nil {
				issues = append(issues, fmt.Sprintf("JSON syntax error in %s: %v", rel, jsonErr))
			}
			checked++

		case ".go":
			// Use gofmt -e to surface syntax errors without modifying the file.
			verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(verifyCtx, "gofmt", "-e", path)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if runErr := cmd.Run(); runErr != nil {
				issues = append(issues, fmt.Sprintf("Go syntax error in %s: %s", rel, strings.TrimSpace(stderr.String())))
			}
			checked++
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("project_critic: verify walk error: %w", err)
	}

	if checked == 0 {
		return "project_critic: no Go or JSON files found to verify.", nil
	}

	if len(issues) == 0 {
		return fmt.Sprintf("project_critic: verified %d file(s) — no syntax errors found.", checked), nil
	}

	return fmt.Sprintf("project_critic: verified %d file(s) — %d issue(s) found:\n\n%s",
		checked, len(issues), strings.Join(issues, "\n")), nil
}

// helpers

func runLinter(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func formatLinterOutput(out string, err error) string {
	if out == "" {
		if err == nil {
			return "✓ No issues found."
		}
		return fmt.Sprintf("✗ Linter error: %v", err)
	}
	return out
}

func isGoProject(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err == nil {
		return true
	}
	// Check if any .go files exist in the dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func isJSProject(dir string) bool {
	for _, name := range []string{"package.json", ".eslintrc.js", ".eslintrc.json", ".eslintrc.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
