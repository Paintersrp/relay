package workflow

import workflowstore "relay/internal/store/workflow"

const (
	RunStageSpecification = "specification"
	RunStageExecute       = "execute"
	RunStageAudit         = "audit"

	DefaultArtifactContentLimit int64 = 64 * 1024
	MaxArtifactContentLimit     int64 = 64 * 1024
)

type ArtifactMetadata struct {
	ArtifactID string
	OwnerType  string
	Kind       string
	MediaType  string
	SHA256     string
	SizeBytes  int64
	CreatedAt  string
}

type RepositoryDetail struct {
	Repository workflowstore.RepositoryTarget
}

type PlanSummary struct {
	Plan                workflowstore.Plan
	PassCount           int
	CompletedPassCount  int
	InProgressPassCount int
	PlannedPassCount    int
	CurrentPassID       string
}

type PlanPassDetail struct {
	Pass      workflowstore.PlanPass
	DependsOn []string
	Runs      []RunSummary
}

type PlanDetail struct {
	Plan         workflowstore.Plan
	Repositories []workflowstore.PlanRepositoryTarget
	Passes       []PlanPassDetail
	Artifacts    []ArtifactMetadata
}

type ExecutionAttemptSummary struct {
	AttemptID               string
	AttemptNumber           int64
	Adapter                 string
	Model                   string
	Status                  string
	CreatedAt               string
	StartedAt               string
	FinishedAt              string
	CancellationRequestedAt string
	Artifacts               []ArtifactMetadata
}

type AuditPacketSummary struct {
	AuditPacketID string
	AuditedCommit string
	PacketSHA256  string
	Status        string
	StaleReason   string
	CreatedAt     string
	SupersededAt  string
}

type AuditDecisionSummary struct {
	AuditDecisionID string
	AuditedCommit   string
	PacketSHA256    string
	Decision        string
	Rationale       string
	CreatedAt       string
}

type RunSummary struct {
	Run             workflowstore.Run
	Stage           string
	PlanID          string
	PassID          string
	PassNumber      int64
	RemediatesRunID string
	LatestAttempt   *ExecutionAttemptSummary
	CurrentPacket   *AuditPacketSummary
	LatestDecision  *AuditDecisionSummary
}

type RunDetail struct {
	Summary   RunSummary
	Attempts  []ExecutionAttemptSummary
	Artifacts []ArtifactMetadata
}

type SpecificationReview struct {
	Run             RunSummary
	ExecutionSpec   ArtifactMetadata
	ExecutorBrief   ArtifactMetadata
	Plan            *workflowstore.Plan
	Pass            *workflowstore.PlanPass
	RemediatesRunID string
}

type ArtifactContent struct {
	Artifact   ArtifactMetadata
	Offset     int64
	Bytes      []byte
	Encoding   string
	Truncated  bool
	NextOffset int64
	HasNext    bool
}

type ListPlansInput struct {
	Status string
	Limit  int
}

type ListRunsInput struct {
	Status string
	PlanID string
	PassID string
	Limit  int
}

type ArtifactContentInput struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}
