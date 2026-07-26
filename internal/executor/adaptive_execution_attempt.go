package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

const (
	adaptiveExecutionInputKind      = "adaptive_execution_input"
	adaptiveExecutionInputMediaType = "application/json"
)

var ErrAdaptiveExecutionAttemptConflict = errors.New("adaptive execution attempt conflicts with recorded preparation")

type AdaptiveExecutionAttemptInput struct {
	RunID   string
	Adapter string
	Model   string
}

type AdaptiveExecutionAttemptResult struct {
	Mode                     EffectiveExecutorBriefMode
	AdaptiveDispatchRequired bool
	Attempt                  *workflowstore.ExecutionAttempt
	InputArtifact            *workflowstore.Artifact
	InputBytes               []byte
}

// AdaptiveExecutionAttemptService records the immutable input for a later
// adaptive Executor dispatch. It never starts that dispatch or changes Run
// lifecycle state.
type AdaptiveExecutionAttemptService struct {
	store  *workflowstore.Store
	briefs *EffectiveExecutorBriefService
}

func NewAdaptiveExecutionAttemptService(store *workflowstore.Store) (*AdaptiveExecutionAttemptService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	briefs, err := NewEffectiveExecutorBriefService(store)
	if err != nil {
		return nil, err
	}
	return &AdaptiveExecutionAttemptService{store: store, briefs: briefs}, nil
}

func (s *AdaptiveExecutionAttemptService) Prepare(ctx context.Context, input AdaptiveExecutionAttemptInput) (AdaptiveExecutionAttemptResult, error) {
	if s == nil || s.store == nil || s.briefs == nil {
		return AdaptiveExecutionAttemptResult{}, fmt.Errorf("adaptive execution attempt service is unavailable")
	}
	if strings.TrimSpace(input.RunID) == "" {
		return AdaptiveExecutionAttemptResult{}, fmt.Errorf("Run ID is required")
	}
	brief, err := s.briefs.Prepare(ctx, input.RunID)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	if brief.Mode == EffectiveExecutorBriefDeterministicComplete {
		return s.resolveComplete(ctx, run, brief.Mode)
	}
	if !brief.AdaptiveDispatchRequired || brief.Artifact == nil || len(brief.Bytes) == 0 {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	adapter, err := NormalizeKnownAdapterID(input.Adapter)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return AdaptiveExecutionAttemptResult{}, fmt.Errorf("executor model is required")
	}
	if existing, err := s.store.ListExecutionAttemptsByRun(ctx, run.ID); err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	} else if len(existing) != 0 {
		return s.resolveExisting(ctx, run, brief, adapter, model, existing)
	}

	attemptID := workflowstore.NewExecutionAttemptID()
	filename, err := adaptiveExecutionInputFilename(*brief.Artifact)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	content, err := marshalAdaptiveExecutionInput(run, brief.Mode, *brief.Artifact, attemptID, 1, adapter, model)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	batch, err := s.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("runs", run.RunID, "attempts", attemptID)))
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	staged, err := batch.Stage(adaptiveExecutionInputKind, filename, adaptiveExecutionInputMediaType, content)
	if err != nil {
		_ = batch.Rollback()
		return AdaptiveExecutionAttemptResult{}, err
	}
	var createdAttempt workflowstore.ExecutionAttempt
	var createdArtifact workflowstore.Artifact
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, loadErr := tx.GetRunByRunID(ctx, run.RunID)
		if loadErr != nil {
			return loadErr
		}
		if current.Status != workflowstore.RunStatusSetupReady {
			return fmt.Errorf("adaptive execution attempt requires a setup_ready Run")
		}
		attempts, listErr := tx.ListExecutionAttemptsByRun(ctx, current.ID)
		if listErr != nil {
			return listErr
		}
		if len(attempts) != 0 {
			return ErrAdaptiveExecutionAttemptConflict
		}
		number, numberErr := tx.NextExecutionAttemptNumber(ctx, current.ID)
		if numberErr != nil {
			return numberErr
		}
		if number != 1 {
			return ErrAdaptiveExecutionAttemptConflict
		}
		createdAttempt, err = tx.CreateExecutionAttempt(ctx, workflowstore.CreateExecutionAttemptParams{AttemptID: attemptID, RunRowID: current.ID, AttemptNumber: 1, Adapter: adapter, Model: model})
		if err != nil {
			return err
		}
		createdArtifact, err = tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerExecutionAttempt, ExecutionAttemptRowID: sql.NullInt64{Int64: createdAttempt.ID, Valid: true}, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		return err
	})
	if err == nil {
		return adaptiveExecutionAttemptResult(brief.Mode, createdAttempt, createdArtifact, content), nil
	}
	if errors.Is(err, ErrAdaptiveExecutionAttemptConflict) || isPackageAttemptUniquenessError(err) {
		if attempts, listErr := s.store.ListExecutionAttemptsByRun(ctx, run.ID); listErr == nil && len(attempts) != 0 {
			return s.resolveExisting(ctx, run, brief, adapter, model, attempts)
		}
	}
	return AdaptiveExecutionAttemptResult{}, err
}

func (s *AdaptiveExecutionAttemptService) resolveComplete(ctx context.Context, run workflowstore.Run, mode EffectiveExecutorBriefMode) (AdaptiveExecutionAttemptResult, error) {
	attempts, err := s.store.ListExecutionAttemptsByRun(ctx, run.ID)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	if len(attempts) != 0 {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	return AdaptiveExecutionAttemptResult{Mode: mode}, nil
}

func (s *AdaptiveExecutionAttemptService) resolveExisting(ctx context.Context, run workflowstore.Run, brief EffectiveExecutorBriefResult, adapter, model string, attempts []workflowstore.ExecutionAttempt) (AdaptiveExecutionAttemptResult, error) {
	if len(attempts) != 1 {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	attempt := attempts[0]
	if attempt.AttemptNumber != 1 || attempt.Adapter != adapter || attempt.Model != model {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	if brief.Artifact == nil || brief.Mode == EffectiveExecutorBriefDeterministicComplete || !brief.AdaptiveDispatchRequired {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	artifacts, err := s.store.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	artifact, err := findAdaptiveExecutionInputArtifact(artifacts, attempt)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	filename, err := adaptiveExecutionInputFilename(*brief.Artifact)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	expected, err := marshalAdaptiveExecutionInput(run, brief.Mode, *brief.Artifact, attempt.AttemptID, attempt.AttemptNumber, adapter, model)
	if err != nil {
		return AdaptiveExecutionAttemptResult{}, err
	}
	wantPath := filepath.ToSlash(filepath.Join("runs", run.RunID, "attempts", attempt.AttemptID, filename))
	if artifact.RelativePath != wantPath || artifact.MediaType != adaptiveExecutionInputMediaType || artifact.SizeBytes != int64(len(expected)) {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	verified, content, readErr := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}, len(expected))
	if readErr != nil || verified.RelativePath != artifact.RelativePath || !bytes.Equal(content, expected) {
		return AdaptiveExecutionAttemptResult{}, ErrAdaptiveExecutionAttemptConflict
	}
	return adaptiveExecutionAttemptResult(brief.Mode, attempt, artifact, content), nil
}

func adaptiveExecutionAttemptResult(mode EffectiveExecutorBriefMode, attempt workflowstore.ExecutionAttempt, artifact workflowstore.Artifact, content []byte) AdaptiveExecutionAttemptResult {
	attemptCopy, artifactCopy := attempt, artifact
	return AdaptiveExecutionAttemptResult{Mode: mode, AdaptiveDispatchRequired: true, Attempt: &attemptCopy, InputArtifact: &artifactCopy, InputBytes: append([]byte(nil), content...)}
}

func findAdaptiveExecutionInputArtifact(artifacts []workflowstore.Artifact, attempt workflowstore.ExecutionAttempt) (workflowstore.Artifact, error) {
	var found *workflowstore.Artifact
	for index := range artifacts {
		artifact := artifacts[index]
		if artifact.Kind != adaptiveExecutionInputKind {
			continue
		}
		if found != nil || artifact.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !artifact.ExecutionAttemptRowID.Valid || artifact.ExecutionAttemptRowID.Int64 != attempt.ID {
			return workflowstore.Artifact{}, ErrAdaptiveExecutionAttemptConflict
		}
		found = &artifact
	}
	if found == nil {
		return workflowstore.Artifact{}, ErrAdaptiveExecutionAttemptConflict
	}
	return *found, nil
}

func adaptiveExecutionInputFilename(brief workflowstore.Artifact) (string, error) {
	base := filepath.Base(brief.RelativePath)
	const suffix = ".effective-executor-brief.md"
	if !strings.HasSuffix(base, suffix) {
		return "", fmt.Errorf("effective Executor Brief filename is not canonical")
	}
	return strings.TrimSuffix(base, suffix) + ".adaptive-execution-input.json", nil
}

func isPackageAttemptUniquenessError(err error) bool {
	return strings.Contains(err.Error(), "package-linked Run may have only one execution attempt")
}

type adaptiveExecutionInputDocument struct {
	SchemaVersion          string                         `json:"schema_version"`
	Run                    adaptiveExecutionInputRun      `json:"run"`
	Mode                   EffectiveExecutorBriefMode     `json:"mode"`
	EffectiveExecutorBrief adaptiveExecutionInputBrief    `json:"effective_executor_brief"`
	ExecutionAttempt       adaptiveExecutionInputAttempt  `json:"execution_attempt"`
	Executor               adaptiveExecutionInputExecutor `json:"executor"`
}

type adaptiveExecutionInputRun struct {
	RunID      string `json:"run_id"`
	RunRowID   int64  `json:"run_row_id"`
	RepoTarget string `json:"repo_target"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"base_commit"`
}

type adaptiveExecutionInputBrief struct {
	ArtifactID    string `json:"artifact_id"`
	ArtifactRowID int64  `json:"artifact_row_id"`
	RelativePath  string `json:"relative_path"`
	MediaType     string `json:"media_type"`
	SHA256        string `json:"sha256"`
	SizeBytes     int64  `json:"size_bytes"`
}

type adaptiveExecutionInputAttempt struct {
	AttemptID     string `json:"attempt_id"`
	AttemptNumber int64  `json:"attempt_number"`
}

type adaptiveExecutionInputExecutor struct {
	Adapter string `json:"adapter"`
	Model   string `json:"model"`
}

func marshalAdaptiveExecutionInput(run workflowstore.Run, mode EffectiveExecutorBriefMode, brief workflowstore.Artifact, attemptID string, attemptNumber int64, adapter, model string) ([]byte, error) {
	content, err := json.Marshal(adaptiveExecutionInputDocument{
		SchemaVersion:          "1.0",
		Run:                    adaptiveExecutionInputRun{RunID: run.RunID, RunRowID: run.ID, RepoTarget: run.RepoTarget, Branch: run.Branch, BaseCommit: run.BaseCommit},
		Mode:                   mode,
		EffectiveExecutorBrief: adaptiveExecutionInputBrief{ArtifactID: brief.ArtifactID, ArtifactRowID: brief.ID, RelativePath: brief.RelativePath, MediaType: brief.MediaType, SHA256: brief.SHA256, SizeBytes: brief.SizeBytes},
		ExecutionAttempt:       adaptiveExecutionInputAttempt{AttemptID: attemptID, AttemptNumber: attemptNumber},
		Executor:               adaptiveExecutionInputExecutor{Adapter: adapter, Model: model},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal adaptive execution input: %w", err)
	}
	return append(content, '\n'), nil
}
