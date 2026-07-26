package audits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	workflowPackageEffectiveBriefKind       = "effective_executor_brief"
)

// ErrWorkflowPackageExecutionEvidenceConflict marks any disagreement between
// the committed runtime evidence and the approved package authority. It never
// covers ordinary infrastructure failures, which remain directly
// distinguishable so callers can separate integrity conflicts from unavailable
// storage.
var ErrWorkflowPackageExecutionEvidenceConflict = errors.New(
	"package execution evidence conflicts with approved audit authority",
)

// WorkflowPackageExecutionEvidence is the verified, read-only view of the exact
// approved package authority and the three immutable runtime evidence
// artifacts for one committed package-linked Run. It never contains newly
// created rows or artifacts.
type WorkflowPackageExecutionEvidence struct {
	Run            workflowstore.Run
	Authority      workflowpackages.ApprovedAuthority
	Assignment     executor.ExecutionAssignmentResult
	Deterministic  executor.DeterministicOutcomeResult
	EffectiveBrief executor.EffectiveExecutorBriefResult
}

// WorkflowPackageExecutionEvidenceService resolves and cross-verifies package
// execution evidence for committed-run audit. It only reads; it never prepares,
// persists, transitions, or leases anything.
type WorkflowPackageExecutionEvidenceService struct {
	store       *workflowstore.Store
	packages    *workflowpackages.Service
	assignments *executor.ExecutionAssignmentService
	outcomes    *executor.DeterministicOutcomeService
	briefs      *executor.EffectiveExecutorBriefService

	// Narrow package-private read seams. Production defaults call the real
	// existing services; tests override them to inject cross-service return
	// shapes that valid storage constraints cannot produce.
	loadRun        func(context.Context, string) (workflowstore.Run, error)
	loadAuthority  func(context.Context, string) (workflowpackages.ApprovedAuthority, error)
	loadAssignment func(context.Context, string) (executor.ExecutionAssignmentResult, error)
	loadOutcome    func(context.Context, string) (executor.DeterministicOutcomeResult, error)
	loadBrief      func(context.Context, string) (executor.EffectiveExecutorBriefResult, error)
}

func NewWorkflowPackageExecutionEvidenceService(store *workflowstore.Store) (*WorkflowPackageExecutionEvidenceService, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	packages, err := workflowpackages.NewService(store)
	if err != nil {
		return nil, err
	}
	assignments, err := executor.NewExecutionAssignmentService(store)
	if err != nil {
		return nil, err
	}
	outcomes, err := executor.NewDeterministicOutcomeService(store)
	if err != nil {
		return nil, err
	}
	briefs, err := executor.NewEffectiveExecutorBriefService(store)
	if err != nil {
		return nil, err
	}
	service := &WorkflowPackageExecutionEvidenceService{
		store: store, packages: packages, assignments: assignments, outcomes: outcomes, briefs: briefs,
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
	service.loadBrief = func(ctx context.Context, runID string) (executor.EffectiveExecutorBriefResult, error) {
		return service.briefs.Load(ctx, runID)
	}
	return service, nil
}

// Load resolves and cross-verifies the exact approved package authority and the
// three immutable runtime evidence artifacts for a committed package-linked
// Run. It performs no writes or lifecycle changes and returns only existing,
// verified evidence.
func (s *WorkflowPackageExecutionEvidenceService) Load(ctx context.Context, runID string) (WorkflowPackageExecutionEvidence, error) {
	if s == nil || s.store == nil || s.packages == nil || s.assignments == nil || s.outcomes == nil || s.briefs == nil {
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
		return WorkflowPackageExecutionEvidence{}, err
	}
	assignment, err := s.loadAssignment(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	outcome, err := s.loadOutcome(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	brief, err := s.loadBrief(ctx, runID)
	if err != nil {
		return WorkflowPackageExecutionEvidence{}, err
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
	if brief.Artifact == nil {
		return WorkflowPackageExecutionEvidence{}, evidenceConflict("effective Executor Brief artifact is missing")
	}
	if err := verifyWorkflowPackageArtifact(*brief.Artifact, workflowPackageEffectiveBriefKind, run, brief.Bytes); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageAssignment(run, authority, assignment); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageOutcome(run, assignment, outcome); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}
	if err := verifyWorkflowPackageEffectiveBrief(run, outcome, brief); err != nil {
		return WorkflowPackageExecutionEvidence{}, err
	}

	return WorkflowPackageExecutionEvidence{
		Run:            run,
		Authority:      authority,
		Assignment:     assignment,
		Deterministic:  outcome,
		EffectiveBrief: brief,
	}, nil
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
	if a.Authority.RevisionRowID != authority.Authority.ID || a.Authority.RevisionID != authority.Authority.AuthorityRevisionID || a.Authority.RevisionNumber != authority.Authority.RevisionNumber || a.Authority.AuthorityBasisSHA256 != authority.Package.AuthoritySha256 {
		return evidenceConflict("assignment authority basis does not match approved authority")
	}
	if a.TicketDesignBrief.DisplayName != authority.TicketDesignBrief.DisplayName || a.TicketDesignBrief.RelativePath != authority.TicketDesignBrief.RelativePath || a.TicketDesignBrief.MediaType != authority.TicketDesignBrief.MediaType || a.TicketDesignBrief.SHA256 != authority.TicketDesignBrief.SHA256 {
		return evidenceConflict("assignment Ticket Design Brief does not match approved authority")
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
	if len(a.ValidationCommands) != len(authority.BriefProjection.ValidationCommands) {
		return evidenceConflict("assignment validation command count does not match approved Brief")
	}
	for index, command := range authority.BriefProjection.ValidationCommands {
		got := a.ValidationCommands[index]
		if got.WorkingDirectory != command.WorkingDirectory || got.Command != command.Command || got.Expected != command.Expected {
			return evidenceConflict("assignment validation command %d does not match approved Brief", index+1)
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

func verifyWorkflowPackageEffectiveBrief(run workflowstore.Run, outcome executor.DeterministicOutcomeResult, brief executor.EffectiveExecutorBriefResult) error {
	mode, adaptive, err := deriveWorkflowPackageEffectiveMode(outcome.Outcome.Outcome)
	if err != nil {
		return err
	}
	if brief.Mode != mode {
		return evidenceConflict("effective Brief mode %q does not match deterministic outcome", brief.Mode)
	}
	if brief.AdaptiveDispatchRequired != adaptive {
		return evidenceConflict("effective Brief adaptive dispatch requirement disagrees with mode")
	}
	if len(brief.Bytes) == 0 {
		return evidenceConflict("effective Brief bytes are empty")
	}
	return nil
}

func deriveWorkflowPackageEffectiveMode(summary executor.DeterministicOutcomeSummary) (executor.EffectiveExecutorBriefMode, bool, error) {
	switch summary.Status {
	case string(executor.DeterministicPreflightNotPresent):
		if summary.Coverage != "" {
			return "", false, evidenceConflict("not_present deterministic outcome must have no coverage")
		}
		return executor.EffectiveExecutorBriefAdaptiveNoOperations, true, nil
	case string(executor.DeterministicPreflightFailed):
		return executor.EffectiveExecutorBriefAdaptivePreflightFailed, true, nil
	case "applied":
		switch summary.Coverage {
		case "partial":
			return executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication, true, nil
		case "complete":
			return executor.EffectiveExecutorBriefDeterministicComplete, false, nil
		}
	}
	return "", false, evidenceConflict("deterministic outcome status or coverage is unsupported")
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
