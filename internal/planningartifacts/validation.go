// Package planningartifacts validates the structural contract of authored
// planning Markdown. It intentionally does not score, interpret, or persist
// authored content.
package planningartifacts

import (
	"fmt"
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
		{Level: 2, Text: "Ticket Identity"},
		{Level: 2, Text: "Context"},
		{Level: 2, Text: "Design"},
		{Level: 2, Text: "Implementation Notes"},
		{Level: 2, Text: "Validation"},
	},
}

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
