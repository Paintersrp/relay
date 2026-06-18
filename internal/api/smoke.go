package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/validation"
)

func SmokeEnabled() bool {
	v := strings.ToLower(os.Getenv("RELAY_DEV_SMOKE"))
	return v == "1" || v == "true" || v == "yes"
}

const (
	smokePacketID = "packet-2026-06-17-relay-aider-repair-smoke-test-eligible-validation-failure"
	smokeRepoName = "Paintersrp/relay"
	smokeRunTitle = "Aider Repair Smoke Test — Eligible Validation Failure"
)

// POST /api/dev/setup-smoke-validation-failure
func (h *APIHandler) SetupSmokeValidationFailure(w http.ResponseWriter, r *http.Request) {
	if !SmokeEnabled() {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Smoke endpoint not available (set RELAY_DEV_SMOKE=true)")
		return
	}

	repo, err := h.store.GetRepoByName(smokeRepoName)
	if err != nil {
		repo, err = h.store.CreateRepo(smokeRepoName, ".")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create smoke repo: %v", err))
			return
		}
	}

	run, err := h.store.CreateRun(repo.ID, smokeRunTitle, "packet_validation_failed", "deepseek-v4-flash", "deepseek-v4-flash", "current working branch")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create smoke run: %v", err))
		return
	}

	packet := buildSmokePacket()
	packetBytes, _ := json.MarshalIndent(packet, "", "  ")
	packetPath, err := artifacts.Write(run.ID, "canonical_packet", "canonical_packet.json", packetBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to write canonical packet artifact: %v", err))
		return
	}
	_, _ = h.store.CreateArtifact(run.ID, "canonical_packet", packetPath, "application/json")

	report := &validation.ValidationReport{
		Valid:          false,
		RepairEligible: true,
		Errors: []validation.ValidationError{
			{
				Type:           "schema",
				Code:           validation.CodeMissingRequiredField,
				Message:        `Required property "audit_seed" is missing`,
				RepairEligible: true,
			},
		},
	}
	reportBytes, _ := json.MarshalIndent(report, "", "  ")
	reportPath, err := artifacts.Write(run.ID, "packet_validation_report", "packet_validation_report.json", reportBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to write validation report artifact: %v", err))
		return
	}
	_, _ = h.store.CreateArtifact(run.ID, "packet_validation_report", reportPath, "application/json")

	_, _ = h.store.CreateEvent(run.ID, "status_change", "Smoke test run created: "+smokePacketID)
	_, _ = h.store.CreateEvent(run.ID, "info", fmt.Sprintf("Validation report contains %s (repair-eligible)", validation.CodeMissingRequiredField))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"runId":    fmt.Sprintf("%d", run.ID),
		"packetId": smokePacketID,
		"status":   "packet_validation_failed",
		"repo":     smokeRepoName,
		"branch":   "current working branch",
		"message":  "Smoke test run created. POST /api/runs/{id}/repair/validation to exercise the repair pipeline.",
	})
}

// buildSmokePacket returns a canonical packet that is missing the audit_seed
// field, causing a schema-level CANONICAL_PACKET_MISSING_REQUIRED_FIELD error.
func buildSmokePacket() map[string]interface{} {
	return map[string]interface{}{
		"packet_meta": map[string]interface{}{
			"packet_id":                   smokePacketID,
			"protocol_version":            "1.0.0",
			"schema_version":              "1.0.0",
			"created_at":                  "2026-06-17T00:00:00Z",
			"producer_kind":               "relay-packet-compiler",
			"source_planner_handoff_path": "handoffs/planner/aider-repair-smoke-test.planner-handoff.md",
			"intended_packet_path":        "handoffs/packets/aider-repair-smoke-test.canonical-packet.json",
			"task_slug":                   "aider-repair-smoke-test",
			"target_executor":             "deepseek-v4-flash",
			"repo_target":                 smokeRepoName,
			"branch_context":              "current working branch",
			"lifecycle_state":             "packet_created",
			"artifact_paths": map[string]interface{}{
				"planner_handoff":  "handoffs/planner/aider-repair-smoke-test.planner-handoff.md",
				"canonical_packet": "handoffs/packets/aider-repair-smoke-test.canonical-packet.json",
				"executor_brief":   "handoffs/briefs/aider-repair-smoke-test.executor-brief.md",
				"executor_result":  "handoffs/results/aider-repair-smoke-test.executor-result.txt",
			},
		},
		"planner_context": map[string]interface{}{
			"user_request_summary": "Create a deterministic smoke test for the Aider validation-repair pipeline.",
			"context_snapshot":     []string{"No prior context for smoke test."},
			"decision_log":         []interface{}{},
			"constraints":          []interface{}{},
			"assumptions":          []interface{}{},
			"known_repo_facts":     []interface{}{},
			"pass_boundary": map[string]interface{}{
				"current_pass":               1,
				"total_planned_passes":       1,
				"this_pass_scope":            "Aider repair pipeline smoke test.",
				"out_of_scope_for_this_pass": []string{"Production data."},
			},
			"unresolved_questions":  []interface{}{},
			"rejected_alternatives": []interface{}{},
			"risk_register":         []interface{}{},
		},
		"execution_payload": map[string]interface{}{
			"goal":                           "Exercise Aider validation-repair pipeline.",
			"scope":                          "Smoke test only.",
			"non_goals":                      []string{},
			"file_targets":                   []interface{}{},
			"implementation_steps":           []interface{}{},
			"code_requirements":              []interface{}{},
			"validation_commands":            []interface{}{},
			"expected_behavior":              []string{},
			"completion_contract":            map[string]interface{}{},
			"executor_final_response_format": "DONE_or_BLOCKED_strict_text",
		},
		// audit_seed intentionally omitted to trigger schema required error
	}
}
