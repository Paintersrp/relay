package pipeline

import (
	"fmt"
	"strings"
)

func PreparePrompt(originalHandoff string) string {
	var b strings.Builder
	b.WriteString("You are working in the selected repository for this run.\n\n")
	b.WriteString("Follow the implementation handoff exactly. Do not perform unrelated cleanup.\n\n")
	b.WriteString(originalHandoff)
	return b.String()
}

func ArtifactFilename(kind string) string {
	switch kind {
	case "original_handoff":
		return "original_handoff.txt"
	case "handoff_validation_json":
		return "handoff_validation.json"
	case "ready_prompt":
		return "ready_prompt.txt"
	case "audit_packet":
		return "audit_packet.json"
	default:
		return fmt.Sprintf("%s.txt", kind)
	}
}
