package workflowruns

import (
	"context"

	workflowstore "relay/internal/store/workflow"
)

type CreateRunInput struct {
	FeatureSlug      string
	RepoTarget       string
	Branch           string
	BaseCommit       string
	CanonicalJSON    []byte
	RenderedMarkdown []byte
	PlanID           string
	PassNumber       int64
	RemediatesRunID  string
}

type CreateRunResult struct {
	Run       workflowstore.Run
	Artifacts []workflowstore.Artifact
}

// PackageRunPreflight is executed inside the Run's artifact/database
// transaction before the normal setup-ready Run is created. It lets the
// ticket-package owner atomically validate and consume its own package basis
// without creating a second Run lifecycle.
type PackageRunPreflight func(context.Context, *workflowstore.Tx) error

type CreatePackageRunInput struct {
	FeatureSlug              string
	RepoTarget               string
	Branch                   string
	BaseCommit               string
	CanonicalJSON            []byte
	RenderedMarkdown         []byte
	ExecutionPackageRowID    int64
	Preflight                PackageRunPreflight
	PackageApprovalRowIDRef  *int64
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
