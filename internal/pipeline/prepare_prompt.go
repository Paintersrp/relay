package pipeline

import (
	"fmt"
	"strings"
)

func PreparePrompt(originalHandoff string) string {
	return BuildAgentPrompt(originalHandoff)
}

const relayValidationRemovedNote = "> Relay validation commands were extracted from the original handoff and removed from this Agent Prompt. Relay will run validation separately."

func BuildAgentPrompt(originalHandoff string) string {
	meta := ParseHandoffMetadata(originalHandoff, "")
	title := meta.Title
	if title == "" {
		title = "Implementation Handoff"
	}

	sectionsToStrip := []string{
		"execution model",
		"agent final output requirement",
		"agent final output",
		"agent final response",
		"final output",
		"output",
	}

	stripped := stripSections(originalHandoff, sectionsToStrip...)
	stripped = stripH1Title(stripped)
	stripped = cleanValidationExecutionMaterial(stripped)

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

// cleanValidationExecutionMaterial removes shell command fences,
// command-like lines, validation-wrapper labels and prose from
// test/validation sections while preserving test implementation
// instructions (prose, bullets, checklists, expected results).
//
// Sections cleaned:
//   - ## Tests / validation
//   - ## Tests
//   - ## Validation
//   - ## Relay validation commands
//   - ## Tests to add or update
func cleanValidationExecutionMaterial(markdown string) string {
	cleanupHeadings := map[string]bool{
		"tests / validation":        true,
		"tests":                     true,
		"validation":                true,
		"relay validation commands": true,
		"tests to add or update":    true,
	}

	lines := strings.Split(markdown, "\n")
	var result []string
	inCleanupSection := false
	inFence := false
	fenceIsShell := false
	sectionRemovedCount := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section heading transitions
		if !inFence && strings.HasPrefix(trimmed, "## ") {
			heading := strings.ToLower(strings.TrimSpace(trimmed[3:]))

			// Finalize previous section if we were in one
			if inCleanupSection && sectionRemovedCount > 0 {
				if !strings.HasSuffix(strings.TrimSpace(strings.Join(result, "\n")), relayValidationRemovedNote) {
					result = append(result, "", relayValidationRemovedNote)
				}
				sectionRemovedCount = 0
			}

			isCleanup := false
			for h := range cleanupHeadings {
				if heading == h || strings.HasPrefix(heading, h+" ") || strings.HasPrefix(heading, h+":") {
					isCleanup = true
					break
				}
			}
			inCleanupSection = isCleanup
			inFence = false
			fenceIsShell = false
			result = append(result, line)
			continue
		}

		// Non-heading line transitions out of cleanup section
		if inCleanupSection && !inFence && strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "## ") {
			if sectionRemovedCount > 0 {
				if !strings.HasSuffix(strings.TrimSpace(strings.Join(result, "\n")), relayValidationRemovedNote) {
					result = append(result, "", relayValidationRemovedNote)
				}
				sectionRemovedCount = 0
			}
			inCleanupSection = false
			result = append(result, line)
			continue
		}

		if !inCleanupSection {
			result = append(result, line)
			continue
		}

		// Handle fenced code blocks inside cleanup sections
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				lang, ok := isShellFenceOpener(line)
				if ok && lang != "" {
					fenceIsShell = true
				} else {
					fenceIsShell = false
				}
				inFence = true
				if fenceIsShell {
					sectionRemovedCount++
				} else {
					result = append(result, line)
				}
			} else {
				// Closing fence
				if fenceIsShell {
					// Don't emit the closing fence for shell blocks
				} else {
					result = append(result, line)
				}
				inFence = false
				fenceIsShell = false
			}
			continue
		}

		if inFence {
			if fenceIsShell {
				// Discard lines inside shell fences
				sectionRemovedCount++
				continue
			}
			result = append(result, line)
			continue
		}

		// Outside fences: check if line is a command-like line to remove
		if isValidationCommandLineForPromptCleanup(trimmed) {
			sectionRemovedCount++
			continue
		}

		// Remove validation wrapper prose unconditionally
		if isValidationWrapperLine(trimmed) {
			sectionRemovedCount++
			continue
		}

		// Check if line is an orphaned label that directly introduced command material
		if isOrphanedCommandLabel(trimmed) {
			// Peek ahead: if the next non-empty line is also command-like, skip this label
			if i+1 < len(lines) {
				nextTrimmed := strings.TrimSpace(lines[i+1])
				if nextTrimmed == "" || isValidationCommandLineForPromptCleanup(nextTrimmed) || strings.HasPrefix(nextTrimmed, "```") {
					sectionRemovedCount++
					continue
				}
			}
		}

		result = append(result, line)
	}

	// Finalize last section if needed
	if inCleanupSection && sectionRemovedCount > 0 {
		if !strings.HasSuffix(strings.TrimSpace(strings.Join(result, "\n")), relayValidationRemovedNote) {
			result = append(result, "", relayValidationRemovedNote)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// isValidationCommandLineForPromptCleanup returns true when line looks like a
// validation command that should be removed from the Agent Prompt.
func isValidationCommandLineForPromptCleanup(line string) bool {
	if line == "" || isCommentOrEmpty(line) {
		return false
	}
	if hasKnownCommandPrefix(line) {
		return true
	}
	return false
}

// isOrphanedCommandLabel returns true when line is a label that only introduces
// command material and would become orphaned after command removal.
func isOrphanedCommandLabel(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	orphanLabels := []string{
		"run:", "run :",
		"validation:", "validation :",
		"rtk preference:", "rtk preference :",
		"if rtk is available, prefer:", "if rtk is available, prefer :",
	}
	for _, label := range orphanLabels {
		if lower == label || strings.HasPrefix(lower, label) {
			return true
		}
	}
	return false
}

// isValidationWrapperLine returns true when a line is validation-specific
// wrapper prose that should be removed from the Agent Prompt. These are
// lines that refer to RTK preference, command execution for Relay, or
// other validation infrastructure not useful to the acting agent.
func isValidationWrapperLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	// Remove lines that are exact labels for validation command material
	wrapperTexts := []string{
		"validation commands:",
		"validation commands :",
		"run:",
		"run :",
		"rtk preference:",
		"rtk preference :",
	}
	for _, w := range wrapperTexts {
		if lower == w {
			return true
		}
	}
	// Remove lines about RTK preference / RTK-wrapped commands
	if strings.Contains(lower, "rtk") && (strings.Contains(lower, "prefer") || strings.Contains(lower, "available") || strings.Contains(lower, "wrapped") || strings.Contains(lower, "wrappers")) {
		return true
	}
	// Remove lines that explicitly refer to commands being for Relay, not agent execution
	if strings.Contains(lower, "relay") && strings.Contains(lower, "command") && strings.Contains(lower, "agent") {
		return true
	}
	// Remove lines about not listing RTK-wrapped commands separately
	if strings.Contains(lower, "do not list") && strings.Contains(lower, "rtk") {
		return true
	}
	return false
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
