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
//  3. Agent soul.md — core values and role
//  4. Agent persona.md — character and communication style
//  5. Agent skills.md — capabilities and capability ceiling
//  6. MCP tool fragment — available tools for this agent's clearance level
//
// The assembled prompt is written to instruction.md and returned.
func compose(paths IdentityPaths, companyIdentityPath, mcpFragment string) (string, error) {
	var sb strings.Builder

	// 1. Company Identity
	companyIdentity := loadOptionalFile(companyIdentityPath, "*(Company identity not configured — set companyIdentityPath in config)*")
	sb.WriteString("# Company Identity\n\n")
	sb.WriteString(companyIdentity)
	sb.WriteString("\n\n---\n\n")

	// 2. Handbook (embedded)
	handbook, err := embeddedFiles.ReadFile("embedded/handbook.md")
	if err != nil {
		return "", fmt.Errorf("compose: read handbook: %w", err)
	}
	sb.WriteString(string(handbook))
	sb.WriteString("\n\n---\n\n")

	// 3. Soul
	soul, err := readIdentityFile(paths.SoulPath, "soul.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(soul)
	sb.WriteString("\n\n---\n\n")

	// 4. Persona
	persona, err := readIdentityFile(paths.PersonaPath, "persona.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(persona)
	sb.WriteString("\n\n---\n\n")

	// 5. Skills (includes capability ceiling)
	skills, err := readIdentityFile(paths.SkillsPath, "skills.md")
	if err != nil {
		return "", err
	}
	sb.WriteString(skills)
	sb.WriteString("\n\n---\n\n")

	// 6. MCP tools
	if mcpFragment != "" {
		sb.WriteString(mcpFragment)
		sb.WriteString("\n\n---\n\n")
	}

	// Closing reminder
	sb.WriteString("## Reminder\n\n")
	sb.WriteString("You are operating within the Kotui Virtual Company. ")
	sb.WriteString("Follow the handbook above at all times. ")
	sb.WriteString("If a task exceeds your capability ceiling, emit `escalation_needed` immediately.\n")

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
