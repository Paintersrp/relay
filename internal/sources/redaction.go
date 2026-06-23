package sources

import "regexp"

var blockedSourcePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
}

type sourceRedactionPattern struct {
	pattern *regexp.Regexp
	marker  string
}

var redactedSourcePatterns = []sourceRedactionPattern{
	{pattern: regexp.MustCompile(`(?i)(Authorization:\s*)[^\r\n]+`), marker: "[REDACTED_AUTH_HEADER]"},
	{pattern: regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/=]+`), marker: "[REDACTED_TOKEN]"},
	{pattern: regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)\S+`), marker: "[REDACTED_TOKEN]"},
	{pattern: regexp.MustCompile(`(?i)(token\s*[:=]\s*)\S+`), marker: "[REDACTED_TOKEN]"},
	{pattern: regexp.MustCompile(`(?i)(password\s*[:=]\s*)\S+`), marker: "[REDACTED_SECRET]"},
}

func redactSourceContent(input string) (string, string) {
	for _, pattern := range blockedSourcePatterns {
		if pattern.MatchString(input) {
			return "", RedactionStatusBlocked
		}
	}

	result := input
	changed := false
	for _, redaction := range redactedSourcePatterns {
		pattern := redaction.pattern
		updated := pattern.ReplaceAllStringFunc(result, func(match string) string {
			locs := pattern.FindStringSubmatchIndex(match)
			if len(locs) >= 4 {
				return match[locs[2]:locs[3]] + redaction.marker
			}
			return redaction.marker
		})
		if updated != result {
			changed = true
			result = updated
		}
	}

	if changed {
		return result, RedactionStatusRedacted
	}
	return result, RedactionStatusNotNeeded
}
