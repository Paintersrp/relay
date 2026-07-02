package pathsafety

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

var windowsDrivePathRE = regexp.MustCompile(`^[A-Za-z]:`)

func NormalizeRepoRelativePath(value string, rejectShellMeta bool) (string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", true
	}
	if hasControl(raw) || windowsDrivePathRE.MatchString(raw) {
		return "", false
	}
	if strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, `//`) {
		return "", false
	}
	if rejectShellMeta && strings.ContainsAny(raw, ";&|$<>`") {
		return "", false
	}
	slash := strings.ReplaceAll(raw, `\`, "/")
	if strings.HasPrefix(slash, "/") {
		return "", false
	}
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return "", false
		}
	}
	clean := path.Clean(slash)
	if clean == "." {
		return "", true
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return clean, true
}

func SafeDisplayBaseName(value, fallback string) string {
	raw := strings.TrimSpace(value)
	if raw == "" || hasControl(raw) || windowsDrivePathRE.MatchString(raw) {
		return fallback
	}
	slash := strings.ReplaceAll(raw, `\`, "/")
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return fallback
		}
	}
	base := path.Base(slash)
	if base == "." || base == "/" || base == "" || strings.Contains(base, "..") || strings.ContainsAny(base, ";&|$<>`") {
		return fallback
	}
	return base
}

func LooksLikePath(value string) bool {
	value = strings.TrimSpace(value)
	return strings.ContainsAny(value, `/\`) || windowsDrivePathRE.MatchString(value) || strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`)
}

func hasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
