package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// JournalEntry is a structured record of a completed task.
// It maps exactly to the journal format described in handbook.md.
type JournalEntry struct {
	Date            time.Time
	Task            string // one-line task description
	Outcome         string // "success", "partial", or "failure"
	Summary         string // 2–4 sentence description
	Lessons         string // what to do differently, or "none"
	SkillsProposed  string // comma-separated, or "none"
}

// writeJournal persists a journal entry to
// {agent.JournalDir}/YYYY-MM-DD-HHMM.md
func writeJournal(paths IdentityPaths, entry JournalEntry) error {
	if err := os.MkdirAll(paths.JournalDir, 0o755); err != nil {
		return fmt.Errorf("journal: mkdir: %w", err)
	}

	if entry.Date.IsZero() {
		entry.Date = time.Now()
	}
	if entry.Outcome == "" {
		entry.Outcome = "success"
	}
	if entry.Lessons == "" {
		entry.Lessons = "none"
	}
	if entry.SkillsProposed == "" {
		entry.SkillsProposed = "none"
	}

	filename := entry.Date.UTC().Format("2006-01-02-1504") + ".md"
	path := filepath.Join(paths.JournalDir, filename)

	content := fmt.Sprintf(`---
Date: %s
Task: %s
Outcome: %s
Summary: %s
Lessons: %s
Skills Proposed: %s
---
`,
		entry.Date.UTC().Format("2006-01-02 15:04"),
		entry.Task,
		entry.Outcome,
		entry.Summary,
		entry.Lessons,
		entry.SkillsProposed,
	)

	return os.WriteFile(path, []byte(content), 0o644)
}

// ListJournals returns the paths of all journal entries for an agent,
// ordered chronologically (oldest first).
func ListJournals(paths IdentityPaths) ([]string, error) {
	entries, err := os.ReadDir(paths.JournalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list journals: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, filepath.Join(paths.JournalDir, e.Name()))
		}
	}
	return files, nil
}
