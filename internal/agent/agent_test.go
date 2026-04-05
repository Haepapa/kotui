package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/pkg/models"
)

// --- helpers ---------------------------------------------------------------

func spawnTestAgent(t *testing.T, role models.AgentRole) (*agent.Agent, string) {
	t.Helper()
	dataDir := t.TempDir()
	a, err := agent.Spawn(agent.SpawnConfig{
		ID:                  "agent-test-01",
		Name:                "Tahi",
		Role:                role,
		Model:               "llama3.1:8b",
		ProjectID:           "proj-x",
		DataDir:             dataDir,
		CompanyIdentityPath: "", // no COMPANY_IDENTITY.md — uses fallback
		MCPFragment:         "## Available Tools\n\n- read_file\n",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return a, dataDir
}

// --- Identity filesystem ---------------------------------------------------

func TestSpawn_CreatesIdentityDirectories(t *testing.T) {
	a, dataDir := spawnTestAgent(t, models.RoleSpecialist)

	paths := a.Paths()
	for _, dir := range []string{paths.IdentityDir, paths.JournalDir} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", dir)
		}
	}
	_ = dataDir
}

func TestSpawn_CreatesDefaultIdentityFiles(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	paths := a.Paths()

	expected := []string{
		paths.SoulPath,
		paths.PersonaPath,
		paths.SkillsPath,
		paths.InstructionPath,
		paths.ProposedSkillsPath,
	}
	for _, f := range expected {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}

func TestSpawn_SoulContainsAgentName(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	data, err := os.ReadFile(a.Paths().SoulPath)
	if err != nil {
		t.Fatalf("read soul.md: %v", err)
	}
	if !strings.Contains(string(data), "Tahi") {
		t.Errorf("soul.md should contain agent name 'Tahi': %q", string(data))
	}
}

func TestSpawn_SkillsContainsModel(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)
	data, err := os.ReadFile(a.Paths().SkillsPath)
	if err != nil {
		t.Fatalf("read skills.md: %v", err)
	}
	if !strings.Contains(string(data), "llama3.1:8b") {
		t.Errorf("skills.md should contain model name: %q", string(data))
	}
}

// Calling Spawn twice with the same ID and dataDir should not overwrite
// existing identity documents.
func TestSpawn_IdempotentDoesNotOverwriteExistingFiles(t *testing.T) {
	dataDir := t.TempDir()
	cfg := agent.SpawnConfig{
		ID: "agent-01", Name: "Tahi", Role: models.RoleLead,
		Model: "qwen2.5-coder:32b", DataDir: dataDir,
	}
	a1, err := agent.Spawn(cfg)
	if err != nil {
		t.Fatalf("first Spawn: %v", err)
	}

	// Manually edit soul.md.
	customContent := "# Custom Soul\nThis is custom content.\n"
	os.WriteFile(a1.Paths().SoulPath, []byte(customContent), 0o644)

	// Second spawn.
	_, err = agent.Spawn(cfg)
	if err != nil {
		t.Fatalf("second Spawn: %v", err)
	}

	data, _ := os.ReadFile(a1.Paths().SoulPath)
	if !strings.Contains(string(data), "Custom Soul") {
		t.Error("second Spawn should not overwrite existing soul.md")
	}
}

// --- System prompt composer ------------------------------------------------

func TestSystemPrompt_ContainsHandbook(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	prompt := a.SystemPrompt()
	if !strings.Contains(prompt, "Hard Constraints") {
		t.Error("system prompt should contain handbook 'Hard Constraints' section")
	}
	if !strings.Contains(prompt, "escalation_needed") {
		t.Error("system prompt should contain escalation_needed signal")
	}
}

func TestSystemPrompt_ContainsMCPFragment(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	if !strings.Contains(a.SystemPrompt(), "Available Tools") {
		t.Error("system prompt should contain MCP tool fragment")
	}
}

func TestSystemPrompt_ContainsCapabilityCeiling(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	if !strings.Contains(a.SystemPrompt(), "Capability Ceiling") {
		t.Error("system prompt should contain capability ceiling section")
	}
}

func TestSystemPrompt_ContainsPersona(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)
	if !strings.Contains(a.SystemPrompt(), "Persona") {
		t.Error("system prompt should contain persona section")
	}
}

func TestSystemPrompt_NonEmpty(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	if len(a.SystemPrompt()) < 500 {
		t.Errorf("system prompt suspiciously short (%d chars)", len(a.SystemPrompt()))
	}
}

// --- Culture Update --------------------------------------------------------

func TestCultureUpdate_ReplacesInstruction(t *testing.T) {
	a, dataDir := spawnTestAgent(t, models.RoleLead)

	// Write a fake COMPANY_IDENTITY.md with a unique value.
	ciPath := filepath.Join(dataDir, "COMPANY_IDENTITY.md")
	os.WriteFile(ciPath, []byte("# Company Identity\n\n## Vision\nBuild the future with AI.\n\n## Values\n- Honesty\n- Courage\n"), 0o644)

	oldPrompt := a.SystemPrompt()

	if err := a.CultureUpdate(ciPath, "", ""); err != nil {
		t.Fatalf("CultureUpdate: %v", err)
	}

	newPrompt := a.SystemPrompt()
	if newPrompt == oldPrompt {
		t.Error("CultureUpdate should produce a different system prompt")
	}
	if !strings.Contains(newPrompt, "Build the future with AI") {
		t.Error("new prompt should contain updated company identity")
	}

	// instruction.md on disk should be updated too.
	data, _ := os.ReadFile(a.Paths().InstructionPath)
	if !strings.Contains(string(data), "Build the future with AI") {
		t.Error("instruction.md on disk should reflect culture update")
	}
}

// --- Journaling ------------------------------------------------------------

func TestTeardown_WritesJournalFile(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)

	entry := agent.JournalEntry{
		Date:           time.Now(),
		Task:           "Create hello world",
		Outcome:        "success",
		Summary:        "Created main.go and ran it successfully.",
		Lessons:        "none",
		SkillsProposed: "none",
	}
	keepAlive, err := a.Teardown(entry)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if keepAlive != 0 {
		t.Errorf("expected keep_alive=0, got %d", keepAlive)
	}

	journals, err := agent.ListJournals(a.Paths())
	if err != nil {
		t.Fatalf("ListJournals: %v", err)
	}
	if len(journals) != 1 {
		t.Fatalf("expected 1 journal file, got %d", len(journals))
	}

	data, _ := os.ReadFile(journals[0])
	content := string(data)
	if !strings.Contains(content, "Create hello world") {
		t.Errorf("journal should contain task: %q", content)
	}
	if !strings.Contains(content, "success") {
		t.Errorf("journal should contain outcome: %q", content)
	}
}

func TestJournal_DateFormattedInFilename(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	fixed := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	a.Teardown(agent.JournalEntry{Date: fixed, Task: "test", Outcome: "success", Summary: "ok"})

	journals, _ := agent.ListJournals(a.Paths())
	if len(journals) == 0 {
		t.Fatal("no journal files found")
	}
	filename := filepath.Base(journals[0])
	if !strings.HasPrefix(filename, "2026-01-15") {
		t.Errorf("expected date-prefixed filename, got %q", filename)
	}
}

// --- Skills management -----------------------------------------------------

func TestParseSkills_ExtractsModel(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)
	skills, err := agent.ParseSkills(a.Paths())
	if err != nil {
		t.Fatalf("ParseSkills: %v", err)
	}
	if skills.Model != "llama3.1:8b" {
		t.Errorf("expected model llama3.1:8b, got %q", skills.Model)
	}
}

func TestParseSkills_ExtractsCapabilityCeiling(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleLead)
	skills, err := agent.ParseSkills(a.Paths())
	if err != nil {
		t.Fatalf("ParseSkills: %v", err)
	}
	if skills.CapabilityCeiling == "" {
		t.Error("capability ceiling should not be empty")
	}
}

func TestParseSkills_ExtractsCapabilities(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)
	skills, err := agent.ParseSkills(a.Paths())
	if err != nil {
		t.Fatalf("ParseSkills: %v", err)
	}
	if len(skills.Capabilities) == 0 {
		t.Error("expected at least one capability")
	}
}

func TestParseSkills_ExtractsLimitations(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)
	skills, err := agent.ParseSkills(a.Paths())
	if err != nil {
		t.Fatalf("ParseSkills: %v", err)
	}
	if len(skills.Limitations) == 0 {
		t.Error("expected at least one limitation")
	}
}

func TestProposeSkill_AppendsToFile(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)

	err := agent.ProposeSkill(a.Paths(), agent.SkillProposal{
		Name:        "gRPC integration",
		Evidence:    "Built a gRPC server for the auth service task",
		Description: "Design and implement gRPC services in Go, including proto definitions and client stubs",
	})
	if err != nil {
		t.Fatalf("ProposeSkill: %v", err)
	}

	data, err := os.ReadFile(a.Paths().ProposedSkillsPath)
	if err != nil {
		t.Fatalf("read proposed_skills.md: %v", err)
	}
	if !strings.Contains(string(data), "gRPC integration") {
		t.Errorf("proposed_skills.md should contain the proposal: %q", string(data))
	}
}

func TestPromoteSkill_MovesToSkills(t *testing.T) {
	a, _ := spawnTestAgent(t, models.RoleSpecialist)

	agent.ProposeSkill(a.Paths(), agent.SkillProposal{
		Name:        "Docker",
		Evidence:    "Containerised the API service",
		Description: "Build and manage Docker images for Go services",
	})

	if err := agent.PromoteSkill(a.Paths(), "Docker"); err != nil {
		t.Fatalf("PromoteSkill: %v", err)
	}

	// Should now be in skills.md
	skillsData, _ := os.ReadFile(a.Paths().SkillsPath)
	if !strings.Contains(string(skillsData), "Docker") {
		t.Error("skills.md should contain promoted skill")
	}

	// Should be removed from proposed_skills.md
	proposedData, _ := os.ReadFile(a.Paths().ProposedSkillsPath)
	if strings.Contains(string(proposedData), "## Proposal: Docker") {
		t.Error("proposed_skills.md should not contain the promoted proposal")
	}
}

// --- Role → Clearance mapping ----------------------------------------------

func TestRoleClearance_LeadIsLead(t *testing.T) {
	c := agent.RoleClearance(models.RoleLead)
	if c != models.ClearanceLead {
		t.Errorf("expected ClearanceLead, got %v", c)
	}
}

func TestRoleClearance_SpecialistIsSpecialist(t *testing.T) {
	c := agent.RoleClearance(models.RoleSpecialist)
	if c != models.ClearanceSpecialist {
		t.Errorf("expected ClearanceSpecialist, got %v", c)
	}
}

func TestRoleClearance_TrialIsTrial(t *testing.T) {
	c := agent.RoleClearance(models.RoleTrial)
	if c != models.ClearanceTrial {
		t.Errorf("expected ClearanceTrial, got %v", c)
	}
}

// --- Company Identity loader -----------------------------------------------

func TestLoadCompanyIdentity_ParsesVision(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "ci-*.md")
	f.WriteString("# Company Identity\n\n## Vision\nBuild AI that helps people.\n\n## Values\n- Integrity\n- Boldness\n")
	f.Close()

	ci := agent.LoadCompanyIdentity(f.Name())
	if !strings.Contains(ci.Vision, "Build AI") {
		t.Errorf("expected vision to contain 'Build AI': %q", ci.Vision)
	}
	if len(ci.Values) == 0 {
		t.Error("expected at least one value")
	}
}

func TestLoadCompanyIdentity_MissingFileReturnsEmpty(t *testing.T) {
	ci := agent.LoadCompanyIdentity("/nonexistent/path/COMPANY_IDENTITY.md")
	if ci.Raw != "" || ci.Vision != "" {
		t.Error("missing file should return empty CompanyIdentity")
	}
}
