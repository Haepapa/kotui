package agent

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Skills holds the parsed capability data from an agent's skills.md.
type Skills struct {
	Model            string
	CapabilityCeiling string   // natural-language description of task types
	Capabilities     []string // bullet list of known capabilities
	Limitations      []string // bullet list of known limitations
	ApprovedSkills   []string // skills approved after Boss review
}

// ParseSkills reads and parses an agent's skills.md file.
// It extracts the model, capability ceiling, capabilities, limitations,
// and approved skills sections.
func ParseSkills(paths IdentityPaths) (*Skills, error) {
	data, err := os.ReadFile(paths.SkillsPath)
	if err != nil {
		return nil, fmt.Errorf("parse skills: read skills.md: %w", err)
	}
	return parseSkillsContent(string(data)), nil
}

func parseSkillsContent(content string) *Skills {
	s := &Skills{}
	lines := strings.Split(content, "\n")

	type section int
	const (
		secNone section = iota
		secModel
		secCeiling
		secCapabilities
		secLimitations
		secApproved
	)
	current := secNone
	var ceilingLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section headers.
		if strings.HasPrefix(trimmed, "## ") {
			header := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			switch {
			case strings.Contains(header, "model"):
				current = secModel
			case strings.Contains(header, "capability ceiling"):
				current = secCeiling
				ceilingLines = nil
			case strings.Contains(header, "known capabilities") || header == "capabilities":
				current = secCapabilities
			case strings.Contains(header, "known limitations") || strings.Contains(header, "limitation"):
				current = secLimitations
			case strings.Contains(header, "approved skill"):
				current = secApproved
			default:
				current = secNone
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch current {
		case secModel:
			if s.Model == "" {
				s.Model = trimmed
			}
		case secCeiling:
			ceilingLines = append(ceilingLines, trimmed)
			s.CapabilityCeiling = strings.Join(ceilingLines, " ")
		case secCapabilities:
			if bullet := parseBullet(trimmed); bullet != "" {
				s.Capabilities = append(s.Capabilities, bullet)
			}
		case secLimitations:
			if bullet := parseBullet(trimmed); bullet != "" {
				s.Limitations = append(s.Limitations, bullet)
			}
		case secApproved:
			if bullet := parseBullet(trimmed); bullet != "" {
				s.ApprovedSkills = append(s.ApprovedSkills, bullet)
			}
		}
	}
	return s
}

func parseBullet(line string) string {
	for _, prefix := range []string{"- ", "* ", "• "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// SkillProposal holds a single proposed skill.
type SkillProposal struct {
	Name        string
	Date        string
	Evidence    string
	Description string
}

// ProposeSkill appends a skill proposal to the agent's proposed_skills.md.
// The proposal requires Boss approval before it is merged into skills.md.
func ProposeSkill(paths IdentityPaths, proposal SkillProposal) error {
	if proposal.Name == "" {
		return fmt.Errorf("skills: proposal name must not be empty")
	}
	if proposal.Date == "" {
		proposal.Date = time.Now().UTC().Format("2006-01-02")
	}

	block := fmt.Sprintf("\n## Proposal: %s\nDate: %s\nEvidence: %s\nDescription: %s\n",
		proposal.Name, proposal.Date, proposal.Evidence, proposal.Description)

	f, err := os.OpenFile(paths.ProposedSkillsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("skills: open proposed_skills.md: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return err
}

// PromoteSkill merges a named proposal into skills.md and removes it from
// proposed_skills.md. Called by the orchestrator after Boss approval.
func PromoteSkill(paths IdentityPaths, skillName string) error {
	// Read proposed_skills.md
	proposed, err := os.ReadFile(paths.ProposedSkillsPath)
	if err != nil {
		return fmt.Errorf("skills: read proposed_skills.md: %w", err)
	}

	// Find and extract the proposal block.
	header := "## Proposal: " + skillName
	content := string(proposed)
	idx := strings.Index(content, header)
	if idx == -1 {
		return fmt.Errorf("skills: proposal %q not found in proposed_skills.md", skillName)
	}

	// Find the end of this proposal (next ## heading or EOF).
	rest := content[idx+len(header):]
	nextHeader := strings.Index(rest, "\n## ")
	var proposalBody string
	if nextHeader == -1 {
		proposalBody = rest
	} else {
		proposalBody = rest[:nextHeader]
	}

	// Extract description from the proposal block.
	description := ""
	for _, line := range strings.Split(proposalBody, "\n") {
		if strings.HasPrefix(line, "Description: ") {
			description = strings.TrimPrefix(line, "Description: ")
		}
	}

	// Append to skills.md.
	skillsData, err := os.ReadFile(paths.SkillsPath)
	if err != nil {
		return fmt.Errorf("skills: read skills.md: %w", err)
	}
	skillsContent := string(skillsData)

	// Replace the placeholder line if present.
	placeholder := "_(none yet — skills are added after Boss approval of proposals)_"
	if strings.Contains(skillsContent, placeholder) {
		skillsContent = strings.Replace(skillsContent, placeholder, "", 1)
	}
	skillsContent += fmt.Sprintf("- **%s**: %s\n", skillName, description)
	if err := os.WriteFile(paths.SkillsPath, []byte(skillsContent), 0o644); err != nil {
		return fmt.Errorf("skills: write skills.md: %w", err)
	}

	// Remove the proposal from proposed_skills.md.
	var newProposed string
	if nextHeader == -1 {
		newProposed = content[:idx]
	} else {
		newProposed = content[:idx] + content[idx+len(header)+nextHeader:]
	}
	newProposed = strings.TrimRight(newProposed, "\n") + "\n"
	return os.WriteFile(paths.ProposedSkillsPath, []byte(newProposed), 0o644)
}
