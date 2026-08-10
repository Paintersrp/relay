package audits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	workflowpackages "relay/internal/app/packages"
	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
)

const (
	workflowPackageExecutionAssignmentKind  = "execution_assignment"
	workflowPackageDeterministicOutcomeKind = "deterministic_outcome"
)

// ErrWorkflowPackageExecutionEvidenceConflict marks any disagreement between
// the committed runtime evidence and the approved package authority. It never
// covers ordinary infrastructure failures, which remain directly
// distinguishable so callers can separate integrity conflicts from unavailable
// storage.
var ErrWorkflowPackageExecutionEvidenceConflict = errors.New(
	"package execution evidence conflicts with approved audit authority",
)

// PackageAttemptEvidence contains the exact execution attempt and verified
// execution_evidence artifact for an adaptive package execution.
type PackageAttemptEvidence struct {
	Attempt  workflowstore.ExecutionAttempt
	Artifact workflowstore.Artifact
	Bytes    []byte
}

// WorkflowPackageExecutionEvidence is the verified, read-only view of the exact
// approved package authority and the runtime evidence for one committed
// package-linked Run. It never contains newly created rows or artifacts. Mode
// is the non-authoritative ExecutionMode mechanically derived from the verified
// deterministic outcome.
type WorkflowPackageExecutionEvidence struct {
	Run           workflowstore.Run
	Authority     workflowpackages.ApprovedAuthority
	Assignment    executor.ExecutionAssignmentResult
	Deterministic executor.DeterministicOutcomeResult
	Mode          executor.ExecutionMode
	Attempt       *PackageAttemptEvidence
	Validation    []WorkflowPackageAuditValidationResult
}

// WorkflowPackageExecutionEvidenceService resolves and cross-verifies package
// execution evidence for committed-run audit. It only reads; it never prepares,
// persists, transitions, or leases anything.
type WorkflowPackageExecutionEvidenceService struct {
	store       *workflowstore.Store
	packages    *workflowpackages.Service
	assignments *executor.ExecutionAssignmentService
	outcomes    *executor.DeterministicOutcomeService

	// Narrow package-private read seams. Production defaults call the real
	// existing services; tests override them to inject cross-service return
	// shapes that valid storage constraints cannot produce.
	loadRun              func(context.Context, string) (workflowstore.Run, error)
	loadAuthority        func(context.Context, string) (workflowpackages.ApprovedAuthority, error)
	loadAssignment       func(context.Context, string) (executor.ExecutionAssignmentResult, error)
	loadOutcome          func(context.Context, string) (executor.DeterministicOutcomeResult, error)
	loadAttempts         func(context.Context, int64) ([]workflowstore.ExecutionAttempt, error)
	loadAttemptArtifacts func(context.Context, int64) ([]workflowstore.Artifact, error)
	readArtifactBytes    func(context.Context, workflowstore.Artifact, int) ([]byte, error)
}

func NewWorkflowPackageExecutionEvidenceService(store *workflowstore.Store, sourceVaults workflowpackages.SourceVaultReader) (*WorkflowPackageExecutionEvidenceService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	packages, err := workflowpackages.NewServiceWithSourceVaults(
		store,
		sourceVaults,
	)
	if err != nil {
		return nil, err
	}

	assignments, err := executor.NewExecutionAssignmentService(
		store,
		sourceVaults,
	)
	if err != nil {
		return nil, err
	}

	outcomes, err := executor.NewDeterministicOutcomeService(
		store,
		sourceVaults,
	)
	if err != nil {
		return nil, err
	}

	service := &WorkflowPackageExecutionEvidenceService{
		store: store, packages: packages, assignments: assignments, outcomes: outcomes,
	}
	service.loadRun = func(ctx context.Context, runID string) (workflowstore.Run, error) {
		return service.store.GetRunByRunID(ctx, runID)
	}
	service.loadAuthority = func(ctx context.Context, runID string) (workflowpackages.ApprovedAuthority, error) {
		return service.packages.LoadApprovedAuthorityForRun(ctx, runID)
	}
	service.loadAssignment = func(ctx context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
		return service.assignments.LoadExecutionAssignment(ctx, runID)
	}
	service.loadOutcome = func(ctx context.Context, runID string) (executor.DeterministicOutcomeResult, error) {
		return service.outcomes.Load(ctx, runID)
	}
	service.loadAttempts = func(ctx context.Context, runRowID int64) ([]workflowstore.ExecutionAttempt, error) {
		return service.store.ListExecutionAttemptsByRun(ctx, runRowID)
	}
	service.loadAttemptArtifacts = func(ctx context.Context, attemptRowID int64) ([]workflowstore.Artifact, error) {
		return service.store.ListArtifactsByExecutionAttempt(ctx, attemptRowID)
	}
	service.readArtifactBytes = func(ctx context.Context, artifact workflowstore.Artifact, maxBytes int) ([]byte, error) {
		return readWorkflowArtifact(service.store, artifact, maxBytes)
	}
	return service, nil
}

// Load resolves and cross-verifies the exact approved package authority and the
// immutable runtime evidence artifacts for a committed package-linked Run.
// It performs no writes or lifecycle changes and returns only existing,
// verified evidence.
func (s *WorkflowPackageExecutionEvidenceService) Load(ctx context.Context, runID string) (WorkflowPackageExecutionEvidence, error) {
	if s == nil || s.store == nil || s.packages == nil || s.assignments == nil || s.outcomes == nil {
		return WorkflowPackageExecutionEvidence{}, fmt.Errorf("package execution evidence service is unavailable")
	}
	if runID == "" || strings.TrimSpace(runID) != runID {
		return WorkflowPackageExecutionEvidence{}, fmt.Errorf("Run ID must be nonblank without outer whitespace")
	}

	run, err := s.loadRun(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if run.RunID != runID {
		return WorkflowPackageExecutionEvidence{}, evidenceConflict("Run identity does not match the requested Run ID")
	}
	if !run.ExecutionPackageRowID.Valid || run.ExecutionPackageRowID.Int64 <= 0 || !run.PackageApprovalRowID.Valid || run.PackageApprovalRowID.Int64 <= 0 {
		return WorkflowPackageExecutionEvidence{}, evidenceConflict("Run is not package-linked")
	}

	authority, err := s.loadAuthority(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, workflowPackageEvidenceReadError("authority", err)
	}
	assignment, err := s.loadAssignment(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, workflowPackageEvidenceReadError("assignment", err)
	}
	outcome, err := s.loadOutcome(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, workflowPackageEvidenceReadError("outcome", err)
	}

	if err := verifyWorkflowPackageRunAndPackage(run, authority); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageArtifact(assignment.Artifact, workflowPackageExecutionAssignmentKind, run, assignment.Bytes); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageArtifact(outcome.Artifact, workflowPackageDeterministicOutcomeKind, run, outcome.Bytes); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageAssignment(run, authority, assignment); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageOutcome(run, assignment, outcome); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}

	mode, _, err := deriveWorkflowPackageExecutionMode(outcome.Outcome.Outcome)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}

	var pkgAttempt *PackageAttemptEvidence
	var validation []WorkflowPackageAuditValidationResult

	switch mode {
	case executor.ExecutionModeAbsent,
		executor.ExecutionModePreflightFailed,
		executor.ExecutionModePartialApplied:
		attempts, err := s.loadAttempts(ctx, run.ID)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, err
		}
		if len(attempts) != 1 {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("adaptive package execution requires exactly one execution attempt, found %d", len(attempts))
		}
		attempt := attempts[0]
		if attempt.RunRowID != run.ID {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution attempt Run row ID does not match Run")
		}
		if attempt.AttemptNumber != 1 {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution attempt number must be 1, got %d", attempt.AttemptNumber)
		}
		if attempt.Status != string(workflowstore.AttemptStatusSucceeded) {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution attempt status must be succeeded, got %q", attempt.Status)
		}
		if attempt.AttemptID == "" || strings.TrimSpace(attempt.AttemptID) != attempt.AttemptID {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution attempt ID is noncanonical or blank")
		}

		artifacts, err := s.loadAttemptArtifacts(ctx, attempt.ID)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, err
		}

		var evidenceArtifact workflowstore.Artifact
		evidenceCount := 0
		for _, art := range artifacts {
			if art.Kind == "execution_evidence" {
				evidenceArtifact = art
				evidenceCount++
			}
		}
		if evidenceCount != 1 {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("expected exactly one execution_evidence artifact for attempt, found %d", evidenceCount)
		}
		if evidenceArtifact.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !evidenceArtifact.ExecutionAttemptRowID.Valid || evidenceArtifact.ExecutionAttemptRowID.Int64 != attempt.ID {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact is not owned by the attempt")
		}
		if evidenceArtifact.MediaType != "application/json" {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact media type must be application/json")
		}
		wantPath := fmt.Sprintf("attempts/%s/execution-evidence.json", attempt.AttemptID)
		if evidenceArtifact.RelativePath != wantPath {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact relative path %q does not match expected %q", evidenceArtifact.RelativePath, wantPath)
		}
		if evidenceArtifact.ID <= 0 || strings.TrimSpace(evidenceArtifact.ArtifactID) == "" {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact identity is incomplete")
		}
		if !validWorkflowPackageSHA256(evidenceArtifact.SHA256) {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact digest is malformed")
		}

		data, err := s.readArtifactBytes(ctx, evidenceArtifact, MaxWorkflowAuditEvidenceBytes)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("read execution_evidence artifact: %w", err)
		}
		if evidenceArtifact.SizeBytes < 0 || evidenceArtifact.SizeBytes != int64(len(data)) {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact size does not match verified bytes")
		}
		if hex.EncodeToString(hashWorkflowPackageBytes(data)) != evidenceArtifact.SHA256 {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution_evidence artifact digest does not match verified bytes")
		}

		hasValidationResults, err := hasWorkflowExecutionEvidenceValidationResultsProperty(data)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("decode execution_evidence artifact: %w", err)
		}

		payload, err := decodeWorkflowExecutionEvidence(data)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("decode execution_evidence artifact: %w", err)
		}

		attemptResult := workflowAuditAttemptResult(attempt.ResultJSON)
		wireMode, wireModeValid := workflowPackageWireMode(mode)
		if !wireModeValid {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("unsupported adaptive mode %q for package execution", mode)
		}

		if payload.ExecutionAssignmentArtifactID != assignment.Artifact.ArtifactID ||
			payload.ExecutionAssignmentSHA256 != assignment.Artifact.SHA256 ||
			payload.ExecutionAssignmentMode != wireMode {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("execution evidence payload execution assignment identity disagrees with assignment")
		}
		if attemptResult.ExecutionAssignmentArtifactID != assignment.Artifact.ArtifactID ||
			attemptResult.ExecutionAssignmentSHA256 != assignment.Artifact.SHA256 ||
			attemptResult.ExecutionAssignmentMode != wireMode {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("attempt ResultJSON execution assignment identity disagrees with assignment")
		}

		valResults, err := mapPackageAdaptiveValidation(assignment.Assignment.ValidationCommands, payload.ValidationResults, hasValidationResults)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("validation mapping: %w", err)
		}
		validation = valResults

		pkgAttempt = &PackageAttemptEvidence{
			Attempt:  attempt,
			Artifact: evidenceArtifact,
			Bytes:    append([]byte(nil), data...),
		}

	case executor.ExecutionModeCompleteApplied:
		attempts, err := s.loadAttempts(ctx, run.ID)
		if err != nil {
			return WorkflowPackageExecutionEvidence{}, err
		}
		if len(attempts) != 0 {
			return WorkflowPackageExecutionEvidence{}, evidenceConflict("deterministic-complete execution requires zero execution attempts, found %d", len(attempts))
		}
		pkgAttempt = nil
		valResults := make([]WorkflowPackageAuditValidationResult, 0, len(assignment.Assignment.ValidationCommands))
		for _, cmd := range assignment.Assignment.ValidationCommands {
			valResults = append(valResults, WorkflowPackageAuditValidationResult{
				Command:       cmd.Command,
				Expected:      cmd.Expected,
				Status:        "not_run",
				ConciseResult: "No adaptive Executor attempt was dispatched for deterministic-complete execution.",
			})
		}
		validation = valResults

	default:
		return WorkflowPackageExecutionEvidence{}, evidenceConflict("unsupported execution mode %q for package execution", mode)
	}

	var attemptCopy *PackageAttemptEvidence
	if pkgAttempt != nil {
		attemptCopy = &PackageAttemptEvidence{
			Attempt:  pkgAttempt.Attempt,
			Artifact: pkgAttempt.Artifact,
			Bytes:    append([]byte(nil), pkgAttempt.Bytes...),
		}
	}
	valCopy := make([]WorkflowPackageAuditValidationResult, len(validation))
	copy(valCopy, validation)

	return WorkflowPackageExecutionEvidence{
		Run:           run,
		Authority:     authority,
		Assignment:    assignment,
		Deterministic: outcome,
		Mode:          mode,
		Attempt:       attemptCopy,
		Validation:    valCopy,
	}, nil
}

func mapPackageAdaptiveValidation(
	commands []executor.ExecutionAssignmentValidationCommand,
	results []workflowAuditValidationEvidence,
	hasValidationResults bool,
) ([]WorkflowPackageAuditValidationResult, error) {
	if len(commands) == 0 {
		if hasValidationResults {
			return nil, fmt.Errorf("execution evidence contains validation_results property but assignment declares no validation commands")
		}
		return []WorkflowPackageAuditValidationResult{}, nil
	}

	type cmdMeta struct {
		cmd   executor.ExecutionAssignmentValidationCommand
		index int
	}
	cmdMap := make(map[string]cmdMeta, len(commands))
	for idx, cmd := range commands {
		cmdMap[cmd.Command] = cmdMeta{cmd: cmd, index: idx}
	}

	matchedMap := make(map[string]workflowAuditValidationEvidence, len(results))
	lastIndex := -1
	for _, res := range results {
		meta, ok := cmdMap[res.Command]
		if !ok {
			return nil, fmt.Errorf("execution evidence reports unknown validation command %q", res.Command)
		}
		if meta.index <= lastIndex {
			return nil, fmt.Errorf("execution evidence validation results are not in canonical order")
		}
		lastIndex = meta.index
		if res.WorkingDirectory != meta.cmd.WorkingDirectory {
			return nil, fmt.Errorf("execution evidence validation working directory %q does not match canonical %q", res.WorkingDirectory, meta.cmd.WorkingDirectory)
		}
		if res.Expected != meta.cmd.Expected {
			return nil, fmt.Errorf("execution evidence validation expected %q does not match canonical %q", res.Expected, meta.cmd.Expected)
		}
		matchedMap[res.Command] = res
	}

	out := make([]WorkflowPackageAuditValidationResult, 0, len(commands))
	for _, cmd := range commands {
		if res, ok := matchedMap[cmd.Command]; ok {
			out = append(out, WorkflowPackageAuditValidationResult{
				Command:       cmd.Command,
				Expected:      cmd.Expected,
				Status:        res.Status,
				ConciseResult: res.ConciseResult,
			})
		} else {
			out = append(out, WorkflowPackageAuditValidationResult{
				Command:       cmd.Command,
				Expected:      cmd.Expected,
				Status:        "not_run",
				ConciseResult: "No trustworthy structured result was available for this approved validation command.",
			})
		}
	}
	return out, nil
}

func verifyWorkflowPackageRunAndPackage(run workflowstore.Run, authority workflowpackages.ApprovedAuthority) error {
	if run.ID != authority.Run.ID || run.RunID != authority.Run.RunID {
		return evidenceConflict("Run identity does not match approved authority")
	}
	if authority.Package.ID <= 0 || authority.Package.ID != run.ExecutionPackageRowID.Int64 {
		return evidenceConflict("Run execution-package linkage is inconsistent")
	}
	if authority.PackageApproval.ID <= 0 || authority.PackageApproval.ID != run.PackageApprovalRowID.Int64 || authority.PackageApproval.PackageRowID != authority.Package.ID {
		return evidenceConflict("Run package-approval linkage is inconsistent")
	}
	if authority.Package.PackageSha256 == "" || authority.Package.PackageSha256 != authority.PackageApproval.PackageSha256 {
		return evidenceConflict("approved package digest does not match approval")
	}
	if run.RepoTarget != authority.Package.RepoTarget || run.Branch != authority.Package.Branch || run.BaseCommit != authority.Package.BaseCommit {
		return evidenceConflict("Run repository basis does not match approved package")
	}
	if run.FeatureSlug != authority.Workspace.FeatureSlug {
		return evidenceConflict("Run feature does not match approved workspace")
	}
	if authority.Ticket.ID == 0 || authority.Ticket.TicketID == "" || authority.TicketRevision.ID == 0 || authority.TicketRevision.RevisionNumber == 0 {
		return evidenceConflict("approved Ticket and revision identities are incomplete")
	}
	return nil
}

func verifyWorkflowPackageAssignment(run workflowstore.Run, authority workflowpackages.ApprovedAuthority, assignment executor.ExecutionAssignmentResult) error {
	a := assignment.Assignment
	if a.Run.RunRowID != run.ID || a.Run.RunID != run.RunID {
		return evidenceConflict("assignment Run identity does not match approved authority")
	}
	if a.Package.PackageRowID != authority.Package.ID || a.Package.PackageID != authority.Package.PackageID || a.Package.SHA256 != authority.Package.PackageSha256 {
		return evidenceConflict("assignment package identity does not match approved authority")
	}
	if a.PackageApproval.ApprovalRowID != authority.PackageApproval.ID || a.PackageApproval.ApprovalID != authority.PackageApproval.ApprovalID || a.PackageApproval.ApprovedPackageSHA256 != authority.PackageApproval.PackageSha256 {
		return evidenceConflict("assignment package approval does not match approved authority")
	}
	if a.Ticket.TicketRowID != authority.Ticket.ID || a.Ticket.TicketID != authority.Ticket.TicketID || a.Ticket.RevisionRowID != authority.TicketRevision.ID || a.Ticket.RevisionNumber != authority.TicketRevision.RevisionNumber {
		return evidenceConflict("assignment Ticket identity does not match approved authority")
	}
	if a.Ticket.DeliveryApprovalRowID != authority.TicketApproval.ID || a.Ticket.DeliveryApprovalID != authority.TicketApproval.ApprovalID {
		return evidenceConflict("assignment delivery approval does not match approved authority")
	}
	if a.Repository.Target != authority.Run.RepoTarget || a.Repository.Branch != authority.Run.Branch || a.Repository.BaseCommit != authority.Run.BaseCommit {
		return evidenceConflict("assignment repository basis does not match approved authority")
	}
	if a.Source.ClosureRowID != authority.Source.ID || a.Source.ClosureID != authority.Source.ClosureID || a.Source.SHA256 != authority.Package.SourceSha256 {
		return evidenceConflict("assignment source closure does not match approved authority")
	}
	if a.Source.CommitOID != authority.Source.CommitOID || a.Source.TreeOID != authority.Source.TreeOID || a.Source.Generation != authority.Source.Generation || a.Source.RefName != authority.Source.RefName || a.Source.State != authority.Source.State {
		return evidenceConflict("assignment source closure identity does not match approved authority")
	}
	if len(a.Dependencies) != len(authority.CompletedDependencies) {
		return evidenceConflict("assignment completed-dependency count does not match approved authority")
	}
	for index, dep := range authority.CompletedDependencies {
		got := a.Dependencies[index]
		if got.Sequence != dep.Sequence || got.TicketID != dep.TicketID || got.Revision != dep.Revision || got.Outcome != dep.Outcome {
			return evidenceConflict("assignment completed dependency %d does not match approved authority", index+1)
		}
	}
	if a.Authority.RevisionRowID != authority.Authority.ID || a.Authority.RevisionID != authority.Authority.AuthorityRevisionID || a.Authority.RevisionNumber != authority.Authority.RevisionNumber || a.Authority.AuthorityBasisSHA256 != authority.Package.AuthoritySha256 {
		return evidenceConflict("assignment authority basis does not match approved authority")
	}
	if a.DeliveryTicket.DisplayName != authority.DeliveryTicket.DisplayName || a.DeliveryTicket.RelativePath != authority.DeliveryTicket.RelativePath || a.DeliveryTicket.MediaType != authority.DeliveryTicket.MediaType || a.DeliveryTicket.SHA256 != authority.DeliveryTicket.SHA256 {
		return evidenceConflict("assignment source document does not match approved authority")
	}
	if len(a.AuthorityLayers) != len(authority.AuthorityLayers) {
		return evidenceConflict("assignment authority-layer count does not match approved authority")
	}
	for index, layer := range authority.AuthorityLayers {
		got := a.AuthorityLayers[index]
		if got.Sequence != layer.Sequence || got.LayerKind != layer.Kind || got.RelativePath != layer.RelativePath || got.MediaType != layer.MediaType || got.SHA256 != layer.SHA256 {
			return evidenceConflict("assignment authority layer %d does not match approved authority", index+1)
		}
	}
	if a.DeterministicOperations != expectedAssignmentOperations(authority) {
		return evidenceConflict("assignment Deterministic Operations do not match approved authority")
	}
	if len(a.ValidationCommands) != len(authority.TicketProjection.ValidationCommands) {
		return evidenceConflict("assignment validation command count does not match approved Delivery Ticket")
	}
	for index, command := range authority.TicketProjection.ValidationCommands {
		got := a.ValidationCommands[index]
		if got.WorkingDirectory != command.WorkingDirectory || got.Command != command.Command || got.Expected != command.Expected {
			return evidenceConflict("assignment validation command %d does not match approved Delivery Ticket", index+1)
		}
	}
	return nil
}

func expectedAssignmentOperations(authority workflowpackages.ApprovedAuthority) executor.ExecutionAssignmentOperations {
	if authority.DeterministicOperations == nil {
		return executor.ExecutionAssignmentOperations{Presence: "absent"}
	}
	operation := authority.DeterministicOperations
	return executor.ExecutionAssignmentOperations{
		Presence:     "present",
		DisplayName:  operation.DisplayName,
		RelativePath: operation.RelativePath,
		MediaType:    operation.MediaType,
		SHA256:       operation.SHA256,
		Coverage:     operation.Coverage,
	}
}

func verifyWorkflowPackageOutcome(run workflowstore.Run, assignment executor.ExecutionAssignmentResult, outcome executor.DeterministicOutcomeResult) error {
	document := outcome.Outcome
	if document.Run.RunID != run.RunID || document.Run.RunRowID != run.ID {
		return evidenceConflict("deterministic outcome Run identity does not match")
	}
	if document.ExecutionAssignment.ArtifactRowID != assignment.Artifact.ID ||
		document.ExecutionAssignment.ArtifactID != assignment.Artifact.ArtifactID ||
		document.ExecutionAssignment.RelativePath != assignment.Artifact.RelativePath ||
		document.ExecutionAssignment.MediaType != assignment.Artifact.MediaType ||
		document.ExecutionAssignment.SHA256 != assignment.Artifact.SHA256 {
		return evidenceConflict("deterministic outcome does not reference the exact execution assignment")
	}
	if document.Repository != assignment.Assignment.Repository {
		return evidenceConflict("deterministic outcome repository identity does not match assignment")
	}
	if document.DeterministicOperations != assignment.Assignment.DeterministicOperations {
		return evidenceConflict("deterministic outcome Deterministic Operations do not match assignment")
	}
	return nil
}

// deriveWorkflowPackageExecutionMode mechanically derives the non-authoritative
// runtime ExecutionMode from the verified deterministic outcome summary. It is
// the audit owner's own derivation: the mode never carries semantic
// implementation authority, and the approved Delivery Ticket remains the sole
// semantic authority for an adaptive Executor.
func deriveWorkflowPackageExecutionMode(summary executor.DeterministicOutcomeSummary) (executor.ExecutionMode, bool, error) {
	switch summary.Status {
	case string(executor.DeterministicPreflightNotPresent):
		if summary.Coverage != "" {
			return "", false, evidenceConflict("not_present deterministic outcome must have no coverage")
		}
		return executor.ExecutionModeAbsent, true, nil
	case string(executor.DeterministicPreflightFailed):
		switch summary.Coverage {
		case "partial", "complete":
			return executor.ExecutionModePreflightFailed, true, nil
		}
		return "", false, evidenceConflict("preflight_failed deterministic outcome coverage must be partial or complete")
	case "applied":
		switch summary.Coverage {
		case "partial":
			return executor.ExecutionModePartialApplied, true, nil
		case "complete":
			return executor.ExecutionModeCompleteApplied, false, nil
		}
	}
	return "", false, evidenceConflict("deterministic outcome status or coverage is unsupported")
}

// workflowPackageWireMode maps an adaptive ExecutionMode to the legacy mode
// string carried by the executor runtime records and the workflowruns
// BeginPreparedAdaptiveExecution wire contract. Deterministic completion is not
// an adaptive wire mode.
func workflowPackageWireMode(mode executor.ExecutionMode) (string, bool) {
	switch mode {
	case executor.ExecutionModeAbsent:
		return "adaptive_no_operations", true
	case executor.ExecutionModePreflightFailed:
		return "adaptive_preflight_failed", true
	case executor.ExecutionModePartialApplied:
		return "adaptive_after_partial_application", true
	default:
		return "", false
	}
}

func verifyWorkflowPackageArtifact(artifact workflowstore.Artifact, wantKind string, run workflowstore.Run, content []byte) error {
	if artifact.OwnerType != workflowstore.ArtifactOwnerRun || !artifact.RunRowID.Valid || artifact.RunRowID.Int64 != run.ID {
		return evidenceConflict("%s artifact is not owned by the exact Run", wantKind)
	}
	if artifact.Kind != wantKind {
		return evidenceConflict("%s artifact kind is inconsistent", wantKind)
	}
	if artifact.ID <= 0 || strings.TrimSpace(artifact.ArtifactID) == "" || strings.TrimSpace(artifact.RelativePath) == "" || strings.TrimSpace(artifact.MediaType) == "" {
		return evidenceConflict("%s artifact identity is incomplete", wantKind)
	}
	if !validWorkflowPackageSHA256(artifact.SHA256) {
		return evidenceConflict("%s artifact digest is malformed", wantKind)
	}
	if artifact.SizeBytes < 0 || artifact.SizeBytes != int64(len(content)) {
		return evidenceConflict("%s artifact size does not match verified bytes", wantKind)
	}
	if hex.EncodeToString(hashWorkflowPackageBytes(content)) != artifact.SHA256 {
		return evidenceConflict("%s artifact digest does not match verified bytes", wantKind)
	}
	return nil
}

func hashWorkflowPackageBytes(content []byte) []byte {
	sum := sha256.Sum256(content)
	return sum[:]
}

func validWorkflowPackageSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func evidenceConflict(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrWorkflowPackageExecutionEvidenceConflict}, args...)...)
}

const (
	workflowPackageExecutionAssignmentMissing    = "Run execution_assignment artifact is missing"
	workflowPackageExecutionDeterministicMissing = "Run deterministic_outcome artifact is missing"
)

// workflowPackageEvidenceReadError classifies loader-originated errors that
// represent missing, duplicate, changed, malformed, or contradictory
// immutable evidence as audit evidence conflicts. It preserves the original
// error so callers can still detect the underlying sentinel.
func workflowPackageEvidenceReadError(stage string, err error) error {
	if err == nil {
		return nil
	}
	switch stage {
	case "authority":
		if errors.Is(err, workflowpackages.ErrRunNotPackage) ||
			errors.Is(err, workflowpackages.ErrPackageApprovalMissing) ||
			errors.Is(err, workflowpackages.ErrApprovedAuthorityInvalid) {
			return fmt.Errorf("%w: %s read conflict: %w", ErrWorkflowPackageExecutionEvidenceConflict, stage, err)
		}
	case "assignment":
		if errors.Is(err, executor.ErrExecutionAssignmentConflict) {
			return fmt.Errorf("%w: %s read conflict: %w", ErrWorkflowPackageExecutionEvidenceConflict, stage, err)
		}
		if err.Error() == workflowPackageExecutionAssignmentMissing {
			return fmt.Errorf("%w: %s read conflict: %w", ErrWorkflowPackageExecutionEvidenceConflict, stage, err)
		}
	case "outcome":
		if errors.Is(err, executor.ErrDeterministicOutcomeConflict) {
			return fmt.Errorf("%w: %s read conflict: %w", ErrWorkflowPackageExecutionEvidenceConflict, stage, err)
		}
		if err.Error() == workflowPackageExecutionDeterministicMissing {
			return fmt.Errorf("%w: %s read conflict: %w", ErrWorkflowPackageExecutionEvidenceConflict, stage, err)
		}
	}
	return err
}

func hasWorkflowExecutionEvidenceValidationResultsProperty(data []byte) (bool, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return false, err
	}
	_, present := root["validation_results"]
	return present, nil
}
