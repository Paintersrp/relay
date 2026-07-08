package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

type canonicalPlanAuditModel struct {
	Passes []json.RawMessage `json:"passes"`
}

type canonicalPassNumber struct {
	Number int64 `json:"number"`
}

func buildWorkflowAuditPacket(
	ctx context.Context,
	store *workflowstore.Store,
	run workflowstore.Run,
	packetID string,
	attempt workflowstore.ExecutionAttempt,
	commit workflowrepos.AuditCommitEvidence,
	diffArtifact workflowstore.Artifact,
) ([]byte, error) {
	runArtifacts, err := store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	executionSpecArtifact, err := requireArtifactKind(runArtifacts, "execution_spec")
	if err != nil {
		return nil, err
	}
	executorBriefArtifact, err := requireArtifactKind(runArtifacts, "executor_brief")
	if err != nil {
		return nil, err
	}
	executionSpec, err := readWorkflowArtifact(store, executionSpecArtifact, MaxWorkflowAuditSourceBytes)
	if err != nil {
		return nil, fmt.Errorf("read canonical Execution Spec: %w", err)
	}
	executorBrief, err := readWorkflowArtifact(store, executorBriefArtifact, MaxWorkflowAuditSourceBytes)
	if err != nil {
		return nil, fmt.Errorf("read rendered Executor Brief: %w", err)
	}
	selectedPass, planID, passID, passNumber, err := selectedPassAuthority(ctx, store, run)
	if err != nil {
		return nil, err
	}
	remediatesRunID := ""
	if run.RemediatesRunRowID.Valid {
		original, err := store.GetRunByRowID(ctx, run.RemediatesRunRowID.Int64)
		if err != nil {
			return nil, fmt.Errorf("load remediated Run: %w", err)
		}
		remediatesRunID = original.RunID
	}
	attemptArtifacts, err := store.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
	if err != nil {
		return nil, err
	}
	evidence, err := workflowAuditEvidence(store, attemptArtifacts)
	if err != nil {
		return nil, err
	}
	auditResult := workflowAuditAttemptResult(attempt.ResultJSON)
	blockers := workflowAuditBlockers(auditResult)
	var executionSpecJSON json.RawMessage
	if err := json.Unmarshal(executionSpec, &executionSpecJSON); err != nil {
		return nil, fmt.Errorf("decode canonical Execution Spec for audit packet: %w", err)
	}
	managedContext, err := workflowAuditManagedContext(selectedPass)
	if err != nil {
		return nil, err
	}
	packet := WorkflowAuditPacket{
		SchemaVersion: WorkflowAuditPacketSchemaVersion,
		Run: WorkflowAuditRunAuthority{
			RunID:           run.RunID,
			FeatureSlug:     run.FeatureSlug,
			RepoTarget:      run.RepoTarget,
			Branch:          run.Branch,
			BaseCommit:      run.BaseCommit,
			CanonicalSHA256: run.CanonicalSHA256,
			PlanID:          planID,
			PassID:          passID,
			PassNumber:      passNumber,
			RemediatesRunID: remediatesRunID,
		},
		Repository: WorkflowAuditRepository{RepoTarget: run.RepoTarget, Branch: commit.Branch, BaseCommit: commit.BaseCommit, AuditedCommit: commit.AuditedCommit},
		Authority: WorkflowAuditAuthority{
			ExecutionSpec: WorkflowAuditEmbeddedJSON{Filename: "execution-spec.json", SHA256: executionSpecArtifact.SHA256, Content: executionSpecJSON},
			ExecutorBrief: WorkflowAuditEmbeddedMarkdown{Filename: "executor-brief.md", SHA256: executorBriefArtifact.SHA256, Content: string(executorBrief)},
			ManagedContext: managedContext,
		},
		Execution: WorkflowAuditExecution{
			Status:                   attempt.Status,
			CommittedSHA:             commit.AuditedCommit,
			CompletionSummary:        workflowAuditCompletionSummary(auditResult),
			BlockersOrIncompleteWork: blockers,
			ReportedChangedFiles:     append([]string(nil), commit.ChangedFiles...),
			Attempt:                  WorkflowAuditAttemptAuthority{AttemptID: attempt.AttemptID, AttemptNumber: attempt.AttemptNumber, Adapter: attempt.Adapter, Model: attempt.Model, Status: attempt.Status, Result: auditResult, StartedAt: nullableString(attempt.StartedAt), FinishedAt: nullableString(attempt.FinishedAt)},
		},
		ChangedFiles:        workflowAuditChangedFiles(commit),
		RelevantSourcePaths: workflowAuditRelevantSourcePaths(commit, selectedPass),
		Validation:          workflowAuditValidation(evidence),
		Artifacts:           workflowAuditArtifacts(evidence, diffArtifact),
	}
	if remediatesRunID != "" {
		packet.RemediationContext = &WorkflowAuditRemediationContext{RemediatesRunID: remediatesRunID}
	}
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > MaxWorkflowAuditPacketBytes {
		return nil, ErrWorkflowAuditPacketTooLarge
	}
	return data, nil
}

func workflowAuditManagedContext(selectedPass *WorkflowAuditPassAuthority) (*WorkflowAuditManagedContext, error) {
	if selectedPass == nil {
		return nil, nil
	}
	var pass struct {
		Goal               string          `json:"goal"`
		Context            string          `json:"context"`
		Scope              json.RawMessage `json:"scope"`
		RepoTarget         string          `json:"repo_target"`
		CompletionCriteria []string        `json:"completion_criteria"`
	}
	if err := json.Unmarshal(selectedPass.CanonicalPass, &pass); err != nil {
		return nil, fmt.Errorf("decode selected pass for managed audit context: %w", err)
	}
	repositoryTarget, _ := json.Marshal(map[string]string{"repo_target": pass.RepoTarget})
	return &WorkflowAuditManagedContext{PlanGoal: pass.Goal, PlanContext: pass.Context, PlanScope: append(json.RawMessage(nil), pass.Scope...), RepositoryTarget: repositoryTarget, SelectedPass: append(json.RawMessage(nil), selectedPass.CanonicalPass...), PlanCompletionCriteria: append([]string(nil), pass.CompletionCriteria...)}, nil
}

func workflowAuditCompletionSummary(result WorkflowAuditAttemptResult) string {
	for _, value := range []string{result.NormalizedStatus, result.BlockerText, result.Error} {
		if strings.TrimSpace(value) != "" {
			return executor.RedactSensitiveText(strings.TrimSpace(value))
		}
	}
	return "Execution attempt completed with status recorded in Relay."
}

func workflowAuditValidation(evidence []WorkflowAuditEvidenceItem) []WorkflowAuditValidationResult {
	out := make([]WorkflowAuditValidationResult, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, WorkflowAuditValidationResult{Command: item.Kind, Status: "captured", ConciseResult: "Evidence captured for audit.", ArtifactReference: item.ArtifactID})
	}
	return out
}

func workflowAuditArtifacts(evidence []WorkflowAuditEvidenceItem, diffArtifact workflowstore.Artifact) []WorkflowAuditPacketArtifact {
	out := make([]WorkflowAuditPacketArtifact, 0, len(evidence)+1)
	out = append(out, WorkflowAuditPacketArtifact{ArtifactReference: "unified_diff", ArtifactType: "unified_diff", SHA256: diffArtifact.SHA256, Description: "Complete unified diff for the audited commit range.", Kind: diffArtifact.Kind, MediaType: diffArtifact.MediaType, SizeBytes: diffArtifact.SizeBytes})
	for _, item := range evidence {
		out = append(out, WorkflowAuditPacketArtifact{ArtifactReference: item.ArtifactID, ArtifactType: item.Kind, SHA256: item.SHA256, Description: "Execution evidence captured by Relay.", Kind: item.Kind, MediaType: item.MediaType, SizeBytes: item.SizeBytes})
	}
	return out
}

func workflowAuditChangedFiles(commit workflowrepos.AuditCommitEvidence) []WorkflowAuditChangedFile {
	out := make([]WorkflowAuditChangedFile, 0, len(commit.ChangedFiles))
	for _, path := range commit.ChangedFiles {
		out = append(out, WorkflowAuditChangedFile{Path: path, ChangeType: "modified", Additions: 0, Deletions: 0})
	}
	return out
}

func workflowAuditRelevantSourcePaths(commit workflowrepos.AuditCommitEvidence, selectedPass *WorkflowAuditPassAuthority) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range commit.ChangedFiles {
		add(value)
	}
	if selectedPass != nil {
		var pass struct {
			SourceTargets []struct {
				Path string `json:"path"`
			} `json:"source_targets"`
		}
		if json.Unmarshal(selectedPass.CanonicalPass, &pass) == nil {
			for _, target := range pass.SourceTargets {
				add(target.Path)
			}
		}
	}
	return out
}

func selectedPassAuthority(ctx context.Context, store *workflowstore.Store, run workflowstore.Run) (*WorkflowAuditPassAuthority, string, string, int64, error) {
	if !run.PlanRowID.Valid || !run.PlanPassRowID.Valid {
		return nil, "", "", 0, nil
	}
	plan, err := store.GetPlanByRowID(ctx, run.PlanRowID.Int64)
	if err != nil {
		return nil, "", "", 0, err
	}
	pass, err := store.GetPlanPassByRowID(ctx, run.PlanPassRowID.Int64)
	if err != nil {
		return nil, "", "", 0, err
	}
	planArtifacts, err := store.ListArtifactsByPlan(ctx, plan.ID)
	if err != nil {
		return nil, "", "", 0, err
	}
	canonicalPlanArtifact, err := requireArtifactKind(planArtifacts, "canonical_plan")
	if err != nil {
		return nil, "", "", 0, err
	}
	canonicalPlan, err := readWorkflowArtifact(store, canonicalPlanArtifact, MaxWorkflowAuditSourceBytes)
	if err != nil {
		return nil, "", "", 0, err
	}
	var model canonicalPlanAuditModel
	if err := json.Unmarshal(canonicalPlan, &model); err != nil {
		return nil, "", "", 0, fmt.Errorf("decode canonical Plan for audit: %w", err)
	}
	for _, raw := range model.Passes {
		var identity canonicalPassNumber
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, "", "", 0, fmt.Errorf("decode canonical Plan pass identity: %w", err)
		}
		if identity.Number == pass.PassNumber {
			return &WorkflowAuditPassAuthority{
				PlanID:              plan.PlanID,
				PlanCanonicalSHA256: plan.CanonicalSHA256,
				PassID:              pass.PassID,
				PassNumber:          pass.PassNumber,
				PassName:            pass.Name,
				CanonicalPass:       append(json.RawMessage(nil), raw...),
			}, plan.PlanID, pass.PassID, pass.PassNumber, nil
		}
	}
	return nil, "", "", 0, fmt.Errorf("canonical Plan does not contain managed pass %d", pass.PassNumber)
}

func workflowAuditEvidence(_ *workflowstore.Store, artifacts []workflowstore.Artifact) ([]WorkflowAuditEvidenceItem, error) {
	sorted := append([]workflowstore.Artifact(nil), artifacts...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Kind == sorted[j].Kind {
			return sorted[i].ArtifactID < sorted[j].ArtifactID
		}
		return sorted[i].Kind < sorted[j].Kind
	})
	out := make([]WorkflowAuditEvidenceItem, 0, len(sorted))
	for _, artifact := range sorted {
		out = append(out, WorkflowAuditEvidenceItem{
			ArtifactID:       artifact.ArtifactID,
			Kind:             artifact.Kind,
			MediaType:        artifact.MediaType,
			SHA256:           artifact.SHA256,
			SizeBytes:        artifact.SizeBytes,
			ContentTruncated: false,
		})
	}
	return out, nil
}

func workflowAuditAttemptResult(raw string) WorkflowAuditAttemptResult {
	var source struct {
		ExitCode              int    `json:"exit_code"`
		TimedOut              bool   `json:"timed_out"`
		TerminationVerified   bool   `json:"termination_verified"`
		CleanupPending        bool   `json:"cleanup_pending"`
		PendingTerminalStatus string `json:"pending_terminal_status"`
		Error                 string `json:"error"`
		NormalizedStatus      string `json:"normalized_status"`
		BlockerText           string `json:"blocker_text"`
		BriefArtifactID       string `json:"brief_artifact_id"`
		BriefSHA256           string `json:"brief_sha256"`
		StdoutTruncated       bool   `json:"stdout_truncated"`
		StderrTruncated       bool   `json:"stderr_truncated"`
		StdoutBytes           int64  `json:"stdout_bytes"`
		StderrBytes           int64  `json:"stderr_bytes"`
	}
	if json.Unmarshal([]byte(raw), &source) != nil {
		return WorkflowAuditAttemptResult{}
	}
	return WorkflowAuditAttemptResult{
		ExitCode:              source.ExitCode,
		TimedOut:              source.TimedOut,
		TerminationVerified:   source.TerminationVerified,
		CleanupPending:        source.CleanupPending,
		PendingTerminalStatus: source.PendingTerminalStatus,
		Error:                 executor.RedactSensitiveText(source.Error),
		NormalizedStatus:      source.NormalizedStatus,
		BlockerText:           executor.RedactSensitiveText(source.BlockerText),
		BriefArtifactID:       source.BriefArtifactID,
		BriefSHA256:           source.BriefSHA256,
		StdoutTruncated:       source.StdoutTruncated,
		StderrTruncated:       source.StderrTruncated,
		StdoutBytes:           source.StdoutBytes,
		StderrBytes:           source.StderrBytes,
	}
}

func workflowAuditBlockers(result WorkflowAuditAttemptResult) []string {
	var out []string
	for _, value := range []string{result.Error, result.BlockerText} {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func requireArtifactKind(artifacts []workflowstore.Artifact, kind string) (workflowstore.Artifact, error) {
	var found workflowstore.Artifact
	count := 0
	for _, artifact := range artifacts {
		if artifact.Kind == kind {
			found = artifact
			count++
		}
	}
	if count != 1 {
		return workflowstore.Artifact{}, fmt.Errorf("expected exactly one %s artifact, found %d", kind, count)
	}
	return found, nil
}

func readWorkflowArtifact(store *workflowstore.Store, artifact workflowstore.Artifact, maxBytes int) ([]byte, error) {
	path, err := workflowArtifactPath(store, artifact)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("artifact %s exceeds %d bytes", artifact.ArtifactID, maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != artifact.SizeBytes {
		return nil, fmt.Errorf("artifact %s size does not match metadata", artifact.ArtifactID)
	}
	if sha256HexBytes(data) != artifact.SHA256 {
		return nil, fmt.Errorf("artifact %s SHA-256 does not match metadata", artifact.ArtifactID)
	}
	return data, nil
}

func readWorkflowArtifactTail(store *workflowstore.Store, artifact workflowstore.Artifact, maxBytes int) ([]byte, bool, error) {
	path, err := workflowArtifactPath(store, artifact)
	if err != nil {
		return nil, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.Size() <= int64(maxBytes) {
		data, err := os.ReadFile(path)
		return data, false, err
	}
	if _, err := file.Seek(-int64(maxBytes), 2); err != nil {
		return nil, false, err
	}
	data := make([]byte, maxBytes)
	count, err := file.Read(data)
	if err != nil {
		return nil, false, err
	}
	return data[:count], true, nil
}

func workflowArtifactPath(store *workflowstore.Store, artifact workflowstore.Artifact) (string, error) {
	root := store.ArtifactStore().Root()
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes workflow artifact root")
	}
	return absolute, nil
}

func nullableString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
