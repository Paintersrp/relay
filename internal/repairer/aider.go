package repairer

import (
	"encoding/json"
	"fmt"

	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
)

// EligibleCodes is the allowlist of validation error types
// that qualify for Aider repair.
var EligibleCodes = map[string]bool{
	"structural": true,
	"schema":     true,
}

// CheckEligibility validates that all errors in the report are
// in the allowlist. Returns false and a reason if ineligible.
func CheckEligibility(report *validation.ValidationReport) (bool, string) {
	if report == nil {
		return false, "no validation report"
	}
	if len(report.Errors) == 0 {
		return false, "no validation errors to repair"
	}
	if !report.RepairEligible {
		return false, "validation report is not repair eligible"
	}
	for _, err := range report.Errors {
		if !EligibleCodes[err.Type] {
			return false, fmt.Sprintf("failure code %q is not eligible for repair", err.Type)
		}
	}
	return true, ""
}

// BuildRepairPrompt constructs a redacted repair prompt for Aider.
// The prompt includes validation errors and the packet JSON for context.
func BuildRepairPrompt(packetJSON []byte, report *validation.ValidationReport) string {
	prompt := "Fix the following validation errors in the canonical packet JSON.\n\n"
	prompt += "Validation errors:\n"
	for _, err := range report.Errors {
		prompt += fmt.Sprintf("- [%s] %s\n", err.Type, err.Message)
	}
	prompt += "\nCurrent packet JSON (repair to fix the above errors):\n"
	prompt += string(packetJSON)
	prompt += "\n\nReturn only the repaired JSON with no additional text."
	return prompt
}

// RepairArtifactKind constants for persisted repair artifacts.
const (
	ArtifactKindRepairRequest      = "repair_request_json"
	ArtifactKindRepairPrompt       = "repair_prompt"
	ArtifactKindRepairOutput       = "repair_output"
	ArtifactKindRepairedPacket     = "repaired_packet"
	ArtifactKindRepairValidation   = "repair_validation_report"
)

// Service orchestrates validation repair via Aider.
type Service struct {
	store *store.Store
}

// NewService creates a new repair service.
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// RepairResult holds the outcome of a repair attempt.
type RepairResult struct {
	Success            bool                        `json:"success"`
	RunID              int64                       `json:"runId"`
	IneligibleReason   string                      `json:"ineligibleReason,omitempty"`
	ReValidationReport *validation.ValidationReport `json:"reValidationReport,omitempty"`
	Error              string                      `json:"error,omitempty"`
}

// RepairValidation runs the full repair flow for a validation-stage failure.
// It checks eligibility, builds a prompt, persists artifacts, re-validates,
// and updates run status on success.
func (svc *Service) RepairValidation(runID int64, packetJSON []byte, report *validation.ValidationReport) *RepairResult {
	result := &RepairResult{RunID: runID}

	eligible, reason := CheckEligibility(report)
	if !eligible {
		result.IneligibleReason = reason
		_, _ = svc.store.CreateEvent(runID, "info", fmt.Sprintf("Repair not eligible: %s", reason))
		return result
	}

	// Build repair prompt
	prompt := BuildRepairPrompt(packetJSON, report)

	// Persist repair request artifact
	reqData := map[string]interface{}{
		"runId":             runID,
		"errorCount":        len(report.Errors),
		"errorTypes":        collectErrorTypes(report),
		"repairEligible":    true,
	}
	reqBytes, _ := json.MarshalIndent(reqData, "", "  ")
	if path, err := artifacts.Write(runID, ArtifactKindRepairRequest, "repair_request.json", reqBytes); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairRequest, path, "application/json")
	}

	// Persist repair prompt artifact
	if path, err := artifacts.Write(runID, ArtifactKindRepairPrompt, "repair_prompt.txt", []byte(prompt)); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairPrompt, path, "text/plain")
	}

	// Call Aider adapter (stubbed: returns original packet for now)
	repairedJSON := packetJSON
	outputLog := "Aider repair stub: returned original packet (no actual Aider call)"

	// Persist repair output artifact
	if path, err := artifacts.Write(runID, ArtifactKindRepairOutput, "repair_output.txt", []byte(outputLog)); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairOutput, path, "text/plain")
	}

	// Persist repaired packet candidate artifact
	if path, err := artifacts.Write(runID, ArtifactKindRepairedPacket, "repaired_packet.json", repairedJSON); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairedPacket, path, "application/json")
	}

	// Re-validate repaired packet
	valReport, valErr := validation.ValidatePacketJSON(repairedJSON, "relay-contracts/schema/canonical_packet.schema.json")
	if valErr != nil {
		result.Error = fmt.Sprintf("re-validation error: %v", valErr)
		return result
	}
	result.ReValidationReport = valReport

	// Persist repair validation report artifact
	valBytes, _ := json.MarshalIndent(valReport, "", "  ")
	if path, err := artifacts.Write(runID, ArtifactKindRepairValidation, "repair_validation_report.json", valBytes); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairValidation, path, "application/json")
	}

	if valReport.Valid {
		result.Success = true
		_, _ = svc.store.UpdateRunStatus(runID, "repair_validated")
		_, _ = svc.store.CreateEvent(runID, "status_change", "Repair validation passed: packet is valid")
	} else {
		result.Error = fmt.Sprintf("repair did not fix all validation errors: %d remaining", len(valReport.Errors))
		_, _ = svc.store.CreateEvent(runID, "warn", "Repair attempted but validation still fails")
	}

	return result
}

func collectErrorTypes(report *validation.ValidationReport) []string {
	seen := make(map[string]bool)
	var types []string
	for _, err := range report.Errors {
		if !seen[err.Type] {
			seen[err.Type] = true
			types = append(types, err.Type)
		}
	}
	return types
}
