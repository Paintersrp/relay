package pipeline

import (
	"encoding/json"
	"regexp"
	"strings"
)

var locChangedRe = regexp.MustCompile(`(?i)^\s*(\d+)\s+LOC changed\s*$`)
var locChangedColonRe = regexp.MustCompile(`(?i)^\s*LOC changed:\s*(\d+)\s*$`)

type AgentResultStatus string

const (
	AgentResultDone    AgentResultStatus = "DONE"
	AgentResultBlocked AgentResultStatus = "BLOCKED"
	AgentResultUnknown AgentResultStatus = "UNKNOWN"
)

type AgentResult struct {
	Status       AgentResultStatus `json:"status"`
	BuildStatus  string            `json:"build_status"`
	TestStatus   string            `json:"test_status"`
	LOCChanged   string            `json:"loc_changed"`
	BlockerError string            `json:"blocker_error,omitempty"`
	Raw          string            `json:"-"`
}

func valueAfterColon(line string) string {
	line = strings.TrimSpace(line)
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// standaloneAgentStatus matches exact standalone status lines after trimming wrapper punctuation.
func standaloneAgentStatus(line string) AgentResultStatus {
	trimmed := strings.TrimSpace(line)
	normalized := strings.Trim(trimmed, "`*_[](){}:.- ")
	switch strings.ToLower(normalized) {
	case "done":
		return AgentResultDone
	case "blocked":
		return AgentResultBlocked
	default:
		return AgentResultUnknown
	}
}

func ParseAgentResult(raw string) AgentResult {
	result := AgentResult{
		Status: AgentResultUnknown,
		Raw:    raw,
	}

	lines := strings.Split(raw, "\n")
	firstNonEmpty := true

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)

		if firstNonEmpty {
			firstNonEmpty = false
			words := strings.Fields(trimmed)
			if len(words) > 0 {
				firstWord := strings.ToLower(words[0])
				if firstWord == "done" {
					result.Status = AgentResultDone
				} else if firstWord == "blocked" {
					result.Status = AgentResultBlocked
				}
			}
		}

		if strings.HasPrefix(lower, "status:") {
			val := strings.TrimSpace(trimmed[7:])
			valLower := strings.ToLower(val)
			if valLower == "done" || strings.HasPrefix(valLower, "done") {
				result.Status = AgentResultDone
			} else if valLower == "blocked" || strings.HasPrefix(valLower, "blocked") {
				result.Status = AgentResultBlocked
			}
		}

		if status := standaloneAgentStatus(trimmed); status != AgentResultUnknown {
			result.Status = status
		}

		if strings.HasPrefix(lower, "build status:") {
			result.BuildStatus = valueAfterColon(trimmed)
		} else if strings.HasPrefix(lower, "build:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.BuildStatus = v
			}
		}

		if strings.HasPrefix(lower, "test status:") {
			result.TestStatus = valueAfterColon(trimmed)
		} else if strings.HasPrefix(lower, "tests:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.TestStatus = v
			}
		}

		if strings.HasPrefix(lower, "count of loc changed:") {
			result.LOCChanged = valueAfterColon(trimmed)
		} else if strings.HasPrefix(lower, "loc changed:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.LOCChanged = v
			}
		} else if strings.HasPrefix(lower, "lines changed:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.LOCChanged = v
			}
		}

		if strings.HasPrefix(lower, "blocker/error only if blocked:") {
			result.BlockerError = valueAfterColon(trimmed)
		} else if strings.HasPrefix(lower, "blocker:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.BlockerError = v
			}
		} else if strings.HasPrefix(lower, "error:") {
			if v := valueAfterColon(trimmed); v != "" {
				result.BlockerError = v
			}
		}

		// Fallback parsing for non-canonical output
		if result.BuildStatus == "" && strings.Contains(lower, "build") {
			result.BuildStatus = trimmed
		}
		if result.TestStatus == "" && strings.Contains(lower, "test") {
			result.TestStatus = trimmed
		}
		if result.LOCChanged == "" {
			if m := locChangedRe.FindStringSubmatch(trimmed); len(m) > 1 {
				result.LOCChanged = m[1]
			} else if m := locChangedColonRe.FindStringSubmatch(trimmed); len(m) > 1 {
				result.LOCChanged = m[1]
			}
		}
	}

	return result
}

func (r AgentResult) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
