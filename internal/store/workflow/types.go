package workflowstore

import "database/sql"

const (
	PlanStatusActive    = "active"
	PlanStatusCompleted = "completed"

	PassStatusPlanned    = "planned"
	PassStatusInProgress = "in_progress"
	PassStatusCompleted  = "completed"

	RunStatusCreated          = "created"
	RunStatusSetupReady       = "setup_ready"
	RunStatusExecuting        = "executing"
	RunStatusExecutionFailed  = "execution_failed"
	RunStatusCancelled        = "cancelled"
	RunStatusValidating       = "validating"
	RunStatusValidationFailed = "validation_failed"
	RunStatusAuditReady       = "audit_ready"
	RunStatusNeedsRevision    = "needs_revision"
	RunStatusCompleted        = "completed"

	AttemptStatusPending   = "pending"
	AttemptStatusRunning   = "running"
	AttemptStatusSucceeded = "succeeded"
	AttemptStatusFailed    = "failed"
	AttemptStatusCancelled = "cancelled"
	AttemptStatusTimedOut  = "timed_out"

	ArtifactOwnerPlan             = "plan"
	ArtifactOwnerRun              = "run"
	ArtifactOwnerExecutionAttempt = "execution_attempt"

	AuditPacketStatusCurrent = "current"
	AuditPacketStatusStale   = "stale"

	AuditDecisionAccepted      = "accepted"
	AuditDecisionNeedsRevision = "needs_revision"
)

type RepositoryTarget struct {
	RepoTarget string
	LocalPath  string
	CreatedAt  string
	UpdatedAt  string
}

type Plan struct {
	ID              int64
	PlanID          string
	FeatureSlug     string
	Status          string
	CanonicalSHA256 string
	CreatedAt       string
	UpdatedAt       string
	CompletedAt     sql.NullString
}

type PlanRepositoryTarget struct {
	ID                 int64
	PlanRowID          int64
	Sequence           int64
	RepoTarget         string
	Branch             string
	PlanningBaseCommit string
	CreatedAt          string
}

type PlanPass struct {
	ID          int64
	PassID      string
	PlanRowID   int64
	PassNumber  int64
	Name        string
	RepoTarget  string
	Status      string
	CreatedAt   string
	UpdatedAt   string
	StartedAt   sql.NullString
	CompletedAt sql.NullString
}

type PlanPassDependency struct {
	PassRowID          int64
	DependsOnPassRowID int64
	CreatedAt          string
}

type Run struct {
	ID                 int64
	RunID              string
	FeatureSlug        string
	RepoTarget         string
	PlanRowID          sql.NullInt64
	PlanPassRowID      sql.NullInt64
	RemediatesRunRowID sql.NullInt64
	Status             string
	Branch             string
	BaseCommit         string
	CanonicalSHA256    string
	CreatedAt          string
	UpdatedAt          string
	CompletedAt        sql.NullString
}

type ExecutionAttempt struct {
	ID                      int64
	AttemptID               string
	RunRowID                int64
	AttemptNumber           int64
	Adapter                 string
	Model                   string
	Status                  string
	ResultJSON              string
	CreatedAt               string
	StartedAt               sql.NullString
	FinishedAt              sql.NullString
	CancellationRequestedAt sql.NullString
}

type Artifact struct {
	ID                    int64
	ArtifactID            string
	OwnerType             string
	PlanRowID             sql.NullInt64
	RunRowID              sql.NullInt64
	ExecutionAttemptRowID sql.NullInt64
	Kind                  string
	RelativePath          string
	MediaType             string
	SHA256                string
	SizeBytes             int64
	CreatedAt             string
}

type AuditPacket struct {
	ID                    int64
	AuditPacketID         string
	RunRowID              int64
	ExecutionAttemptRowID int64
	ArtifactRowID         int64
	BaseCommit            string
	AuditedCommit         string
	PacketSHA256          string
	Status                string
	StaleReason           string
	CreatedAt             string
	SupersededAt          sql.NullString
}

type AuditDecision struct {
	ID                       int64
	AuditDecisionID          string
	RunRowID                 int64
	AuditPacketArtifactRowID int64
	AuditedCommit            string
	PacketSHA256             string
	Decision                 string
	Rationale                string
	CreatedAt                string
}

type CreatePlanParams struct {
	PlanID          string
	FeatureSlug     string
	CanonicalSHA256 string
}

type CreatePlanRepositoryTargetParams struct {
	PlanRowID          int64
	Sequence           int64
	RepoTarget         string
	Branch             string
	PlanningBaseCommit string
}

type CreatePlanPassParams struct {
	PassID     string
	PlanRowID  int64
	PassNumber int64
	Name       string
	RepoTarget string
}

type CreateRunParams struct {
	RunID              string
	FeatureSlug        string
	RepoTarget         string
	PlanRowID          sql.NullInt64
	PlanPassRowID      sql.NullInt64
	RemediatesRunRowID sql.NullInt64
	Status             string
	Branch             string
	BaseCommit         string
	CanonicalSHA256    string
}

type CreateExecutionAttemptParams struct {
	AttemptID     string
	RunRowID      int64
	AttemptNumber int64
	Adapter       string
	Model         string
}

type CreateArtifactParams struct {
	ArtifactID            string
	OwnerType             string
	PlanRowID             sql.NullInt64
	RunRowID              sql.NullInt64
	ExecutionAttemptRowID sql.NullInt64
	Kind                  string
	RelativePath          string
	MediaType             string
	SHA256                string
	SizeBytes             int64
}

type CreateAuditPacketParams struct {
	AuditPacketID         string
	RunRowID              int64
	ExecutionAttemptRowID int64
	ArtifactRowID         int64
	BaseCommit            string
	AuditedCommit         string
	PacketSHA256          string
}

type CreateAuditDecisionParams struct {
	AuditDecisionID          string
	RunRowID                 int64
	AuditPacketArtifactRowID int64
	AuditedCommit            string
	PacketSHA256             string
	Decision                 string
	Rationale                string
}
