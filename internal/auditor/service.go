package auditor

import (
	"fmt"
	"time"

	"relay/internal/artifacts"
	"relay/internal/executor"
	"relay/internal/store"
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

// Generate collects evidence, generates an audit input summary and audit packet,
// persists both as artifacts, and transitions the run to audit_ready.
// It does not auto-accept, auto-close, auto-commit, or auto-push any repository state.
func (svc *Service) Generate(runID int64) (*GeneratedAudit, error) {
	run, err := svc.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	if run.Status != executor.StatusExecutorDone && run.Status != executor.StatusExecutorBlocked {
		return nil, fmt.Errorf("audit generation requires executor_done or executor_blocked status, got %q", run.Status)
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
