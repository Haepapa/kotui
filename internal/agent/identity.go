package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// IdentityPaths holds the complete filesystem layout for one agent.
type IdentityPaths struct {
	Root               string // /data/agents/{id}
	IdentityDir        string // /data/agents/{id}/identity
	SoulPath           string // soul.md    — core values (rewritten on Culture Update)
	PersonaPath        string // persona.md — character and communication style
	SkillsPath         string // skills.md  — capabilities + capability ceiling
	InstructionPath    string // instruction.md — compiled system prompt (output of composer)
	JournalDir         string // /data/agents/{id}/journal
	ProposedSkillsPath string // proposed_skills.md
}

// agentPaths constructs all paths for the given agent under dataDir.
func agentPaths(dataDir, agentID string) IdentityPaths {
	root := filepath.Join(dataDir, "agents", agentID)
	identityDir := filepath.Join(root, "identity")
	return IdentityPaths{
		Root:               root,
		IdentityDir:        identityDir,
		SoulPath:           filepath.Join(identityDir, "soul.md"),
		PersonaPath:        filepath.Join(identityDir, "persona.md"),
		SkillsPath:         filepath.Join(identityDir, "skills.md"),
		InstructionPath:    filepath.Join(identityDir, "instruction.md"),
		JournalDir:         filepath.Join(root, "journal"),
		ProposedSkillsPath: filepath.Join(root, "proposed_skills.md"),
	}
}

// initIdentity creates the agent's directory structure and default identity
// documents if they do not already exist. If the agent already has an
// identity directory, the call is a no-op.
func initIdentity(paths IdentityPaths, id, name string, role models.AgentRole, model string) error {
	for _, dir := range []string{paths.IdentityDir, paths.JournalDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Write default files only if they don't exist.
	if err := writeIfAbsent(paths.SoulPath, defaultSoul(name, role)); err != nil {
		return err
	}
	if err := writeIfAbsent(paths.PersonaPath, defaultPersona(name, role)); err != nil {
		return err
	}
	if err := writeIfAbsent(paths.SkillsPath, defaultSkills(name, model, role)); err != nil {
		return err
	}
	if err := writeIfAbsent(paths.ProposedSkillsPath, "# Proposed Skills\n\n_(none yet)_\n"); err != nil {
		return err
	}
	return nil
}

// writeIfAbsent writes content to path only if the file does not already exist.
func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// AgentPaths is the exported wrapper around agentPaths for use by packages
// outside the agent package (e.g. warroom service, tools).
func AgentPaths(dataDir, agentID string) IdentityPaths {
	return agentPaths(dataDir, agentID)
}

// writeInstruction writes the compiled system prompt to instruction.md.
// Called by Spawn() and CultureUpdate() — always overwrites.
func writeInstruction(paths IdentityPaths, content string) error {
	return os.WriteFile(paths.InstructionPath, []byte(content), 0o644)
}

// ReadInstruction reads the current compiled system prompt.
func ReadInstruction(paths IdentityPaths) (string, error) {
	data, err := os.ReadFile(paths.InstructionPath)
	if err != nil {
		return "", fmt.Errorf("read instruction.md: %w", err)
	}
	return string(data), nil
}

// ReadAgentName extracts the agent's display name from persona.md.
// It looks for a "## Name" section and returns the first non-empty line after it.
// Falls back to agentID if the file cannot be read or the section is absent.
func ReadAgentName(dataDir, agentID string) string {
	paths := agentPaths(dataDir, agentID)
	data, err := os.ReadFile(paths.PersonaPath)
	if err != nil {
		return agentID
	}
	lines := strings.Split(string(data), "\n")
	inNameSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Name") {
			inNameSection = true
			continue
		}
		if inNameSection {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "#") {
				break // next section started
			}
			return trimmed
		}
	}
	return agentID
}

// --- Default document generators -----------------------------------------

func defaultSoul(name string, role models.AgentRole) string {
	return fmt.Sprintf(`# Soul — %s

## Core Values
*(Populated from COMPANY_IDENTITY.md by the System Prompt Composer on first spawn.
These values will be replaced on every Culture Update.)*

## Role
%s

## Created
%s
`, name, strings.ToUpper(string(role)), time.Now().UTC().Format("2006-01-02"))
}

func defaultPersona(name string, role models.AgentRole) string {
	var style string
	switch role {
	case models.RoleLead:
		style = "Analytical, structured, and decisive. Communicates plans clearly and delegates effectively. " +
			"Keeps Group Chat concise and milestone-focused."
	case models.RoleSpecialist:
		style = "Precise, detail-oriented, and thorough. Focuses on execution quality. " +
			"Reports outcomes, not process."
	default:
		style = "Curious and observant. Asks clarifying questions before acting. " +
			"Does not use any tools without explicit direction."
	}
	return fmt.Sprintf(`# Persona — %s

## Communication Style
%s

## Name
%s

## Role
%s
`, name, style, name, strings.ToUpper(string(role)))
}

func defaultSkills(name, model string, role models.AgentRole) string {
	var capabilities, limits, ceiling string
	switch role {
	case models.RoleLead:
		capabilities = "- Task decomposition and planning\n- Code architecture and system design\n- Delegation and result verification\n- Multi-file reasoning"
		limits = "- Real-time data analysis or live API calls\n- Multi-step mathematical proofs\n- Tasks requiring specialised domain knowledge (medical, legal, financial)"
		ceiling = "Complex multi-file software architecture, orchestration, and planning. " +
			"Signal escalation_needed for highly specialised domains or mathematical proofs."
	case models.RoleSpecialist:
		capabilities = "- Code generation and refactoring\n- Writing and running shell commands\n- File system manipulation\n- Reading and interpreting documentation"
		limits = "- High-level architecture design (defer to Lead)\n- Tasks requiring simultaneous access to many large files"
		ceiling = "Focused code generation, file manipulation, and command execution. " +
			"Defer architecture decisions to the Lead agent."
	default:
		capabilities = "- Reading and discussing code\n- Answering questions about the codebase\n- Basic code comprehension"
		limits = "- Cannot execute code or write to files (Trial clearance)\n- Cannot modify system state"
		ceiling = "Read-only code review and discussion. Cannot take any action that modifies state."
	}

	return fmt.Sprintf(`# Skills — %s

## Model
%s

## Capability Ceiling
%s

## Known Capabilities
%s

## Known Limitations (signal escalation_needed for these)
%s

## Approved Skills
_(none yet — skills are added after Boss approval of proposals)_
`, name, model, ceiling, capabilities, limits)
}
