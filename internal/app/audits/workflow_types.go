package audits

import (
	"context"
	"encoding/json"
	"errors"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

const (
	WorkflowAuditPacketSchemaVersion = "1.0"
	MaxWorkflowAuditPacketBytes      = 2 * 1024 * 1024
	MaxWorkflowAuditSourceBytes      = 512 * 1024
	MaxWorkflowAuditEvidenceBytes    = 128 * 1024
	MaxWorkflowAuditReadBytes        = 64 * 1024
)

var (
	ErrWorkflowAuditNotReady            = errors.New("workflow Run is not ready to prepare an audit packet")
	ErrWorkflowAuditPacketNotFound      = errors.New("workflow audit packet was not found")
	ErrWorkflowAuditPacketStale         = errors.New("workflow audit packet is stale")
	ErrWorkflowAuditDecisionRecorded    = errors.New("workflow audit decision has already been recorded")
	ErrWorkflowAuditConfirmation        = errors.New("operator confirmation is required")
	ErrWorkflowAuditPacketTooLarge      = errors.New("workflow audit packet exceeds the configured bound")
	ErrWorkflowAuditArtifactReference   = errors.New("workflow audit artifact reference is not declared by the current packet")
	ErrWorkflowAuditArtifactOwnership   = errors.New("workflow audit artifact does not belong to the packet execution attempt")
	ErrWorkflowAuditArtifactIntegrity   = errors.New("workflow audit artifact failed integrity verification")
	ErrWorkflowAuditArtifactUnsupported = errors.New("workflow audit artifact is not supported for textual readback")
)

type WorkflowAuditInspector func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error)

type WorkflowAuditPacket struct {
	SchemaVersion      string                        `json:"schema_version"`
	AuditPacketID      string                        `json:"audit_packet_id"`
	Run                WorkflowAuditRunAuthority     `json:"run"`
	SelectedPass       *WorkflowAuditPassAuthority   `json:"selected_pass,omitempty"`
	ExecutionSpec      string                        `json:"execution_spec"`
	ExecutorBrief      string                        `json:"executor_brief"`
	Attempt            WorkflowAuditAttemptAuthority `json:"attempt"`
	ValidationEvidence []WorkflowAuditEvidenceItem   `json:"validation_evidence"`
	Commit             WorkflowAuditCommitAuthority  `json:"commit"`
	Blockers           []string                      `json:"blockers"`
}

type WorkflowAuditRunAuthority struct {
	RunID           string `json:"run_id"`
	FeatureSlug     string `json:"feature_slug"`
	RepoTarget      string `json:"repo_target"`
	Branch          string `json:"branch"`
	BaseCommit      string `json:"base_commit"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	PlanID          string `json:"plan_id,omitempty"`
	PassID          string `json:"pass_id,omitempty"`
	PassNumber      int64  `json:"pass_number,omitempty"`
	RemediatesRunID string `json:"remediates_run_id,omitempty"`
}

type WorkflowAuditPassAuthority struct {
	PlanID              string          `json:"plan_id"`
	PlanCanonicalSHA256 string          `json:"plan_canonical_sha256"`
	PassID              string          `json:"pass_id"`
	PassNumber          int64           `json:"pass_number"`
	PassName            string          `json:"pass_name"`
	CanonicalPass       json.RawMessage `json:"canonical_pass"`
}

type WorkflowAuditAttemptAuthority struct {
	AttemptID     string                     `json:"attempt_id"`
	AttemptNumber int64                      `json:"attempt_number"`
	Adapter       string                     `json:"adapter"`
	Model         string                     `json:"model"`
	Status        string                     `json:"status"`
	Result        WorkflowAuditAttemptResult `json:"result"`
	StartedAt     string                     `json:"started_at,omitempty"`
	FinishedAt    string                     `json:"finished_at,omitempty"`
}

type WorkflowAuditAttemptResult struct {
	ExitCode              int    `json:"exit_code"`
	TimedOut              bool   `json:"timed_out"`
	TerminationVerified   bool   `json:"termination_verified"`
	CleanupPending        bool   `json:"cleanup_pending,omitempty"`
	PendingTerminalStatus string `json:"pending_terminal_status,omitempty"`
	Error                 string `json:"error,omitempty"`
	NormalizedStatus      string `json:"normalized_status,omitempty"`
	BlockerText           string `json:"blocker_text,omitempty"`
	BriefArtifactID       string `json:"brief_artifact_id,omitempty"`
	BriefSHA256           string `json:"brief_sha256,omitempty"`
	StdoutTruncated       bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated       bool   `json:"stderr_truncated,omitempty"`
	StdoutBytes           int64  `json:"stdout_bytes,omitempty"`
	StderrBytes           int64  `json:"stderr_bytes,omitempty"`
}

type WorkflowAuditEvidenceItem struct {
	ArtifactID       string `json:"artifact_id"`
	Kind             string `json:"kind"`
	MediaType        string `json:"media_type"`
	SHA256           string `json:"sha256"`
	SizeBytes        int64  `json:"size_bytes"`
	ContentTruncated bool   `json:"content_truncated"`
}

type WorkflowAuditCommitAuthority struct {
	Branch        string   `json:"branch"`
	BaseCommit    string   `json:"base_commit"`
	AuditedCommit string   `json:"audited_commit"`
	ChangedFiles  []string `json:"changed_files"`
	NameStatus    string   `json:"name_status"`
	DiffStat      string   `json:"diff_stat"`
	CommitLog     string   `json:"commit_log"`
	Diff          string   `json:"diff"`
}

type GetWorkflowAuditArtifactInput struct {
	RunID             string
	ArtifactReference string
	MaxBytes          int
}

type GetWorkflowAuditArtifactResult struct {
	Run       workflowstore.Run
	Packet    workflowstore.AuditPacket
	Artifact  workflowstore.Artifact
	Content   []byte
	Truncated bool
}

type PrepareWorkflowAuditInput struct {
	RunID         string
	AuditedCommit string
}

type PrepareWorkflowAuditResult struct {
	Run      workflowstore.Run
	Packet   workflowstore.AuditPacket
	Artifact workflowstore.Artifact
}

type GetWorkflowAuditPacketResult struct {
	Run         workflowstore.Run
	Packet      workflowstore.AuditPacket
	Artifact    workflowstore.Artifact
	PacketBytes []byte
}

type WorkflowAuditStatus struct {
	RunID         string                       `json:"run_id"`
	RunStatus     string                       `json:"run_status"`
	CurrentPacket *workflowstore.AuditPacket   `json:"current_packet,omitempty"`
	LatestPacket  *workflowstore.AuditPacket   `json:"latest_packet,omitempty"`
	Decision      *workflowstore.AuditDecision `json:"decision,omitempty"`
}

type RecordWorkflowAuditDecisionInput struct {
	RunID             string
	AuditPacketID     string
	PacketSHA256      string
	AuditedCommit     string
	Decision          string
	Rationale         string
	OperatorConfirmed bool
}

type RecordWorkflowAuditDecisionResult struct {
	Run      workflowstore.Run
	Pass     *workflowstore.PlanPass
	Plan     *workflowstore.Plan
	Packet   workflowstore.AuditPacket
	Decision workflowstore.AuditDecision
	Artifact workflowstore.Artifact
}
