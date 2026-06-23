package auditor

import "time"

// Decision is the auditor's recommended action for a run.
type Decision string

const (
	DecisionManualReviewRequired Decision = "manual_review_required"
	DecisionAccepted             Decision = "accepted"
	DecisionAcceptedWithWarnings Decision = "accepted_with_warnings"
	DecisionRevisionRequired     Decision = "revision_required"
	DecisionBlocked              Decision = "blocked"
)

var SupportedDecisions = map[Decision]bool{
	DecisionManualReviewRequired: true,
	DecisionAccepted:             true,
	DecisionAcceptedWithWarnings: true,
	DecisionRevisionRequired:     true,
	DecisionBlocked:              true,
}

// CheckResult is the outcome for a single audit checklist item or file-scope/non-goal check.
type CheckResult string

const (
	CheckPass          CheckResult = "pass"
	CheckFail          CheckResult = "fail"
	CheckUnknown       CheckResult = "unknown"
	CheckNotApplicable CheckResult = "not_applicable"
)

// CheckSeverity captures severity when a check fails.
type CheckSeverity string

const (
	SeverityBlocker CheckSeverity = "blocker"
	SeverityError   CheckSeverity = "error"
	SeverityWarning CheckSeverity = "warning"
	SeverityInfo    CheckSeverity = "info"
	SeverityUnknown CheckSeverity = "unknown"
)

// EvidenceWarning carries a message and severity for missing or degraded evidence.
type EvidenceWarning struct {
	Message  string        `json:"message"`
	Severity CheckSeverity `json:"severity"`
}

// PacketMetadata carries normalized, human-readable fields parsed from canonical_packet.json.
// Goal, Scope, and NonGoals are never raw JSON dumps.
type PacketMetadata struct {
	// PacketID is derived from the run or from the packet's own packet_id field.
	PacketID string `json:"packetId"`
	// Goal is parsed from execution_payload.goal (string or string array joined).
	Goal string `json:"goal"`
	// Scope is parsed from execution_payload.scope.
	Scope string `json:"scope"`
	// NonGoals is parsed from execution_payload.non_goals (joined list).
	NonGoals string `json:"nonGoals"`
	// FileTargets lists expected changed files from execution_payload.file_targets.
	FileTargets []string `json:"fileTargets"`
	// ValidationCommands holds per-command specs from execution_payload.validation_commands.
	ValidationCommands []ValidationCommandSpec `json:"validationCommands"`
	// AuditChecklist holds the checklist items from audit_seed.audit_checklist.
	AuditChecklist []ChecklistItem `json:"auditChecklist"`
	// NonGoalChecks from audit_seed.non_goal_checks.
	NonGoalChecks []string `json:"nonGoalChecks"`
	// FileScopeChecks from audit_seed.file_scope_checks.
	FileScopeChecks []string `json:"fileScopeChecks"`
	// MissingFields lists any execution_payload fields that were absent.
	MissingFields []string `json:"missingFields"`
}

// ValidationCommandSpec is a single validation command from the packet.
type ValidationCommandSpec struct {
	ID              string `json:"id"`
	Command         string `json:"command"`
	Required        bool   `json:"required"`
	Purpose         string `json:"purpose"`
	SuccessSignal   string `json:"successSignal"`
	FailureHandling string `json:"failureHandling"`
}

// ChecklistItem is one item from audit_seed.audit_checklist (supports old flat-string
// and new typed-object formats from the canonical packet).
type ChecklistItem struct {
	ID               string        `json:"id"`
	Check            string        `json:"check"`
	SeverityIfFailed CheckSeverity `json:"severityIfFailed"`
}

// PerCheckResult is the auditor's evaluation of a single checklist item.
type PerCheckResult struct {
	ID               string        `json:"id"`
	Check            string        `json:"check"`
	Result           CheckResult   `json:"result"`
	SeverityIfFailed CheckSeverity `json:"severityIfFailed"`
	EvidenceSource   string        `json:"evidenceSource"`
	Rationale        string        `json:"rationale"`
}

// ValidationCommandResult is the collected result for one validation command.
type ValidationCommandResult struct {
	ID              string      `json:"id"`
	Command         string      `json:"command"`
	Required        bool        `json:"required"`
	Status          CheckResult `json:"status"`
	ExitResult      string      `json:"exitResult"`
	EvidenceSummary string      `json:"evidenceSummary"`
	RawArtifactPath string      `json:"rawArtifactPath"`
}

// ChangedFileEntry represents one changed file with its status.
type ChangedFileEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"` // e.g. "M", "A", "D", or raw line if unparsed
}

// ExecutorResultEvidence holds executor result artifact data.
type ExecutorResultEvidence struct {
	Present         bool   `json:"present"`
	Content         string `json:"content"` // bounded preview
	Summary         string `json:"summary"` // extracted status/exit lines
	ExitCode        string `json:"exitCode"`
	RawArtifactPath string `json:"rawArtifactPath"`
}

// DiffEvidence holds git diff artifact data.
type DiffEvidence struct {
	Present         bool   `json:"present"`
	Preview         string `json:"preview"` // bounded to MaxPreviewBytes
	RawArtifactPath string `json:"rawArtifactPath"`
}

// ChangedFilesEvidence holds the list of changed files.
type ChangedFilesEvidence struct {
	Present         bool               `json:"present"`
	Files           []ChangedFileEntry `json:"files"`
	RawArtifactPath string             `json:"rawArtifactPath"`
	SourceKind      string             `json:"sourceKind"`
}

// AcceptanceEvidence holds validation failure acceptance evidence.
type AcceptanceEvidence struct {
	Present         bool   `json:"present"`
	Content         string `json:"content"`
	RawArtifactPath string `json:"rawArtifactPath"`
}

// RevisionRequirement describes a specific thing the executor must correct.
type RevisionRequirement struct {
	Reason   string        `json:"reason"`
	Severity CheckSeverity `json:"severity"`
}

// Evidence is the complete structured evidence package collected for one run's audit.
type Evidence struct {
	RunID     int64  `json:"runId"`
	RunTitle  string `json:"runTitle"`
	RunStatus string `json:"runStatus"`

	// Packet holds normalized, human-readable packet metadata (never raw JSON).
	Packet PacketMetadata `json:"packet"`

	// ExecutorResult holds executor result evidence.
	ExecutorResult ExecutorResultEvidence `json:"executorResult"`

	// ValidationResults holds per-command validation evidence.
	ValidationResults []ValidationCommandResult `json:"validationResults"`

	// ChangedFiles holds changed file evidence.
	ChangedFiles ChangedFilesEvidence `json:"changedFiles"`

	// GitDiff holds diff evidence.
	GitDiff DiffEvidence `json:"gitDiff"`

	// AcceptanceEvidence holds validation failure acceptance evidence.
	AcceptanceEvidence AcceptanceEvidence `json:"acceptanceEvidence"`

	// ChecklistResults holds per-check audit results.
	ChecklistResults []PerCheckResult `json:"checklistResults"`

	// FileScopeResults holds file-scope check results.
	FileScopeResults []PerCheckResult `json:"fileScopeResults"`

	// NonGoalResults holds non-goal enforcement results.
	NonGoalResults []PerCheckResult `json:"nonGoalResults"`

	// Warnings holds evidence-gap warnings with severity.
	Warnings []EvidenceWarning `json:"warnings"`

	// RevisionRequirements lists specific items requiring correction.
	RevisionRequirements []RevisionRequirement `json:"revisionRequirements"`
}

type AuditManifestPacket struct {
	PacketID               string   `json:"packet_id"`
	GoalPresent            bool     `json:"goal_present"`
	ScopePresent           bool     `json:"scope_present"`
	NonGoalsPresent        bool     `json:"non_goals_present"`
	FileTargets            []string `json:"file_targets"`
	ValidationCommandCount int      `json:"validation_command_count"`
	AuditChecklistCount    int      `json:"audit_checklist_count"`
	MissingFields          []string `json:"missing_fields"`
}

type AuditManifestExecutorResult struct {
	Present          bool   `json:"present"`
	ArtifactPath     string `json:"artifact_path"`
	PreviewTruncated bool   `json:"preview_truncated"`
}

type AuditManifestValidationResult struct {
	ID              string      `json:"id"`
	Required        bool        `json:"required"`
	Status          CheckResult `json:"status"`
	RawArtifactPath string      `json:"raw_artifact_path"`
}

type AuditManifestChangedFiles struct {
	Present      bool   `json:"present"`
	SourceKind   string `json:"source_kind"`
	Count        int    `json:"count"`
	ArtifactPath string `json:"artifact_path"`
}

type AuditManifestDiff struct {
	Present          bool   `json:"present"`
	ArtifactPath     string `json:"artifact_path"`
	PreviewTruncated bool   `json:"preview_truncated"`
}

type AuditManifestAcceptanceEvidence struct {
	Present      bool   `json:"present"`
	ArtifactPath string `json:"artifact_path"`
}

type AuditManifestEvidence struct {
	ExecutorResult     AuditManifestExecutorResult     `json:"executor_result"`
	ValidationResults  []AuditManifestValidationResult `json:"validation_results"`
	ChangedFiles       AuditManifestChangedFiles       `json:"changed_files"`
	GitDiff            AuditManifestDiff               `json:"git_diff"`
	AcceptanceEvidence AuditManifestAcceptanceEvidence `json:"acceptance_evidence"`
}

type AuditManifestRemoteEvidence struct {
	GitHubPR      string `json:"github_pr"`
	GitHubCI      string `json:"github_ci"`
	GitHubActions string `json:"github_actions"`
}

type AuditEvidenceManifest struct {
	SchemaVersion         string                      `json:"schema_version"`
	RunID                 int64                       `json:"run_id"`
	RunStatusAtCollection string                      `json:"run_status_at_collection"`
	GeneratedAt           time.Time                   `json:"generated_at"`
	Packet                AuditManifestPacket         `json:"packet"`
	Evidence              AuditManifestEvidence       `json:"evidence"`
	Warnings              []EvidenceWarning           `json:"warnings"`
	RevisionRequirements  []RevisionRequirement       `json:"revision_requirements"`
	PreliminaryDecision   Decision                    `json:"preliminary_decision"`
	LocalOnly             bool                        `json:"local_only"`
	RemoteEvidence        AuditManifestRemoteEvidence `json:"remote_evidence"`
}

// GeneratedAudit is the output of a successful audit generation pass.
type GeneratedAudit struct {
	RunID                int64     `json:"runId"`
	Status               string    `json:"status"`
	InputSummary         string    `json:"inputSummary"`
	EvidenceManifest     string    `json:"evidenceManifest"`
	AuditPacket          string    `json:"auditPacket"`
	Decision             Decision  `json:"decision"`
	CreatedAt            time.Time `json:"createdAt"`
	Warnings             []string  `json:"warnings"`
	RevisionRequirements []string  `json:"revisionRequirements"`
}

// ManualAuditSubmission is the payload for a manual auditor submission.
type ManualAuditSubmission struct {
	RunID               int64    `json:"runId"`
	AuditPacketMarkdown string   `json:"auditPacketMarkdown"`
	Decision            Decision `json:"decision"`
	Notes               string   `json:"notes"`
}

type DecisionSubmission struct {
	RunID               int64    `json:"run_id"`
	Decision            Decision `json:"decision"`
	AuditPacketMarkdown string   `json:"audit_packet_markdown"`
	Notes               string   `json:"notes"`
	Source              string   `json:"source"`
	ClientTraceID       string   `json:"client_trace_id"`
}

type AuditDecisionRecord struct {
	SchemaVersion        string    `json:"schema_version"`
	RunID                int64     `json:"run_id"`
	SubmittedAt          time.Time `json:"submitted_at"`
	RunStatusBefore      string    `json:"run_status_before"`
	RunStatusAfter       string    `json:"run_status_after"`
	Decision             Decision  `json:"decision"`
	MappedStatus         string    `json:"mapped_status"`
	Notes                string    `json:"notes,omitempty"`
	Source               string    `json:"source"`
	ClientTraceID        string    `json:"client_trace_id,omitempty"`
	AuditPacketPath      string    `json:"audit_packet_path,omitempty"`
	RevisionArtifactPath string    `json:"revision_artifact_path,omitempty"`
	LocalOnly            bool      `json:"local_only"`
}

type DecisionResult struct {
	RunID                int64     `json:"run_id"`
	Status               string    `json:"status"`
	LifecycleState       string    `json:"lifecycle_state"`
	Decision             Decision  `json:"decision"`
	AuditPacketPath      string    `json:"audit_packet_path,omitempty"`
	DecisionArtifactPath string    `json:"decision_artifact_path"`
	RevisionArtifactPath string    `json:"revision_artifact_path,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

// MaxPreviewBytes is the bounded preview limit for raw artifact content.
const MaxPreviewBytes = 10000

// MaxDiffPreviewBytes is the bounded preview limit for diff content specifically.
const MaxDiffPreviewBytes = 3000
