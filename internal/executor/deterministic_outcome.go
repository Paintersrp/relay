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

	executionpackages "relay/internal/app/packages"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const (
	deterministicOutcomeKind      = "deterministic_outcome"
	deterministicOutcomeMediaType = "application/json"
	deterministicOutcomeFilename  = "deterministic-outcome.json"
	deterministicEvidenceLimit    = 4096
)

var ErrDeterministicOutcomeConflict = errors.New("deterministic outcome conflicts with recorded execution state")

type DeterministicOutcomeInput struct {
	RunID       string
	Preflight   DeterministicPreflightResult
	Application *DeterministicApplicationResult
}

type DeterministicOutcomeResult struct {
	Artifact workflowstore.Artifact
	Outcome  DeterministicOutcome
	Bytes    []byte
}

// DeterministicOutcome is the canonical, immutable representation of the
// deterministic preflight/application result. Field order defines JSON order.
type DeterministicOutcome struct {
	SchemaVersion           string                                `json:"schema_version"`
	Run                     DeterministicOutcomeRun               `json:"run"`
	ExecutionAssignment     DeterministicOutcomeAssignment        `json:"execution_assignment"`
	Repository              ExecutionAssignmentRepository         `json:"repository"`
	DeterministicOperations ExecutionAssignmentOperations         `json:"deterministic_operations"`
	Outcome                 DeterministicOutcomeSummary           `json:"outcome"`
	PreflightFailure        *DeterministicOutcomePreflightFailure `json:"preflight_failure"`
	Application             *DeterministicApplicationEvidence     `json:"application"`
}

type DeterministicOutcomeRun struct {
	RunID    string `json:"run_id"`
	RunRowID int64  `json:"run_row_id"`
}

type DeterministicOutcomeAssignment struct {
	ArtifactID    string `json:"artifact_id"`
	ArtifactRowID int64  `json:"artifact_row_id"`
	RelativePath  string `json:"relative_path"`
	MediaType     string `json:"media_type"`
	SHA256        string `json:"sha256"`
}

type DeterministicOutcomeSummary struct {
	Status   string `json:"status"`
	Coverage string `json:"coverage,omitempty"`
}

type DeterministicOutcomePreflightFailure struct {
	Code           string `json:"code"`
	OperationIndex int    `json:"operation_index"`
	DirectiveIndex int    `json:"directive_index"`
	Path           string `json:"path"`
	Destination    string `json:"destination"`
	Expected       string `json:"expected"`
	Observed       string `json:"observed"`
}

type DeterministicApplicationEvidence struct {
	Operations   []AppliedDeterministicOperationEvidence `json:"operations"`
	ChangedPaths []string                                `json:"changed_paths"`
}

type AppliedDeterministicOperationEvidence struct {
	Index             int                           `json:"index"`
	Operation         string                        `json:"operation"`
	SourcePath        string                        `json:"source_path"`
	DestinationPath   string                        `json:"destination_path"`
	SourceBefore      DeterministicOutcomeFileState `json:"source_before"`
	SourceAfter       DeterministicOutcomeFileState `json:"source_after"`
	DestinationBefore DeterministicOutcomeFileState `json:"destination_before"`
	DestinationAfter  DeterministicOutcomeFileState `json:"destination_after"`
}

type DeterministicOutcomeFileState struct {
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type DeterministicOutcomeService struct {
	store       *workflowstore.Store
	assignments *ExecutionAssignmentService
}

func NewDeterministicOutcomeService(store *workflowstore.Store) (*DeterministicOutcomeService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	assignments, err := NewExecutionAssignmentService(store)
	if err != nil {
		return nil, err
	}
	return &DeterministicOutcomeService{store: store, assignments: assignments}, nil
}

func (s *DeterministicOutcomeService) Persist(ctx context.Context, input DeterministicOutcomeInput) (DeterministicOutcomeResult, error) {
	if s == nil || s.store == nil || s.assignments == nil {
		return DeterministicOutcomeResult{}, fmt.Errorf("deterministic outcome service is unavailable")
	}
	assignment, err := s.assignments.LoadExecutionAssignment(ctx, input.RunID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	run, err := s.store.GetRunByRunID(ctx, input.RunID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	if run.ID != assignment.Assignment.Run.RunRowID || run.RunID != assignment.Assignment.Run.RunID {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	outcome, content, err := buildDeterministicOutcome(run, assignment, input)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	var existing *workflowstore.Artifact
	for index := range artifacts {
		if artifacts[index].Kind != deterministicOutcomeKind {
			continue
		}
		if existing != nil || artifacts[index].OwnerType != workflowstore.ArtifactOwnerRun || !artifacts[index].RunRowID.Valid || artifacts[index].RunRowID.Int64 != run.ID {
			return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
		}
		candidate := artifacts[index]
		existing = &candidate
	}
	if existing != nil {
		return s.resolveExistingOutcome(*existing, run, outcome, content)
	}
	if run.Status != workflowstore.RunStatusSetupReady {
		return DeterministicOutcomeResult{}, fmt.Errorf("deterministic outcome requires a setup_ready Run")
	}

	batch, err := s.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("runs", run.RunID)))
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	staged, err := batch.Stage(deterministicOutcomeKind, deterministicOutcomeFilename, deterministicOutcomeMediaType, content)
	if err != nil {
		_ = batch.Rollback()
		return DeterministicOutcomeResult{}, err
	}
	var created workflowstore.Artifact
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, createErr := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID: workflowstore.NewArtifactID(), OwnerType: workflowstore.ArtifactOwnerRun,
			RunRowID: sql.NullInt64{Int64: run.ID, Valid: true}, Kind: staged.Kind,
			RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes,
		})
		if createErr != nil {
			return createErr
		}
		created = artifact
		return nil
	})
	if err == nil {
		return deterministicOutcomeResult(created, outcome, content), nil
	}
	if artifacts, listErr := s.store.ListArtifactsByRun(ctx, run.ID); listErr == nil {
		for index := range artifacts {
			if artifacts[index].Kind == deterministicOutcomeKind {
				return s.resolveExistingOutcome(artifacts[index], run, outcome, content)
			}
		}
	}
	return DeterministicOutcomeResult{}, err
}

// Load resolves the one immutable deterministic outcome for a Run.  Unlike
// Persist, it never creates an outcome: it verifies the recorded bytes against
// the current, verified execution assignment and Run identity.
func (s *DeterministicOutcomeService) Load(ctx context.Context, runID string) (DeterministicOutcomeResult, error) {
	if s == nil || s.store == nil || s.assignments == nil {
		return DeterministicOutcomeResult{}, fmt.Errorf("deterministic outcome service is unavailable")
	}
	assignment, err := s.assignments.LoadExecutionAssignment(ctx, runID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	authority, err := s.assignments.packages.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	if run.ID != assignment.Assignment.Run.RunRowID || run.RunID != assignment.Assignment.Run.RunID {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
	if err != nil {
		return DeterministicOutcomeResult{}, err
	}
	var existing *workflowstore.Artifact
	for index := range artifacts {
		if artifacts[index].Kind != deterministicOutcomeKind {
			continue
		}
		if existing != nil || artifacts[index].OwnerType != workflowstore.ArtifactOwnerRun || !artifacts[index].RunRowID.Valid || artifacts[index].RunRowID.Int64 != run.ID {
			return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
		}
		candidate := artifacts[index]
		existing = &candidate
	}
	if existing == nil {
		return DeterministicOutcomeResult{}, fmt.Errorf("Run deterministic_outcome artifact is missing")
	}
	wantPath := filepath.ToSlash(filepath.Join("runs", run.RunID, deterministicOutcomeFilename))
	if existing.OwnerType != workflowstore.ArtifactOwnerRun || !existing.RunRowID.Valid || existing.RunRowID.Int64 != run.ID || existing.RelativePath != wantPath || existing.MediaType != deterministicOutcomeMediaType {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	verified, content, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{Kind: existing.Kind, RelativePath: existing.RelativePath, MediaType: existing.MediaType, SHA256: existing.SHA256, SizeBytes: existing.SizeBytes}, deterministicOutcomeReadLimit)
	if err != nil || verified.RelativePath != existing.RelativePath {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	var outcome DeterministicOutcome
	if err := json.Unmarshal(content, &outcome); err != nil {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	canonical, err := marshalDeterministicOutcome(outcome)
	if err != nil || !bytes.Equal(content, canonical) || !validLoadedDeterministicOutcome(outcome, run, assignment, authority.DeterministicOperations) {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	return deterministicOutcomeResult(*existing, outcome, content), nil
}

const deterministicOutcomeReadLimit = 64 << 20

func validLoadedDeterministicOutcome(outcome DeterministicOutcome, run workflowstore.Run, assignment ExecutionAssignmentResult, approvedOperations *executionpackages.ApprovedDeterministicOperations) bool {
	if outcome.SchemaVersion != "1.0" || outcome.Run != (DeterministicOutcomeRun{RunID: run.RunID, RunRowID: run.ID}) || outcome.Repository != assignment.Assignment.Repository || outcome.DeterministicOperations != assignment.Assignment.DeterministicOperations {
		return false
	}
	wantAssignment := DeterministicOutcomeAssignment{ArtifactID: assignment.Artifact.ArtifactID, ArtifactRowID: assignment.Artifact.ID, RelativePath: assignment.Artifact.RelativePath, MediaType: assignment.Artifact.MediaType, SHA256: assignment.Artifact.SHA256}
	if outcome.ExecutionAssignment != wantAssignment || !validAssignmentOperations(outcome.DeterministicOperations) {
		return false
	}
	switch outcome.Outcome.Status {
	case string(DeterministicPreflightNotPresent):
		return approvedOperations == nil && outcome.DeterministicOperations.Presence == "absent" && outcome.Outcome.Coverage == "" && outcome.PreflightFailure == nil && outcome.Application == nil
	case string(DeterministicPreflightFailed):
		if approvedOperations == nil || outcome.DeterministicOperations.Presence != "present" || outcome.Outcome.Coverage != outcome.DeterministicOperations.Coverage || outcome.PreflightFailure == nil || outcome.Application != nil {
			return false
		}
		_, err := verifiedPreflightFailure(DeterministicPreflightFailure{Code: outcome.PreflightFailure.Code, OperationIndex: outcome.PreflightFailure.OperationIndex, DirectiveIndex: outcome.PreflightFailure.DirectiveIndex, Path: outcome.PreflightFailure.Path, Destination: outcome.PreflightFailure.Destination, Expected: outcome.PreflightFailure.Expected, Observed: outcome.PreflightFailure.Observed})
		return err == nil && validFailureAgainstDocument(*outcome.PreflightFailure, approvedOperations.Document)
	case "applied":
		return approvedOperations != nil && outcome.DeterministicOperations.Presence == "present" && outcome.Outcome.Coverage == outcome.DeterministicOperations.Coverage && outcome.PreflightFailure == nil && validApplicationEvidence(outcome.Application, approvedOperations.Document)
	default:
		return false
	}
}

func validFailureAgainstDocument(value DeterministicOutcomePreflightFailure, document *speccompiler.DeterministicOperationsDocument) bool {
	return document != nil && value.OperationIndex >= 1 && value.OperationIndex <= len(document.Operations) && (value.DirectiveIndex == 0 || (document.Operations[value.OperationIndex-1].Operation == "modify" && value.DirectiveIndex <= len(document.Operations[value.OperationIndex-1].Implementation.Changes)))
}

func validApplicationEvidence(value *DeterministicApplicationEvidence, document *speccompiler.DeterministicOperationsDocument) bool {
	if document == nil || value == nil || len(value.Operations) != len(document.Operations) || len(value.Operations) == 0 {
		return false
	}
	for index, operation := range value.Operations {
		expected := document.Operations[index]
		if operation.Index != index+1 || operation.Operation != expected.Operation || operation.SourcePath != expected.Path || operation.DestinationPath != expected.DestinationPath || !validDeterministicPath(operation.SourcePath) || (operation.DestinationPath != "" && !validDeterministicPath(operation.DestinationPath)) || !validOutcomeFileState(operation.SourceBefore) || !validOutcomeFileState(operation.SourceAfter) || !validOutcomeFileState(operation.DestinationBefore) || !validOutcomeFileState(operation.DestinationAfter) {
			return false
		}
	}
	for _, path := range value.ChangedPaths {
		if !validDeterministicPath(path) {
			return false
		}
	}
	return true
}

func validOutcomeFileState(value DeterministicOutcomeFileState) bool {
	if !value.Exists {
		return value.SHA256 == "" && value.Size == 0
	}
	if len(value.SHA256) != 64 || value.Size < 0 {
		return false
	}
	for _, character := range value.SHA256 {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (s *DeterministicOutcomeService) resolveExistingOutcome(artifact workflowstore.Artifact, run workflowstore.Run, outcome DeterministicOutcome, expected []byte) (DeterministicOutcomeResult, error) {
	wantPath := filepath.ToSlash(filepath.Join("runs", run.RunID, deterministicOutcomeFilename))
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID || artifact.Kind != deterministicOutcomeKind || artifact.RelativePath != wantPath || artifact.MediaType != deterministicOutcomeMediaType {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	verified, content, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{Kind: artifact.Kind, RelativePath: artifact.RelativePath, MediaType: artifact.MediaType, SHA256: artifact.SHA256, SizeBytes: artifact.SizeBytes}, len(expected))
	if err != nil || verified.RelativePath != artifact.RelativePath || !bytes.Equal(content, expected) {
		return DeterministicOutcomeResult{}, ErrDeterministicOutcomeConflict
	}
	return deterministicOutcomeResult(artifact, outcome, content), nil
}

func deterministicOutcomeResult(artifact workflowstore.Artifact, outcome DeterministicOutcome, content []byte) DeterministicOutcomeResult {
	return DeterministicOutcomeResult{Artifact: artifact, Outcome: cloneDeterministicOutcome(outcome), Bytes: append([]byte(nil), content...)}
}

func buildDeterministicOutcome(run workflowstore.Run, assignment ExecutionAssignmentResult, input DeterministicOutcomeInput) (DeterministicOutcome, []byte, error) {
	if input.RunID != run.RunID || assignment.Assignment.Run.RunID != run.RunID || assignment.Assignment.Run.RunRowID != run.ID {
		return DeterministicOutcome{}, nil, fmt.Errorf("deterministic outcome Run identity is inconsistent")
	}
	operations := assignment.Assignment.DeterministicOperations
	if !validAssignmentOperations(operations) {
		return DeterministicOutcome{}, nil, ErrDeterministicOutcomeConflict
	}
	outcome := DeterministicOutcome{
		SchemaVersion:           "1.0",
		Run:                     DeterministicOutcomeRun{RunID: run.RunID, RunRowID: run.ID},
		ExecutionAssignment:     DeterministicOutcomeAssignment{ArtifactID: assignment.Artifact.ArtifactID, ArtifactRowID: assignment.Artifact.ID, RelativePath: assignment.Artifact.RelativePath, MediaType: assignment.Artifact.MediaType, SHA256: assignment.Artifact.SHA256},
		Repository:              assignment.Assignment.Repository,
		DeterministicOperations: operations,
	}

	switch input.Preflight.Status {
	case DeterministicPreflightNotPresent:
		if operations.Presence != "absent" || input.Preflight.Coverage != "" || input.Preflight.Plan != nil || input.Preflight.Failure != nil || input.Application != nil {
			return DeterministicOutcome{}, nil, fmt.Errorf("invalid not_present deterministic outcome")
		}
		outcome.Outcome = DeterministicOutcomeSummary{Status: string(DeterministicPreflightNotPresent)}
	case DeterministicPreflightFailed:
		if operations.Presence != "present" || input.Preflight.Coverage != operations.Coverage || input.Preflight.Plan != nil || input.Preflight.Failure == nil || input.Application != nil {
			return DeterministicOutcome{}, nil, fmt.Errorf("invalid preflight_failed deterministic outcome")
		}
		failure, err := verifiedPreflightFailure(*input.Preflight.Failure)
		if err != nil {
			return DeterministicOutcome{}, nil, err
		}
		outcome.Outcome = DeterministicOutcomeSummary{Status: string(DeterministicPreflightFailed), Coverage: operations.Coverage}
		outcome.PreflightFailure = &failure
	case DeterministicPreflightReady:
		if operations.Presence != "present" || input.Preflight.Coverage != operations.Coverage || input.Preflight.Plan == nil || input.Preflight.Failure != nil || input.Application == nil {
			return DeterministicOutcome{}, nil, fmt.Errorf("invalid applied deterministic outcome")
		}
		model, err := validateDeterministicPlan(input.Preflight.Plan)
		if err != nil || model.coverage != operations.Coverage || input.Application.Coverage != operations.Coverage {
			return DeterministicOutcome{}, nil, fmt.Errorf("invalid applied deterministic plan or coverage")
		}
		expected := applicationResult(model)
		if !equalApplicationResult(*input.Application, expected) {
			return DeterministicOutcome{}, nil, fmt.Errorf("application evidence does not match deterministic plan")
		}
		outcome.Outcome = DeterministicOutcomeSummary{Status: "applied", Coverage: operations.Coverage}
		evidence := applicationEvidence(model, expected.ChangedPaths)
		outcome.Application = &evidence
	default:
		return DeterministicOutcome{}, nil, fmt.Errorf("unsupported deterministic preflight status %q", input.Preflight.Status)
	}
	content, err := marshalDeterministicOutcome(outcome)
	if err != nil {
		return DeterministicOutcome{}, nil, err
	}
	return outcome, content, nil
}

func validAssignmentOperations(value ExecutionAssignmentOperations) bool {
	if value.Presence == "absent" {
		return value.DisplayName == "" && value.RelativePath == "" && value.MediaType == "" && value.SHA256 == "" && value.Coverage == ""
	}
	return value.Presence == "present" && value.DisplayName != "" && value.RelativePath != "" && value.MediaType == "application/json" && len(value.SHA256) == 64 && (value.Coverage == "partial" || value.Coverage == "complete")
}

func verifiedPreflightFailure(value DeterministicPreflightFailure) (DeterministicOutcomePreflightFailure, error) {
	if strings.TrimSpace(value.Code) == "" || strings.TrimSpace(value.Code) != value.Code || value.OperationIndex < 1 || value.DirectiveIndex < 0 || !validOptionalDeterministicPath(value.Path) || !validOptionalDeterministicPath(value.Destination) || !boundedDeterministicEvidence(value.Expected) || !boundedDeterministicEvidence(value.Observed) {
		return DeterministicOutcomePreflightFailure{}, fmt.Errorf("invalid deterministic preflight failure")
	}
	return DeterministicOutcomePreflightFailure{Code: value.Code, OperationIndex: value.OperationIndex, DirectiveIndex: value.DirectiveIndex, Path: value.Path, Destination: value.Destination, Expected: value.Expected, Observed: value.Observed}, nil
}

func validOptionalDeterministicPath(value string) bool {
	return value == "" || validDeterministicPath(value)
}

func boundedDeterministicEvidence(value string) bool {
	return len(value) <= deterministicEvidenceLimit
}

func equalApplicationResult(actual, expected DeterministicApplicationResult) bool {
	if actual.Coverage != expected.Coverage || len(actual.Operations) != len(expected.Operations) || len(actual.ChangedPaths) != len(expected.ChangedPaths) {
		return false
	}
	for index := range expected.Operations {
		if actual.Operations[index] != expected.Operations[index] {
			return false
		}
	}
	for index := range expected.ChangedPaths {
		if actual.ChangedPaths[index] != expected.ChangedPaths[index] {
			return false
		}
	}
	return true
}

func applicationEvidence(model deterministicPlanModel, changedPaths []string) DeterministicApplicationEvidence {
	evidence := DeterministicApplicationEvidence{Operations: make([]AppliedDeterministicOperationEvidence, len(model.operations)), ChangedPaths: append([]string(nil), changedPaths...)}
	for index, operation := range model.operations {
		evidence.Operations[index] = AppliedDeterministicOperationEvidence{
			Index: operation.Index, Operation: operation.Operation, SourcePath: operation.SourcePath, DestinationPath: operation.DestinationPath,
			SourceBefore: outcomeFileState(operation.Before), SourceAfter: outcomeFileState(operation.After),
			DestinationBefore: outcomeFileState(operation.DestinationBefore), DestinationAfter: outcomeFileState(operation.DestinationAfter),
		}
	}
	return evidence
}

func outcomeFileState(value FileState) DeterministicOutcomeFileState {
	return DeterministicOutcomeFileState{Exists: value.Exists, SHA256: value.SHA256, Size: value.Size}
}

func marshalDeterministicOutcome(outcome DeterministicOutcome) ([]byte, error) {
	content, err := json.Marshal(outcome)
	if err != nil {
		return nil, fmt.Errorf("marshal deterministic outcome: %w", err)
	}
	return append(content, '\n'), nil
}

func cloneDeterministicOutcome(value DeterministicOutcome) DeterministicOutcome {
	if value.Application != nil {
		copyApplication := *value.Application
		copyApplication.Operations = append([]AppliedDeterministicOperationEvidence(nil), value.Application.Operations...)
		copyApplication.ChangedPaths = append([]string(nil), value.Application.ChangedPaths...)
		value.Application = &copyApplication
	}
	if value.PreflightFailure != nil {
		copyFailure := *value.PreflightFailure
		value.PreflightFailure = &copyFailure
	}
	return value
}
