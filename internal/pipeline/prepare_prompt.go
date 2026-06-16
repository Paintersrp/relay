package pipeline

import (
	"fmt"
	"strings"
)

// AgentPromptMode controls whether the prompt is full (orchestration) or compact (execution).
type AgentPromptMode string

const (
	AgentPromptModeFull    AgentPromptMode = "full"
	AgentPromptModeCompact AgentPromptMode = "compact"
)

func PreparePrompt(originalHandoff string) string {
	return BuildAgentPrompt(originalHandoff)
}

const relayValidationRemovedNote = "> Relay validation commands were extracted from the original handoff and removed from this Agent Prompt. Relay will run validation separately."

// fenceState tracks whether we are inside a fenced code block.
// For md/markdown fences, it tracks nesting depth to allow
// inner fences (```bash, ```go) without closing the outer md fence.
type fenceState struct {
	inFence bool
	marker  byte
	length  int
	lang    string
	// depth tracks nesting for md/markdown fences only.
	// depth=1 means inside the md fence, depth=2 means inside a nested fence.
	depth int
}

// isFenceLine checks if a trimmed line is a fence opener/closer.
// Returns the fence character, length, language (if opening), and whether it's a fence.
func isFenceLine(trimmed string) (char byte, length int, lang string, ok bool) {
	if len(trimmed) < 3 {
		return 0, 0, "", false
	}
	first := trimmed[0]
	if first != '`' && first != '~' {
		return 0, 0, "", false
	}
	// Count consecutive same characters
	count := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == first {
			count++
		} else {
			break
		}
	}
	if count < 3 {
		return 0, 0, "", false
	}
	rest := strings.TrimSpace(trimmed[count:])
	return first, count, rest, true
}

// updateFenceState updates fence state given a line.
func updateFenceState(line string, state fenceState) fenceState {
	trimmed := strings.TrimSpace(line)
	char, length, lang, ok := isFenceLine(trimmed)
	if !ok {
		return state
	}

	if !state.inFence {
		// Opening a new fence
		return fenceState{
			inFence: true,
			marker:  char,
			length:  length,
			lang:    lang,
			depth:   1,
		}
	}

	// Inside a fence; only same marker char can close it
	if char != state.marker {
		return state
	}
	if length < state.length {
		return state
	}

	// Handle md/markdown fence nesting: inner language fences should not
	// close the outer md fence.
	if state.lang == "md" || state.lang == "markdown" {
		rest := strings.TrimSpace(trimmed[length:])
		if state.depth == 1 {
			if rest != "" {
				// ```bash or ```go inside md fence -> open nested fence
				return fenceState{
					inFence: true,
					marker:  state.marker,
					length:  state.length,
					lang:    state.lang,
					depth:   2,
				}
			}
			// Plain ``` closes the md fence
			return fenceState{}
		} else {
			// depth == 2, inside a nested fence
			if rest != "" {
				// Another non-plain fence inside nested fence? Stay at depth 2.
				return state
			}
			// Plain ``` closes nested fence, back to md fence
			return fenceState{
				inFence: true,
				marker:  state.marker,
				length:  state.length,
				lang:    state.lang,
				depth:   1,
			}
		}
	}

	// Normal fence closing
	return fenceState{}
}

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
	writeValidationGuidance(&b)

	if validationPlan != "" {
		b.WriteString("## Relay validation plan\n\n")
		b.WriteString("Relay extracted validation commands from the original handoff and will run them after implementation. Use them as context for what Relay will verify.\n\n")
	}

	writeAgentFinalOutputContract(&b)

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
//
// Only headings OUTSIDE fenced blocks activate cleanup mode.
// Only shell fences inside cleanup sections are removed.
// Non-shell fences (md, go, etc.) are preserved entirely.
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
	fs := fenceState{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track overall fence state (independent from shell-fence tracking)
		fs = updateFenceState(line, fs)

		// Detect section heading transitions (only outside fenced blocks)
		if !fs.inFence && strings.HasPrefix(trimmed, "## ") {
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

		// Non-heading line transitions out of cleanup section (only outside fences)
		if inCleanupSection && !fs.inFence && strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "## ") {
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

		// Inside a cleanup section: handle fenced code blocks
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence {
				// Opening fence - determine if it's a shell fence
				_, _, lang, ok := isFenceLine(trimmed)
				if ok {
					fenceIsShell = isShellLang(lang)
					inFence = true
					if fenceIsShell {
						// Skip shell fence opener
						sectionRemovedCount++
					} else {
						// Preserve non-shell fence opener
						result = append(result, line)
					}
				} else {
					// Not a valid fence line, treat as content
					result = append(result, line)
				}
			} else {
				// Closing fence
				if fenceIsShell {
					// Don't emit closing fence for shell blocks
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
			// Preserve lines inside non-shell fences
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

// isShellLang returns true if the language identifier is a shell language.
func isShellLang(lang string) bool {
	shellLangs := map[string]bool{
		"sh": true, "shell": true, "bash": true, "zsh": true,
		"fish": true, "powershell": true, "pwsh": true, "ps1": true,
		"cmd": true, "bat": true, "console": true, "terminal": true,
	}
	return shellLangs[strings.ToLower(lang)]
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
	fs := fenceState{}
	for _, line := range lines {
		fs = updateFenceState(line, fs)
		if !fs.inFence && strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// stripSections removes sections whose headings match the given list.
// It is fence-aware: headings inside fenced blocks are not treated as real headings.
func stripSections(markdown string, headings ...string) string {
	lowerHeadings := make([]string, len(headings))
	for i, h := range headings {
		lowerHeadings[i] = strings.ToLower(strings.TrimSpace(h))
	}

	lines := strings.Split(markdown, "\n")
	var result []string
	skipping := false
	fs := fenceState{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fence state
		fs = updateFenceState(line, fs)

		// Only recognize headings outside fenced blocks
		if !fs.inFence && strings.HasPrefix(trimmed, "## ") {
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
	case "audit_handoff":
		return "audit_handoff.md"
	case "opencode_handoff_packet":
		return "opencode_handoff_packet.json"
	case "agent_result_raw":
		return "agent_result.txt"
	case "agent_result_json":
		return "agent_result.json"
	case "opencode_lifecycle_diagnostic_json":
		return "opencode_lifecycle_diagnostic.json"
	case "validation_run_json":
		return "validation_run.json"
	case "validation_progress_json":
		return "validation_progress.json"
	case "validation_stdout":
		return "validation_stdout.txt"
	case "validation_stderr":
		return "validation_stderr.txt"
	case "opencode_dry_run_json":
		return "opencode_dry_run.json"
	case "opencode_cli_check_json":
		return "opencode_cli_check.json"
	case "intake_remediation_handoff":
		return "intake-remediation-handoff.md"
	case "git_status_text":
		return "git_status.txt"
	case "git_diff_stat":
		return "git_diff_stat.txt"
	case "git_diff_numstat":
		return "git_diff_numstat.txt"
	case "git_diff_patch":
		return "git_diff.patch"
	case "git_diff_name_status":
		return "git_diff_name_status.txt"
	case "commit_message_text":
		return "commit-message.txt"
	case "commit_suggestion_json":
		return "commit-suggestion.json"
	case "git_baseline_json":
		return "git_baseline.json"
	case "git_change_evidence_json":
		return "git_change_evidence.json"
	case "audit_clearance_json":
		return "audit-clearance.json"
	case "git_commit_state_json":
		return "git-commit-state.json"
	case "git_commit_result_json":
		return "git-commit-result.json"
	case "git_push_dry_run_json":
		return "git-push-dry-run.json"
	case "git_push_result_json":
		return "git-push-result.json"
	case "executor_stdout":
		return "executor_stdout.txt"
	case "executor_stderr":
		return "executor_stderr.txt"
	case "command_log":
		return "command_log.txt"
	case "executor_result":
		return "executor_result.txt"
	default:
		return fmt.Sprintf("%s.txt", kind)
	}
}

// BuildAgentPromptWithMode generates an Agent Prompt using the given mode.
// Full mode preserves the current full prompt behavior.
// Compact mode produces a shorter execution-focused prompt without orchestration text.
func BuildAgentPromptWithMode(originalHandoff string, mode AgentPromptMode) string {
	if mode == AgentPromptModeCompact {
		return BuildCompactAgentPrompt(originalHandoff)
	}
	return BuildAgentPrompt(originalHandoff)
}

// BuildCompactAgentPrompt generates a compact execution prompt for the repo agent.
// It preserves Goal, Scope, Do not change, Task checklist, Direct files, Context files,
// Current behavior, Tests, and Surgical implementation details.
// It removes Execution model, Relay validation commands, RTK preference, and
// appends a clean final output contract.
func BuildCompactAgentPrompt(originalHandoff string) string {
	meta := ParseHandoffMetadata(originalHandoff, "")
	title := meta.Title
	if title == "" {
		title = "Implementation Handoff"
	}

	// Sections to strip entirely
	sectionsToStrip := []string{
		"execution model",
		"relay validation commands",
		"agent final output requirement",
		"agent final output",
		"agent final response",
		"final output",
		"output",
	}

	handoff := originalHandoff
	handoff = stripH1Title(handoff)
	handoff = stripSections(handoff, sectionsToStrip...)
	handoff = cleanValidationExecutionMaterial(handoff)
	handoff = stripRTKPreference(handoff)

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" Agent Execution Prompt\n\n")
	b.WriteString("You are working from a Relay implementation handoff.\n\n")
	b.WriteString(handoff)
	if !strings.HasSuffix(handoff, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	writeValidationGuidance(&b)
	writeAgentFinalOutputContract(&b)

	return b.String()
}

func writeValidationGuidance(b *strings.Builder) {
	b.WriteString("## Validation responsibility\n\n")
	b.WriteString("Run relevant tests/checks during implementation when practical. If a command passes, report concise PASS status only. If it fails, inspect the shortest useful failure output, fix the issue, and re-run the failing command. Do not paste full passing logs into the final response.\n\n")
	b.WriteString("Relay will run the extracted validation commands after you finish. Relay validation is the authoritative final gate, but you should still use tests/checks as implementation feedback.\n\n")
	b.WriteString("If you run any checks yourself, summarize only:\n\n")
	b.WriteString("- command run\n")
	b.WriteString("- pass/fail\n")
	b.WriteString("- blocker if failed\n\n")
	b.WriteString("Relay owns the final validation result.\n\n")
}

func writeAgentFinalOutputContract(b *strings.Builder) {
	b.WriteString("## Agent final output requirement\n\n")
	b.WriteString("Return only:\n\n")
	b.WriteString("- DONE or BLOCKED\n")
	b.WriteString("- build status\n")
	b.WriteString("- test status\n")
	b.WriteString("- count of LOC changed\n")
	b.WriteString("- blocker/error only if BLOCKED\n")
}

// stripRTKPreference removes lines about RTK preference from the text.
func stripRTKPreference(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "rtk") && (strings.Contains(lower, "prefer") || strings.Contains(lower, "available") || strings.Contains(lower, "wrapped") || strings.Contains(lower, "wrappers")) {
			continue
		}
		if strings.Contains(lower, "do not list") && strings.Contains(lower, "rtk") {
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// PromptEstimate holds byte count and an approximate token count for a prompt.
type PromptEstimate struct {
	Bytes        int
	ApproxTokens int
}

// EstimateTokens returns a PromptEstimate from a text.
// The token estimate is approximate and uses the heuristic: runes / 4.
func EstimateTokens(text string) PromptEstimate {
	runes := len([]rune(text))
	return PromptEstimate{
		Bytes:        len(text),
		ApproxTokens: (runes + 3) / 4,
	}
}
