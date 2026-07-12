package executor

import (
	"fmt"
	"strings"

	"relay/internal/speccompiler"
)

type workflowValidationStatus string

const (
	workflowValidationPassed workflowValidationStatus = "passed"
	workflowValidationFailed workflowValidationStatus = "failed"
	workflowValidationNotRun workflowValidationStatus = "not_run"

	maxWorkflowValidationConciseRunes = 1024
)

type workflowValidationResult struct {
	Command          string                   `json:"command"`
	WorkingDirectory string                   `json:"working_directory,omitempty"`
	Expected         string                   `json:"expected"`
	Status           workflowValidationStatus `json:"status"`
	ConciseResult    string                   `json:"concise_result"`
}

type workflowValidationParseResult struct {
	Results     []workflowValidationResult
	Diagnostics []string
}

func parseWorkflowValidationReport(raw string, commands []speccompiler.ProjectedValidationCommand) workflowValidationParseResult {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"), "\n")
	sectionStart := -1
	for index, line := range lines {
		if line == "## Validation" {
			sectionStart = index + 1
		}
	}
	if sectionStart < 0 {
		return workflowValidationParseResult{
			Diagnostics: []string{"final Validation section is missing"},
		}
	}
	sectionEnd := len(lines)
	for index := sectionStart; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			sectionEnd = index
			break
		}
	}

	canonical := make(map[string]speccompiler.ProjectedValidationCommand, len(commands))
	for _, command := range commands {
		canonical[command.Command] = command
	}
	matched := make(map[string]workflowValidationResult, len(commands))
	invalid := make(map[string]bool, len(commands))
	result := workflowValidationParseResult{}
	for _, line := range lines[sectionStart:sectionEnd] {
		command, status, concise, accepted, commandLine := parseWorkflowValidationLine(line)
		if !commandLine {
			continue
		}
		projected, known := canonical[command]
		if !known {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("reported validation command is not canonical: %q", command))
			continue
		}
		if invalid[command] {
			continue
		}
		if _, duplicate := matched[command]; duplicate {
			delete(matched, command)
			invalid[command] = true
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("canonical validation command is reported more than once: %q", command))
			continue
		}
		if !accepted {
			invalid[command] = true
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("canonical validation command has a malformed result: %q", command))
			continue
		}
		matched[command] = workflowValidationResult{
			Command:          projected.Command,
			WorkingDirectory: projected.WorkingDirectory,
			Expected:         projectedWorkflowValidationExpected(projected),
			Status:           status,
			ConciseResult:    boundedWorkflowValidationConcise(concise),
		}
	}

	for _, command := range commands {
		if value, ok := matched[command.Command]; ok && !invalid[command.Command] {
			result.Results = append(result.Results, value)
		}
	}
	return result
}

func parseWorkflowValidationLine(line string) (string, workflowValidationStatus, string, bool, bool) {
	if !strings.HasPrefix(line, "- `") {
		return "", "", "", false, false
	}
	remainder := strings.TrimPrefix(line, "- `")
	closing := strings.IndexByte(remainder, '`')
	if closing < 0 {
		return "", "", "", false, true
	}
	command := remainder[:closing]
	tail := remainder[closing+1:]
	if !strings.HasPrefix(tail, " - ") {
		return command, "", "", false, true
	}
	tail = strings.TrimPrefix(tail, " - ")
	switch {
	case tail == "passed":
		return command, workflowValidationPassed, "Validation command passed.", true, true
	case strings.HasPrefix(tail, "failed: "):
		reason := strings.TrimSpace(strings.TrimPrefix(tail, "failed: "))
		if reason == "" {
			return command, "", "", false, true
		}
		return command, workflowValidationFailed, reason, true, true
	case strings.HasPrefix(tail, "not run: "):
		reason := strings.TrimSpace(strings.TrimPrefix(tail, "not run: "))
		if reason == "" {
			return command, "", "", false, true
		}
		return command, workflowValidationNotRun, reason, true, true
	default:
		return command, "", "", false, true
	}
}

func projectedWorkflowValidationExpected(command speccompiler.ProjectedValidationCommand) string {
	if strings.TrimSpace(command.Expected) != "" {
		return strings.TrimSpace(command.Expected)
	}
	if strings.TrimSpace(command.SuccessSignal) != "" {
		return strings.TrimSpace(command.SuccessSignal)
	}
	return "Command execution succeeds."
}

func boundedWorkflowValidationConcise(value string) string {
	value = strings.TrimSpace(redactSensitive(value))
	runes := []rune(value)
	if len(runes) > maxWorkflowValidationConciseRunes {
		value = string(runes[:maxWorkflowValidationConciseRunes])
	}
	return value
}
