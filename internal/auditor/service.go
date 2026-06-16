package auditor

import (
	"fmt"
	"time"

	"relay/internal/artifacts"
	"relay/internal/executor"
	"relay/internal/store"
)

type Service struct {
	store   *store.Store
	collect *Collector
}

func NewService(s *store.Store) *Service {
	return &Service{
		store:   s,
		collect: NewCollector(s),
	}
}

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

	inputSummary := GenerateInputSummary(ev)
	summaryPath, err := artifacts.Write(runID, "audit_input_summary", "audit_input_summary.md", []byte(inputSummary))
	if err != nil {
		return nil, fmt.Errorf("write audit input summary: %w", err)
	}
	_, _ = svc.store.CreateArtifact(runID, "audit_input_summary", summaryPath, "text/markdown")

	packet := GenerateAuditPacket(ev, decision)
	packetPath, err := artifacts.Write(runID, "audit_packet", "audit_packet.md", []byte(packet))
	if err != nil {
		return nil, fmt.Errorf("write audit packet: %w", err)
	}
	_, _ = svc.store.CreateArtifact(runID, "audit_packet", packetPath, "text/markdown")

	updatedRun, err := svc.store.UpdateRunStatus(runID, "audit_ready")
	if err != nil {
		return nil, fmt.Errorf("update run status to audit_ready: %w", err)
	}

	_, _ = svc.store.CreateEvent(runID, "status_change", "Audit packet generated; run is ready for review")

	return &GeneratedAudit{
		RunID:        runID,
		Status:       updatedRun.Status,
		InputSummary: summaryPath,
		AuditPacket:  packetPath,
		Decision:     decision,
		CreatedAt:    time.Now().UTC(),
		Warnings:     ev.Warnings,
	}, nil
}
