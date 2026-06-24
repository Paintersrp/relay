package runs

// RelayRun is the run response contract.
type RelayRun struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Repo                  string                 `json:"repo"`
	Branch                string                 `json:"branch"`
	ActiveStep            string                 `json:"activeStep"`
	Status                string                 `json:"status"`
	LifecycleState        string                 `json:"lifecycleState"`
	CreatedAt             string                 `json:"createdAt"`
	UpdatedAt             string                 `json:"updatedAt"`
	Summary               string                 `json:"summary"`
	Model                 string                 `json:"model"`
	RiskLevel             string                 `json:"riskLevel"`
	Validation            RelayValidationResult  `json:"validation"`
	Artifacts             []RelayArtifact        `json:"artifacts"`
	LatestEvents          []RelayRunEvent        `json:"latestEvents"`
	StatusSeverity        string                 `json:"statusSeverity"`
	State                 string                 `json:"state"`
	Title                 string                 `json:"title"`
	PacketID              string                 `json:"packetId"`
	Worktree              string                 `json:"worktree,omitempty"`
	Executor              string                 `json:"executor"`
	ExecutorAdapter       string                 `json:"executorAdapter"`
	ValidationSummary     RelayValidationResult  `json:"validationSummary"`
	ApprovalGate          RelayApprovalGate      `json:"approvalGate"`
	LogPreview            RelayLogPreview        `json:"logPreview"`
	StepLabels            map[string]string      `json:"stepLabels"`
	LatestExecutionStatus string                 `json:"latestExecutionStatus,omitempty"`
	PlanContext           *RelayRunPlanContext   `json:"planContext,omitempty"`
	Provenance            *RelayRunProvenance    `json:"provenance,omitempty"`
	SourceContext         *RelayRunSourceContext `json:"source_context,omitempty"`
}

type RelayRunPlanContext struct {
	PlanID               string `json:"planId,omitempty"`
	PlanTitle            string `json:"planTitle,omitempty"`
	PlanRowID            string `json:"planRowId,omitempty"`
	PassID               string `json:"passId,omitempty"`
	PassName             string `json:"passName,omitempty"`
	PassRowID            string `json:"passRowId,omitempty"`
	PassSequence         *int64 `json:"passSequence,omitempty"`
	PassStatus           string `json:"passStatus,omitempty"`
	SourceArtifactPath   string `json:"sourceArtifactPath,omitempty"`
	ContextPacketID      string `json:"contextPacketId,omitempty"`
	SourceSnapshotID     string `json:"sourceSnapshotId,omitempty"`
	PlannerHandoffSHA256 string `json:"plannerHandoffSha256,omitempty"`
	ProjectID            string `json:"projectId,omitempty"`
	ProjectRowID         string `json:"projectRowId,omitempty"`
}

type RelayRunProvenance struct {
	PlannerHandoffSHA256 string `json:"plannerHandoffSha256,omitempty"`
	PlannerHandoffBytes  *int64 `json:"plannerHandoffBytes,omitempty"`
	SourceArtifactPath   string `json:"sourceArtifactPath,omitempty"`
	Source               string `json:"source,omitempty"`
	ClientTraceID        string `json:"clientTraceId,omitempty"`
	PlanID               string `json:"planId,omitempty"`
	PassID               string `json:"passId,omitempty"`
	ContextPacketID      string `json:"contextPacketId,omitempty"`
	SourceSnapshotID     string `json:"sourceSnapshotId,omitempty"`
	ArtifactKind         string `json:"artifactKind,omitempty"`
}

type RelayRunSourceContext struct {
	PlanID             string `json:"plan_id,omitempty"`
	PassID             string `json:"pass_id,omitempty"`
	SourceSnapshotID   string `json:"source_snapshot_id,omitempty"`
	ContextPacketID    string `json:"context_packet_id,omitempty"`
	CoverageReportPath string `json:"coverage_report_path,omitempty"`
	RecordedAt         string `json:"recorded_at,omitempty"`
}

type RelayValidationResult struct {
	Errors   int                    `json:"errors"`
	Warnings int                    `json:"warnings"`
	Passed   int                    `json:"passed"`
	Issues   []RelayValidationIssue `json:"issues"`
}

type RelayValidationIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

type RelayArtifact struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	StorageKind string `json:"storageKind,omitempty"`
	ContentURL  string `json:"contentUrl,omitempty"`
	SizeHint    string `json:"sizeHint,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Status      string `json:"status"`
	Filename    string `json:"filename"`
	Preview     string `json:"preview,omitempty"`
}

type RelayRunEvent struct {
	ID        string                 `json:"id"`
	RunID     string                 `json:"runId"`
	Kind      string                 `json:"kind"`
	Message   string                 `json:"message"`
	CreatedAt string                 `json:"createdAt"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

type RelayApprovalGate struct {
	Label string `json:"label"`
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

type RelayLogPreview struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}
