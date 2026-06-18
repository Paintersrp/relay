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
			"packet_id":        smokePacketID,
			"protocol_version": "1.0.0",
			"schema_version":   "1.0.0",
			"created_at":       "2026-06-17T00:00:00Z",
			"producer": map[string]interface{}{
				"kind":    "middleware",
				"name":    "relay-packet-compiler",
				"version": "1.0.0",
			},
			"source_planner_handoff_path": "handoffs/planner/2026-06-17_aider-repair-smoke-test.planner-handoff.md",
			"intended_packet_path":        "handoffs/packets/2026-06-17_aider-repair-smoke-test.canonical-packet.json",
			"task_slug":                   "aider-repair-smoke-test",
			"target_executor":             "deepseek-v4-flash",
			"repo_target":                 smokeRepoName,
			"branch_context":              "current working branch",
			"content_profile":             "implementation_ready",
			"lifecycle_state":             "packet_created",
			"artifact_paths": map[string]interface{}{
				"planner_handoff":          "handoffs/planner/2026-06-17_aider-repair-smoke-test.planner-handoff.md",
				"canonical_packet":         "handoffs/packets/2026-06-17_aider-repair-smoke-test.canonical-packet.json",
				"validation_report":        "handoffs/validation/2026-06-17_aider-repair-smoke-test.validation-report.json",
				"executor_brief":           "handoffs/briefs/2026-06-17_aider-repair-smoke-test.executor-brief.md",
				"executor_result":          "handoffs/results/2026-06-17_aider-repair-smoke-test.executor-result.txt",
				"audit_packet":             "handoffs/audits/2026-06-17_aider-repair-smoke-test.audit-packet.md",
				"repair_prompt":            "handoffs/repair/2026-06-17_aider-repair-smoke-test.repair-prompt.md",
				"repaired_packet":          "handoffs/packets/2026-06-17_aider-repair-smoke-test.canonical-packet.repaired.json",
				"repair_validation_report": "handoffs/validation/2026-06-17_aider-repair-smoke-test.repair-validation-report.json",
			},
		},
		"planner_context": map[string]interface{}{
			"user_request_summary": "Create a deterministic smoke test for the Aider validation-repair pipeline.",
			"context_snapshot": []string{
				"No prior context for smoke test.",
			},
			"decision_log": []interface{}{
				map[string]interface{}{
					"id":        "D1",
					"summary":   "Create a repair smoke fixture that exercises the validation-repair pipeline.",
					"rationale": "The smoke endpoint should cover the current packet schema and repair gating shape.",
				},
			},
			"constraints": []interface{}{
				map[string]interface{}{
					"id":         "C1",
					"statement":  "Keep the smoke fixture focused on packet validation and repair gating.",
					"applies_to": []interface{}{"packet_validator", "relay_middleware"},
				},
			},
			"assumptions": []interface{}{
				map[string]interface{}{
					"id":        "AS1",
					"statement": "The current packet schema is the source of truth for smoke validation.",
					"if_false":  "block",
				},
			},
			"known_repo_facts": []interface{}{
				map[string]interface{}{
					"id":     "F1",
					"fact":   "The smoke fixture is expected to remain repair-eligible except for the intentionally omitted audit_seed.",
					"source": "user_provided",
				},
			},
			"pass_boundary": map[string]interface{}{
				"current_pass":               1,
				"total_planned_passes":       1,
				"this_pass_scope":            "Aider repair pipeline smoke test.",
				"out_of_scope_for_this_pass": []string{"Production data."},
			},
			"unresolved_questions": []interface{}{},
			"rejected_alternatives": []interface{}{
				map[string]interface{}{
					"id":              "RA1",
					"alternative":     "Leave the old smoke packet shape in place.",
					"reason_rejected": "It would not exercise the current schema or validation contract shape.",
				},
			},
			"risk_register": []interface{}{
				map[string]interface{}{
					"id":          "R1",
					"severity":    "medium",
					"description": "Smoke packet drift could mask schema regressions.",
					"mitigation":  "Keep the smoke packet aligned with the current canonical packet contract.",
				},
			},
		},
		"execution_payload": map[string]interface{}{
			"goal":  "Exercise Aider validation-repair pipeline.",
			"scope": "Smoke test only.",
			"non_goals": []string{
				"Do not change production behavior.",
			},
			"file_targets": []interface{}{
				map[string]interface{}{
					"path":   "internal/api/smoke.go",
					"role":   "primary",
					"action": "must_edit",
					"reason": "Maintain the smoke fixture shape used by the repair pipeline.",
				},
			},
			"implementation_steps": []interface{}{
				map[string]interface{}{
					"id":           "S1",
					"title":        "Refresh the smoke fixture packet shape.",
					"action":       "modify",
					"target_paths": []interface{}{"internal/api/smoke.go"},
					"instructions": "Keep the smoke packet aligned with the current canonical packet schema while preserving the intentional audit_seed omission.",
					"acceptance_criteria": []interface{}{
						"The smoke packet uses producer and validation_contract fields.",
						"The smoke packet remains missing audit_seed to exercise the failure path.",
					},
				},
			},
			"code_requirements": []interface{}{
				map[string]interface{}{
					"id":          "CR1",
					"requirement": "Emit packet_meta.producer and execution_payload.validation_contract in the smoke packet.",
					"applies_to":  []interface{}{"internal/api/smoke.go"},
				},
			},
			"validation_contract": map[string]interface{}{
				"mode":           "commands",
				"failure_policy": "block",
				"commands": []interface{}{
					map[string]interface{}{
						"id":               "V1",
						"command":          "go test ./...",
						"required":         true,
						"purpose":          "Verify the smoke packet still passes the repo test suite shape checks.",
						"success_signal":   "Command exits 0.",
						"failure_handling": "attempt_fix_once_then_block",
					},
				},
			},
			"expected_behavior": []string{
				"The smoke packet exercises the repair pipeline using the current canonical packet schema.",
			},
			"implementation_contract": []interface{}{
				map[string]interface{}{
					"section_name": "artifact_contract_table",
					"summary":      "Describe the smoke packet and its validation artifact contract.",
					"details": []interface{}{
						"The packet remains intentionally invalid only because audit_seed is omitted.",
					},
					"required": true,
				},
			},
			"pass_exit_evidence": []interface{}{
				map[string]interface{}{
					"requirement":         "Smoke packet uses current producer and validation_contract fields.",
					"observable_evidence": "Packet JSON contains packet_meta.producer and execution_payload.validation_contract.",
					"evidence_source":     "artifact_output",
					"acceptance_check":    "The smoke packet remains consistent with the current schema except for the intentional audit_seed omission.",
					"failure_meaning":     "BLOCKED if the smoke packet drifts from the current canonical packet shape.",
				},
			},
			"completion_contract": map[string]interface{}{
				"done_when": []interface{}{
					"Smoke packet uses current canonical packet fields and still triggers the intended failure path.",
				},
				"blocked_when": []interface{}{
					"The smoke packet no longer reflects the current canonical packet schema.",
				},
				"allowed_discretion": []interface{}{
					"Use minimal packet content that keeps the smoke path deterministic.",
				},
				"forbidden_discretion": []interface{}{
					"Do not change production behavior.",
				},
			},
			"executor_final_response_format": "DONE_or_BLOCKED_strict_text",
		},
		// audit_seed intentionally omitted to trigger schema required error
	}
}
