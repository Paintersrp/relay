package repairer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
	"relay/internal/validation"
)

// EligibleCodes is the allowlist of specific validation error codes
// that qualify for Aider repair.
var EligibleCodes = map[string]bool{
	validation.CodeJSONSyntax:            true,
	validation.CodeMissingRequiredField:  true,
	validation.CodeInvalidEnum:           true,
	validation.CodeExtraProperty:         true,
	validation.CodeInvalidType:           true,
	validation.CodeStringPatternMismatch: true,
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
		if err.Code == "" {
			return false, fmt.Sprintf("failure code is missing for error %q", err.Message)
		}
		if !EligibleCodes[err.Code] {
			return false, fmt.Sprintf("failure code %q is not eligible for repair", err.Code)
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
	return validation.RedactSecrets(prompt)
}

// RepairArtifactKind constants for persisted repair artifacts.
const (
	ArtifactKindRepairRequest      = "repair_request_json"
	ArtifactKindRepairPrompt       = "repair_prompt"
	ArtifactKindRepairOutput       = "repair_output"
	ArtifactKindRepairedPacket     = "repaired_packet"
	ArtifactKindRepairValidation   = "repair_validation_report"
)

// Adapter defines the contract for an Aider repair execution.
type Adapter interface {
	Repair(ctx context.Context, prompt string) (stdout string, stderr string, err error)
}

// CommandAdapter executes Aider via a local shell command.
type CommandAdapter struct {
	Command string
}

// Repair implements the Adapter interface.
func (a *CommandAdapter) Repair(ctx context.Context, prompt string) (string, string, error) {
	if a.Command == "" {
		return "", "", nil
	}
	args := strings.Fields(a.Command)
	if len(args) == 0 {
		return "", "", nil
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(prompt)
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// Service orchestrates validation repair via Aider.
type Service struct {
	store   *store.Store
	adapter Adapter
}

// NewService creates a new repair service with a default CommandAdapter.
func NewService(s *store.Store) *Service {
	return &Service{
		store: s,
		adapter: &CommandAdapter{
			Command: os.Getenv("RELAY_AIDER_REPAIR_COMMAND"),
		},
	}
}

// NewServiceWithAdapter creates a service with a custom adapter (for testing).
func NewServiceWithAdapter(s *store.Store, adapter Adapter) *Service {
	return &Service{
		store:   s,
		adapter: adapter,
	}
}

// RepairResult holds the outcome of a repair attempt.
type RepairResult struct {
	Success            bool                         `json:"success"`
	RunID              int64                        `json:"runId"`
	Eligible           bool                         `json:"eligible"`
	RepairAttempted    bool                         `json:"repairAttempted"`
	BlockedReason      string                       `json:"blockedReason,omitempty"`
	IneligibleReason   string                       `json:"ineligibleReason,omitempty"`
	ReValidationValid  *bool                        `json:"reValidationValid,omitempty"`
	ReValidationReport *validation.ValidationReport `json:"reValidationReport,omitempty"`
	ReValidationError  string                       `json:"reValidationError,omitempty"`
	Error              string                       `json:"error,omitempty"`
	RepairArtifacts    map[string]string            `json:"repairArtifacts,omitempty"`
}

// RepairValidation runs the full repair flow for a validation-stage failure.
// It checks eligibility, builds a prompt, persists artifacts, re-validates,
// and updates run status on success.
func (svc *Service) RepairValidation(runID int64, packetJSON []byte, report *validation.ValidationReport) *RepairResult {
	result := &RepairResult{
		RunID:           runID,
		RepairArtifacts: make(map[string]string),
	}

	eligible, reason := CheckEligibility(report)
	if !eligible {
		result.Eligible = false
		result.IneligibleReason = reason
		_, _ = svc.store.CreateEvent(runID, "info", fmt.Sprintf("Repair not eligible: %s", reason))
		return result
	}
	result.Eligible = true

	// Persist repair request artifact
	reqData := map[string]interface{}{
		"runId":          runID,
		"errorCount":     len(report.Errors),
		"errorTypes":     collectErrorTypes(report),
		"repairEligible": true,
	}
	reqBytes, _ := json.MarshalIndent(reqData, "", "  ")
	if path, err := artifacts.Write(runID, ArtifactKindRepairRequest, "repair_request.json", []byte(validation.RedactSecrets(string(reqBytes)))); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairRequest, path, "application/json")
		result.RepairArtifacts[ArtifactKindRepairRequest] = path
	}

	// Build repair prompt
	prompt := BuildRepairPrompt(packetJSON, report)

	// Persist repair prompt artifact
	if path, err := artifacts.Write(runID, ArtifactKindRepairPrompt, "repair_prompt.txt", []byte(prompt)); err == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairPrompt, path, "text/plain")
		result.RepairArtifacts[ArtifactKindRepairPrompt] = path
	}

	// Check if adapter has a command/is available
	stdout, stderr, err := svc.adapter.Repair(context.Background(), prompt)
	if stdout == "" && stderr == "" && err == nil {
		// Missing command/blocked behavior
		result.BlockedReason = "no configured repair adapter"
		_, _ = svc.store.CreateEvent(runID, "warn", "Repair blocked: "+result.BlockedReason)
		return result
	}

	result.RepairAttempted = true

	// Persist repair output artifact
	outputLog := fmt.Sprintf("STDOUT:\n%s\n\nSTDERR:\n%s\n", stdout, stderr)
	if err != nil {
		outputLog += fmt.Sprintf("\nERROR: %v\n", err)
	}
	outputLog = validation.RedactSecrets(outputLog)

	if path, writeErr := artifacts.Write(runID, ArtifactKindRepairOutput, "repair_output.txt", []byte(outputLog)); writeErr == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairOutput, path, "text/plain")
		result.RepairArtifacts[ArtifactKindRepairOutput] = path
	}

	if err != nil {
		result.Error = fmt.Sprintf("adapter error: %v", err)
		_, _ = svc.store.CreateEvent(runID, "warn", result.Error)
		return result
	}

	repairedJSON := []byte(stdout)

	// Persist repaired packet candidate artifact
	if path, writeErr := artifacts.Write(runID, ArtifactKindRepairedPacket, "repaired_packet.json", []byte(validation.RedactSecrets(string(repairedJSON)))); writeErr == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairedPacket, path, "application/json")
		result.RepairArtifacts[ArtifactKindRepairedPacket] = path
	}

	// Re-validate repaired packet
	valReport, valErr := validation.ValidatePacketJSON(repairedJSON, "relay-contracts/schema/canonical_packet.schema.json")
	if valErr != nil {
		result.ReValidationError = fmt.Sprintf("re-validation error: %v", valErr)
		result.Error = result.ReValidationError
		return result
	}
	result.ReValidationReport = valReport
	isValid := valReport.Valid
	result.ReValidationValid = &isValid

	// Persist repair validation report artifact
	valBytes, _ := json.MarshalIndent(valReport, "", "  ")
	if path, writeErr := artifacts.Write(runID, ArtifactKindRepairValidation, "repair_validation_report.json", []byte(validation.RedactSecrets(string(valBytes)))); writeErr == nil {
		_, _ = svc.store.CreateArtifact(runID, ArtifactKindRepairValidation, path, "application/json")
		result.RepairArtifacts[ArtifactKindRepairValidation] = path
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
