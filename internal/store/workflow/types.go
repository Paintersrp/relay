package workflowstore

import "database/sql"

const (
	OperationPacketSchemaVersion  = "relay.operation-packet.v1"
	OperationPacketReadinessReady = "ready"

	OperationPacketLifecycleActive     = "active"
	OperationPacketLifecycleSuperseded = "superseded"
	OperationPacketLifecycleClosed     = "closed"

	OperationPacketDependencyPacketDocument   = "packet_document"
	OperationPacketDependencyInputArtifact    = "input_artifact"
	OperationPacketDependencyWorkflowSnapshot = "workflow_snapshot"
	OperationPacketDependencyRepositoryVault  = "repository_vault"
	OperationPacketDependencyGitPathObject    = "git_path_object"
	OperationPacketDependencyManifestMember   = "manifest_member"
	OperationPacketDependencyRunArtifact      = "run_artifact"
)

type OperationPacketArtifact struct {
	ID           int64
	ArtifactID   string
	Kind         string
	RelativePath string
	MediaType    string
	SHA256       string
	SizeBytes    int64
	CreatedAt    string
}

type OperationPacket struct {
	ID                     int64
	PacketID               string
	PacketSHA256           string
	SchemaVersion          string
	Role                   string
	OperationID            string
	SurfaceContractID      string
	ProjectID              string
	ReadinessState         string
	LifecycleState         string
	PriorPacketRowID       sql.NullInt64
	ReplacementPacketRowID sql.NullInt64
	CreatedAt              string
	SupersededAt           sql.NullString
	ClosedAt               sql.NullString
	PacketArtifactRowID    int64
}

type OperationPacketReplacement struct {
	PacketID          string
	PacketSHA256      string
	Role              string
	OperationID       string
	SurfaceContractID string
}

type OperationPacketRetentionDependency struct {
	ID              int64
	PacketRowID     int64
	DependencyClass string
	DependencyKey   string
	Required        bool
	Attached        bool
	Retained        bool
	OwnerIdentity   sql.NullString
	CreatedAt       string
	UpdatedAt       string
}

type CreateOperationPacketArtifactParams struct {
	ArtifactID   string
	Kind         string
	RelativePath string
	MediaType    string
	SHA256       string
	SizeBytes    int64
}

type CreateOperationPacketParams struct {
	PacketID            string
	PacketSHA256        string
	SchemaVersion       string
	Role                string
	OperationID         string
	SurfaceContractID   string
	ProjectID           string
	ReadinessState      string
	PriorPacketRowID    sql.NullInt64
	CreatedAt           string
	PacketArtifactRowID int64
}

type SupersedeOperationPacketParams struct {
	PacketID               string
	ReplacementPacketRowID int64
	SupersededAt           string
}

type CloseOperationPacketParams struct {
	PacketID string
	ClosedAt string
}

type AttachOperationPacketDependencyParams struct {
	PacketRowID     int64
	DependencyClass string
	DependencyKey   string
	Required        bool
	Attached        bool
	Retained        bool
	OwnerIdentity   sql.NullString
}

type UpdateOperationPacketDependencyAvailabilityParams struct {
	PacketRowID     int64
	DependencyClass string
	DependencyKey   string
	Attached        bool
	Retained        bool
	OwnerIdentity   sql.NullString
}

type MCPMutationKey struct {
	SurfaceContractID string
	ToolName          string
	MutationID        string
}

type MCPMutationResult struct {
	ID                      int64
	SurfaceContractID       string
	ToolName                string
	MutationID              string
	SurfaceManifestSHA256   string
	SemanticIdentityVersion string
	SemanticRequestSHA256   string
	ResultKind              string
	ResultIdentityJSON      string
	ResultSHA256            string
	CommittedAt             string
}

type CreateMCPMutationResultParams struct {
	SurfaceContractID       string
	ToolName                string
	MutationID              string
	SurfaceManifestSHA256   string
	SemanticIdentityVersion string
	SemanticRequestSHA256   string
	ResultKind              string
	ResultIdentityJSON      string
	ResultSHA256            string
}

const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"

	ProjectNoteStatusOpen = "open"
	ProjectNoteStatusDone = "done"

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

	ImplementationActorApplier  = "applier"
	ImplementationActorExecutor = "executor"
	ImplementationActorHybrid   = "hybrid"

	AuditDecisionAccepted      = "accepted"
	AuditDecisionNeedsRevision = "needs_revision"
)

type Project struct {
	ID          int64
	ProjectID   string
	Name        string
	Description string
	Status      string
	CreatedAt   string
	UpdatedAt   string
}

type ProjectRepositoryTarget struct {
	ProjectRowID int64
	RepoTarget   string
	CreatedAt    string
}

type ProjectNote struct {
	ID           int64
	NoteID       string
	ProjectRowID int64
	Title        string
	Body         string
	Status       string
	CreatedAt    string
	UpdatedAt    string
}

type RepositoryTarget struct {
	RepoTarget           string
	LocalPath            string
	ConfiguredBranchRef  sql.NullString
	ConfigurationVersion int64
	CreatedAt            string
	UpdatedAt            string
}

type CreateRepositoryTargetParams struct {
	RepoTarget          string
	LocalPath           string
	ConfiguredBranchRef sql.NullString
}

type ConfigureRepositoryTargetParams struct {
	RepoTarget                   string
	ExpectedConfigurationVersion int64
	ConfiguredBranchRef          string
}

type Plan struct {
	ID              int64
	ProjectRowID    int64
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
	ID                      int64
	AuditPacketID           string
	RunRowID                int64
	ImplementationActorKind string
	ExecutionAttemptRowID   sql.NullInt64
	ArtifactRowID           int64
	BaseCommit              string
	AuditedCommit           string
	PacketSHA256            string
	Status                  string
	StaleReason             string
	CreatedAt               string
	SupersededAt            sql.NullString
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

type CreateProjectParams struct {
	ProjectID   string
	Name        string
	Description string
}

type CreateProjectNoteParams struct {
	NoteID       string
	ProjectRowID int64
	Title        string
	Body         string
}

type UpdateProjectNoteParams struct {
	NoteID       string
	ProjectRowID int64
	Title        string
	Body         string
	Status       string
}

type CreatePlanParams struct {
	ProjectRowID    int64
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
	AuditPacketID           string
	RunRowID                int64
	ImplementationActorKind string
	ExecutionAttemptRowID   sql.NullInt64
	ArtifactRowID           int64
	BaseCommit              string
	AuditedCommit           string
	PacketSHA256            string
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
