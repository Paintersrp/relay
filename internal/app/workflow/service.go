package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidWorkflowRequest = errors.New("invalid workflow request")
	ErrArtifactIntegrity      = errors.New("artifact integrity check failed")
)

type Service struct {
	store    *workflowstore.Store
	registry *workflowrepos.Registry
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, registry: registry}, nil
}

func ResolveRunStage(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case workflowstore.RunStatusCreated, workflowstore.RunStatusSetupReady:
		return RunStageSpecification, nil
	case workflowstore.RunStatusExecuting, workflowstore.RunStatusExecutionFailed, workflowstore.RunStatusCancelled:
		return RunStageExecute, nil
	case workflowstore.RunStatusValidating,
		workflowstore.RunStatusValidationFailed,
		workflowstore.RunStatusAuditReady,
		workflowstore.RunStatusNeedsRevision,
		workflowstore.RunStatusCompleted:
		return RunStageAudit, nil
	default:
		return "", fmt.Errorf("%w: unsupported Run status %q", ErrInvalidWorkflowRequest, status)
	}
}

func (s *Service) ListRepositories(ctx context.Context) ([]workflowstore.RepositoryTarget, error) {
	return s.store.ListRepositoryTargets(ctx)
}

func (s *Service) GetRepository(ctx context.Context, repoTarget string) (workflowstore.RepositoryTarget, error) {
	return s.registry.Resolve(ctx, strings.TrimSpace(repoTarget))
}

func (s *Service) RegisterRepository(ctx context.Context, repoTarget, localPath string) (workflowstore.RepositoryTarget, error) {
	return s.registry.Register(ctx, strings.TrimSpace(repoTarget), strings.TrimSpace(localPath))
}

func (s *Service) InspectRepository(
	ctx context.Context,
	input RepositoryInspectionInput,
) (RepositoryInspection, error) {
	return s.registry.Inspect(ctx, input)
}

func (s *Service) ConfirmRepository(
	ctx context.Context,
	input RepositoryConfirmationInput,
) (RepositoryRegistrationResult, error) {
	return s.registry.Confirm(ctx, input)
}

func (s *Service) ListPlans(ctx context.Context, input ListPlansInput) ([]PlanSummary, error) {
	input.Status = strings.TrimSpace(input.Status)
	if err := workflowstore.ValidateWorkflowListStatus(
		input.Status,
		workflowstore.PlanStatusActive,
		workflowstore.PlanStatusCompleted,
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidWorkflowRequest, err)
	}
	query := workflowstore.PlanListQuery{Status: input.Status, Limit: input.Limit}
	if strings.TrimSpace(input.ProjectID) != "" {
		project, err := s.store.GetProjectByProjectID(ctx, strings.TrimSpace(input.ProjectID))
		if err != nil {
			return nil, err
		}
		query.ProjectRowID = sql.NullInt64{Int64: project.ID, Valid: true}
	}
	plans, err := s.store.ListPlans(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]PlanSummary, 0, len(plans))
	for _, plan := range plans {
		summary, err := s.planSummary(ctx, plan)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) GetPlan(ctx context.Context, planID string) (PlanDetail, error) {
	plan, err := s.store.GetPlanByPlanID(ctx, strings.TrimSpace(planID))
	if err != nil {
		return PlanDetail{}, err
	}
	project, err := s.store.GetProjectByRowID(ctx, plan.ProjectRowID)
	if err != nil {
		return PlanDetail{}, err
	}
	repositories, err := s.store.ListPlanRepositoryTargets(ctx, plan.ID)
	if err != nil {
		return PlanDetail{}, err
	}
	passes, err := s.store.ListPlanPasses(ctx, plan.ID)
	if err != nil {
		return PlanDetail{}, err
	}
	dependencies, err := s.store.ListPlanPassDependencies(ctx, plan.ID)
	if err != nil {
		return PlanDetail{}, err
	}
	artifacts, err := s.store.ListArtifactsByPlan(ctx, plan.ID)
	if err != nil {
		return PlanDetail{}, err
	}
	passIDsByRow := make(map[int64]string, len(passes))
	for _, pass := range passes {
		passIDsByRow[pass.ID] = pass.PassID
	}
	dependenciesByPass := make(map[int64][]string, len(passes))
	for _, dependency := range dependencies {
		dependenciesByPass[dependency.PassRowID] = append(
			dependenciesByPass[dependency.PassRowID],
			passIDsByRow[dependency.DependsOnPassRowID],
		)
	}
	passDetails := make([]PlanPassDetail, 0, len(passes))
	for _, pass := range passes {
		runs, err := s.store.ListRuns(ctx, workflowstore.RunListQuery{
			PlanRowID:     sql.NullInt64{Int64: plan.ID, Valid: true},
			PlanPassRowID: sql.NullInt64{Int64: pass.ID, Valid: true},
			Limit:         workflowstore.MaxWorkflowListLimit,
		})
		if err != nil {
			return PlanDetail{}, err
		}
		runSummaries := make([]RunSummary, 0, len(runs))
		for _, run := range runs {
			summary, err := s.runSummary(ctx, run)
			if err != nil {
				return PlanDetail{}, err
			}
			runSummaries = append(runSummaries, summary)
		}
		passDetails = append(passDetails, PlanPassDetail{
			Pass:      pass,
			DependsOn: append([]string{}, dependenciesByPass[pass.ID]...),
			Runs:      runSummaries,
		})
	}
	return PlanDetail{
		Plan:         plan,
		Project:      projectReference(project),
		Repositories: repositories,
		Passes:       passDetails,
		Artifacts:    artifactMetadataList(artifacts),
	}, nil
}

func (s *Service) GetPlanPass(ctx context.Context, planID, passID string) (PlanPassDetail, error) {
	detail, err := s.GetPlan(ctx, planID)
	if err != nil {
		return PlanPassDetail{}, err
	}
	for _, pass := range detail.Passes {
		if pass.Pass.PassID == strings.TrimSpace(passID) {
			return pass, nil
		}
	}
	return PlanPassDetail{}, sql.ErrNoRows
}

func (s *Service) ListRuns(ctx context.Context, input ListRunsInput) ([]RunSummary, error) {
	input.Status = strings.TrimSpace(input.Status)
	if err := workflowstore.ValidateWorkflowListStatus(
		input.Status,
		workflowstore.RunStatusCreated,
		workflowstore.RunStatusSetupReady,
		workflowstore.RunStatusExecuting,
		workflowstore.RunStatusExecutionFailed,
		workflowstore.RunStatusCancelled,
		workflowstore.RunStatusValidating,
		workflowstore.RunStatusValidationFailed,
		workflowstore.RunStatusAuditReady,
		workflowstore.RunStatusNeedsRevision,
		workflowstore.RunStatusCompleted,
	); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidWorkflowRequest, err)
	}
	query := workflowstore.RunListQuery{Status: input.Status, Limit: input.Limit}
	if strings.TrimSpace(input.PlanID) != "" {
		plan, err := s.store.GetPlanByPlanID(ctx, strings.TrimSpace(input.PlanID))
		if err != nil {
			return nil, err
		}
		query.PlanRowID = sql.NullInt64{Int64: plan.ID, Valid: true}
		if strings.TrimSpace(input.PassID) != "" {
			pass, err := s.store.GetPlanPassByPassID(ctx, strings.TrimSpace(input.PassID))
			if err != nil || pass.PlanRowID != plan.ID {
				return nil, sql.ErrNoRows
			}
			query.PlanPassRowID = sql.NullInt64{Int64: pass.ID, Valid: true}
		}
	} else if strings.TrimSpace(input.PassID) != "" {
		return nil, fmt.Errorf("%w: passId requires planId", ErrInvalidWorkflowRequest)
	}
	runs, err := s.store.ListRuns(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]RunSummary, 0, len(runs))
	for _, run := range runs {
		summary, err := s.runSummary(ctx, run)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) GetRun(ctx context.Context, runID string) (RunDetail, error) {
	run, err := s.store.GetRunByRunID(ctx, strings.TrimSpace(runID))
	if err != nil {
		return RunDetail{}, err
	}
	summary, err := s.runSummary(ctx, run)
	if err != nil {
		return RunDetail{}, err
	}
	attempts, err := s.store.ListRecentExecutionAttemptsByRun(ctx, run.ID, workflowstore.MaxWorkflowAttemptLimit)
	if err != nil {
		return RunDetail{}, err
	}
	attemptSummaries := make([]ExecutionAttemptSummary, 0, len(attempts))
	for _, attempt := range attempts {
		summary, err := s.attemptSummary(ctx, attempt)
		if err != nil {
			return RunDetail{}, err
		}
		attemptSummaries = append(attemptSummaries, summary)
	}
	artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{
		Summary:   summary,
		Attempts:  attemptSummaries,
		Artifacts: artifactMetadataList(artifacts),
	}, nil
}

func (s *Service) GetSpecification(ctx context.Context, runID string) (SpecificationReview, error) {
	detail, err := s.GetRun(ctx, runID)
	if err != nil {
		return SpecificationReview{}, err
	}
	var executionSpec ArtifactMetadata
	var executorBrief ArtifactMetadata
	for _, artifact := range detail.Artifacts {
		switch artifact.Kind {
		case "execution_spec":
			if executionSpec.ArtifactID != "" {
				return SpecificationReview{}, fmt.Errorf("Run has multiple execution_spec artifacts")
			}
			executionSpec = artifact
		case "executor_brief":
			if executorBrief.ArtifactID != "" {
				return SpecificationReview{}, fmt.Errorf("Run has multiple executor_brief artifacts")
			}
			executorBrief = artifact
		}
	}
	if executionSpec.ArtifactID == "" || executorBrief.ArtifactID == "" {
		return SpecificationReview{}, fmt.Errorf("Run specification artifacts are incomplete")
	}
	review := SpecificationReview{
		Run:             detail.Summary,
		ExecutionSpec:   executionSpec,
		ExecutorBrief:   executorBrief,
		RemediatesRunID: detail.Summary.RemediatesRunID,
	}
	if detail.Summary.Run.PlanRowID.Valid {
		plan, err := s.store.GetPlanByRowID(ctx, detail.Summary.Run.PlanRowID.Int64)
		if err != nil {
			return SpecificationReview{}, err
		}
		review.Plan = &plan
	}
	if detail.Summary.Run.PlanPassRowID.Valid {
		pass, err := s.store.GetPlanPassByRowID(ctx, detail.Summary.Run.PlanPassRowID.Int64)
		if err != nil {
			return SpecificationReview{}, err
		}
		review.Pass = &pass
	}
	return review, nil
}

func (s *Service) GetArtifact(ctx context.Context, artifactID string) (ArtifactMetadata, error) {
	artifact, err := s.store.GetArtifactByArtifactID(ctx, strings.TrimSpace(artifactID))
	if err != nil {
		return ArtifactMetadata{}, err
	}
	return artifactMetadata(artifact), nil
}

func (s *Service) GetArtifactContent(ctx context.Context, input ArtifactContentInput) (ArtifactContent, error) {
	artifact, err := s.store.GetArtifactByArtifactID(ctx, strings.TrimSpace(input.ArtifactID))
	if err != nil {
		return ArtifactContent{}, err
	}
	if input.Offset < 0 {
		return ArtifactContent{}, fmt.Errorf("%w: offset must not be negative", ErrInvalidWorkflowRequest)
	}
	limit := input.Limit
	if limit <= 0 {
		limit = DefaultArtifactContentLimit
	}
	if limit > MaxArtifactContentLimit {
		limit = MaxArtifactContentLimit
	}
	path, err := s.artifactPath(artifact)
	if err != nil {
		return ArtifactContent{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return ArtifactContent{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ArtifactContent{}, err
	}
	if info.Size() != artifact.SizeBytes || input.Offset > info.Size() {
		return ArtifactContent{}, ErrArtifactIntegrity
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return ArtifactContent{}, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return ArtifactContent{}, ErrArtifactIntegrity
	}
	if _, err := file.Seek(input.Offset, io.SeekStart); err != nil {
		return ArtifactContent{}, err
	}
	data := make([]byte, limit)
	count, err := file.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		return ArtifactContent{}, err
	}
	data = data[:count]
	encoding := "utf-8"
	if !utf8.Valid(data) {
		encoding = "base64"
	}
	nextOffset := input.Offset + int64(count)
	return ArtifactContent{
		Artifact:   artifactMetadata(artifact),
		Offset:     input.Offset,
		Bytes:      data,
		Encoding:   encoding,
		Truncated:  nextOffset < artifact.SizeBytes,
		NextOffset: nextOffset,
		HasNext:    nextOffset < artifact.SizeBytes,
	}, nil
}

func (s *Service) planSummary(ctx context.Context, plan workflowstore.Plan) (PlanSummary, error) {
	project, err := s.store.GetProjectByRowID(ctx, plan.ProjectRowID)
	if err != nil {
		return PlanSummary{}, err
	}
	passes, err := s.store.ListPlanPasses(ctx, plan.ID)
	if err != nil {
		return PlanSummary{}, err
	}
	summary := PlanSummary{Plan: plan, Project: projectReference(project), PassCount: len(passes)}
	for _, pass := range passes {
		switch pass.Status {
		case workflowstore.PassStatusCompleted:
			summary.CompletedPassCount++
		case workflowstore.PassStatusInProgress:
			summary.InProgressPassCount++
			if summary.CurrentPassID == "" {
				summary.CurrentPassID = pass.PassID
			}
		case workflowstore.PassStatusPlanned:
			summary.PlannedPassCount++
			if summary.CurrentPassID == "" {
				summary.CurrentPassID = pass.PassID
			}
		}
	}
	return summary, nil
}

func (s *Service) runSummary(ctx context.Context, run workflowstore.Run) (RunSummary, error) {
	stage, err := ResolveRunStage(run.Status)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{Run: run, Stage: stage}
	if run.PlanRowID.Valid {
		plan, err := s.store.GetPlanByRowID(ctx, run.PlanRowID.Int64)
		if err != nil {
			return RunSummary{}, err
		}
		summary.PlanID = plan.PlanID
		project, err := s.store.GetProjectByRowID(ctx, plan.ProjectRowID)
		if err != nil {
			return RunSummary{}, err
		}
		value := projectReference(project)
		summary.Project = &value
	}
	if run.PlanPassRowID.Valid {
		pass, err := s.store.GetPlanPassByRowID(ctx, run.PlanPassRowID.Int64)
		if err != nil {
			return RunSummary{}, err
		}
		summary.PassID = pass.PassID
		summary.PassNumber = pass.PassNumber
	}
	if run.RemediatesRunRowID.Valid {
		remediates, err := s.store.GetRunByRowID(ctx, run.RemediatesRunRowID.Int64)
		if err != nil {
			return RunSummary{}, err
		}
		summary.RemediatesRunID = remediates.RunID
	}
	attempts, err := s.store.ListRecentExecutionAttemptsByRun(ctx, run.ID, 1)
	if err != nil {
		return RunSummary{}, err
	}
	if len(attempts) == 1 {
		attemptSummary, err := s.attemptSummary(ctx, attempts[0])
		if err != nil {
			return RunSummary{}, err
		}
		summary.LatestAttempt = &attemptSummary
	}
	if packet, err := s.store.GetCurrentAuditPacketByRun(ctx, run.ID); err == nil {
		value := auditPacketSummary(packet)
		summary.CurrentPacket = &value
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RunSummary{}, err
	}
	if decision, err := s.store.GetAuditDecisionByRun(ctx, run.ID); err == nil {
		value := auditDecisionSummary(decision)
		summary.LatestDecision = &value
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RunSummary{}, err
	}
	return summary, nil
}

func (s *Service) attemptSummary(ctx context.Context, attempt workflowstore.ExecutionAttempt) (ExecutionAttemptSummary, error) {
	artifacts, err := s.store.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
	if err != nil {
		return ExecutionAttemptSummary{}, err
	}
	summary := ExecutionAttemptSummary{
		AttemptID:     attempt.AttemptID,
		AttemptNumber: attempt.AttemptNumber,
		Adapter:       attempt.Adapter,
		Model:         attempt.Model,
		Status:        attempt.Status,
		CreatedAt:     attempt.CreatedAt,
		Artifacts:     artifactMetadataList(artifacts),
	}
	if attempt.StartedAt.Valid {
		summary.StartedAt = attempt.StartedAt.String
	}
	if attempt.FinishedAt.Valid {
		summary.FinishedAt = attempt.FinishedAt.String
	}
	if attempt.CancellationRequestedAt.Valid {
		summary.CancellationRequestedAt = attempt.CancellationRequestedAt.String
	}
	return summary, nil
}

func (s *Service) artifactPath(artifact workflowstore.Artifact) (string, error) {
	root := s.store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact path escapes workflow artifact root")
	}
	return path, nil
}

func projectReference(value workflowstore.Project) ProjectReference {
	return ProjectReference{ProjectID: value.ProjectID, Name: value.Name, Status: value.Status}
}

func artifactMetadataList(values []workflowstore.Artifact) []ArtifactMetadata {
	out := make([]ArtifactMetadata, 0, len(values))
	for _, value := range values {
		out = append(out, artifactMetadata(value))
	}
	return out
}

func artifactMetadata(value workflowstore.Artifact) ArtifactMetadata {
	return ArtifactMetadata{
		ArtifactID: value.ArtifactID,
		OwnerType:  value.OwnerType,
		Kind:       value.Kind,
		MediaType:  value.MediaType,
		SHA256:     value.SHA256,
		SizeBytes:  value.SizeBytes,
		CreatedAt:  value.CreatedAt,
	}
}

func auditPacketSummary(value workflowstore.AuditPacket) AuditPacketSummary {
	out := AuditPacketSummary{
		AuditPacketID:           value.AuditPacketID,
		ImplementationActorKind: value.ImplementationActorKind,
		AuditedCommit:           value.AuditedCommit,
		PacketSHA256:            value.PacketSHA256,
		Status:                  value.Status,
		StaleReason:             value.StaleReason,
		CreatedAt:               value.CreatedAt,
	}
	if value.SupersededAt.Valid {
		out.SupersededAt = value.SupersededAt.String
	}
	return out
}

func auditDecisionSummary(value workflowstore.AuditDecision) AuditDecisionSummary {
	return AuditDecisionSummary{
		AuditDecisionID: value.AuditDecisionID,
		AuditedCommit:   value.AuditedCommit,
		PacketSHA256:    value.PacketSHA256,
		Decision:        value.Decision,
		Rationale:       value.Rationale,
		CreatedAt:       value.CreatedAt,
	}
}
