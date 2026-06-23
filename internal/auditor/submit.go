package auditor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/plans"
	"relay/internal/store"
)

var (
	ErrUnsupportedDecision   = errors.New("unsupported audit decision")
	ErrCompletedRun          = errors.New("completed runs cannot accept audit decisions")
	ErrAuditDecisionNotReady = errors.New("audit decision requires audit_ready or audit_ready_for_review status")
)

type SubmissionService struct {
	store *store.Store
}

func NewSubmissionService(s *store.Store) *SubmissionService {
	return &SubmissionService{store: s}
}

func (s *SubmissionService) SubmitManual(input ManualAuditSubmission) (*DecisionResult, error) {
	if strings.TrimSpace(input.AuditPacketMarkdown) == "" {
		return nil, fmt.Errorf("audit_packet_markdown is required")
	}

	return s.SubmitDecision(DecisionSubmission{
		RunID:               input.RunID,
		Decision:            input.Decision,
		AuditPacketMarkdown: input.AuditPacketMarkdown,
		Notes:               input.Notes,
		Source:              "api",
	})
}

func (s *SubmissionService) SubmitDecision(input DecisionSubmission) (*DecisionResult, error) {
	if !SupportedDecisions[input.Decision] {
		return nil, fmt.Errorf("%w %q: must be one of accepted, accepted_with_warnings, revision_required, blocked, manual_review_required", ErrUnsupportedDecision, input.Decision)
	}

	run, err := s.store.GetRun(input.RunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	if run.Status == "completed" {
		return nil, ErrCompletedRun
	}
	if run.Status != "audit_ready" && run.Status != "audit_ready_for_review" {
		return nil, fmt.Errorf("%w, got %q", ErrAuditDecisionNotReady, run.Status)
	}

	mappedStatus, noteSuffix := mapDecisionToStatus(input.Decision)
	submittedAt := time.Now().UTC()
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "api"
	}

	var auditPacketPath string
	if strings.TrimSpace(input.AuditPacketMarkdown) != "" {
		artifactData := fmt.Sprintf(
			"# Manual Audit Packet\n\n## Metadata\n- Run ID: %d\n- Decision: %s\n- Source: %s\n- Notes: %s\n- Submitted: %s\n\n## Packet Content\n\n%s\n",
			run.ID,
			input.Decision,
			source,
			input.Notes,
			submittedAt.Format(time.RFC3339),
			input.AuditPacketMarkdown,
		)

		auditPacketPath, err = artifacts.Write(run.ID, "audit_packet", "audit_packet_manual.md", []byte(artifactData))
		if err != nil {
			return nil, fmt.Errorf("write manual audit artifact: %w", err)
		}
		if _, err := s.store.CreateArtifact(run.ID, "audit_packet", auditPacketPath, "text/markdown"); err != nil {
			return nil, fmt.Errorf("create manual audit artifact record: %w", err)
		}
	}

	var revisionArtifactPath string
	if mappedStatus == "revision_required" {
		revisionData := fmt.Sprintf(
			"# Audit Revision Decision\n\n- Run ID: %d\n- Decision: %s\n- Source: %s\n- Notes: %s\n- Submitted: %s\n",
			run.ID,
			input.Decision,
			source,
			input.Notes,
			submittedAt.Format(time.RFC3339),
		)
		revisionArtifactPath, err = artifacts.Write(run.ID, "audit_revision", "audit_revision.md", []byte(revisionData))
		if err != nil {
			return nil, fmt.Errorf("write audit revision artifact: %w", err)
		}
		if _, err := s.store.CreateArtifact(run.ID, "audit_revision", revisionArtifactPath, "text/markdown"); err != nil {
			return nil, fmt.Errorf("create audit revision artifact record: %w", err)
		}
	}

	record := AuditDecisionRecord{
		SchemaVersion:        "1.0.0",
		RunID:                run.ID,
		SubmittedAt:          submittedAt,
		RunStatusBefore:      run.Status,
		RunStatusAfter:       mappedStatus,
		Decision:             input.Decision,
		MappedStatus:         mappedStatus,
		Notes:                input.Notes,
		Source:               source,
		ClientTraceID:        input.ClientTraceID,
		AuditPacketPath:      auditPacketPath,
		RevisionArtifactPath: revisionArtifactPath,
		LocalOnly:            true,
	}
	recordBytes, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal audit decision artifact: %w", err)
	}

	decisionArtifactPath, err := artifacts.Write(run.ID, "audit_decision_json", "audit_decision.json", recordBytes)
	if err != nil {
		return nil, fmt.Errorf("write audit decision artifact: %w", err)
	}
	if _, err := s.store.CreateArtifact(run.ID, "audit_decision_json", decisionArtifactPath, "application/json"); err != nil {
		return nil, fmt.Errorf("create audit decision artifact record: %w", err)
	}

	updatedRun, err := s.store.UpdateRunStatus(run.ID, mappedStatus)
	if err != nil {
		return nil, fmt.Errorf("update run status: %w", err)
	}
	if err := plans.NewRunLifecycleService(s.store).ApplyAuditDecision(updatedRun, mappedStatus); err != nil {
		return nil, fmt.Errorf("apply associated pass audit decision: %w", err)
	}

	eventMsg := decisionEventMessage(input.Decision, input.Notes, noteSuffix)
	if _, err := s.store.CreateEvent(run.ID, "status_change", eventMsg); err != nil {
		return nil, fmt.Errorf("create audit decision event: %w", err)
	}

	return &DecisionResult{
		RunID:                run.ID,
		Status:               mappedStatus,
		LifecycleState:       "audit",
		Decision:             input.Decision,
		AuditPacketPath:      auditPacketPath,
		DecisionArtifactPath: decisionArtifactPath,
		RevisionArtifactPath: revisionArtifactPath,
		CreatedAt:            submittedAt,
	}, nil
}

func mapDecisionToStatus(decision Decision) (string, string) {
	switch decision {
	case DecisionAccepted:
		return "accepted", ""
	case DecisionAcceptedWithWarnings:
		return "accepted_with_warnings", ""
	case DecisionRevisionRequired:
		return "revision_required", ""
	case DecisionBlocked:
		return "revision_required", "decision preserved as blocked"
	case DecisionManualReviewRequired:
		return "revision_required", "decision preserved as manual_review_required"
	default:
		return "revision_required", "decision preserved as revision_required"
	}
}

func decisionEventMessage(decision Decision, notes string, noteSuffix string) string {
	var base string
	switch decision {
	case DecisionAccepted:
		base = "Audit approved"
	case DecisionAcceptedWithWarnings:
		base = "Audit approved with warnings"
	case DecisionRevisionRequired:
		base = "Audit revision requested"
	case DecisionBlocked:
		base = "Audit blocked; revision required"
	case DecisionManualReviewRequired:
		base = "Audit manual review required; revision required"
	default:
		base = "Audit decision submitted"
	}
	if noteSuffix != "" {
		base = fmt.Sprintf("%s (%s)", base, noteSuffix)
	}
	if strings.TrimSpace(notes) != "" {
		base = fmt.Sprintf("%s: %s", base, notes)
	}
	return base
}
