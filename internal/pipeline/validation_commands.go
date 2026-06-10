package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"
)

type ValidationCommand struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Source  string `json:"source"`
}

var validationSectionRe = regexp.MustCompile(`(?i)^#{2,3}\s+(?:relay\s+validation\s+commands|validation\s+commands|tests\s*/\s*validation|validation|tests)\s*$`)
var shellFenceRe = regexp.MustCompile("```")
var shellLangRe = regexp.MustCompile(`(?i)^(sh|shell|bash|zsh|fish|powershell|pwsh|ps1|cmd|bat|console|terminal)$`)

var destructiveRe = regexp.MustCompile(`(?i)\b(rm\s+\-rf|del\s+/s|remove-item|git\s+reset\s+--hard|git\s+clean\s+-fd|shutdown|format)\b`)
var agentRe = regexp.MustCompile(`(?i)\b(opencode|cline|codex)\b`)
var chainedRe = regexp.MustCompile(`&&|\|\||;|\|`)

var knownCommandPrefixes = []string{
	"go ", "npm ", "pnpm ", "yarn ", "bun ", "node ", "npx ",
	"templ ", "sqlc ", "goose ",
	"rtk ", "rtk.exe ", "rtk.exe", "make ", "task ", "just ",
}

func canonicalValidationCommand(command string) string {
	cmd := strings.TrimSpace(command)
	if strings.HasPrefix(cmd, "rtk.exe test \"") && strings.HasSuffix(cmd, "\"") {
		inner := cmd[len("rtk.exe test \"") : len(cmd)-1]
		return strings.TrimSpace(inner)
	}
	if strings.HasPrefix(cmd, "rtk test \"") && strings.HasSuffix(cmd, "\"") {
		inner := cmd[len("rtk test \"") : len(cmd)-1]
		return strings.TrimSpace(inner)
	}
	if strings.HasPrefix(cmd, "rtk.exe ") {
		return strings.TrimSpace(cmd[len("rtk.exe "):])
	}
	if strings.HasPrefix(cmd, "rtk ") {
		return strings.TrimSpace(cmd[len("rtk "):])
	}
	return cmd
}

func hasKnownCommandPrefix(line string) bool {
	lower := strings.ToLower(line)
	for _, prefix := range knownCommandPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	// also allow .exe as first token
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.HasSuffix(strings.ToLower(fields[0]), ".exe") {
		return true
	}
	return false
}

func isShellFenceOpener(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[3:])
	if rest == "" {
		return "", true
	}
	fields := strings.Fields(rest)
	if shellLangRe.MatchString(fields[0]) {
		return fields[0], true
	}
	return "", false
}

func isDestructiveOrAgentOrChained(cmd string) bool {
	if destructiveRe.MatchString(cmd) {
		return true
	}
	if agentRe.MatchString(cmd) {
		return true
	}
	if chainedRe.MatchString(cmd) {
		return true
	}
	return false
}

// headingLevel returns the markdown heading level of a line (0 if not a heading).
func headingLevel(line string) int {
	for i, ch := range line {
		if ch != '#' {
			if ch == ' ' && i >= 1 && i <= 6 {
				return i
			}
			return 0
		}
	}
	return 0
}

func isCommentOrEmpty(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

func isLikelyLabel(line string) bool {
	return strings.HasSuffix(strings.TrimSpace(line), ":")
}

func ExtractValidationCommands(handoffText string, repoDefaultCommandsJSON string) []ValidationCommand {
	seen := map[string]bool{}
	var commands []ValidationCommand

	// Extract from handoff
	lines := strings.Split(handoffText, "\n")
	inValidationSection := false
	validationSectionLevel := 0
	inFence := false
	fenceIsShell := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inFence && validationSectionRe.MatchString(trimmed) {
			validationSectionLevel = headingLevel(trimmed)
			inValidationSection = true
			continue
		}

		if inValidationSection && !inFence {
			if level := headingLevel(trimmed); level > 0 && level <= validationSectionLevel {
				inValidationSection = false
				continue
			}
		}

		if !inValidationSection {
			continue
		}

		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				lang, ok := isShellFenceOpener(line)
				if ok && lang != "" {
					fenceIsShell = true
				} else {
					fenceIsShell = false
				}
				inFence = true
			} else {
				inFence = false
				fenceIsShell = false
			}
			continue
		}

		if inFence && !fenceIsShell {
			continue
		}

		if inFence && fenceIsShell {
			if isCommentOrEmpty(trimmed) {
				continue
			}
		} else {
			if isCommentOrEmpty(trimmed) || isLikelyLabel(trimmed) || !hasKnownCommandPrefix(trimmed) {
				continue
			}
		}

		if isDestructiveOrAgentOrChained(trimmed) {
			continue
		}

		canonical := canonicalValidationCommand(trimmed)
		if !seen[canonical] {
			seen[canonical] = true
			commands = append(commands, ValidationCommand{
				Label:   canonical,
				Command: canonical,
				Source:  "handoff",
			})
		}
	}

	if len(commands) > 0 {
		return commands
	}

	// Fall back to repo defaults
	trimmed := strings.TrimSpace(repoDefaultCommandsJSON)
	if trimmed == "" || trimmed == "null" {
		return []ValidationCommand{}
	}

	var rawStrings []string
	if err := json.Unmarshal([]byte(trimmed), &rawStrings); err == nil {
		for _, cmd := range rawStrings {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" || isDestructiveOrAgentOrChained(cmd) {
				continue
			}
			if seen[cmd] {
				continue
			}
			seen[cmd] = true
			commands = append(commands, ValidationCommand{
				Label:   cmd,
				Command: cmd,
				Source:  "repo_default",
			})
		}
		return commands
	}

	var rawObjects []struct {
		Label   string `json:"label"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(trimmed), &rawObjects); err == nil {
		for _, obj := range rawObjects {
			cmd := strings.TrimSpace(obj.Command)
			if cmd == "" || isDestructiveOrAgentOrChained(cmd) {
				continue
			}
			label := strings.TrimSpace(obj.Label)
			if label == "" {
				label = cmd
			}
			if seen[cmd] {
				continue
			}
			seen[cmd] = true
			commands = append(commands, ValidationCommand{
				Label:   label,
				Command: cmd,
				Source:  "repo_default",
			})
		}
		return commands
	}

	return []ValidationCommand{}
}
