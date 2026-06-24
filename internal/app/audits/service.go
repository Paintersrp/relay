package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appplans "relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/auditor"
	"relay/internal/executor"
	"relay/internal/store"
	"relay/internal/validationrunner"
)

type Service struct {
	store     *store.Store
	lifecycle *appplans.RunLifecycleService
}

func NewService(st *store.Store, lifecycle *appplans.RunLifecycleService) *Service {
	if lifecycle == nil && st != nil {
		lifecycle = appplans.NewRunLifecycleService(st)
	}
	return &Service{store: st, lifecycle: lifecycle}
}

func (s *Service) CreateLocalAudit(ctx context.Context, input LocalAuditInput) (*LocalAuditResult, error) {
	return auditor.NewLocalAuditService(s.store).Create(ctx, input)
}

func (s *Service) GetLocalAudit(ctx context.Context, auditID string) (*LocalAuditRecordResult, error) {
	return auditor.NewLocalAuditService(s.store).Get(ctx, auditID)
}

func (s *Service) ListProjectLocalAudits(ctx context.Context, projectID, mode string, limit int64) ([]LocalAuditRecordResult, error) {
	return auditor.NewLocalAuditService(s.store).ListByProject(ctx, projectID, mode, limit)
}

func (s *Service) GetAuditStatus(ctx context.Context, runID int64) (*AuditStatus, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return nil, err
	}

	idStr := strconv.FormatInt(runID, 10)
	artifactsByRun, err := s.store.ListArtifactsByRun(run.ID)
	if err != nil {
		return nil, err
	}

	var evidenceManifestArtifact *AuditArtifact
	var generatedAuditPacketArtifact *AuditArtifact
	var manualAuditPacketArtifact *AuditArtifact
	var decisionArtifact *AuditArtifact

	for _, art := range artifactsByRun {
		aa := buildAuditArtifact(idStr, art)
		switch art.Kind {
		case "audit_evidence_manifest_json":
			if evidenceManifestArtifact == nil {
				copy := aa
				evidenceManifestArtifact = &copy
			}
		case "audit_decision_json":
			if decisionArtifact == nil {
				copy := aa
				decisionArtifact = &copy
			}
		case "audit_packet":
			if strings.Contains(strings.ToLower(filepath.Base(art.Path)), "manual") {
				if manualAuditPacketArtifact == nil {
					copy := aa
					manualAuditPacketArtifact = &copy
				}
			} else if generatedAuditPacketArtifact == nil {
				copy := aa
				generatedAuditPacketArtifact = &copy
			}
		}
	}

	manifest := readAuditEvidenceManifest(artifactsByRun, "audit_evidence_manifest_json")
	decisionRecord := readAuditDecisionRecord(artifactsByRun, "audit_decision_json")

	valSvc := validationrunner.NewService(s.store)
	requiredValidation, _ := valSvc.RequiredCommandsInPacket(run.ID)
	hasFinalValidationEvidence := valSvc.HasValidationArtifacts(run.ID)
	hasAcceptanceArtifact := hasArtifactKind(artifactsByRun, "validation_failure_acceptance_json")
	validationAllowsAudit := hasFinalValidationEvidence &&
		(run.Status == "validation_passed" || (run.Status == "validation_failed_accepted" && hasAcceptanceArtifact))

	canGenerateAudit := false
	switch run.Status {
	case executor.StatusExecutorDone, executor.StatusExecutorBlocked:
		canGenerateAudit = !requiredValidation || validationAllowsAudit
	case "validation_passed", "validation_failed_accepted":
		canGenerateAudit = validationAllowsAudit
	}

	canSubmitDecision := run.Status == "audit_ready" || run.Status == "audit_ready_for_review"
	canApprove := canSubmitDecision
	canRequestRevision := canSubmitDecision
	canCloseRun := run.Status == "accepted" || run.Status == "accepted_with_warnings"

	blockers := make([]string, 0)
	warnings := make([]string, 0)
	revisionRequirements := make([]string, 0)

	if manifest != nil {
		for _, warning := range manifest.Warnings {
			warnings = append(warnings, warning.Message)
			if warning.Severity == auditor.SeverityBlocker || warning.Severity == auditor.SeverityError {
				blockers = append(blockers, warning.Message)
			}
		}
		for _, requirement := range manifest.RevisionRequirements {
			revisionRequirements = append(revisionRequirements, requirement.Reason)
		}
	}

	switch run.Status {
	case "local_validation_running":
		blockers = append(blockers, "Local validation is still running.")
	case executor.StatusExecutorDone, executor.StatusExecutorBlocked:
		if requiredValidation && !hasFinalValidationEvidence {
			blockers = append(blockers, "Audit generation requires existing validation artifacts. Run validation explicitly via POST /api/runs/"+idStr+"/validate before generating audit.")
		}
	case "validation_failed":
		blockers = append(blockers, "Validation failed. Accept the failed validation with a reason or rerun validation before generating audit.")
	case "revision_required":
		blockers = append(blockers, "Revision is required before audit closeout can continue.")
	}

	if decisionRecord != nil && strings.TrimSpace(decisionRecord.Notes) != "" &&
		(decisionRecord.Decision == auditor.DecisionRevisionRequired ||
			decisionRecord.Decision == auditor.DecisionBlocked ||
			decisionRecord.Decision == auditor.DecisionManualReviewRequired) {
		revisionRequirements = append(revisionRequirements, decisionRecord.Notes)
	}

	auditState := "not_ready"
	switch run.Status {
	case executor.StatusExecutorDone, executor.StatusExecutorBlocked, "validation_passed", "validation_failed_accepted":
		if canGenerateAudit {
			auditState = "candidate"
		}
	case "audit_ready", "audit_ready_for_review":
		if manualAuditPacketArtifact != nil || decisionArtifact != nil {
			auditState = "decision_submitted"
		} else {
			auditState = "ready"
		}
	case "revision_required":
		auditState = "revision_required"
	case "accepted", "accepted_with_warnings":
		auditState = "accepted"
	case "completed":
		auditState = "completed"
	}

	return &AuditStatus{
		RunID:                        idStr,
		RunStatus:                    run.Status,
		AuditState:                   auditState,
		CanGenerateAudit:             canGenerateAudit,
		CanSubmitDecision:            canSubmitDecision,
		CanApprove:                   canApprove,
		CanRequestRevision:           canRequestRevision,
		CanCloseRun:                  canCloseRun,
		EvidenceManifestArtifact:     evidenceManifestArtifact,
		GeneratedAuditPacketArtifact: generatedAuditPacketArtifact,
		ManualAuditPacketArtifact:    manualAuditPacketArtifact,
		DecisionArtifact:             decisionArtifact,
		Blockers:                     uniqueStrings(blockers),
		Warnings:                     uniqueStrings(warnings),
		RevisionRequirements:         uniqueStrings(revisionRequirements),
		LocalOnly:                    true,
	}, nil
}

func (s *Service) GenerateAudit(ctx context.Context, runID int64) (*auditor.GeneratedAudit, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return nil, err
	}

	if run.Status == executor.StatusExecutorDone || run.Status == executor.StatusExecutorBlocked {
		valSvc := validationrunner.NewService(s.store)
		required, _ := valSvc.RequiredCommandsInPacket(runID)
		if required && !valSvc.HasValidationArtifacts(runID) {
			return nil, &AuditGenerationConflictError{
				Message: fmt.Sprintf("Audit generation requires existing validation artifacts. Run validation explicitly via POST /api/runs/%d/validate before generating audit.", runID),
			}
		}
	}

	return auditor.NewService(s.store).Generate(runID)
}

func (s *Service) SubmitAuditPacket(ctx context.Context, input SubmitAuditPacketInput) (*auditor.DecisionResult, error) {
	return auditor.NewSubmissionService(s.store).SubmitDecision(auditor.DecisionSubmission{
		RunID:               input.RunID,
		AuditPacketMarkdown: input.AuditPacketMarkdown,
		Decision:            input.Decision,
		Notes:               input.Notes,
		Source:              "api",
	})
}

func (s *Service) SubmitAuditDecision(ctx context.Context, input AuditDecisionInput) (*auditor.DecisionResult, error) {
	return auditor.NewSubmissionService(s.store).SubmitDecision(auditor.DecisionSubmission{
		RunID:    input.RunID,
		Decision: input.Decision,
		Notes:    input.Notes,
		Source:   "api",
	})
}

func (s *Service) RequestRevision(ctx context.Context, input RevisionInput) (*auditor.DecisionResult, error) {
	notes := strings.TrimSpace(input.Reason)
	if strings.TrimSpace(input.Notes) != "" {
		if notes != "" {
			notes += " (" + strings.TrimSpace(input.Notes) + ")"
		} else {
			notes = strings.TrimSpace(input.Notes)
		}
	}

	return auditor.NewSubmissionService(s.store).SubmitDecision(auditor.DecisionSubmission{
		RunID:    input.RunID,
		Decision: auditor.DecisionRevisionRequired,
		Notes:    notes,
		Source:   "api",
	})
}

func (s *Service) PrepareCommitMessage(ctx context.Context, runID int64) (*CommitMessageResult, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return nil, err
	}

	if run.Status != "accepted" && run.Status != "accepted_with_warnings" {
		return nil, fmt.Errorf("run status is %q, must be accepted or accepted_with_warnings to prepare commit message", run.Status)
	}

	storeArts, _ := s.store.ListArtifactsByRun(runID)

	changedFiles := "No changed files detected."
	for _, art := range storeArts {
		if art.Kind == "git_diff_name_status" || art.Kind == "git_diff_stat" || art.Kind == "git_status" {
			if data, readErr := os.ReadFile(art.Path); readErr == nil {
				changedFiles = string(data)
				break
			}
		}
	}

	title := run.Title
	if title == "" {
		title = "Untitled Run"
	}

	msgContent := fmt.Sprintf(`Commit Title: feat: %s

Commit Body:

%s

---
Prepared by Relay — review before committing.
`, title, changedFiles)

	path, err := artifacts.Write(runID, "commit_message_text", "commit_message.txt", []byte(msgContent))
	if err != nil {
		return nil, fmt.Errorf("failed to write commit message artifact: %w", err)
	}

	_, _ = s.store.CreateArtifact(runID, "commit_message_text", path, "text/plain")
	_, _ = s.store.CreateEvent(runID, "info", "Commit message prepared")

	return &CommitMessageResult{
		RunID:         runID,
		CommitMessage: msgContent,
		ArtifactPath:  path,
		ArtifactKind:  "commit_message_text",
	}, nil
}

func (s *Service) CloseRun(ctx context.Context, runID int64) (*CloseRunResult, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return nil, err
	}

	if run.Status != "accepted" && run.Status != "accepted_with_warnings" {
		return nil, fmt.Errorf("run status is %q, must be accepted or accepted_with_warnings to close", run.Status)
	}

	updatedRun, err := s.store.UpdateRunStatus(runID, "completed")
	if err != nil {
		return nil, fmt.Errorf("failed to close run: %w", err)
	}

	if err := s.lifecycle.SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return nil, fmt.Errorf("failed to update associated pass status: %w", err)
	}

	_, _ = s.store.CreateEvent(runID, "status_change", "Run closed")

	return &CloseRunResult{
		Run:       updatedRun,
		UpdatedAt: time.Now().UTC(),
	}, nil
}

func buildAuditArtifact(idStr string, art store.Artifact) AuditArtifact {
	filename := filepath.Base(art.Path)
	sizeHint := getFileSizeHint(art.Path)

	preview := ""
	if art.MimeType == "text/plain" || art.MimeType == "application/json" || art.MimeType == "text/markdown" {
		if data, err := os.ReadFile(art.Path); err == nil {
			if len(data) > 500 {
				preview = string(data[:500]) + "..."
			} else {
				preview = string(data)
			}
		}
	}

	return AuditArtifact{
		ID:          strconv.FormatInt(art.ID, 10),
		Label:       mapArtifactKindToLabel(art.Kind),
		Path:        fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
		Kind:        mapArtifactKindToType(art.Kind),
		StorageKind: art.Kind,
		ContentURL:  fmt.Sprintf("/api/runs/%s/artifacts/%s", idStr, art.Kind),
		SizeHint:    sizeHint,
		CreatedAt:   parseAndFormatTime(art.CreatedAt),
		Status:      "ready",
		Filename:    filename,
		Preview:     preview,
	}
}

func getFileSizeHint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	size := info.Size()
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	} else {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

func parseAndFormatTime(value string) string {
	if value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return t.Format(time.RFC3339)
}

func mapArtifactKindToType(kind string) string {
	switch kind {
	case "original_handoff", "planner_handoff", "parsed_frontmatter", "run_config",
		"planner_handoff_provenance_json", "context_packet_json", "context_packet_markdown",
		"repaired_packet":
		return "handoff"
	case "agent_prompt", "repair_prompt":
		return "prompt"
	case "agent_result_raw", "agent_result_json", "opencode_stdout", "opencode_stderr",
		"opencode_combined", "repair_output":
		return "result"
	case "validation_stdout", "validation_stderr", "validation_run_json",
		"validation_failure_acceptance_json", "intake_validation_report",
		"context_coverage_report_json", "opencode_dry_run_json", "opencode_cli_check_json",
		"repair_request_json", "repair_validation_report":
		return "validation"
	case "audit_handoff", "audit_patch", "audit_input_summary", "audit_evidence_manifest_json",
		"audit_decision_json", "audit_revision", "commit_message_text", "commit_suggestion_json",
		"git_commit_suggestion":
		return "audit"
	case "git_status", "git_diff_stat", "git_diff_patch", "git_diff_name_status":
		return "diff"
	default:
		lower := strings.ToLower(kind)
		if strings.Contains(lower, "diff") || strings.Contains(lower, "patch") || strings.Contains(lower, "status") {
			return "diff"
		}
		if strings.Contains(lower, "validation") || strings.Contains(lower, "check") {
			return "validation"
		}
		if strings.Contains(lower, "prompt") {
			return "prompt"
		}
		if strings.Contains(lower, "handoff") {
			return "handoff"
		}
		if strings.Contains(lower, "audit") {
			return "audit"
		}
		return "result"
	}
}

func mapArtifactKindToLabel(kind string) string {
	switch kind {
	case "audit_evidence_manifest_json":
		return "Audit Evidence Manifest (JSON)"
	case "audit_decision_json":
		return "Audit Decision (JSON)"
	case "audit_packet":
		return "Audit Packet"
	case "commit_message_text":
		return "Commit Message (Text)"
	default:
		return strings.Title(strings.ReplaceAll(kind, "_", " "))
	}
}

func hasArtifactKind(artifactsByRun []store.Artifact, kind string) bool {
	for _, art := range artifactsByRun {
		if art.Kind == kind {
			return true
		}
	}
	return false
}

func readAuditEvidenceManifest(artifactsByRun []store.Artifact, kind string) *auditor.AuditEvidenceManifest {
	for _, art := range artifactsByRun {
		if art.Kind != kind {
			continue
		}
		data, err := os.ReadFile(art.Path)
		if err != nil {
			return nil
		}
		var manifest auditor.AuditEvidenceManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil
		}
		return &manifest
	}
	return nil
}

func readAuditDecisionRecord(artifactsByRun []store.Artifact, kind string) *auditor.AuditDecisionRecord {
	for _, art := range artifactsByRun {
		if art.Kind != kind {
			continue
		}
		data, err := os.ReadFile(art.Path)
		if err != nil {
			return nil
		}
		var record auditor.AuditDecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return nil
		}
		return &record
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func IsAuditDecisionError(err error) bool {
	return errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, auditor.ErrUnsupportedDecision) ||
		errors.Is(err, auditor.ErrCompletedRun) ||
		errors.Is(err, auditor.ErrAuditDecisionNotReady) ||
		strings.Contains(err.Error(), "audit_packet_markdown is required")
}
