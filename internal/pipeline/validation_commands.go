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

var validationSectionRe = regexp.MustCompile(`(?i)^##\s+(?:tests\s*/\s*validation|validation|tests)\s*$`)
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
	inFence := false
	fenceIsShell := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inFence && validationSectionRe.MatchString(trimmed) {
			inValidationSection = true
			continue
		}

		if inValidationSection && !inFence && strings.HasPrefix(trimmed, "## ") {
			inValidationSection = false
			continue
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

		if !seen[trimmed] {
			seen[trimmed] = true
			commands = append(commands, ValidationCommand{
				Label:   trimmed,
				Command: trimmed,
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
