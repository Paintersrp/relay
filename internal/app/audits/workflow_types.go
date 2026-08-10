package audits

import (
	"context"
	"encoding/json"
	"errors"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

// Type aliases for API packages to use app-layer names instead of importing internal/store/workflow
type (
	AuditPacket   = workflowstore.AuditPacket
	AuditDecision = workflowstore.AuditDecision
)

const (
	MaxWorkflowAuditPacketBytes   = 2 * 1024 * 1024
	MaxWorkflowAuditEvidenceBytes = 128 * 1024
	MaxWorkflowAuditReadBytes     = 64 * 1024
)

var (
	ErrWorkflowAuditNotReady            = errors.New("workflow Run is not ready to prepare an audit packet")
	ErrWorkflowAuditPackageRequired     = errors.New("workflow audit requires an execution-package-linked Run")
	ErrWorkflowAuditPacketNotFound      = errors.New("workflow audit packet was not found")
	ErrWorkflowAuditPacketStale         = errors.New("workflow audit packet is stale")
	ErrWorkflowAuditDecisionRecorded    = errors.New("workflow audit decision has already been recorded")
	ErrWorkflowAuditConfirmation        = errors.New("operator confirmation is required")
	ErrWorkflowAuditArtifactReference   = errors.New("workflow audit artifact reference is not declared by the current packet")
	ErrWorkflowAuditArtifactOwnership   = errors.New("workflow audit artifact does not belong to the packet execution attempt")
	ErrWorkflowAuditArtifactIntegrity   = errors.New("workflow audit artifact failed integrity verification")
	ErrWorkflowAuditArtifactUnsupported = errors.New("workflow audit artifact is not supported for textual readback")
	ErrWorkflowAuditDecisionInput       = errors.New("workflow audit decision input is invalid")
	ErrWorkflowAuditTicketIneligible    = errors.New("workflow audit ticket effect is no longer eligible")
)

type WorkflowAuditInspector func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error)

type WorkflowAuditMaterialFinding struct {
	Source              string `json:"source"`
	Summary             string `json:"summary"`
	Evidence            string `json:"evidence"`
	RequiredRemediation string `json:"required_remediation"`
}

// workflowPackageDecisionDocument is the immutable package-native audit
// decision record. Field order is the canonical JSON order.
type workflowPackageDecisionDocument struct {
	AuditDecisionID              string                         `json:"audit_decision_id"`
	RunID                        string                         `json:"run_id"`
	RunRowID                     int64                          `json:"run_row_id"`
	Decision                     string                         `json:"decision"`
	Rationale                    string                         `json:"rationale"`
	MaterialFindings             []WorkflowAuditMaterialFinding `json:"material_findings"`
	Observations                 []string                       `json:"observations"`
	AuditPacketID                string                         `json:"audit_packet_id"`
	AuditPacketRowID             int64                          `json:"audit_packet_row_id"`
	AuditPacketArtifactRowID     int64                          `json:"audit_packet_artifact_row_id"`
	PacketSHA256                 string                         `json:"packet_sha256"`
	AuditedCommit                string                         `json:"audited_commit"`
	ExecutionPackageID           string                         `json:"execution_package_id"`
	ExecutionPackageRowID        int64                          `json:"execution_package_row_id"`
	PackageSHA256                string                         `json:"package_sha256"`
	PackageApprovalID            string                         `json:"package_approval_id"`
	PackageApprovalRowID         int64                          `json:"package_approval_row_id"`
	ApprovedPackageSHA256        string                         `json:"approved_package_sha256"`
	DeliveryTicketID             string                         `json:"delivery_ticket_id"`
	DeliveryTicketRowID          int64                          `json:"delivery_ticket_row_id"`
	DeliveryTicketRevisionRowID  int64                          `json:"delivery_ticket_revision_row_id"`
	DeliveryTicketRevisionNumber int64                          `json:"delivery_ticket_revision_number"`
	DeliveryTicketApprovalID     string                         `json:"delivery_ticket_approval_id"`
	DeliveryTicketApprovalRowID  int64                          `json:"delivery_ticket_approval_row_id"`
	AuthorityRevisionID          string                         `json:"authority_revision_id"`
	AuthorityRevisionRowID       int64                          `json:"authority_revision_row_id"`
	SourceClosureID              string                         `json:"source_closure_id"`
	SourceClosureRowID           int64                          `json:"source_closure_row_id"`
	SourceCommit                 string                         `json:"source_commit"`
}

type WorkflowAuditAttemptResult struct {
	ExitCode                      int    `json:"exit_code"`
	TimedOut                      bool   `json:"timed_out"`
	TerminationVerified           bool   `json:"termination_verified"`
	CleanupPending                bool   `json:"cleanup_pending,omitempty"`
	PendingTerminalStatus         string `json:"pending_terminal_status,omitempty"`
	Error                         string `json:"error,omitempty"`
	NormalizedStatus              string `json:"normalized_status,omitempty"`
	BlockerText                   string `json:"blocker_text,omitempty"`
	ExecutionAssignmentArtifactID string `json:"execution_assignment_artifact_id,omitempty"`
	ExecutionAssignmentSHA256     string `json:"execution_assignment_sha256,omitempty"`
	ExecutionAssignmentMode       string `json:"execution_assignment_mode,omitempty"`
	StdoutTruncated               bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated               bool   `json:"stderr_truncated,omitempty"`
	StdoutBytes                   int64  `json:"stdout_bytes,omitempty"`
	StderrBytes                   int64  `json:"stderr_bytes,omitempty"`
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
	Run      workflowstore.Run
	Packet   workflowstore.AuditPacket
	Artifact workflowstore.Artifact
	Document json.RawMessage
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
	MaterialFindings  []WorkflowAuditMaterialFinding
	Observations      []string
	OperatorConfirmed bool
}

type RecordWorkflowAuditDecisionResult struct {
	Run                     workflowstore.Run
	Pass                    *workflowstore.PlanPass
	Plan                    *workflowstore.Plan
	Packet                  workflowstore.AuditPacket
	Decision                workflowstore.AuditDecision
	Artifact                workflowstore.Artifact
	TicketRevisionDecisions []workflowstore.AuditTicketRevisionDecision
	TicketSatisfactions     []workflowstore.DeliveryTicketRevisionSatisfaction
	RemediationSeeds        []workflowstore.AuditRemediationSeed
}
