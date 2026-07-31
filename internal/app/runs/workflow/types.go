package workflowruns

import (
	"context"

	workflowstore "relay/internal/store/workflow"
)

type CreatePackageRunResult struct {
	Run       workflowstore.Run
	Artifacts []workflowstore.Artifact
}

// PackageRunPreflight is executed inside the Run's artifact/database
// transaction before the normal setup-ready Run is created. It lets the
// ticket-package owner atomically validate and consume its own package basis
// without creating a second Run lifecycle.
type PackageRunPreflight func(context.Context, *workflowstore.Tx) error

type CreatePackageRunInput struct {
	FeatureSlug             string
	RepoTarget              string
	Branch                  string
	BaseCommit              string
	ExecutionPackageRowID   int64
	Preflight               PackageRunPreflight
	PackageApprovalRowIDRef *int64
}

type BeginExecutionAttemptInput struct {
	RunID   string
	Adapter string
	Model   string
}

type BeginExecutionAttemptResult struct {
	Run     workflowstore.Run
	Attempt workflowstore.ExecutionAttempt
}

// BeginPreparedAdaptiveExecutionInput contains identities that were verified
// by executor preparation. The Run service rechecks them transactionally.
type BeginPreparedAdaptiveExecutionInput struct {
	RunID                       string
	RunRowID                    int64
	AttemptID                   string
	AttemptRowID                int64
	AttemptNumber               int64
	Adapter                     string
	Model                       string
	InputArtifactRowID          int64
	InputArtifactSHA256         string
	EffectiveBriefArtifactRowID int64
	EffectiveBriefArtifactID    string
	EffectiveBriefSHA256        string
	EffectiveBriefMode          string
	ProposedLeaseID             string
	RunningResultJSON           string
}

type BeginPreparedAdaptiveExecutionResult struct {
	Run           workflowstore.Run
	Attempt       workflowstore.ExecutionAttempt
	Lease         workflowstore.RepositoryBranchMutationLease
	NewlyAdmitted bool
}

type FinishExecutionAttemptInput struct {
	AttemptID  string
	Status     string
	ResultJSON string
}

type FinishExecutionAttemptResult struct {
	Run     workflowstore.Run
	Attempt workflowstore.ExecutionAttempt
}

type RecordAuditDecisionInput struct {
	RunID                 string
	AuditPacketArtifactID string
	AuditedCommit         string
	PacketSHA256          string
	Decision              string
	Rationale             string
}

type RecordAuditDecisionResult struct {
	Run      workflowstore.Run
	Decision workflowstore.AuditDecision
	Pass     *workflowstore.PlanPass
	Plan     *workflowstore.Plan
}
