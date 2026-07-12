package executor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/applier"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

type workflowApplierFunc func(context.Context, applier.Input) (applier.Result, error)

// WorkflowApplierResult is the public workflow-start summary of deterministic
// pre-application. It deliberately represents applier evidence as applier
// evidence, not as model-backed Executor evidence.
type WorkflowApplierResult struct {
	Outcome               string                           `json:"outcome"`
	ActorKind             string                           `json:"actor_kind"`
	Partition             applier.Partition                `json:"partition"`
	ImplementationResult  applier.ImplementationResult     `json:"implementation_result"`
	FailurePacket         *applier.FailurePacket           `json:"failure_packet,omitempty"`
	ChangedFiles          []string                         `json:"changed_files,omitempty"`
	Evidence              []applier.EvidenceArtifact       `json:"evidence,omitempty"`
	ProjectionDiagnostics []speccompiler.Diagnostic        `json:"projection_diagnostics,omitempty"`
	Document              *speccompiler.ExecutionDocument  `json:"-"`
	Projection            speccompiler.ExecutionProjection `json:"-"`
}

type workflowRunEvidenceWriter struct {
	store *workflowstore.Store
	run   workflowstore.Run
}

func defaultWorkflowApplier() workflowApplierFunc {
	service := applier.NewService()
	return service.Apply
}

func (s *WorkflowExecutionService) applyDeterministicFirst(
	ctx context.Context,
	run workflowstore.Run,
	repoPath string,
	executionSpec []byte,
	executionSpecArtifact workflowstore.Artifact,
) (*WorkflowApplierResult, error) {
	filename := filepath.Base(filepath.FromSlash(executionSpecArtifact.RelativePath))
	compiled, document := speccompiler.CompileExecutionSpec(filename, executionSpec)
	if len(compiled.Errors) > 0 || document == nil {
		result := projectionDiagnosticResult(compiled.Errors)
		writer := workflowRunEvidenceWriter{store: s.store, run: run}
		if err := writeJSONEvidence(ctx, writer, "applier_failure_packet_json", "applier-failure-packet.json", result.FailurePacket, &result.Evidence); err != nil {
			return nil, err
		}
		if err := writeJSONEvidence(ctx, writer, "applier_projection_diagnostics_json", "applier-projection-diagnostics.json", compiled.Errors, &result.Evidence); err != nil {
			return nil, err
		}
		return result, nil
	}
	projection, diagnostics := speccompiler.ProjectExecutionSpec(document)
	if len(diagnostics) > 0 {
		result := projectionDiagnosticResult(diagnostics)
		writer := workflowRunEvidenceWriter{store: s.store, run: run}
		if err := writeJSONEvidence(ctx, writer, "applier_failure_packet_json", "applier-failure-packet.json", result.FailurePacket, &result.Evidence); err != nil {
			return nil, err
		}
		if err := writeJSONEvidence(ctx, writer, "applier_projection_diagnostics_json", "applier-projection-diagnostics.json", diagnostics, &result.Evidence); err != nil {
			return nil, err
		}
		return result, nil
	}
	if len(projection.FileWork) == 0 {
		return nil, nil
	}
	apply := s.applier
	if apply == nil {
		apply = defaultWorkflowApplier()
	}
	writer := workflowRunEvidenceWriter{store: s.store, run: run}
	raw, err := apply(ctx, applier.Input{WorkspaceRoot: repoPath, Projection: projection, EvidenceWriter: writer})
	if err != nil {
		return nil, fmt.Errorf("deterministic applier: %w", err)
	}
	result := workflowApplierResult(raw)
	result.Document = document
	result.Projection = projection
	return &result, nil
}

func projectionDiagnosticResult(diagnostics []speccompiler.Diagnostic) *WorkflowApplierResult {
	summary := "canonical Execution Spec compilation or projection diagnostics blocked deterministic-first execution"
	failure := &applier.FailurePacket{
		FailureClass:      applier.FailureClassMaterialSpecGap,
		Summary:           summary,
		BlockedPathChains: []string{"execution_spec"},
		BlockedFileWork:   []string{},
	}
	implementation := applier.ImplementationResult{
		Outcome:               applier.OutcomeBlocked,
		ActorKind:             applier.ActorKindApplier,
		BlockedPathChains:     []string{"execution_spec"},
		BlockedFileWork:       []string{},
		ChangedFiles:          []string{},
		ProtectedPaths:        []string{},
		ModelExecutorRequired: false,
		FailureClass:          applier.FailureClassMaterialSpecGap,
		FailureReason:         summary,
	}
	return &WorkflowApplierResult{
		Outcome:               string(applier.OutcomeBlocked),
		ActorKind:             string(applier.ActorKindApplier),
		ImplementationResult:  implementation,
		FailurePacket:         failure,
		ProjectionDiagnostics: diagnostics,
	}
}

func workflowApplierResult(raw applier.Result) WorkflowApplierResult {
	return WorkflowApplierResult{
		Outcome:              string(raw.Outcome),
		ActorKind:            string(raw.ActorKind),
		Partition:            raw.Partition,
		ImplementationResult: raw.ImplementationResult,
		FailurePacket:        raw.FailurePacket,
		ChangedFiles:         append([]string(nil), raw.ChangedFiles...),
		Evidence:             append([]applier.EvidenceArtifact(nil), raw.Evidence...),
	}
}

func (w workflowRunEvidenceWriter) WriteEvidence(ctx context.Context, file applier.EvidenceFile) (applier.EvidenceArtifact, error) {
	if w.store == nil {
		return applier.EvidenceArtifact{}, fmt.Errorf("workflow store is unavailable")
	}
	if strings.TrimSpace(file.Kind) == "" || strings.TrimSpace(file.Filename) == "" || strings.TrimSpace(file.MediaType) == "" {
		return applier.EvidenceArtifact{}, fmt.Errorf("applier evidence file is incomplete")
	}
	namespace := "runs/" + w.run.RunID + "/applier/" + workflowstore.NewArtifactID()
	batch, err := w.store.ArtifactStore().Begin(namespace)
	if err != nil {
		return applier.EvidenceArtifact{}, err
	}
	staged, err := batch.Stage(file.Kind, file.Filename, file.MediaType, file.Data)
	if err != nil {
		_ = batch.Rollback()
		return applier.EvidenceArtifact{}, err
	}
	var out applier.EvidenceArtifact
	err = w.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID:   workflowstore.NewArtifactID(),
			OwnerType:    workflowstore.ArtifactOwnerRun,
			RunRowID:     sql.NullInt64{Int64: w.run.ID, Valid: true},
			Kind:         staged.Kind,
			RelativePath: staged.RelativePath,
			MediaType:    staged.MediaType,
			SHA256:       staged.SHA256,
			SizeBytes:    staged.SizeBytes,
		})
		if err != nil {
			return err
		}
		out = applier.EvidenceArtifact{Kind: artifact.Kind, Filename: file.Filename, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}
		return nil
	})
	if err != nil {
		return applier.EvidenceArtifact{}, err
	}
	return out, nil
}

func writeJSONEvidence(ctx context.Context, writer workflowRunEvidenceWriter, kind, filename string, value any, out *[]applier.EvidenceArtifact) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	artifact, err := writer.WriteEvidence(ctx, applier.EvidenceFile{Kind: kind, Filename: filename, MediaType: "application/json", Data: data})
	if err != nil {
		return err
	}
	*out = append(*out, artifact)
	return nil
}

func (s *WorkflowExecutionService) loadVerifiedExecutionSpec(ctx context.Context, run workflowstore.Run) ([]byte, workflowstore.Artifact, error) {
	return s.loadVerifiedRunArtifact(ctx, run, "execution_spec")
}

func (s *WorkflowExecutionService) loadVerifiedRunArtifact(ctx context.Context, run workflowstore.Run, kind string) ([]byte, workflowstore.Artifact, error) {
	artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return nil, workflowstore.Artifact{}, err
	}
	var selected workflowstore.Artifact
	found := false
	for _, artifact := range artifacts {
		if artifact.Kind == kind {
			if found {
				return nil, workflowstore.Artifact{}, fmt.Errorf("Run has multiple %s artifacts", kind)
			}
			selected = artifact
			found = true
		}
	}
	if !found {
		return nil, workflowstore.Artifact{}, fmt.Errorf("Run %s artifact is missing", kind)
	}
	root := s.store.ArtifactStore().Root()
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(selected.RelativePath)))
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, workflowstore.Artifact{}, fmt.Errorf("Run %s artifact path is invalid", kind)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, workflowstore.Artifact{}, fmt.Errorf("read Run %s artifact: %w", kind, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != selected.SHA256 || int64(len(data)) != selected.SizeBytes {
		return nil, workflowstore.Artifact{}, fmt.Errorf("Run %s artifact integrity check failed", kind)
	}
	return data, selected, nil
}

var _ applier.EvidenceWriter = workflowRunEvidenceWriter{}
