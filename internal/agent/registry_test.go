package agent_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/pkg/models"
)

// setupBrainFiles creates a temporary directory with all three brain files and
// returns the IdentityPaths pointing at them.
func setupBrainFiles(t *testing.T, soul, persona, skills string) agent.IdentityPaths {
	t.Helper()
	dir := t.TempDir()
	identityDir := filepath.Join(dir, "identity")
	if err := os.MkdirAll(identityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soulPath := filepath.Join(identityDir, "soul.md")
	personaPath := filepath.Join(identityDir, "persona.md")
	skillsPath := filepath.Join(identityDir, "skills.md")

	if err := os.WriteFile(soulPath, []byte(soul), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personaPath, []byte(persona), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillsPath, []byte(skills), 0o644); err != nil {
		t.Fatal(err)
	}
	return agent.IdentityPaths{
		Root:        dir,
		IdentityDir: identityDir,
		SoulPath:    soulPath,
		PersonaPath: personaPath,
		SkillsPath:  skillsPath,
	}
}

// --- IdentityRegistry tests ---------------------------------------------------

func TestIdentityRegistry_CacheMissLoadsFromDisk(t *testing.T) {
	paths := setupBrainFiles(t, "soul content", "persona content", "skills content")
	reg := agent.NewIdentityRegistry()

	cached, err := reg.Get(paths, "lead")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cached.Soul != "soul content" {
		t.Errorf("Soul = %q, want %q", cached.Soul, "soul content")
	}
	if cached.Persona != "persona content" {
		t.Errorf("Persona = %q", cached.Persona)
	}
	if cached.Skills != "skills content" {
		t.Errorf("Skills = %q", cached.Skills)
	}
}

func TestIdentityRegistry_CacheHitSkipsDiskRead(t *testing.T) {
	paths := setupBrainFiles(t, "original soul", "original persona", "original skills")
	reg := agent.NewIdentityRegistry()

	// Warm the cache.
	if _, err := reg.Get(paths, "lead"); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// Overwrite the file on disk — the cache should return the old value.
	if err := os.WriteFile(paths.SoulPath, []byte("updated soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	cached, err := reg.Get(paths, "lead")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if cached.Soul != "original soul" {
		t.Errorf("expected cache hit; got Soul = %q", cached.Soul)
	}
}

func TestIdentityRegistry_InvalidateTriggersDiskRead(t *testing.T) {
	paths := setupBrainFiles(t, "original soul", "original persona", "original skills")
	reg := agent.NewIdentityRegistry()

	// Warm the cache.
	if _, err := reg.Get(paths, "lead"); err != nil {
		t.Fatal(err)
	}

	// Update the file on disk and invalidate the cache.
	if err := os.WriteFile(paths.SoulPath, []byte("new soul"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Invalidate("lead")

	cached, err := reg.Get(paths, "lead")
	if err != nil {
		t.Fatalf("Get after invalidate: %v", err)
	}
	if cached.Soul != "new soul" {
		t.Errorf("expected re-read; got Soul = %q", cached.Soul)
	}
}

func TestIdentityRegistry_InvalidateNonExistentEntry(t *testing.T) {
	reg := agent.NewIdentityRegistry()
	// Must not panic when invalidating an entry that was never loaded.
	reg.Invalidate("ghost-agent")

	// After invalidation of a sentinel entry, Get should still load from disk.
	paths := setupBrainFiles(t, "s", "p", "k")
	cached, err := reg.Get(paths, "ghost-agent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cached.Soul != "s" || cached.Persona != "p" || cached.Skills != "k" {
		t.Errorf("unexpected cached values: %+v", cached)
	}
}

func TestIdentityRegistry_Set(t *testing.T) {
	reg := agent.NewIdentityRegistry()
	reg.Set("agent1", "soul-val", "persona-val", "skills-val")

	// Paths point at non-existent files — should not be read.
	paths := agent.IdentityPaths{
		SoulPath:    "/nonexistent/soul.md",
		PersonaPath: "/nonexistent/persona.md",
		SkillsPath:  "/nonexistent/skills.md",
	}
	cached, err := reg.Get(paths, "agent1")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if cached.Soul != "soul-val" || cached.Persona != "persona-val" || cached.Skills != "skills-val" {
		t.Errorf("unexpected values after Set: %+v", cached)
	}
}

func TestIdentityRegistry_ConcurrentAccess(t *testing.T) {
	paths := setupBrainFiles(t, "soul", "persona", "skills")
	reg := agent.NewIdentityRegistry()

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Get(paths, "lead")
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Get: %v", err)
	}
}

func TestIdentityRegistry_MultipleAgents(t *testing.T) {
	p1 := setupBrainFiles(t, "soul-A", "persona-A", "skills-A")
	p2 := setupBrainFiles(t, "soul-B", "persona-B", "skills-B")
	reg := agent.NewIdentityRegistry()

	a, _ := reg.Get(p1, "agent-A")
	b, _ := reg.Get(p2, "agent-B")

	if a.Soul != "soul-A" {
		t.Errorf("agent-A soul = %q", a.Soul)
	}
	if b.Soul != "soul-B" {
		t.Errorf("agent-B soul = %q", b.Soul)
	}

	// Invalidate only agent-A; agent-B cache should remain warm.
	if err := os.WriteFile(p1.SoulPath, []byte("soul-A-updated"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.Invalidate("agent-A")

	a2, _ := reg.Get(p1, "agent-A")
	b2, _ := reg.Get(p2, "agent-B")

	if a2.Soul != "soul-A-updated" {
		t.Errorf("agent-A after invalidate = %q", a2.Soul)
	}
	if b2.Soul != "soul-B" {
		t.Errorf("agent-B affected by agent-A invalidation: got %q", b2.Soul)
	}
}

// --- EnsureDefaultFiles tests (existing functionality, regression guard) --------

func TestEnsureDefaultFiles_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	paths := agent.AgentPaths(dir, "lead")
	if err := agent.EnsureDefaultFiles(paths, "lead", "Lead", models.RoleLead, "llama3"); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}
	for _, p := range []string{paths.SoulPath, paths.PersonaPath, paths.SkillsPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestEnsureDefaultFiles_IdempotentOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	paths := agent.AgentPaths(dir, "lead")
	if err := agent.EnsureDefaultFiles(paths, "lead", "Lead", models.RoleLead, "llama3"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Write custom content to one file.
	if err := os.WriteFile(paths.SoulPath, []byte("custom soul"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Second call must not overwrite.
	if err := agent.EnsureDefaultFiles(paths, "lead", "Lead", models.RoleLead, "llama3"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	b, _ := os.ReadFile(paths.SoulPath)
	if string(b) != "custom soul" {
		t.Errorf("EnsureDefaultFiles overwrote existing file; got %q", string(b))
	}
}
