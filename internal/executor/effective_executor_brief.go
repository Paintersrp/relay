package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	executionpackages "relay/internal/app/packages"
	workflowartifacts "relay/internal/artifacts/workflow"
	workflowstore "relay/internal/store/workflow"
)

const (
	effectiveExecutorBriefKind      = "effective_executor_brief"
	effectiveExecutorBriefMediaType = "text/markdown"
	effectiveExecutorBriefReadLimit = 64 << 20
)

var ErrEffectiveExecutorBriefConflict = errors.New("effective Executor Brief inputs are inconsistent")

type EffectiveExecutorBriefMode string

const (
	EffectiveExecutorBriefAdaptiveNoOperations            EffectiveExecutorBriefMode = "adaptive_no_operations"
	EffectiveExecutorBriefAdaptivePreflightFailed         EffectiveExecutorBriefMode = "adaptive_preflight_failed"
	EffectiveExecutorBriefAdaptiveAfterPartialApplication EffectiveExecutorBriefMode = "adaptive_after_partial_application"
	EffectiveExecutorBriefDeterministicComplete           EffectiveExecutorBriefMode = "deterministic_complete"
)

type EffectiveExecutorBriefResult struct {
	Mode                     EffectiveExecutorBriefMode
	AdaptiveDispatchRequired bool
	Artifact                 *workflowstore.Artifact
	Bytes                    []byte
}

// EffectiveExecutorBriefService prepares only the immutable decision artifact
// consumed by a later Executor dispatch. It neither touches a repository nor
// advances Run lifecycle state.
type EffectiveExecutorBriefService struct {
	store       *workflowstore.Store
	packages    *executionpackages.Service
	assignments *ExecutionAssignmentService
	outcomes    *DeterministicOutcomeService
}

func NewEffectiveExecutorBriefService(store *workflowstore.Store) (*EffectiveExecutorBriefService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	packages, err := executionpackages.NewService(store)
	if err != nil {
		return nil, err
	}
	assignments, err := NewExecutionAssignmentService(store)
	if err != nil {
		return nil, err
	}
	outcomes, err := NewDeterministicOutcomeService(store)
	if err != nil {
		return nil, err
	}
	return &EffectiveExecutorBriefService{store: store, packages: packages, assignments: assignments, outcomes: outcomes}, nil
}

func (s *EffectiveExecutorBriefService) Prepare(ctx context.Context, runID string) (EffectiveExecutorBriefResult, error) {
	prepared, err := s.loadPreparedEffectiveExecutorBrief(ctx, runID)
	if err != nil {
		return EffectiveExecutorBriefResult{}, err
	}
	if prepared.mode == EffectiveExecutorBriefDeterministicComplete {
		if prepared.existing != nil {
			return EffectiveExecutorBriefResult{}, ErrEffectiveExecutorBriefConflict
		}
		return EffectiveExecutorBriefResult{Mode: prepared.mode}, nil
	}
	if prepared.existing != nil {
		return s.resolveExistingEffectiveExecutorBrief(*prepared.existing, prepared.authority.Run, prepared.filename, prepared.mode, prepared.content)
	}
	if prepared.authority.Run.Status != workflowstore.RunStatusSetupReady {
		return EffectiveExecutorBriefResult{}, fmt.Errorf("effective Executor Brief requires a setup_ready Run")
	}
	batch, err := s.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("runs", prepared.authority.Run.RunID)))
	if err != nil {
		return EffectiveExecutorBriefResult{}, err
	}
	staged, err := batch.Stage(effectiveExecutorBriefKind, prepared.filename, effectiveExecutorBriefMediaType, prepared.content)
	if err != nil {
		_ = batch.Rollback()
		return EffectiveExecutorBriefResult{}, err
	}
	var created workflowstore.Artifact
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, createErr := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun, RunRowID: sql.NullInt64{Int64: prepared.authority.Run.ID, Valid: true}, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		if createErr != nil {
			return createErr
		}
		created = artifact
		return nil
	})
	if err == nil {
		return effectiveExecutorBriefResult(prepared.mode, created, prepared.content), nil
	}
	if current, listErr := s.store.ListArtifactsByRun(ctx, prepared.authority.Run.ID); listErr == nil {
		if candidate, findErr := findEffectiveExecutorBrief(current, prepared.authority.Run); findErr == nil && candidate != nil {
			return s.resolveExistingEffectiveExecutorBrief(*candidate, prepared.authority.Run, prepared.filename, prepared.mode, prepared.content)
		}
	}
	return EffectiveExecutorBriefResult{}, err
}

// Load resolves the already-prepared effective Brief without creating an
// artifact. It regenerates the expected decision and uses the same integrity
// verification as Prepare so dispatch can consume only durable preparation.
func (s *EffectiveExecutorBriefService) Load(ctx context.Context, runID string) (EffectiveExecutorBriefResult, error) {
	prepared, err := s.loadPreparedEffectiveExecutorBrief(ctx, runID)
	if err != nil {
		return EffectiveExecutorBriefResult{}, err
	}
	if prepared.mode == EffectiveExecutorBriefDeterministicComplete {
		if prepared.existing != nil {
			return EffectiveExecutorBriefResult{}, ErrEffectiveExecutorBriefConflict
		}
		return EffectiveExecutorBriefResult{Mode: prepared.mode}, nil
	}
	if prepared.existing == nil {
		return EffectiveExecutorBriefResult{}, ErrEffectiveExecutorBriefConflict
	}
	return s.resolveExistingEffectiveExecutorBrief(*prepared.existing, prepared.authority.Run, prepared.filename, prepared.mode, prepared.content)
}

type preparedEffectiveExecutorBrief struct {
	authority executionpackages.ApprovedAuthority
	mode      EffectiveExecutorBriefMode
	existing  *workflowstore.Artifact
	content   []byte
	filename  string
}

func (s *EffectiveExecutorBriefService) loadPreparedEffectiveExecutorBrief(ctx context.Context, runID string) (preparedEffectiveExecutorBrief, error) {
	if s == nil || s.store == nil || s.packages == nil || s.assignments == nil || s.outcomes == nil {
		return preparedEffectiveExecutorBrief{}, fmt.Errorf("effective Executor Brief service is unavailable")
	}
	authority, err := s.packages.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	assignment, err := s.assignments.LoadExecutionAssignment(ctx, runID)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	outcome, err := s.outcomes.Load(ctx, runID)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	if !effectiveBriefInputsAgree(authority, assignment, outcome) {
		return preparedEffectiveExecutorBrief{}, ErrEffectiveExecutorBriefConflict
	}
	mode, err := effectiveBriefMode(outcome.Outcome)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, ErrEffectiveExecutorBriefConflict
	}
	artifacts, err := s.store.ListArtifactsByRun(ctx, authority.Run.ID)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	existing, err := findEffectiveExecutorBrief(artifacts, authority.Run)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	if mode == EffectiveExecutorBriefDeterministicComplete {
		return preparedEffectiveExecutorBrief{authority: authority, mode: mode, existing: existing}, nil
	}
	content, filename, err := renderEffectiveExecutorBrief(authority, assignment, outcome, mode)
	if err != nil {
		return preparedEffectiveExecutorBrief{}, err
	}
	return preparedEffectiveExecutorBrief{authority: authority, mode: mode, existing: existing, content: content, filename: filename}, nil
}

func effectiveExecutorBriefResult(mode EffectiveExecutorBriefMode, artifact workflowstore.Artifact, content []byte) EffectiveExecutorBriefResult {
	copyArtifact := artifact
	return EffectiveExecutorBriefResult{Mode: mode, AdaptiveDispatchRequired: true, Artifact: &copyArtifact, Bytes: append([]byte(nil), content...)}
}

func findEffectiveExecutorBrief(artifacts []workflowstore.Artifact, run workflowstore.Run) (*workflowstore.Artifact, error) {
	var existing *workflowstore.Artifact
	for index := range artifacts {
		if artifacts[index].Kind != effectiveExecutorBriefKind {
			continue
		}
		if existing != nil || artifacts[index].OwnerType != workflowstore.ArtifactOwnerRun || !artifacts[index].RunRowID.Valid || artifacts[index].RunRowID.Int64 != run.ID {
			return nil, ErrEffectiveExecutorBriefConflict
		}
		candidate := artifacts[index]
		existing = &candidate
	}
	return existing, nil
}

func (s *EffectiveExecutorBriefService) resolveExistingEffectiveExecutorBrief(artifact workflowstore.Artifact, run workflowstore.Run, filename string, mode EffectiveExecutorBriefMode, expected []byte) (EffectiveExecutorBriefResult, error) {
	wantPath := filepath.ToSlash(filepath.Join("runs", run.RunID, filename))
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.Kind != effectiveExecutorBriefKind || artifact.RelativePath != wantPath || artifact.MediaType != effectiveExecutorBriefMediaType {
		return EffectiveExecutorBriefResult{}, ErrEffectiveExecutorBriefConflict
	}
	verified, content, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}, len(expected))
	if err != nil || verified.RelativePath != artifact.RelativePath || !bytes.Equal(content, expected) {
		return EffectiveExecutorBriefResult{}, ErrEffectiveExecutorBriefConflict
	}
	return effectiveExecutorBriefResult(mode, artifact, content), nil
}

func effectiveBriefMode(outcome DeterministicOutcome) (EffectiveExecutorBriefMode, error) {
	switch outcome.Outcome.Status {
	case string(DeterministicPreflightNotPresent):
		return EffectiveExecutorBriefAdaptiveNoOperations, nil
	case string(DeterministicPreflightFailed):
		return EffectiveExecutorBriefAdaptivePreflightFailed, nil
	case "applied":
		if outcome.Outcome.Coverage == "partial" {
			return EffectiveExecutorBriefAdaptiveAfterPartialApplication, nil
		}
		if outcome.Outcome.Coverage == "complete" {
			return EffectiveExecutorBriefDeterministicComplete, nil
		}
	}
	return "", fmt.Errorf("unsupported deterministic outcome")
}

func effectiveBriefInputsAgree(authority executionpackages.ApprovedAuthority, assignment ExecutionAssignmentResult, outcome DeterministicOutcomeResult) bool {
	a := assignment.Assignment
	if authority.Run.ID != a.Run.RunRowID || authority.Run.RunID != a.Run.RunID || authority.Package.PackageID != a.Package.PackageID || authority.Package.ID != a.Package.PackageRowID || authority.Package.PackageSha256 != a.Package.SHA256 || authority.PackageApproval.ApprovalID != a.PackageApproval.ApprovalID || authority.PackageApproval.ID != a.PackageApproval.ApprovalRowID || authority.Ticket.TicketID != a.Ticket.TicketID || authority.Ticket.ID != a.Ticket.TicketRowID || authority.TicketRevision.ID != a.Ticket.RevisionRowID || authority.TicketRevision.RevisionNumber != a.Ticket.RevisionNumber || authority.Run.RepoTarget != a.Repository.Target || authority.Run.Branch != a.Repository.Branch || authority.Run.BaseCommit != a.Repository.BaseCommit || authority.Authority.AuthorityRevisionID != a.Authority.RevisionID || authority.Authority.ID != a.Authority.RevisionRowID || authority.Authority.RevisionNumber != a.Authority.RevisionNumber || authority.TicketDesignBrief.RelativePath != a.TicketDesignBrief.RelativePath || authority.TicketDesignBrief.SHA256 != a.TicketDesignBrief.SHA256 || authority.TicketDesignBrief.MediaType != a.TicketDesignBrief.MediaType {
		return false
	}
	if len(authority.AuthorityLayers) != len(a.AuthorityLayers) {
		return false
	}
	for index, layer := range authority.AuthorityLayers {
		if layer.Sequence != a.AuthorityLayers[index].Sequence || layer.Kind != a.AuthorityLayers[index].LayerKind || layer.RelativePath != a.AuthorityLayers[index].RelativePath || layer.MediaType != a.AuthorityLayers[index].MediaType || layer.SHA256 != a.AuthorityLayers[index].SHA256 {
			return false
		}
	}
	operations := ExecutionAssignmentOperations{Presence: "absent"}
	if authority.DeterministicOperations != nil {
		operation := authority.DeterministicOperations
		operations = ExecutionAssignmentOperations{Presence: "present", DisplayName: operation.DisplayName, RelativePath: operation.RelativePath, MediaType: operation.MediaType, SHA256: operation.SHA256, Coverage: operation.Coverage}
	}
	if a.DeterministicOperations != operations {
		return false
	}
	return outcome.Outcome.Run == (DeterministicOutcomeRun{RunID: authority.Run.RunID, RunRowID: authority.Run.ID}) && outcome.Outcome.ExecutionAssignment.ArtifactID == assignment.Artifact.ArtifactID && outcome.Outcome.ExecutionAssignment.ArtifactRowID == assignment.Artifact.ID && outcome.Outcome.ExecutionAssignment.RelativePath == assignment.Artifact.RelativePath && outcome.Outcome.ExecutionAssignment.MediaType == assignment.Artifact.MediaType && outcome.Outcome.ExecutionAssignment.SHA256 == assignment.Artifact.SHA256 && outcome.Outcome.Repository == a.Repository && outcome.Outcome.DeterministicOperations == operations
}

func renderEffectiveExecutorBrief(authority executionpackages.ApprovedAuthority, assignment ExecutionAssignmentResult, outcome DeterministicOutcomeResult, mode EffectiveExecutorBriefMode) ([]byte, string, error) {
	if !validApprovedDocument(authority.TicketDesignBrief.Bytes, authority.TicketDesignBrief.MediaType, authority.TicketDesignBrief.SHA256) {
		return nil, "", ErrEffectiveExecutorBriefConflict
	}
	for _, layer := range authority.AuthorityLayers {
		if !validApprovedDocument(layer.Bytes, layer.MediaType, layer.SHA256) || layer.SizeBytes != int64(len(layer.Bytes)) {
			return nil, "", ErrEffectiveExecutorBriefConflict
		}
	}
	var b strings.Builder
	b.WriteString("# Effective Executor Brief\n\n## Relay Execution Mode\n\n")
	switch mode {
	case EffectiveExecutorBriefAdaptiveNoOperations:
		b.WriteString("- Mode: adaptive_no_operations\n- Adaptive Executor dispatch required: yes\n- Deterministic Operations: absent\n- Required behavior: Implement the complete approved Ticket Design Brief adaptively.\n")
	case EffectiveExecutorBriefAdaptivePreflightFailed:
		b.WriteString("- Mode: adaptive_preflight_failed\n- Adaptive Executor dispatch required: yes\n- Deterministic Operations coverage: " + outcome.Outcome.Outcome.Coverage + "\n- Deterministic application: not performed\n- Required behavior: Implement the complete approved Ticket Design Brief adaptively from the unchanged worktree.\n- Evidence authority: The failure record below is source-state evidence only and is not semantic implementation authority.\n")
	case EffectiveExecutorBriefAdaptiveAfterPartialApplication:
		b.WriteString("- Mode: adaptive_after_partial_application\n- Adaptive Executor dispatch required: yes\n- Deterministic Operations coverage: partial\n- Deterministic application: applied successfully\n- Required behavior: Preserve Relay-applied work and complete the remaining approved Ticket Design Brief obligations adaptively.\n- Prohibition: Do not repeat, revert, repair, complete, or reinterpret the deterministic operations.\n")
	default:
		return nil, "", ErrEffectiveExecutorBriefConflict
	}
	b.WriteString("\n## Execution Identity\n\n")
	b.WriteString("- Run ID: " + authority.Run.RunID + "\n- Package ID: " + authority.Package.PackageID + "\n- Package SHA-256: " + authority.Package.PackageSha256 + "\n- Package approval ID: " + authority.PackageApproval.ApprovalID + "\n- Ticket ID: " + authority.Ticket.TicketID + "\n- Ticket revision: " + fmt.Sprint(authority.TicketRevision.RevisionNumber) + "\n- Repository target: " + authority.Run.RepoTarget + "\n- Branch: " + authority.Run.Branch + "\n- Base commit: " + authority.Run.BaseCommit + "\n- Execution-assignment artifact ID and SHA-256: " + assignment.Artifact.ArtifactID + " " + assignment.Artifact.SHA256 + "\n- Deterministic-outcome artifact ID and SHA-256: " + outcome.Artifact.ArtifactID + " " + outcome.Artifact.SHA256 + "\n")
	b.WriteString("\n## Executor Instruction Authority\n\n- Authority repository: " + assignment.Assignment.ExecutorInstructions.AuthorityRepository + "\n- Authority commit: " + assignment.Assignment.ExecutorInstructions.AuthorityCommit + "\n- Source path: " + assignment.Assignment.ExecutorInstructions.SourcePath + "\n")
	b.WriteString("\n## Deterministic Pre-Application\n\n")
	switch mode {
	case EffectiveExecutorBriefAdaptiveNoOperations:
		b.WriteString("No Deterministic Operations artifact was approved for this package.\n")
	case EffectiveExecutorBriefAdaptivePreflightFailed:
		failure := outcome.Outcome.PreflightFailure
		b.WriteString("Source-state evidence only; it is not semantic implementation authority.\n\n")
		for _, field := range []struct{ name, value string }{{"Code", failure.Code}, {"Operation index", fmt.Sprint(failure.OperationIndex)}, {"Directive index", fmt.Sprint(failure.DirectiveIndex)}, {"Path", failure.Path}, {"Destination", failure.Destination}, {"Expected", failure.Expected}, {"Observed", failure.Observed}} {
			b.WriteString("- " + field.name + ": " + field.value + "\n")
		}
	case EffectiveExecutorBriefAdaptiveAfterPartialApplication:
		application := outcome.Outcome.Application
		b.WriteString("- Coverage: partial\n- Changed paths:\n")
		for _, path := range application.ChangedPaths {
			b.WriteString("  - " + path + "\n")
		}
		b.WriteString("\nApplied operations:\n")
		for _, operation := range application.Operations {
			b.WriteString("- Operation " + fmt.Sprint(operation.Index) + ": " + operation.Operation + "\n  - Source path: " + operation.SourcePath + "\n  - Destination path: " + operation.DestinationPath + "\n")
			for _, state := range []struct {
				name  string
				value DeterministicOutcomeFileState
			}{{"Source before", operation.SourceBefore}, {"Source after", operation.SourceAfter}, {"Destination before", operation.DestinationBefore}, {"Destination after", operation.DestinationAfter}} {
				b.WriteString("  - " + state.name + ": exists=" + fmt.Sprint(state.value.Exists) + " sha256=" + state.value.SHA256 + " size=" + fmt.Sprint(state.value.Size) + "\n")
			}
		}
		b.WriteString("\nThese mutations are already applied source state. They are not a replacement for, subtraction from, or reinterpretation of the approved Ticket Design Brief.\n")
	}
	b.WriteString("\n## Approved Authority Layers\n")
	for _, layer := range authority.AuthorityLayers {
		b.WriteString("\n- Sequence: " + fmt.Sprint(layer.Sequence) + "\n- Kind: " + layer.Kind + "\n- Relative path: " + layer.RelativePath + "\n- Media type: " + layer.MediaType + "\n- SHA-256: " + layer.SHA256 + "\n")
		writeFencedContent(&b, layer.MediaType, layer.Bytes, layer.SizeBytes, layer.SHA256)
	}
	b.WriteString("\n## Approved Ticket Design Brief\n\n- Display name: " + authority.TicketDesignBrief.DisplayName + "\n- Relative path: " + authority.TicketDesignBrief.RelativePath + "\n- Media type: " + authority.TicketDesignBrief.MediaType + "\n- SHA-256: " + authority.TicketDesignBrief.SHA256 + "\n- Byte count: " + fmt.Sprint(len(authority.TicketDesignBrief.Bytes)) + "\n")
	writeFencedContent(&b, authority.TicketDesignBrief.MediaType, authority.TicketDesignBrief.Bytes, int64(len(authority.TicketDesignBrief.Bytes)), authority.TicketDesignBrief.SHA256)
	return []byte(strings.TrimRight(b.String(), "\n") + "\n"), fmt.Sprintf("%s.ticket-%s.r%d.effective-executor-brief.md", authority.Workspace.FeatureSlug, authority.Ticket.TicketID, authority.TicketRevision.RevisionNumber), nil
}

func textualMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "text/") || mediaType == "application/json"
}

func validApprovedDocument(content []byte, mediaType, digest string) bool {
	if !textualMediaType(mediaType) || !utf8.Valid(content) || len(digest) != sha256.Size*2 {
		return false
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]) == digest
}

func writeFencedContent(b *strings.Builder, mediaType string, content []byte, size int64, digest string) {
	b.WriteString("- Source byte count: " + fmt.Sprint(size) + "\n- Source SHA-256: " + digest + "\n")
	if len(content) == 0 || content[len(content)-1] == '\n' {
		fence := deterministicFence(content)
		b.WriteString("\n" + fence + mediaType + "\n")
		b.Write(content)
		b.WriteString(fence + "\n")
		return
	}
	encoded := base64.StdEncoding.EncodeToString(content)
	b.WriteString("- Source representation: base64\n\n```text/base64\n" + encoded + "\n```\n")
}

func deterministicFence(content []byte) string {
	maxRun, run := 0, 0
	for _, value := range content {
		if value == '`' {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", maxRun+3)
}
