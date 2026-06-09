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
	case "opencode_handoff_packet":
		return "opencode_handoff_packet.json"
	case "agent_result_raw":
		return "agent_result.txt"
	case "agent_result_json":
		return "agent_result.json"
	case "validation_run_json":
		return "validation_run.json"
	case "validation_stdout":
		return "validation_stdout.txt"
	case "validation_stderr":
		return "validation_stderr.txt"
	default:
		return fmt.Sprintf("%s.txt", kind)
	}
}
