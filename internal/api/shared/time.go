package shared

import (
	"strings"
	"time"
)

func ParseAndFormatTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, value, time.UTC); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return value
}
