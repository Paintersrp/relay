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
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

type canonicalPlanAuditModel struct {
	Goal               string            `json:"goal"`
	Context            string            `json:"context"`
	Scope              json.RawMessage   `json:"scope"`
	RepoTargets        []json.RawMessage `json:"repo_targets"`
	Passes             []json.RawMessage `json:"passes"`
	CompletionCriteria []string          `json:"completion_criteria"`
}

type canonicalPassNumber struct {
	Number int64 `json:"number"`
}

func buildWorkflowAuditPacket(
	ctx context.Context,
	store *workflowstore.Store,
	run workflowstore.Run,
	packetID string,
	implementation WorkflowImplementationEvidence,
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
	selectedPass, plan, pass, planModel, matchedRepoTarget, err := selectedPassAuthority(ctx, store, run)
	if err != nil {
		return nil, err
	}
	var remediatedRunRowID int64
	var originalRunID string
	if run.RemediatesRunRowID.Valid {
		original, err := store.GetRunByRowID(ctx, run.RemediatesRunRowID.Int64)
		if err != nil {
			return nil, fmt.Errorf("load remediated Run: %w", err)
		}
		remediatedRunRowID = original.ID
		originalRunID = original.RunID
	}
	var userIntent string
	if (!run.PlanRowID.Valid || !run.PlanPassRowID.Valid) && !run.RemediatesRunRowID.Valid {
		var specGoal struct {
			Goal string `json:"goal"`
		}
		_ = json.Unmarshal(executionSpec, &specGoal)
		userIntent = strings.TrimSpace(specGoal.Goal)
		if userIntent == "" {
			userIntent = firstNonblankLine(string(executorBrief))
		}
	}
	evidence, err := workflowAuditEvidence(store, implementation.Artifacts)
	if err != nil {
		return nil, err
	}
	auditResult := WorkflowAuditAttemptResult{}
	if implementation.Executor != nil {
		auditResult = implementation.Executor.AttemptResult
	}
	blockers := workflowAuditBlockers(auditResult)
	if blockers == nil {
		blockers = []string{}
	}
	var executionSpecJSON json.RawMessage
	if err := json.Unmarshal(executionSpec, &executionSpecJSON); err != nil {
		return nil, fmt.Errorf("decode canonical Execution Spec for audit packet: %w", err)
	}
	var executionRoot map[string]json.RawMessage
	if err := json.Unmarshal(executionSpec, &executionRoot); err != nil {
		return nil, fmt.Errorf("decode canonical Execution Spec for audit packet: %w", err)
	}
	executionProjection := speccompiler.ExecutionProjection{}
	if _, hasSteps := executionRoot["steps"]; hasSteps {
		compiled, executionDocument := speccompiler.CompileExecutionSpec(filepath.Base(executionSpecArtifact.RelativePath), executionSpec)
		if len(compiled.Errors) != 0 || executionDocument == nil {
			if len(compiled.Errors) == 0 {
				return nil, fmt.Errorf("project canonical Execution Spec metadata: execution document is unavailable")
			}
			first := compiled.Errors[0]
			return nil, fmt.Errorf("project canonical Execution Spec metadata: %s: %s", first.Code, first.Message)
		}
		var projectionDiagnostics []speccompiler.Diagnostic
		executionProjection, projectionDiagnostics = speccompiler.ProjectExecutionSpec(executionDocument)
		if len(projectionDiagnostics) != 0 {
			first := projectionDiagnostics[0]
			return nil, fmt.Errorf("project canonical Execution Spec metadata: %s: %s", first.Code, first.Message)
		}
	}
	validation, err := mapWorkflowAuditValidation(implementation, executionProjection.ValidationCommands)
	if err != nil {
		return nil, err
	}
	managedContext, err := workflowAuditManagedContext(planModel, matchedRepoTarget, selectedPass)
	if err != nil {
		return nil, err
	}

	runAuthority := WorkflowAuditRunAuthority{
		RunID:           run.ID,
		FeatureSlug:     run.FeatureSlug,
		RepoTarget:      run.RepoTarget,
		Branch:          run.Branch,
		BaseCommit:      run.BaseCommit,
		CanonicalSHA256: run.CanonicalSHA256,
	}
	if plan != nil {
		runAuthority.PlanID = plan.ID
	}
	if pass != nil {
		runAuthority.PassID = pass.ID
		runAuthority.PassNumber = pass.PassNumber
	}
	if run.RemediatesRunRowID.Valid {
		runAuthority.RemediatesRunID = remediatedRunRowID
	}
	if userIntent != "" {
		runAuthority.UserIntent = userIntent
	}

	reportedFiles := append([]string(nil), commit.ChangedFiles...)
	if reportedFiles == nil {
		reportedFiles = []string{}
	}

	packet := WorkflowAuditPacket{
		SchemaVersion: WorkflowAuditPacketSchemaVersion,
		Run:           runAuthority,
		Repository:    WorkflowAuditRepository{RepoTarget: run.RepoTarget, Branch: commit.Branch, BaseCommit: commit.BaseCommit, AuditedCommit: commit.AuditedCommit},
		Authority: WorkflowAuditAuthority{
			ExecutionSpec:  WorkflowAuditEmbeddedJSON{Filename: filepath.Base(executionSpecArtifact.RelativePath), SHA256: executionSpecArtifact.SHA256, Content: executionSpecJSON},
			ExecutorBrief:  WorkflowAuditEmbeddedMarkdown{Filename: filepath.Base(executorBriefArtifact.RelativePath), SHA256: executorBriefArtifact.SHA256, Content: string(executorBrief)},
			ManagedContext: managedContext,
		},
		Execution:           workflowAuditExecution(implementation, commit.AuditedCommit, auditResult, blockers, reportedFiles),
		ChangedFiles:        workflowAuditChangedFiles(commit),
		RelevantSourcePaths: workflowAuditRelevantSourcePaths(commit, selectedPass),
		Validation:          validation,
		Artifacts:           workflowAuditArtifacts(evidence, implementation, diffArtifact),
	}
	if run.RemediatesRunRowID.Valid {
		var specFindings struct {
			MaterialFindings []WorkflowAuditMaterialFinding `json:"material_findings"`
		}
		var materialFindings []WorkflowAuditMaterialFinding
		if json.Unmarshal(executionSpec, &specFindings) == nil && len(specFindings.MaterialFindings) > 0 {
			materialFindings = specFindings.MaterialFindings
		} else {
			materialFindings = []WorkflowAuditMaterialFinding{
				{
					Source:              "both",
					Summary:             fmt.Sprintf("Remediation of run %s", originalRunID),
					Evidence:            fmt.Sprintf("Remediated run ID: %s, execution spec: %s", originalRunID, executionSpecArtifact.ArtifactID),
					RequiredRemediation: "Please review the embedded execution_spec and changed_files to verify.",
				},
			}
		}
		packet.RemediationContext = &WorkflowAuditRemediationContext{
			RemediatedRunID:  remediatedRunRowID,
			MaterialFindings: materialFindings,
		}
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

func firstNonblankLine(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return "Execute run tasks."
}

func workflowAuditManagedContext(
	model *canonicalPlanAuditModel,
	matchedRepoTarget json.RawMessage,
	selectedPass *WorkflowAuditPassAuthority,
) (*WorkflowAuditManagedContext, error) {
	if selectedPass == nil || model == nil {
		return nil, nil
	}
	return &WorkflowAuditManagedContext{
		PlanGoal:               model.Goal,
		PlanContext:            model.Context,
		PlanScope:              append(json.RawMessage(nil), model.Scope...),
		RepositoryTarget:       append(json.RawMessage(nil), matchedRepoTarget...),
		SelectedPass:           append(json.RawMessage(nil), selectedPass.CanonicalPass...),
		PlanCompletionCriteria: append([]string(nil), model.CompletionCriteria...),
	}, nil
}

func workflowAuditExecution(implementation WorkflowImplementationEvidence, committedSHA string, auditResult WorkflowAuditAttemptResult, blockers []string, reportedFiles []string) WorkflowAuditExecution {
	out := WorkflowAuditExecution{
		ActorKind:                implementation.ActorKind,
		Status:                   implementation.ActorKind,
		CommittedSHA:             committedSHA,
		CompletionSummary:        workflowAuditCompletionSummary(auditResult),
		BlockersOrIncompleteWork: blockers,
		ReportedChangedFiles:     reportedFiles,
	}
	if implementation.Applier != nil {
		out.Applier = &WorkflowAuditApplierEvidence{
			Outcome:                               implementation.Applier.Result.Outcome,
			ImplementationResultArtifactReference: implementation.Applier.ImplementationResultArtifact.ArtifactID,
			LedgerArtifactReference:               implementation.Applier.LedgerArtifact.ArtifactID,
			ChangedFiles:                          implementation.Applier.Result.ChangedFiles,
			ResidualOperationIDs:                  append(append([]string(nil), implementation.Applier.Result.ResidualOperationIDs...), implementation.Applier.Result.ResidualOperations...),
			FailureClass:                          implementation.Applier.Result.FailureClass,
			FailureReason:                         implementation.Applier.Result.FailureReason,
		}
		if out.CompletionSummary == "Execution attempt completed with status recorded in Relay." {
			out.CompletionSummary = "Deterministic applier completed approved implementation evidence."
		}
	}
	if implementation.Executor != nil {
		executor := &WorkflowAuditExecutorEvidence{
			AttemptID:                       implementation.Executor.Attempt.AttemptID,
			AttemptNumber:                   implementation.Executor.Attempt.AttemptNumber,
			Adapter:                         implementation.Executor.Attempt.Adapter,
			Model:                           implementation.Executor.Attempt.Model,
			Status:                          implementation.Executor.Attempt.Status,
			Result:                          auditResult,
			EffectiveBriefArtifactReference: implementation.Executor.EffectiveBriefArtifact.ArtifactID,
			EffectiveBriefSHA256:            implementation.Executor.EffectiveBriefArtifact.SHA256,
			EffectiveBriefMode:              implementation.Executor.ExecutionEvidence.EffectiveBriefMode,
		}
		if implementation.Executor.Attempt.StartedAt.Valid {
			executor.StartedAt = implementation.Executor.Attempt.StartedAt.String
		}
		if implementation.Executor.Attempt.FinishedAt.Valid {
			executor.FinishedAt = implementation.Executor.Attempt.FinishedAt.String
		}
		out.Executor = executor
		out.Status = implementation.Executor.Attempt.Status
	}
	return out
}

func workflowAuditCompletionSummary(result WorkflowAuditAttemptResult) string {
	for _, value := range []string{result.NormalizedStatus, result.BlockerText, result.Error} {
		if strings.TrimSpace(value) != "" {
			return executor.RedactSensitiveText(strings.TrimSpace(value))
		}
	}
	return "Execution attempt completed with status recorded in Relay."
}

func workflowAuditArtifacts(evidence []WorkflowAuditEvidenceItem, implementation WorkflowImplementationEvidence, diffArtifact workflowstore.Artifact) []WorkflowAuditPacketArtifact {
	out := make([]WorkflowAuditPacketArtifact, 0, len(evidence)+2)
	out = append(out, WorkflowAuditPacketArtifact{
		ArtifactReference: diffArtifact.ArtifactID,
		ArtifactType:      "unified_diff",
		SHA256:            diffArtifact.SHA256,
		Description:       "Complete unified diff for the audited commit range.",
	})
	seen := make(map[string]struct{}, len(evidence)+1)
	seen[diffArtifact.ArtifactID] = struct{}{}
	for _, item := range evidence {
		if _, duplicate := seen[item.ArtifactID]; duplicate {
			continue
		}
		seen[item.ArtifactID] = struct{}{}
		out = append(out, WorkflowAuditPacketArtifact{
			ArtifactReference: item.ArtifactID,
			ArtifactType:      item.Kind,
			SHA256:            item.SHA256,
			Description:       "Execution evidence captured by Relay.",
		})
	}
	if implementation.Executor != nil {
		effective := implementation.Executor.EffectiveBriefArtifact
		if _, duplicate := seen[effective.ArtifactID]; !duplicate {
			out = append(out, WorkflowAuditPacketArtifact{
				ArtifactReference: effective.ArtifactID,
				ArtifactType:      effective.Kind,
				SHA256:            effective.SHA256,
				Description:       "Exact effective Executor Brief used by the selected model attempt.",
			})
		}
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

func selectedPassAuthority(
	ctx context.Context,
	store *workflowstore.Store,
	run workflowstore.Run,
) (*WorkflowAuditPassAuthority, *workflowstore.Plan, *workflowstore.PlanPass, *canonicalPlanAuditModel, json.RawMessage, error) {
	if !run.PlanRowID.Valid || !run.PlanPassRowID.Valid {
		return nil, nil, nil, nil, nil, nil
	}
	plan, err := store.GetPlanByRowID(ctx, run.PlanRowID.Int64)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	pass, err := store.GetPlanPassByRowID(ctx, run.PlanPassRowID.Int64)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	planArtifacts, err := store.ListArtifactsByPlan(ctx, plan.ID)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	canonicalPlanArtifact, err := requireArtifactKind(planArtifacts, "canonical_plan")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	canonicalPlan, err := readWorkflowArtifact(store, canonicalPlanArtifact, MaxWorkflowAuditSourceBytes)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var model canonicalPlanAuditModel
	if err := json.Unmarshal(canonicalPlan, &model); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("decode canonical Plan for audit: %w", err)
	}
	for _, raw := range model.Passes {
		var identity canonicalPassNumber
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("decode canonical Plan pass identity: %w", err)
		}
		if identity.Number == pass.PassNumber {
			var passStruct struct {
				RepoTarget string `json:"repo_target"`
			}
			var matchedRepoTarget json.RawMessage
			if json.Unmarshal(raw, &passStruct) == nil {
				for _, rtRaw := range model.RepoTargets {
					var rt struct {
						RepoTarget string `json:"repo_target"`
					}
					if json.Unmarshal(rtRaw, &rt) == nil && rt.RepoTarget == passStruct.RepoTarget {
						matchedRepoTarget = rtRaw
						break
					}
				}
			}
			passAuth := &WorkflowAuditPassAuthority{
				PlanID:              plan.PlanID,
				PlanCanonicalSHA256: plan.CanonicalSHA256,
				PassID:              pass.PassID,
				PassNumber:          pass.PassNumber,
				PassName:            pass.Name,
				CanonicalPass:       append(json.RawMessage(nil), raw...),
			}
			return passAuth, &plan, &pass, &model, matchedRepoTarget, nil
		}
	}
	return nil, nil, nil, nil, nil, fmt.Errorf("canonical Plan does not contain managed pass %d", pass.PassNumber)
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
		ExitCode                 int    `json:"exit_code"`
		TimedOut                 bool   `json:"timed_out"`
		TerminationVerified      bool   `json:"termination_verified"`
		CleanupPending           bool   `json:"cleanup_pending"`
		PendingTerminalStatus    string `json:"pending_terminal_status"`
		Error                    string `json:"error"`
		NormalizedStatus         string `json:"normalized_status"`
		BlockerText              string `json:"blocker_text"`
		EffectiveBriefArtifactID string `json:"effective_brief_artifact_id"`
		EffectiveBriefSHA256     string `json:"effective_brief_sha256"`
		EffectiveBriefMode       string `json:"effective_brief_mode"`
		StdoutTruncated          bool   `json:"stdout_truncated"`
		StderrTruncated          bool   `json:"stderr_truncated"`
		StdoutBytes              int64  `json:"stdout_bytes"`
		StderrBytes              int64  `json:"stderr_bytes"`
	}
	if json.Unmarshal([]byte(raw), &source) != nil {
		return WorkflowAuditAttemptResult{}
	}
	return WorkflowAuditAttemptResult{
		ExitCode:                 source.ExitCode,
		TimedOut:                 source.TimedOut,
		TerminationVerified:      source.TerminationVerified,
		CleanupPending:           source.CleanupPending,
		PendingTerminalStatus:    source.PendingTerminalStatus,
		Error:                    executor.RedactSensitiveText(source.Error),
		NormalizedStatus:         source.NormalizedStatus,
		BlockerText:              executor.RedactSensitiveText(source.BlockerText),
		EffectiveBriefArtifactID: source.EffectiveBriefArtifactID,
		EffectiveBriefSHA256:     source.EffectiveBriefSHA256,
		EffectiveBriefMode:       source.EffectiveBriefMode,
		StdoutTruncated:          source.StdoutTruncated,
		StderrTruncated:          source.StderrTruncated,
		StdoutBytes:              source.StdoutBytes,
		StderrBytes:              source.StderrBytes,
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
