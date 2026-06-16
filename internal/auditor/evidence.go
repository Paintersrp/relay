package auditor

import "time"

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

type PacketScope struct {
	PacketID  string `json:"packetId"`
	Goal      string `json:"goal"`
	Scope     string `json:"scope"`
	NonGoals  string `json:"nonGoals"`
	AuditSeed string `json:"auditSeed"`
}

type ExecutorResultEvidence struct {
	Present  bool   `json:"present"`
	Content  string `json:"content"`
	Summary  string `json:"summary"`
	ExitCode string `json:"exitCode"`
}

type ValidationOutputEvidence struct {
	Present bool   `json:"present"`
	Content string `json:"content"`
	Summary string `json:"summary"`
}

type ChangedFilesEvidence struct {
	Present bool     `json:"present"`
	Files   []string `json:"files"`
	Preview string   `json:"preview"`
}

type DiffEvidence struct {
	Present bool   `json:"present"`
	Content string `json:"content"`
	Preview string `json:"preview"`
}

type Evidence struct {
	RunID            int64                    `json:"runId"`
	RunTitle         string                   `json:"runTitle"`
	RunStatus        string                   `json:"runStatus"`
	Packet           PacketScope              `json:"packet"`
	ExecutorResult   ExecutorResultEvidence   `json:"executorResult"`
	ValidationOutput ValidationOutputEvidence `json:"validationOutput"`
	ChangedFiles     ChangedFilesEvidence     `json:"changedFiles"`
	GitDiff          DiffEvidence             `json:"gitDiff"`
	Warnings         []string                 `json:"warnings"`
}

type GeneratedAudit struct {
	RunID        int64     `json:"runId"`
	Status       string    `json:"status"`
	InputSummary string    `json:"inputSummary"`
	AuditPacket  string    `json:"auditPacket"`
	Decision     Decision  `json:"decision"`
	CreatedAt    time.Time `json:"createdAt"`
	Warnings     []string  `json:"warnings"`
}

type ManualAuditSubmission struct {
	RunID               int64    `json:"runId"`
	AuditPacketMarkdown string   `json:"auditPacketMarkdown"`
	Decision            Decision `json:"decision"`
	Notes               string   `json:"notes"`
}

const MaxPreviewBytes = 10000
