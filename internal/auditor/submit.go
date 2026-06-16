package auditor

import (
	"fmt"
	"time"

	"relay/internal/artifacts"
	"relay/internal/store"
)

type SubmissionService struct {
	store *store.Store
}

func NewSubmissionService(s *store.Store) *SubmissionService {
	return &SubmissionService{store: s}
}

func (s *SubmissionService) SubmitManual(input ManualAuditSubmission) (*GeneratedAudit, error) {
	if !SupportedDecisions[input.Decision] {
		return nil, fmt.Errorf("unsupported decision %q: must be one of accepted, accepted_with_warnings, revision_required, blocked, manual_review_required", input.Decision)
	}

	if input.AuditPacketMarkdown == "" {
		return nil, fmt.Errorf("audit_packet_markdown is required")
	}

	run, err := s.store.GetRun(input.RunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	artifactData := fmt.Sprintf("# Manual Audit Packet\n\n## Metadata\n- Run ID: %d\n- Decision: %s\n- Notes: %s\n- Submitted: %s\n\n## Packet Content\n\n%s\n",
		run.ID, input.Decision, input.Notes, time.Now().UTC().Format(time.RFC3339), input.AuditPacketMarkdown)

	path, err := artifacts.Write(run.ID, "audit_packet", "audit_packet_manual.md", []byte(artifactData))
	if err != nil {
		return nil, fmt.Errorf("write manual audit artifact: %w", err)
	}

	_, err = s.store.CreateArtifact(run.ID, "audit_packet", path, "text/markdown")
	if err != nil {
		return nil, fmt.Errorf("create artifact record: %w", err)
	}

	_, _ = s.store.CreateEvent(run.ID, "info", fmt.Sprintf("Manual audit packet submitted: decision=%s", input.Decision))

	return &GeneratedAudit{
		RunID:       run.ID,
		Status:      run.Status,
		AuditPacket: path,
		Decision:    input.Decision,
		CreatedAt:   time.Now().UTC(),
	}, nil
}
