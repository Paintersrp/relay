// Package planningartifacts validates the structural contract of authored
// planning Markdown. It intentionally does not score, interpret, or persist
// authored content.
package planningartifacts

import (
	"fmt"
	"regexp"
	"strings"

	"relay/internal/speccompiler"
)

type heading struct {
	Level int
	Text  string
}

var requiredHeadings = map[speccompiler.ArtifactKind][]heading{
	speccompiler.ArtifactRequirements: {
		{Level: 1, Text: "Requirements"},
		{Level: 2, Text: "Goal"},
		{Level: 2, Text: "Scope"},
		{Level: 2, Text: "Requirements"},
		{Level: 2, Text: "Acceptance Criteria"},
	},
	speccompiler.ArtifactSharedDesign: {
		{Level: 1, Text: "Shared Design"},
		{Level: 2, Text: "Context"},
		{Level: 2, Text: "Design"},
		{Level: 2, Text: "Risks"},
		{Level: 2, Text: "Validation"},
	},
	speccompiler.ArtifactTicketDesignBrief: {
		{Level: 1, Text: "Ticket Design Brief"},
		{Level: 2, Text: "Selected Ticket"},
		{Level: 2, Text: "Package Authority and Scope"},
		{Level: 2, Text: "Approved Decisions Carried Forward"},
		{Level: 2, Text: "Forbidden Behavior"},
		{Level: 2, Text: "Required Invariants"},
		{Level: 2, Text: "Source Contracts"},
		{Level: 2, Text: "Required Proof Obligations"},
		{Level: 2, Text: "Blockers or Unresolved Source Facts"},
		{Level: 2, Text: "Implementation Goal"},
		{Level: 2, Text: "Files to Create or Modify"},
		{Level: 2, Text: "New Types, Functions, Methods, or Fields"},
		{Level: 2, Text: "Control Flow"},
		{Level: 2, Text: "State Mutations"},
		{Level: 2, Text: "Error Behavior"},
		{Level: 2, Text: "Evidence or Artifact Behavior"},
		{Level: 2, Text: "Concurrency or Lifecycle Behavior"},
		{Level: 2, Text: "Implementation Guidance"},
		{Level: 2, Text: "Test Matrix"},
		{Level: 2, Text: "Validation Commands"},
		{Level: 2, Text: "Explicit Deferrals"},
		{Level: 2, Text: "Non-Decisions"},
	},
}

var validationFieldPattern = regexp.MustCompile(`(?i)(working directory|command|expected(?: successful result| result)?|proof purpose)\s*:`)
var unresolvedValidationPlaceholderPattern = regexp.MustCompile(`(?i)<[a-z][^>]*>|\b(?:TODO|TBD)\b`)

// Validate returns concrete, deterministic diagnostics for the required
// headings of an authored Markdown artifact. Callers must establish the
// artifact kind with speccompiler.ParseFilename before calling Validate.
func Validate(kind speccompiler.ArtifactKind, raw []byte) []speccompiler.Diagnostic {
	required, ok := requiredHeadings[kind]
	if !ok {
		return []speccompiler.Diagnostic{{
			Code:    "unsupported_artifact_kind",
			Path:    "",
			Message: fmt.Sprintf("Artifact kind %q is not authored planning Markdown.", kind),
		}}
	}
	if kind == speccompiler.ArtifactTicketDesignBrief {
		return validateTicketDesignBrief(raw, required)
	}

	found := make(map[heading]bool, len(required))
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if fence, closing := markdownFence(line); fence {
			if !inFence {
				inFence = true
			} else if closing {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		if value, ok := markdownHeading(line); ok {
			found[value] = true
		}
	}

	diagnostics := make([]speccompiler.Diagnostic, 0, len(required))
	for _, value := range required {
		if found[value] {
			continue
		}
		diagnostics = append(diagnostics, speccompiler.Diagnostic{
			Code:    "missing_required_heading",
			Path:    "/headings",
			Message: fmt.Sprintf("Required heading %q is missing.", headingLabel(value)),
		})
	}
	return diagnostics
}

func validateTicketDesignBrief(raw []byte, required []heading) []speccompiler.Diagnostic {
	lines := strings.Split(strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff"), "\n")
	if hasFrontmatter(lines) {
		return []speccompiler.Diagnostic{{
			Code:    "frontmatter_not_permitted",
			Path:    "/frontmatter",
			Message: "Ticket Design Brief frontmatter is not permitted.",
		}}
	}

	type occurrence struct {
		heading
		line int
	}
	occurrences := make([]occurrence, 0, len(required))
	inFence := false
	for lineNumber, line := range lines {
		if fence, closing := markdownFence(line); fence {
			if !inFence {
				inFence = true
			} else if closing {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		value, ok := markdownHeading(line)
		if ok && value.Level <= 2 {
			occurrences = append(occurrences, occurrence{heading: value, line: lineNumber})
		}
	}

	requiredSet := make(map[heading]struct{}, len(required))
	counts := make(map[heading]int, len(required))
	for _, value := range required {
		requiredSet[value] = struct{}{}
	}
	diagnostics := make([]speccompiler.Diagnostic, 0)
	for _, value := range occurrences {
		if _, ok := requiredSet[value.heading]; !ok {
			diagnostics = append(diagnostics, speccompiler.Diagnostic{
				Code:    "unexpected_heading",
				Path:    "/headings",
				Message: fmt.Sprintf("Heading %q is not permitted in a Ticket Design Brief.", headingLabel(value.heading)),
			})
			continue
		}
		counts[value.heading]++
	}
	for _, value := range required {
		switch counts[value] {
		case 0:
			diagnostics = append(diagnostics, speccompiler.Diagnostic{
				Code:    "missing_required_heading",
				Path:    "/headings",
				Message: fmt.Sprintf("Required heading %q is missing.", headingLabel(value)),
			})
		case 2:
			diagnostics = append(diagnostics, speccompiler.Diagnostic{
				Code:    "duplicate_required_heading",
				Path:    "/headings",
				Message: fmt.Sprintf("Required heading %q must appear exactly once.", headingLabel(value)),
			})
		default:
			if counts[value] > 2 {
				diagnostics = append(diagnostics, speccompiler.Diagnostic{
					Code:    "duplicate_required_heading",
					Path:    "/headings",
					Message: fmt.Sprintf("Required heading %q must appear exactly once.", headingLabel(value)),
				})
			}
		}
	}
	if len(occurrences) == len(required) {
		for index, value := range occurrences {
			if value.heading != required[index] {
				diagnostics = append(diagnostics, speccompiler.Diagnostic{
					Code:    "heading_order",
					Path:    "/headings",
					Message: "Ticket Design Brief headings must appear in the required order.",
				})
				break
			}
		}
	}

	validationHeading := heading{Level: 2, Text: "Validation Commands"}
	validationIndex := -1
	for index, value := range occurrences {
		if value.heading == validationHeading {
			validationIndex = index
			break
		}
	}
	if validationIndex >= 0 {
		endLine := len(lines)
		for index := validationIndex + 1; index < len(occurrences); index++ {
			if occurrences[index].line > occurrences[validationIndex].line {
				endLine = occurrences[index].line
				break
			}
		}
		diagnostics = append(diagnostics, validateValidationCommands(lines[occurrences[validationIndex].line+1:endLine])...)
	}
	return diagnostics
}

func hasFrontmatter(lines []string) bool {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	for _, line := range lines[1:] {
		if trimmed := strings.TrimSpace(line); trimmed == "---" || trimmed == "..." {
			return true
		}
	}
	return true
}

func validateValidationCommands(lines []string) []speccompiler.Diagnostic {
	type validationEntry struct {
		lines []string
	}
	entries := make([]validationEntry, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, content, ok := markdownListItem(line); ok {
			if len(entries) > 0 && isValidationFieldLine(content) && !isValidationWorkingDirectoryLine(content) {
				entries[len(entries)-1].lines = append(entries[len(entries)-1].lines, content)
				continue
			}
			entries = append(entries, validationEntry{lines: []string{content}})
			continue
		}
		if len(entries) == 0 {
			continue
		}
		entries[len(entries)-1].lines = append(entries[len(entries)-1].lines, trimmed)
	}
	if len(entries) == 0 {
		return []speccompiler.Diagnostic{{
			Code:    "missing_validation_command",
			Path:    "/validation_commands",
			Message: "At least one validation command entry is required.",
		}}
	}

	diagnostics := make([]speccompiler.Diagnostic, 0)
	for index, entry := range entries {
		fields := validationEntryFields(entry.lines)
		path := fmt.Sprintf("/validation_commands/%d", index)
		if strings.TrimSpace(fields["working directory"]) == "" {
			diagnostics = append(diagnostics, speccompiler.Diagnostic{Code: "missing_validation_working_directory", Path: path + "/working_directory", Message: "Validation entry must identify a working directory."})
		}
		command := strings.TrimSpace(fields["command"])
		if command == "" {
			diagnostics = append(diagnostics, speccompiler.Diagnostic{Code: "missing_validation_command_text", Path: path + "/command", Message: "Validation entry must identify an exact command."})
		} else if unresolvedValidationPlaceholderPattern.MatchString(command) {
			diagnostics = append(diagnostics, speccompiler.Diagnostic{Code: "unresolved_validation_placeholder", Path: path + "/command", Message: "Validation command contains an unresolved placeholder."})
		}
		if strings.TrimSpace(fields["expected"]) == "" {
			diagnostics = append(diagnostics, speccompiler.Diagnostic{Code: "missing_validation_expected", Path: path + "/expected", Message: "Validation entry must identify an expected result or proof purpose."})
		}
	}
	return diagnostics
}

func isValidationFieldLine(line string) bool {
	return validationFieldPattern.MatchString(strings.TrimSpace(line))
}

func isValidationWorkingDirectoryLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "working directory")
}

func markdownListItem(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 {
		return "", "", false
	}
	if strings.Contains("-*+", trimmed[:1]) && trimmed[1] == ' ' {
		return trimmed[:1], strings.TrimSpace(trimmed[2:]), true
	}
	index := 0
	for index < len(trimmed) && trimmed[index] >= '0' && trimmed[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(trimmed) && (trimmed[index] == '.' || trimmed[index] == ')') && trimmed[index+1] == ' ' {
		return trimmed[:index+1], strings.TrimSpace(trimmed[index+2:]), true
	}
	return "", "", false
}

func validationEntryFields(lines []string) map[string]string {
	joined := strings.Join(lines, "\n")
	matches := validationFieldPattern.FindAllStringIndex(joined, -1)
	fields := make(map[string]string, 3)
	for index, match := range matches {
		label := strings.ToLower(strings.TrimSpace(joined[match[0] : match[1]-1]))
		label = strings.Join(strings.Fields(label), " ")
		if strings.HasPrefix(label, "expected") || label == "proof purpose" {
			label = "expected"
		}
		valueStart := match[1]
		valueEnd := len(joined)
		if index+1 < len(matches) {
			valueEnd = matches[index+1][0]
		}
		value := strings.TrimSpace(joined[valueStart:valueEnd])
		value = strings.Trim(value, "| ")
		if len(value) >= 2 && value[0] == '`' && value[len(value)-1] == '`' {
			value = strings.TrimSpace(value[1 : len(value)-1])
		}
		fields[label] = value
	}
	return fields
}

func headingLabel(value heading) string {
	return strings.Repeat("#", value.Level) + " " + value.Text
}

func markdownFence(line string) (bool, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return false, false
	}
	marker := trimmed[0]
	if marker != '`' && marker != '~' {
		return false, false
	}
	count := 0
	for count < len(trimmed) && trimmed[count] == marker {
		count++
	}
	if count < 3 {
		return false, false
	}
	return true, strings.TrimSpace(trimmed[count:]) == ""
}

func markdownHeading(line string) (heading, bool) {
	indent := 0
	for indent < len(line) && indent < 4 && line[indent] == ' ' {
		indent++
	}
	if indent == 4 || indent == len(line) {
		return heading{}, false
	}
	line = line[indent:]
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level == len(line) || line[level] != ' ' {
		return heading{}, false
	}
	text := strings.TrimSpace(line[level:])
	text = strings.TrimSpace(strings.TrimRight(text, "#"))
	if text == "" {
		return heading{}, false
	}
	return heading{Level: level, Text: text}, true
}
