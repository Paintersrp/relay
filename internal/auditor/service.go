package auditor

import (
	"encoding/json"
	"fmt"
	"time"

	"relay/internal/artifacts"
	"relay/internal/executor"
	"relay/internal/store"
	"relay/internal/validationrunner"
)

// Service orchestrates audit evidence collection, packet generation, and artifact persistence.
type Service struct {
	store   *store.Store
	collect *Collector
}

// NewService creates an audit Service backed by the given store.
func NewService(s *store.Store) *Service {
	return &Service{
		store:   s,
		collect: NewCollector(s),
	}
}

type packetCommandsCheck struct {
	ExecutionPayload *struct {
		ValidationCommands []struct {
			Required bool `json:"required"`
		} `json:"validation_commands"`
	} `json:"execution_payload"`
}

func (svc *Service) requiredValidationCommandsExist(runID int64) (bool, error) {
	data, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		return false, nil
	}
	var pkt packetCommandsCheck
	if err := json.Unmarshal(data, &pkt); err != nil {
		return false, nil
	}
	if pkt.ExecutionPayload == nil {
		return false, nil
	}
	for _, c := range pkt.ExecutionPayload.ValidationCommands {
		if c.Required {
			return true, nil
		}
	}
	return false, nil
}

func (svc *Service) hasValidationArtifacts(runID int64) bool {
	jsonArts, err := svc.store.ListArtifactsByRunKind(runID, validationrunner.ArtifactKindJSON)
	if err != nil || len(jsonArts) == 0 {
		return false
	}
	stdoutArts, err := svc.store.ListArtifactsByRunKind(runID, validationrunner.ArtifactKindStdout)
	if err != nil || len(stdoutArts) == 0 {
		return false
	}
	stderrArts, err := svc.store.ListArtifactsByRunKind(runID, validationrunner.ArtifactKindStderr)
	if err != nil || len(stderrArts) == 0 {
		return false
	}
	return true
}

// Generate collects evidence, generates an audit input summary and audit packet,
// persists both as artifacts, and transitions the run to audit_ready.
// It does not auto-accept, auto-close, auto-commit, or auto-push any repository state.
func (svc *Service) Generate(runID int64) (*GeneratedAudit, error) {
	run, err := svc.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	required, _ := svc.requiredValidationCommandsExist(runID)

	if run.Status == "validation_failed" {
		return nil, fmt.Errorf("validation failed: rerun validation or accept failed validation with a reason before generating audit")
	}

	if required {
		if run.Status != "validation_passed" && run.Status != "validation_failed_accepted" {
			return nil, fmt.Errorf("audit generation for runs with required validation commands requires validation_passed or validation_failed_accepted status, got %q", run.Status)
		}
	} else {
		if run.Status != executor.StatusExecutorDone && run.Status != executor.StatusExecutorBlocked &&
			run.Status != "validation_passed" && run.Status != "validation_failed_accepted" {
			return nil, fmt.Errorf("audit generation requires executor_done, executor_blocked, validation_passed, or validation_failed_accepted status, got %q", run.Status)
		}
	}

	if run.Status == "validation_passed" {
		jsonArts, err := svc.store.ListArtifactsByRunKind(runID, validationrunner.ArtifactKindJSON)
		if err != nil || len(jsonArts) == 0 {
			return nil, fmt.Errorf("audit generation requires validation_run_json for validation_passed status")
		}
	}

	if run.Status == "validation_failed_accepted" {
		jsonArts, err := svc.store.ListArtifactsByRunKind(runID, validationrunner.ArtifactKindJSON)
		if err != nil || len(jsonArts) == 0 {
			return nil, fmt.Errorf("audit generation requires validation_run_json for validation_failed_accepted status")
		}
		acceptanceArts, err := svc.store.ListArtifactsByRunKind(runID, "validation_failure_acceptance_json")
		if err != nil || len(acceptanceArts) == 0 {
			return nil, fmt.Errorf("audit generation requires validation_failure_acceptance_json for validation_failed_accepted status")
		}
	}

	if required && !svc.hasValidationArtifacts(runID) {
		return nil, fmt.Errorf("required validation commands exist but validation artifacts are missing — run validation first via POST /api/runs/%d/validate", runID)
	}

	ev, err := svc.collect.Collect(runID)
	if err != nil {
		return nil, fmt.Errorf("collect evidence: %w", err)
	}

	decision := DetermineDefaultDecision(ev)

	// Write audit_input_summary.md
	inputSummary := GenerateInputSummary(ev)
	summaryPath, err := artifacts.Write(runID, "audit_input_summary", "audit_input_summary.md", []byte(inputSummary))
	if err != nil {
		return nil, fmt.Errorf("write audit input summary: %w", err)
	}
	_, _ = svc.store.CreateArtifact(runID, "audit_input_summary", summaryPath, "text/markdown")

	// Write audit_packet.md
	packet := GenerateAuditPacket(ev, decision)
	packetPath, err := artifacts.Write(runID, "audit_packet", "audit_packet.md", []byte(packet))
	if err != nil {
		return nil, fmt.Errorf("write audit packet: %w", err)
	}
	_, _ = svc.store.CreateArtifact(runID, "audit_packet", packetPath, "text/markdown")

	// Transition to audit_ready — this is NOT acceptance; it signals readiness for human review.
	updatedRun, err := svc.store.UpdateRunStatus(runID, "audit_ready")
	if err != nil {
		return nil, fmt.Errorf("update run status to audit_ready: %w", err)
	}

	_, _ = svc.store.CreateEvent(runID, "status_change", "Audit packet generated; run is ready for auditor review")

	warnings := flattenWarningMessages(ev.Warnings)
	revReqs := flattenRevisionRequirements(ev.RevisionRequirements)

	return &GeneratedAudit{
		RunID:                runID,
		Status:               updatedRun.Status,
		InputSummary:         summaryPath,
		AuditPacket:          packetPath,
		Decision:             decision,
		CreatedAt:            time.Now().UTC(),
		Warnings:             warnings,
		RevisionRequirements: revReqs,
	}, nil
}
