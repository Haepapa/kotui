package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/pkg/models"
)

// kbAgentID is the stable agent namespace used for all project-wide RAG index
// entries.  Using a distinct ID keeps KB embeddings separate from per-agent
// journal entries while reusing the same store schema.
const kbAgentID = "knowledge_base"

// kbProjectID is the project namespace for the global knowledge base.
// It is intentionally decoupled from the per-agent project IDs so that the
// index persists across project restarts.
const kbProjectID = "kb-global"

// kbMaxFileBytes is the maximum file size (bytes) indexed per file.
const kbMaxFileBytes = 64 * 1024 // 64 KB

// kbTextExtensions lists the file suffixes that are indexed as plain text.
var kbTextExtensions = map[string]bool{
	".go": true, ".ts": true, ".svelte": true, ".json": true,
	".md": true, ".yaml": true, ".yml": true, ".txt": true,
	".py": true, ".sh": true, ".toml": true, ".js": true,
	".html": true, ".css": true, ".env": true,
}

var knowledgeBaseSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "index_project | query"
		},
		"query": {
			"type": "string",
			"description": "Search query — required for the query operation"
		},
		"top_k": {
			"type": "number",
			"description": "Number of results to return for query (default 5, max 20)"
		}
	}
}`)

// knowledgeBaseTool returns the knowledge_base MCP tool. getStore is called at
// handler invocation time; it may return nil if no embedder is configured.
func knowledgeBaseTool(box *mcp.Sandbox, getStore func() *memory.Store) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "knowledge_base",
		Clearance: models.ClearanceSpecialist,
		Description: "Index all project files into a vector store or query for relevant content. " +
			"Use operation=index_project to build (or refresh) the index for the current workspace, " +
			"then operation=query to search for relevant context. " +
			"The index persists between sessions.",
		Schema:  knowledgeBaseSchema,
		Handler: knowledgeBaseHandler(box, getStore),
	}
}

func knowledgeBaseHandler(box *mcp.Sandbox, getStore func() *memory.Store) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		mem := getStore()
		if mem == nil {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "The knowledge_base tool requires an embedder model. Configure one in Settings → Models → Embedder.",
				Underlying:    fmt.Errorf("knowledge_base: memory store not initialised (no embedder model configured)"),
			}
		}

		op, _ := args["operation"].(string)
		switch op {
		case "index_project":
			return kbIndexProject(ctx, box, mem)
		case "query":
			query, _ := args["query"].(string)
			if query == "" {
				return "", fmt.Errorf("knowledge_base: query must not be empty for operation=query")
			}
			topK := 5
			if v, ok := args["top_k"]; ok {
				if k := int(toFloat64(v)); k > 0 && k <= 20 {
					topK = k
				}
			}
			return kbQuery(ctx, mem, query, topK)
		default:
			return "", fmt.Errorf("knowledge_base: unknown operation %q (must be index_project or query)", op)
		}
	}
}

// kbIndexProject walks the sandbox, reads text files, and indexes them into
// the vector store. Returns a summary of how many files were indexed.
func kbIndexProject(ctx context.Context, box *mcp.Sandbox, mem *memory.Store) (string, error) {
	root := box.Root()
	if root == "" {
		return "", fmt.Errorf("knowledge_base: sandbox root is not configured")
	}

	indexed := 0
	skipped := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			// Skip hidden directories and common noise folders.
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !kbTextExtensions[ext] {
			skipped++
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil || info.Size() > kbMaxFileBytes {
			skipped++
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			skipped++
			return nil
		}

		// Use the relative path as a namespace prefix so the agent knows where
		// the content came from.
		rel, _ := filepath.Rel(root, path)
		content := fmt.Sprintf("# File: %s\n\n%s", rel, string(data))

		mem.IndexAsync(ctx, kbAgentID, kbProjectID, content, false)
		indexed++
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("knowledge_base: walk error: %w", err)
	}

	return fmt.Sprintf("✅ Knowledge base indexing started: %d files queued, %d skipped. "+
		"Embeddings are generated asynchronously — wait a moment before querying.", indexed, skipped), nil
}

// kbQuery performs a semantic search against the indexed knowledge base.
func kbQuery(ctx context.Context, mem *memory.Store, query string, topK int) (string, error) {
	entries, err := mem.Recall(ctx, kbAgentID, kbProjectID, query, topK)
	if err != nil {
		return "", fmt.Errorf("knowledge_base: query failed: %w", err)
	}
	if len(entries) == 0 {
		return "No relevant content found. Run operation=index_project first to build the knowledge base.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Knowledge Base Results (%d matches for %q)\n\n", len(entries), query))
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("### Match %d\n%s\n\n---\n\n", i+1, e.Content))
	}
	return sb.String(), nil
}
