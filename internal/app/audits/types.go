package audits

import (
	"time"

	"relay/internal/auditor"
	"relay/internal/store"
)

type LocalAuditInput = auditor.LocalAuditInput
type LocalAuditResult = auditor.LocalAuditResult
type LocalAuditRecordResult = auditor.LocalAuditRecordResult

// Decision exposes audit decision values at the app boundary while the legacy
// auditor package remains the internal implementation adapter.
type Decision = auditor.Decision

const (
	DecisionManualReviewRequired = auditor.DecisionManualReviewRequired
	DecisionAccepted             = auditor.DecisionAccepted
	DecisionAcceptedWithWarnings = auditor.DecisionAcceptedWithWarnings
	DecisionRevisionRequired     = auditor.DecisionRevisionRequired
	DecisionBlocked              = auditor.DecisionBlocked
)

// LocalAuditMode exposes local audit mode values at the app boundary.
type LocalAuditMode = auditor.LocalAuditMode

const (
	LocalAuditModeRecentCommit        = auditor.LocalAuditModeRecentCommit
	LocalAuditModeSelectedPassChanges = auditor.LocalAuditModeSelectedPassChanges
	LocalAuditModeFeatureSlice        = auditor.LocalAuditModeFeatureSlice
	LocalAuditModeFullRepository      = auditor.LocalAuditModeFullRepository
)

var (
	ErrUnsupportedDecision   = auditor.ErrUnsupportedDecision
	ErrCompletedRun          = auditor.ErrCompletedRun
	ErrAuditDecisionNotReady = auditor.ErrAuditDecisionNotReady
)

type AuditArtifact struct {
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

type AuditStatus struct {
	RunID                        string         `json:"runId"`
	RunStatus                    string         `json:"runStatus"`
	AuditState                   string         `json:"auditState"`
	CanGenerateAudit             bool           `json:"canGenerateAudit"`
	CanSubmitDecision            bool           `json:"canSubmitDecision"`
	CanApprove                   bool           `json:"canApprove"`
	CanRequestRevision           bool           `json:"canRequestRevision"`
	CanCloseRun                  bool           `json:"canCloseRun"`
	EvidenceManifestArtifact     *AuditArtifact `json:"evidenceManifestArtifact,omitempty"`
	GeneratedAuditPacketArtifact *AuditArtifact `json:"generatedAuditPacketArtifact,omitempty"`
	ManualAuditPacketArtifact    *AuditArtifact `json:"manualAuditPacketArtifact,omitempty"`
	DecisionArtifact             *AuditArtifact `json:"decisionArtifact,omitempty"`
	Blockers                     []string       `json:"blockers"`
	Warnings                     []string       `json:"warnings"`
	RevisionRequirements         []string       `json:"revisionRequirements"`
	LocalOnly                    bool           `json:"localOnly"`
}

type SubmitAuditPacketInput struct {
	RunID               int64
	AuditPacketMarkdown string
	Decision            Decision
	Notes               string
}

type AuditDecisionInput struct {
	RunID    int64
	Decision Decision
	Notes    string
}

type RevisionInput struct {
	RunID  int64
	Notes  string
	Reason string
}

type CommitMessageResult struct {
	RunID         int64
	CommitMessage string
	ArtifactPath  string
	ArtifactKind  string
}

type CloseRunResult struct {
	Run       *store.Run
	UpdatedAt time.Time
}

type AuditGenerationConflictError struct {
	Message string
}

func (e *AuditGenerationConflictError) Error() string {
	return e.Message
}
