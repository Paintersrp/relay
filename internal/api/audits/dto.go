package audits

import appaudits "relay/internal/app/audits"

type RelayAuditStatus struct {
	RunID                        string         `json:"runId"`
	RunStatus                    string         `json:"runStatus"`
	AuditState                   string         `json:"auditState"`
	CanGenerateAudit             bool           `json:"canGenerateAudit"`
	CanSubmitDecision            bool           `json:"canSubmitDecision"`
	CanApprove                   bool           `json:"canApprove"`
	CanRequestRevision           bool           `json:"canRequestRevision"`
	CanCloseRun                  bool           `json:"canCloseRun"`
	EvidenceManifestArtifact     *RelayArtifact `json:"evidenceManifestArtifact,omitempty"`
	GeneratedAuditPacketArtifact *RelayArtifact `json:"generatedAuditPacketArtifact,omitempty"`
	ManualAuditPacketArtifact    *RelayArtifact `json:"manualAuditPacketArtifact,omitempty"`
	DecisionArtifact             *RelayArtifact `json:"decisionArtifact,omitempty"`
	Blockers                     []string       `json:"blockers"`
	Warnings                     []string       `json:"warnings"`
	RevisionRequirements         []string       `json:"revisionRequirements"`
	LocalOnly                    bool           `json:"localOnly"`
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

type localAuditAPIRequest struct {
	Mode             string   `json:"mode"`
	ProjectID        string   `json:"project_id"`
	RepoIDs          []string `json:"repo_ids"`
	PlanID           string   `json:"plan_id"`
	PassID           string   `json:"pass_id"`
	SourceSnapshotID string   `json:"source_snapshot_id"`
	ContextPacketID  string   `json:"context_packet_id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	Paths            []string `json:"paths"`
	SearchTerms      []string `json:"search_terms"`
	DiffMode         string   `json:"diff_mode"`
	MaxFiles         int      `json:"max_files"`
	MaxBytes         int      `json:"max_bytes"`
	ContextLines     int      `json:"context_lines"`
}

type submitAuditPacketRequest struct {
	AuditPacketMarkdown string             `json:"audit_packet_markdown"`
	Decision            appaudits.Decision `json:"decision"`
	Notes               string             `json:"notes"`
}

type approveAuditRequest struct {
	Decision string `json:"decision"`
	Notes    string `json:"notes"`
}

type requestRevisionRequest struct {
	Notes  string `json:"notes"`
	Reason string `json:"reason"`
}
