package audits

import (
	"time"

	"relay/internal/auditor"
	"relay/internal/store"
)

type LocalAuditInput = auditor.LocalAuditInput
type LocalAuditResult = auditor.LocalAuditResult
type LocalAuditRecordResult = auditor.LocalAuditRecordResult

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
	Decision            auditor.Decision
	Notes               string
}

type AuditDecisionInput struct {
	RunID    int64
	Decision auditor.Decision
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
