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
	WorkflowAuditPacketSchemaVersion                = "2.0"
	WorkflowAuditTicketPackageEvidenceSchemaVersion = "relay.audit-ticket-package-evidence.v1"
	MaxWorkflowAuditPacketBytes                     = 2 * 1024 * 1024
	MaxWorkflowAuditSourceBytes                     = 512 * 1024
	MaxWorkflowAuditEvidenceBytes                   = 128 * 1024
	MaxWorkflowAuditReadBytes                       = 64 * 1024
)

var (
	ErrWorkflowAuditNotReady            = errors.New("workflow Run is not ready to prepare an audit packet")
	ErrWorkflowAuditPacketNotFound      = errors.New("workflow audit packet was not found")
	ErrWorkflowAuditPacketStale         = errors.New("workflow audit packet is stale")
	ErrWorkflowAuditDecisionRecorded    = errors.New("workflow audit decision has already been recorded")
	ErrWorkflowAuditConfirmation        = errors.New("operator confirmation is required")
	ErrWorkflowAuditPacketTooLarge      = errors.New("workflow audit packet exceeds the configured bound")
	ErrWorkflowAuditPacketSchemaInvalid = errors.New("workflow audit packet does not satisfy the current schema")
	ErrWorkflowAuditArtifactReference   = errors.New("workflow audit artifact reference is not declared by the current packet")
	ErrWorkflowAuditArtifactOwnership   = errors.New("workflow audit artifact does not belong to the packet execution attempt")
	ErrWorkflowAuditArtifactIntegrity   = errors.New("workflow audit artifact failed integrity verification")
	ErrWorkflowAuditArtifactUnsupported = errors.New("workflow audit artifact is not supported for textual readback")
	ErrWorkflowAuditDecisionInput       = errors.New("workflow audit decision input is invalid")
	ErrWorkflowAuditTicketIneligible    = errors.New("workflow audit ticket effect is no longer eligible")
)

type WorkflowAuditInspector func(context.Context, string, string, string, string) (workflowrepos.AuditCommitEvidence, error)

type WorkflowAuditPacket struct {
	SchemaVersion       string                           `json:"schema_version"`
	Run                 WorkflowAuditRunAuthority        `json:"run"`
	Repository          WorkflowAuditRepository          `json:"repository"`
	Authority           WorkflowAuditAuthority           `json:"authority"`
	Execution           WorkflowAuditExecution           `json:"execution"`
	ChangedFiles        []WorkflowAuditChangedFile       `json:"changed_files"`
	RelevantSourcePaths []string                         `json:"relevant_source_paths"`
	Validation          []WorkflowAuditValidationResult  `json:"validation"`
	Artifacts           []WorkflowAuditPacketArtifact    `json:"artifacts"`
	RemediationContext  *WorkflowAuditRemediationContext `json:"remediation_context,omitempty"`
}

type WorkflowAuditRepository struct {
	RepoTarget    string `json:"repo_target"`
	Branch        string `json:"branch"`
	BaseCommit    string `json:"base_commit"`
	AuditedCommit string `json:"audited_commit"`
}

type WorkflowAuditAuthority struct {
	ExecutionSpec  WorkflowAuditEmbeddedJSON     `json:"execution_spec"`
	ExecutorBrief  WorkflowAuditEmbeddedMarkdown `json:"executor_brief"`
	ManagedContext *WorkflowAuditManagedContext  `json:"managed_context,omitempty"`
}

type WorkflowAuditEmbeddedJSON struct {
	Filename string          `json:"filename"`
	SHA256   string          `json:"sha256"`
	Content  json.RawMessage `json:"content"`
}

type WorkflowAuditEmbeddedMarkdown struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Content  string `json:"content"`
}

type WorkflowAuditManagedContext struct {
	PlanGoal               string          `json:"plan_goal"`
	PlanContext            string          `json:"plan_context"`
	PlanScope              json.RawMessage `json:"plan_scope"`
	RepositoryTarget       json.RawMessage `json:"repository_target"`
	SelectedPass           json.RawMessage `json:"selected_pass"`
	PlanCompletionCriteria []string        `json:"plan_completion_criteria"`
}

type WorkflowAuditExecution struct {
	ActorKind                string                         `json:"actor_kind"`
	Status                   string                         `json:"status"`
	CommittedSHA             string                         `json:"committed_sha"`
	CompletionSummary        string                         `json:"completion_summary"`
	BlockersOrIncompleteWork []string                       `json:"blockers_or_incomplete_work"`
	ReportedChangedFiles     []string                       `json:"reported_changed_files"`
	Applier                  *WorkflowAuditApplierEvidence  `json:"applier,omitempty"`
	Executor                 *WorkflowAuditExecutorEvidence `json:"executor,omitempty"`
}

type WorkflowAuditApplierEvidence struct {
	Outcome                               string   `json:"outcome"`
	ImplementationResultArtifactReference string   `json:"implementation_result_artifact_reference"`
	LedgerArtifactReference               string   `json:"ledger_artifact_reference"`
	ChangedFiles                          []string `json:"changed_files"`
	ResidualOperationIDs                  []string `json:"residual_operation_ids"`
	FailureClass                          string   `json:"failure_class,omitempty"`
	FailureReason                         string   `json:"failure_reason,omitempty"`
}

type WorkflowAuditExecutorEvidence struct {
	AttemptID                       string                           `json:"attempt_id"`
	AttemptNumber                   int64                            `json:"attempt_number"`
	Adapter                         string                           `json:"adapter"`
	Model                           string                           `json:"model"`
	Status                          string                           `json:"status"`
	Result                          WorkflowAuditPacketAttemptResult `json:"result"`
	EffectiveBriefArtifactReference string                           `json:"effective_brief_artifact_reference"`
	EffectiveBriefSHA256            string                           `json:"effective_brief_sha256"`
	EffectiveBriefMode              string                           `json:"effective_brief_mode"`
	StartedAt                       string                           `json:"started_at,omitempty"`
	FinishedAt                      string                           `json:"finished_at,omitempty"`
}

type WorkflowAuditChangedFile struct {
	Path       string `json:"path"`
	ChangeType string `json:"change_type"`
	Additions  int64  `json:"additions"`
	Deletions  int64  `json:"deletions"`
}

type WorkflowAuditValidationResult struct {
	Command           string `json:"command"`
	WorkingDirectory  string `json:"working_directory,omitempty"`
	Expected          string `json:"expected"`
	Status            string `json:"status"`
	ConciseResult     string `json:"concise_result"`
	ExitCode          *int   `json:"exit_code,omitempty"`
	ArtifactReference string `json:"artifact_reference,omitempty"`
}

type WorkflowAuditPacketArtifact struct {
	ArtifactReference string `json:"artifact_reference"`
	ArtifactType      string `json:"artifact_type"`
	SHA256            string `json:"sha256"`
	Description       string `json:"description"`
}

// WorkflowAuditTicketPackageEvidence is a bounded, immutable audit artifact
// declared by a ticket-oriented packet. It carries the package provenance that
// cannot fit into the legacy packet schema while remaining available through
// the packet's ordinary artifact readback contract.
type WorkflowAuditTicketPackageEvidence struct {
	SchemaVersion     string                                   `json:"schema_version"`
	Package           WorkflowAuditExecutionPackageEvidence    `json:"package"`
	Tickets           []WorkflowAuditTicketObligationEvidence  `json:"tickets"`
	MutationLeases    []WorkflowAuditMutationLeaseEvidence     `json:"mutation_leases"`
	BundleIntegration WorkflowAuditBundleIntegrationEvidence   `json:"bundle_integration"`
	Commit            WorkflowAuditTicketPackageCommitEvidence `json:"commit"`
	Implementation    []WorkflowAuditPacketArtifact            `json:"implementation"`
	Validation        []WorkflowAuditValidationResult          `json:"validation"`
}

type WorkflowAuditPackageApprovalEvidence struct {
	ApprovalRowID              int64  `json:"approval_row_id"`
	ApprovalID                 string `json:"approval_id"`
	PackageRowID               int64  `json:"package_row_id"`
	ApprovedPackageSha256      string `json:"approved_package_sha256"`
	OperatorConfirmationEvidence string `json:"operator_confirmation_evidence"`
}

type WorkflowAuditExecutionPackageEvidence struct {
	PackageRowID        int64                               `json:"package_row_id"`
	PackageID           string                              `json:"package_id"`
	PackageSHA256       string                              `json:"package_sha256"`
	RepoTarget          string                              `json:"repo_target"`
	Branch              string                              `json:"branch"`
	BaseCommit          string                              `json:"base_commit"`
	SelectionRowID      int64                               `json:"selection_row_id"`
	SelectionID         string                              `json:"selection_id"`
	SelectionState      string                              `json:"selection_state"`
	WorkspaceRowID      int64                               `json:"workspace_row_id"`
	WorkspaceID         string                              `json:"workspace_id"`
	FeatureSlug         string                              `json:"feature_slug"`
	Authority           WorkflowAuditAuthorityBasisEvidence `json:"authority"`
	Source              WorkflowAuditSourceBasisEvidence    `json:"source"`
	DesignBriefSHA256   string                              `json:"design_brief_sha256"`
	ExecutionSpecSHA256 string                              `json:"execution_spec_sha256"`
	ExecutionSpec       WorkflowAuditPacketArtifact         `json:"execution_spec"`
	PackageApproval     WorkflowAuditPackageApprovalEvidence `json:"package_approval,omitempty"`
}

type WorkflowAuditAuthorityBasisEvidence struct {
	AuthorityRevisionRowID int64  `json:"authority_revision_row_id"`
	AuthorityRevisionID    string `json:"authority_revision_id"`
	RevisionNumber         int64  `json:"revision_number"`
	SHA256                 string `json:"sha256"`
	SourceClosureRowID     int64  `json:"source_closure_row_id"`
}

type WorkflowAuditSourceBasisEvidence struct {
	SourceClosureRowID int64  `json:"source_closure_row_id"`
	ClosureID          string `json:"closure_id"`
	CommitOID          string `json:"commit_oid"`
	TreeOID            string `json:"tree_oid"`
	RefName            string `json:"ref_name"`
	SHA256             string `json:"sha256"`
}

type WorkflowAuditTicketObligationEvidence struct {
	PackageMemberRowID       int64                         `json:"package_member_row_id"`
	SelectionMemberRowID     int64                         `json:"selection_member_row_id"`
	Sequence                 int64                         `json:"sequence"`
	DeliveryTicketRowID      int64                         `json:"delivery_ticket_row_id"`
	TicketID                 string                        `json:"ticket_id"`
	DeliveryTicketRevisionID int64                         `json:"delivery_ticket_revision_row_id"`
	RevisionNumber           int64                         `json:"revision_number"`
	SourcePath               string                        `json:"source_path"`
	MemberSHA256             string                        `json:"member_sha256"`
	Approval                 WorkflowAuditApprovalEvidence `json:"approval"`
	DesignBrief              WorkflowAuditPacketArtifact   `json:"design_brief"`
}

type WorkflowAuditApprovalEvidence struct {
	ApprovalRowID          int64  `json:"approval_row_id"`
	ApprovalID             string `json:"approval_id"`
	ApprovalBasisSHA256    string `json:"approval_basis_sha256"`
	AuthorityRevisionRowID int64  `json:"authority_revision_row_id"`
	SourceClosureRowID     int64  `json:"source_closure_row_id"`
}

type WorkflowAuditMutationLeaseEvidence struct {
	LeaseID                 string `json:"lease_id"`
	OwnerKind               string `json:"owner_kind"`
	OwnerIdentity           string `json:"owner_identity"`
	State                   string `json:"state"`
	Certainty               string `json:"certainty"`
	ReconciliationState     string `json:"reconciliation_state"`
	AcquiredAt              string `json:"acquired_at"`
	ReleasedAt              string `json:"released_at"`
	ReconciliationStartedAt string `json:"reconciliation_started_at,omitempty"`
	ReconciledAt            string `json:"reconciled_at,omitempty"`
}

type WorkflowAuditBundleIntegrationEvidence struct {
	RunID                 string `json:"run_id"`
	ExecutionPackageRowID int64  `json:"execution_package_row_id"`
	ExecutionPackageID    string `json:"execution_package_id"`
	SelectionID           string `json:"selection_id"`
	SelectionState        string `json:"selection_state"`
	ApprovedRunStatus     string `json:"approved_run_status"`
}

type WorkflowAuditTicketPackageCommitEvidence struct {
	RepoTarget    string                      `json:"repo_target"`
	Branch        string                      `json:"branch"`
	BaseCommit    string                      `json:"base_commit"`
	AuditedCommit string                      `json:"audited_commit"`
	NameStatus    string                      `json:"name_status"`
	DiffStat      string                      `json:"diff_stat"`
	CommitLog     string                      `json:"commit_log"`
	UnifiedDiff   WorkflowAuditPacketArtifact `json:"unified_diff"`
}

type WorkflowAuditRemediationContext struct {
	RemediatedRunID  int64                          `json:"remediated_run_id"`
	MaterialFindings []WorkflowAuditMaterialFinding `json:"material_findings"`
}

type WorkflowAuditMaterialFinding struct {
	Source              string `json:"source"`
	Summary             string `json:"summary"`
	Evidence            string `json:"evidence"`
	RequiredRemediation string `json:"required_remediation"`
}

type WorkflowAuditRunAuthority struct {
	RunID           int64  `json:"run_id"`
	FeatureSlug     string `json:"feature_slug"`
	RepoTarget      string `json:"repo_target"`
	Branch          string `json:"branch"`
	BaseCommit      string `json:"base_commit"`
	CanonicalSHA256 string `json:"canonical_sha256"`
	PlanID          int64  `json:"plan_id,omitempty"`
	PassID          int64  `json:"pass_id,omitempty"`
	PassNumber      int64  `json:"pass_number,omitempty"`
	RemediatesRunID int64  `json:"remediates_run_id,omitempty"`
	UserIntent      string `json:"user_intent,omitempty"`
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

type WorkflowAuditPacketAttemptResult struct {
	ExitCode              int    `json:"exit_code"`
	TimedOut              bool   `json:"timed_out"`
	TerminationVerified   bool   `json:"termination_verified"`
	CleanupPending        bool   `json:"cleanup_pending,omitempty"`
	PendingTerminalStatus string `json:"pending_terminal_status,omitempty"`
	Error                 string `json:"error,omitempty"`
	NormalizedStatus      string `json:"normalized_status,omitempty"`
	BlockerText           string `json:"blocker_text,omitempty"`
	StdoutTruncated       bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated       bool   `json:"stderr_truncated,omitempty"`
	StdoutBytes           int64  `json:"stdout_bytes,omitempty"`
	StderrBytes           int64  `json:"stderr_bytes,omitempty"`
}

type WorkflowAuditAttemptResult struct {
	ExitCode                 int    `json:"exit_code"`
	TimedOut                 bool   `json:"timed_out"`
	TerminationVerified      bool   `json:"termination_verified"`
	CleanupPending           bool   `json:"cleanup_pending,omitempty"`
	PendingTerminalStatus    string `json:"pending_terminal_status,omitempty"`
	Error                    string `json:"error,omitempty"`
	NormalizedStatus         string `json:"normalized_status,omitempty"`
	BlockerText              string `json:"blocker_text,omitempty"`
	EffectiveBriefArtifactID string `json:"effective_brief_artifact_id,omitempty"`
	EffectiveBriefSHA256     string `json:"effective_brief_sha256,omitempty"`
	EffectiveBriefMode       string `json:"effective_brief_mode,omitempty"`
	StdoutTruncated          bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated          bool   `json:"stderr_truncated,omitempty"`
	StdoutBytes              int64  `json:"stdout_bytes,omitempty"`
	StderrBytes              int64  `json:"stderr_bytes,omitempty"`
}

type WorkflowAuditEvidenceItem struct {
	ArtifactID       string `json:"artifact_id"`
	Kind             string `json:"kind"`
	MediaType        string `json:"media_type"`
	SHA256           string `json:"sha256"`
	SizeBytes        int64  `json:"size_bytes"`
	ContentTruncated bool   `json:"content_truncated"`
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
