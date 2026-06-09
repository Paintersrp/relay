package pipeline

import (
	"fmt"
	"strings"
)

func PreparePrompt(originalHandoff string) string {
	return BuildAgentPrompt(originalHandoff)
}

func BuildAgentPrompt(originalHandoff string) string {
	meta := ParseHandoffMetadata(originalHandoff, "")
	title := meta.Title
	if title == "" {
		title = "Implementation Handoff"
	}

	sectionsToStrip := []string{
		"execution model",
		"tests / validation",
		"tests",
		"validation",
		"agent final output requirement",
		"agent final output",
		"agent final response",
		"final output",
		"output",
	}

	stripped := stripSections(originalHandoff, sectionsToStrip...)
	stripped = stripH1Title(stripped)

	commands := ExtractValidationCommands(originalHandoff, "")
	var cmdList strings.Builder
	for _, c := range commands {
		cmdList.WriteString("- `")
		cmdList.WriteString(c.Command)
		cmdList.WriteString("`\n")
	}
	validationPlan := cmdList.String()

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" Agent Execution Prompt\n\n")
	b.WriteString("You are working from a Relay implementation handoff.\n\n")
	b.WriteString("Follow the implementation instructions exactly. Do not perform unrelated cleanup.\n\n")
	b.WriteString("## Implementation handoff\n\n")
	b.WriteString(stripped)
	if !strings.HasSuffix(stripped, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n## Validation responsibility\n\n")
	b.WriteString("Relay will run validation after your implementation.\n\n")
	b.WriteString("Do not run validation commands unless explicitly instructed by the user or unless a command is required to generate source files before editing can continue.\n\n")
	b.WriteString("Do not paste full validation logs.\n\n")
	b.WriteString("If you run any checks yourself, summarize only:\n\n")
	b.WriteString("- command run\n")
	b.WriteString("- pass/fail\n")
	b.WriteString("- blocker if failed\n\n")
	b.WriteString("Relay owns the final validation result.\n\n")

	if validationPlan != "" {
		b.WriteString("## Relay validation plan\n\n")
		b.WriteString("Relay extracted validation commands from the original handoff and will run them after implementation.\n\n")
		b.WriteString("You do not need to run these commands.\n\n")
	}

	b.WriteString("## Agent final output requirement\n\n")
	b.WriteString("Return only:\n\n")
	b.WriteString("- DONE or BLOCKED\n")
	b.WriteString("- build status\n")
	b.WriteString("- test status\n")
	b.WriteString("- count of LOC changed\n")
	b.WriteString("- blocker/error only if BLOCKED\n")

	return b.String()
}

func stripH1Title(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

func stripSections(markdown string, headings ...string) string {
	lowerHeadings := make([]string, len(headings))
	for i, h := range headings {
		lowerHeadings[i] = strings.ToLower(strings.TrimSpace(h))
	}

	lines := strings.Split(markdown, "\n")
	var result []string
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimSpace(trimmed[3:]))
			shouldSkip := false
			for _, h := range lowerHeadings {
				if heading == h || strings.HasPrefix(heading, h+" ") || strings.HasPrefix(heading, h+":") {
					shouldSkip = true
					break
				}
			}
			if shouldSkip {
				skipping = true
				continue
			}
			skipping = false
		}
		if !skipping {
			result = append(result, line)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

func ArtifactFilename(kind string) string {
	switch kind {
	case "original_handoff":
		return "original_handoff.txt"
	case "handoff_validation_json":
		return "handoff_validation.json"
	case "ready_prompt":
		return "ready_prompt.txt"
	case "agent_prompt":
		return "agent_prompt.txt"
	case "audit_packet":
		return "audit_packet.json"
	case "opencode_handoff_packet":
		return "opencode_handoff_packet.json"
	case "agent_result_raw":
		return "agent_result.txt"
	case "agent_result_json":
		return "agent_result.json"
	case "validation_run_json":
		return "validation_run.json"
	case "validation_stdout":
		return "validation_stdout.txt"
	case "validation_stderr":
		return "validation_stderr.txt"
	default:
		return fmt.Sprintf("%s.txt", kind)
	}
}
