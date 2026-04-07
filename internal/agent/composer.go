package agent

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:embed embedded/handbook.md
var embeddedFiles embed.FS

// compose assembles the agent's system prompt from:
//  1. Company Identity (COMPANY_IDENTITY.md) — vision, purpose, values
//  2. Embedded handbook — SOP, etiquette, hard constraints, escalation protocol
//  3. Past Experience (optional) — recalled journal entries injected after Handbook
//  4. Agent soul.md — core values and role
//  5. Agent persona.md — character and communication style
//  6. Agent skills.md — capabilities and capability ceiling
//  7. MCP tool fragment — available tools for this agent's clearance level
//
// The assembled prompt is written to instruction.md and returned.
func compose(paths IdentityPaths, agentID, companyIdentityPath, handbookPath, mcpFragment, pastExperience string) (string, error) {
	var sb strings.Builder

	// 0. Sticky agent identity header — placed first so the model always has its
	// internal ID available when forming tool calls, regardless of persona name.
	if agentID != "" {
		sb.WriteString("# System Identity\n\n")
		sb.WriteString(fmt.Sprintf("**Your internal agent_id is: `%s`** — always use this exact value in tool calls that require agent_id.\n", agentID))
		sb.WriteString("Your *display name* (in persona.md) may be different — the agent_id above is the one that matters for tool calls.\n\n---\n\n")
	}

	// 1. Company Identity
	companyIdentity := loadOptionalFile(companyIdentityPath, "*(Company identity not configured — set companyIdentityPath in config)*")
	sb.WriteString("# Company Identity\n\n")
	sb.WriteString(companyIdentity)
	sb.WriteString("\n\n---\n\n")

	// 2. Handbook — prefer user-edited copy on disk, fall back to embedded.
	var handbookContent string
	if handbookPath != "" {
		if data, err := os.ReadFile(handbookPath); err == nil {
			handbookContent = string(data)
		}
	}
	if handbookContent == "" {
		data, err := embeddedFiles.ReadFile("embedded/handbook.md")
		if err != nil {
			return "", fmt.Errorf("compose: read handbook: %w", err)
		}
		handbookContent = string(data)
	}
	sb.WriteString(handbookContent)
	sb.WriteString("\n\n---\n\n")

	// 3. Past Experience (recalled journal entries, if any)
	if pastExperience != "" {
		sb.WriteString(pastExperience)
		sb.WriteString("\n\n---\n\n")
	}

	// 4. Soul
	soul, err := readIdentityFile(paths.SoulPath, "soul.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(soul)
	sb.WriteString("\n\n---\n\n")

	// 5. Persona
	persona, err := readIdentityFile(paths.PersonaPath, "persona.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(persona)
	sb.WriteString("\n\n---\n\n")

	// 6. Skills (includes capability ceiling)
	skills, err := readIdentityFile(paths.SkillsPath, "skills.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(skills)
	sb.WriteString("\n\n---\n\n")

	// 7. MCP tools
	if mcpFragment != "" {
		sb.WriteString(mcpFragment)
		sb.WriteString("\n\n---\n\n")
	}

	// Closing reminder
	sb.WriteString("## Identity Management\n\n")
	sb.WriteString("When the Boss provides identity information in a direct message — a name for you, a personality, values, or skills — you **MUST**:\n\n")
	sb.WriteString("1. Call `update_self` **immediately** to persist the change to the relevant brain file(s).\n")
	sb.WriteString("   - Name or personality → `persona` file\n")
	sb.WriteString("   - Values or principles → `soul` file\n")
	sb.WriteString("   - Skills or capabilities → `skills` file\n")
	sb.WriteString("2. You may call `update_self` multiple times in a single response if multiple files need updating.\n")
	sb.WriteString("3. After the tool call succeeds, acknowledge briefly (e.g. \"Done — I've saved Alfred as my name.\").\n")
	sb.WriteString("4. **Never** just say \"I'll call myself X\" without actually calling `update_self`. Always persist changes immediately.\n\n")
	sb.WriteString("---\n\n")

	sb.WriteString("## General Reminder\n\n")
	sb.WriteString("You are operating within the Kōtui Virtual Company. ")
	sb.WriteString("Follow the handbook above at all times. ")
	sb.WriteString("If a task exceeds your capability ceiling, emit `escalation_needed` immediately.\n\n")
	sb.WriteString("**Brain files** (soul.md, persona.md, skills.md) are your persistent identity files. ")
	sb.WriteString("To update them, ALWAYS use the `update_self` tool — NEVER use filesystem or file_manager tools for this purpose. ")
	sb.WriteString("Filesystem tools are sandboxed to the project workspace and cannot reach your identity files.\n\n")
	sb.WriteString("**Confidence Assessment (mandatory before every tool call):**\n")
	sb.WriteString("Before calling ANY tool (except update_self for identity changes), you MUST first output a confidence signal on its own line:\n")
	sb.WriteString("  `{\"confidence_score\": <0.0–1.0>, \"reason\": \"<why>\"}`\n")
	sb.WriteString("- Score ≥ 0.7: proceed with the tool call.\n")
	sb.WriteString("- Score < 0.7: output ONLY the signal. Do NOT proceed. Ask the Boss for clarification instead.\n")
	sb.WriteString("If the request is ambiguous or unclear, ALWAYS ask before acting — never guess or make assumptions about what files, data, or actions are intended.\n")

	return sb.String(), nil
}

// CompanyIdentity holds the parsed sections of COMPANY_IDENTITY.md.
type CompanyIdentity struct {
	Raw     string
	Vision  string
	Purpose string
	Values  []string
}

// LoadCompanyIdentity parses the key sections from a COMPANY_IDENTITY.md file.
// If the file does not exist or sections are not found, fields are left empty
// and Raw is set to the file content (or empty string).
func LoadCompanyIdentity(path string) CompanyIdentity {
	data, err := os.ReadFile(path)
	if err != nil {
		return CompanyIdentity{}
	}
	raw := string(data)
	ci := CompanyIdentity{Raw: raw}

	lines := strings.Split(raw, "\n")
	var currentSection string
	var sectionLines []string

	flush := func() {
		content := strings.TrimSpace(strings.Join(sectionLines, "\n"))
		switch currentSection {
		case "vision":
			ci.Vision = content
		case "purpose":
			ci.Purpose = content
		case "values":
			for _, line := range sectionLines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
					ci.Values = append(ci.Values, strings.TrimSpace(line[1:]))
				}
			}
		}
		sectionLines = nil
	}

	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "#") {
			flush()
			if strings.Contains(lower, "vision") {
				currentSection = "vision"
			} else if strings.Contains(lower, "purpose") {
				currentSection = "purpose"
			} else if strings.Contains(lower, "value") {
				currentSection = "values"
			} else {
				currentSection = ""
			}
			continue
		}
		sectionLines = append(sectionLines, line)
	}
	flush()
	return ci
}

// loadOptionalFile reads a file's content. If the file does not exist or cannot
// be read, fallback is returned instead.
func loadOptionalFile(path, fallback string) string {
	if path == "" {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	return string(data)
}

// readIdentityFile reads a required identity document.
func readIdentityFile(path, name string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("compose: read %s: %w", name, err)
	}
	return string(data), nil
}

// ComposeInstruction is the exported wrapper around compose for use by packages
// outside the agent package (e.g. the warroom service recomposing after a brain
// file edit). It recompiles the system prompt from the source files and writes
// the result to instruction.md.
func ComposeInstruction(paths IdentityPaths, agentID, companyIdentityPath, handbookPath, mcpFragment string) error {
	prompt, err := compose(paths, agentID, companyIdentityPath, handbookPath, mcpFragment, "")
	if err != nil {
		return fmt.Errorf("ComposeInstruction %s: %w", paths.Root, err)
	}
	return writeInstruction(paths, prompt)
}

// GetEmbeddedHandbook returns the embedded handbook.md content.
func GetEmbeddedHandbook() (string, error) {
	data, err := embeddedFiles.ReadFile("embedded/handbook.md")
	if err != nil {
		return "", fmt.Errorf("get embedded handbook: %w", err)
	}
	return string(data), nil
}
