package contextpackets

import "relay/internal/sources"

const (
	ContextPacketStatusCreated = "created"
	ContextPacketStatusPartial = "partial"
	ContextPacketStatusBlocked = "blocked"

	ContextPacketSchemaVersion = "1.0.0"

	SourceTypeFileRead    = "file_read"
	SourceTypeSearchMatch = "search_match"
	SourceTypeInventory   = "inventory"

	CoverageStatusCovered = "covered"
	CoverageStatusPartial = "partial"
	CoverageStatusBlocked = "blocked"
	CoverageStatusMissing = "missing"
)

type ContextPacketInput struct {
	ProjectID        string
	PlanID           string
	PassID           string
	TaskSlug         string
	SourceSnapshotID string

	SeedFiles    []ContextSeedFile
	SeedSearches []ContextSeedSearch

	MaxSources       int
	MaxTotalBytes    int
	IncludeInventory bool
}

type ContextSeedFile struct {
	RepoID    string
	Path      string
	LineStart int
	LineEnd   int
	Reason    string
	Required  bool
	MaxBytes  int
}

type ContextSeedSearch struct {
	RepoIDs       []string
	Pattern       string
	Literal       bool
	CaseSensitive bool
	ContextLines  int
	MaxResults    int
	Reason        string
	Required      bool
}

type ContextPacketResult struct {
	ContextPacketID    string
	ProjectID          string
	PlanID             string
	PassID             string
	TaskSlug           string
	SourceSnapshotID   string
	Status             string
	PacketJSONPath     string
	PacketMarkdownPath string
	CoverageReportPath string
	SourceCount        int
	CoveredSeedCount   int
	BlockedSeedCount   int
	MissingSeedCount   int
	Truncated          bool
	Blockers           []sources.SourceBlocker
}

type ContextPacket struct {
	SchemaVersion    string                  `json:"schema_version"`
	ContextPacketID  string                  `json:"context_packet_id"`
	ProjectID        string                  `json:"project_id"`
	PlanID           string                  `json:"plan_id,omitempty"`
	PassID           string                  `json:"pass_id,omitempty"`
	TaskSlug         string                  `json:"task_slug"`
	SourceSnapshotID string                  `json:"source_snapshot_id"`
	Status           string                  `json:"status"`
	GeneratedAt      string                  `json:"generated_at"`
	Summary          ContextPacketSummary    `json:"summary"`
	Sources          []ContextSource         `json:"sources"`
	Coverage         []ContextCoverageEntry  `json:"coverage"`
	Blockers         []sources.SourceBlocker `json:"blockers,omitempty"`
}

type ContextPacketSummary struct {
	SourceCount       int  `json:"source_count"`
	CoveredSeedCount  int  `json:"covered_seed_count"`
	BlockedSeedCount  int  `json:"blocked_seed_count"`
	MissingSeedCount  int  `json:"missing_seed_count"`
	Truncated         bool `json:"truncated"`
	MaxSources        int  `json:"max_sources"`
	MaxTotalBytes     int  `json:"max_total_bytes"`
	TotalSourceBytes  int  `json:"total_source_bytes"`
	InventoryIncluded bool `json:"inventory_included"`
}

type ContextSource struct {
	SourceID         string `json:"source_id"`
	SourceType       string `json:"source_type"`
	ProjectID        string `json:"project_id"`
	RepoID           string `json:"repo_id"`
	SourceSnapshotID string `json:"source_snapshot_id"`
	Path             string `json:"path"`
	LineStart        int    `json:"line_start,omitempty"`
	LineEnd          int    `json:"line_end,omitempty"`
	Content          string `json:"content,omitempty"`
	Snippet          string `json:"snippet,omitempty"`
	ContentHash      string `json:"content_hash,omitempty"`
	SnippetHash      string `json:"snippet_hash,omitempty"`
	RedactionStatus  string `json:"redaction_status"`
	Truncated        bool   `json:"truncated"`
	GeneratedAt      string `json:"generated_at"`
	Reason           string `json:"reason,omitempty"`
}

type ContextCoverageReport struct {
	SchemaVersion    string                 `json:"schema_version"`
	ContextPacketID  string                 `json:"context_packet_id"`
	ProjectID        string                 `json:"project_id"`
	PlanID           string                 `json:"plan_id,omitempty"`
	PassID           string                 `json:"pass_id,omitempty"`
	TaskSlug         string                 `json:"task_slug"`
	SourceSnapshotID string                 `json:"source_snapshot_id"`
	Status           string                 `json:"status"`
	GeneratedAt      string                 `json:"generated_at"`
	Summary          ContextPacketSummary   `json:"summary"`
	Entries          []ContextCoverageEntry `json:"entries"`
}

type ContextCoverageEntry struct {
	SeedID       string                  `json:"seed_id"`
	SeedType     string                  `json:"seed_type"`
	Required     bool                    `json:"required"`
	Status       string                  `json:"status"`
	RepoID       string                  `json:"repo_id,omitempty"`
	Path         string                  `json:"path,omitempty"`
	Pattern      string                  `json:"pattern,omitempty"`
	Reason       string                  `json:"reason,omitempty"`
	SourceIDs    []string                `json:"source_ids,omitempty"`
	Truncated    bool                    `json:"truncated"`
	Blockers     []sources.SourceBlocker `json:"blockers,omitempty"`
	MissingCause string                  `json:"missing_cause,omitempty"`
}
