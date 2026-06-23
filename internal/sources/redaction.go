package sources

import "regexp"

var blockedSourcePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
}

var redactedSourcePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(Authorization:\s*)[^\r\n]+`),
	regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-._~+/=]+`),
	regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)(token\s*[:=]\s*)\S+`),
	regexp.MustCompile(`(?i)(password\s*[:=]\s*)\S+`),
}

func redactSourceContent(input string) (string, string) {
	for _, pattern := range blockedSourcePatterns {
		if pattern.MatchString(input) {
			return "", RedactionStatusBlocked
		}
	}

	result := input
	changed := false
	for _, pattern := range redactedSourcePatterns {
		updated := pattern.ReplaceAllStringFunc(result, func(match string) string {
			locs := pattern.FindStringSubmatchIndex(match)
			if len(locs) >= 4 {
				return match[locs[2]:locs[3]] + "[REDACTED]"
			}
			return "[REDACTED]"
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
