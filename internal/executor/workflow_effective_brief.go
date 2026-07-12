package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	workflowruns "relay/internal/app/runs/workflow"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

type effectiveBriefInput struct {
	Mode     speccompiler.EffectiveBriefMode
	Content  []byte
	Artifact workflowstore.Artifact
	Path     string
}

func fullEffectiveBriefInput(content []byte, artifact workflowstore.Artifact, path string) effectiveBriefInput {
	return effectiveBriefInput{
		Mode:     speccompiler.EffectiveBriefFull,
		Content:  append([]byte(nil), content...),
		Artifact: artifact,
		Path:     path,
	}
}

func (s *WorkflowExecutionService) prepareResidualEffectiveBrief(
	ctx context.Context,
	attempt workflowstore.ExecutionAttempt,
	applierResult *WorkflowApplierResult,
) (effectiveBriefInput, error) {
	if applierResult == nil || applierResult.Document == nil {
		return effectiveBriefInput{}, fmt.Errorf("hybrid execution requires the validated Execution Spec document")
	}
	selection := speccompiler.EffectiveBriefSelection{
		Mode:                  speccompiler.EffectiveBriefResidual,
		ResidualFileWorkRefs:  append([]string(nil), applierResult.Partition.ResidualFileWork...),
		CompletedFileWorkRefs: append([]string(nil), applierResult.Partition.DeterministicFileWork...),
		ProtectedPaths:        append([]string(nil), applierResult.Partition.ProtectedPaths...),
	}
	rendered, err := speccompiler.RenderEffectiveExecutorBrief(applierResult.Document, selection)
	if err != nil {
		return effectiveBriefInput{}, fmt.Errorf("render residual effective brief: %w", err)
	}
	artifact, err := s.persistResidualBrief(ctx, attempt, []byte(rendered))
	if err != nil {
		return effectiveBriefInput{}, fmt.Errorf("persist residual effective brief: %w", err)
	}
	content, verified, absolute, err := s.loadVerifiedAttemptArtifact(ctx, attempt, artifact.ArtifactID, "executor_residual_brief", "text/markdown")
	if err != nil {
		return effectiveBriefInput{}, err
	}
	return effectiveBriefInput{
		Mode:     speccompiler.EffectiveBriefResidual,
		Content:  content,
		Artifact: verified,
		Path:     absolute,
	}, nil
}

func (s *WorkflowExecutionService) persistResidualBrief(ctx context.Context, attempt workflowstore.ExecutionAttempt, content []byte) (workflowstore.Artifact, error) {
	if len(content) == 0 {
		return workflowstore.Artifact{}, fmt.Errorf("residual effective brief is empty")
	}
	batch, err := s.store.ArtifactStore().Begin("attempts/" + attempt.AttemptID)
	if err != nil {
		return workflowstore.Artifact{}, err
	}
	staged, err := batch.Stage("executor_residual_brief", "executor-residual-brief.md", "text/markdown", content)
	if err != nil {
		_ = batch.Rollback()
		return workflowstore.Artifact{}, err
	}
	var created workflowstore.Artifact
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:            workflowstore.NewArtifactID(),
			OwnerType:             workflowstore.ArtifactOwnerExecutionAttempt,
			ExecutionAttemptRowID: sql.NullInt64{Int64: attempt.ID, Valid: true},
			Kind:                  staged.Kind,
			RelativePath:          staged.RelativePath,
			MediaType:             staged.MediaType,
			SHA256:                staged.SHA256,
			SizeBytes:             staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		created = artifact
		return nil
	})
	if err != nil {
		return workflowstore.Artifact{}, err
	}
	return created, nil
}

func (s *WorkflowExecutionService) loadVerifiedAttemptArtifact(
	ctx context.Context,
	attempt workflowstore.ExecutionAttempt,
	artifactID, kind, mediaType string,
) ([]byte, workflowstore.Artifact, string, error) {
	artifacts, err := s.store.ListArtifactsByExecutionAttempt(ctx, attempt.ID)
	if err != nil {
		return nil, workflowstore.Artifact{}, "", err
	}
	var selected workflowstore.Artifact
	found := false
	for _, artifact := range artifacts {
		if artifact.ArtifactID != artifactID {
			continue
		}
		if found {
			return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact ID %s is duplicated", artifactID)
		}
		selected = artifact
		found = true
	}
	if !found {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact %s is missing", artifactID)
	}
	if selected.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !selected.ExecutionAttemptRowID.Valid || selected.ExecutionAttemptRowID.Int64 != attempt.ID {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact ownership is invalid")
	}
	if selected.Kind != kind || selected.MediaType != mediaType {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact identity is invalid")
	}
	root := s.store.ArtifactStore().Root()
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(selected.RelativePath)))
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact path is invalid")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("read execution attempt artifact: %w", err)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != selected.SHA256 || int64(len(content)) != selected.SizeBytes {
		return nil, workflowstore.Artifact{}, "", fmt.Errorf("execution attempt artifact integrity check failed")
	}
	return content, selected, absolute, nil
}

func verifyInvocationUsesEffectiveBrief(invocation ExecutorInvocation, selected effectiveBriefInput) error {
	if selected.Artifact.ArtifactID == "" || selected.Artifact.SHA256 == "" || strings.TrimSpace(selected.Path) == "" || len(selected.Content) == 0 {
		return fmt.Errorf("selected effective brief identity is incomplete")
	}
	digest := sha256.Sum256(selected.Content)
	if hex.EncodeToString(digest[:]) != selected.Artifact.SHA256 || int64(len(selected.Content)) != selected.Artifact.SizeBytes {
		return fmt.Errorf("selected effective brief bytes do not match artifact identity")
	}
	if invocation.Stdin != "" {
		if !bytes.Equal([]byte(invocation.Stdin), selected.Content) {
			return fmt.Errorf("executor invocation changed the selected effective brief bytes")
		}
		if invocation.StdinSource != selected.Path {
			return fmt.Errorf("executor invocation stdin source does not identify the selected effective brief")
		}
		if invocation.StdinBytes != len(selected.Content) {
			return fmt.Errorf("executor invocation stdin size does not match the selected effective brief")
		}
		return nil
	}
	matches := 0
	for _, arg := range invocation.Args {
		if arg == selected.Path {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("executor invocation must reference exactly one selected effective brief path")
	}
	if invocation.StdinSource != "" && invocation.StdinSource != selected.Path {
		return fmt.Errorf("executor invocation identifies an alternate brief source")
	}
	return nil
}

func (s *WorkflowExecutionService) failPrelaunchAttempt(
	ctx context.Context,
	begun workflowruns.BeginExecutionAttemptResult,
	preflight workflowrepos.ExecutionPreflightResult,
	applierResult *WorkflowApplierResult,
	cause error,
) (WorkflowStartResult, error) {
	s.finishPrelaunchFailure(begun.Attempt, cause.Error())
	result := WorkflowStartResult{Run: begun.Run, Attempt: begun.Attempt, Preflight: preflight, Applier: applierResult}
	if attempt, err := s.store.GetExecutionAttemptByAttemptID(ctx, begun.Attempt.AttemptID); err == nil {
		result.Attempt = attempt
	}
	if run, err := s.store.GetRunByRunID(ctx, begun.Run.RunID); err == nil {
		result.Run = run
	}
	return result, cause
}
