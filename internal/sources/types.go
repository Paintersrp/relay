package sources

const (
	SnapshotKindCleanCommit   = "clean_commit"
	SnapshotKindDirtyWorktree = "dirty_worktree"
	SnapshotKindMixed         = "mixed"
	SnapshotKindUnavailable   = "unavailable"

	SnapshotStatusCreated = "created"
	SnapshotStatusPartial = "partial"
	SnapshotStatusBlocked = "blocked"

	DiffModeWorktree     = "worktree"
	DiffModeStaged       = "staged"
	DiffModeRecentCommit = "recent_commit"

	RedactionStatusNotNeeded  = "not_needed"
	RedactionStatusRedacted   = "redacted"
	RedactionStatusBlocked    = "blocked"
	RedactionStatusNotScanned = "not_scanned"

	SourceOperationStatusOK      = "ok"
	SourceOperationStatusBlocked = "blocked"
	SourceOperationStatusPartial = "partial"

	SourceBlockerUnsafePath          = "unsafe_path"
	SourceBlockerExcludedPath        = "excluded_path"
	SourceBlockerOversized           = "oversized"
	SourceBlockerBinary              = "binary"
	SourceBlockerRedactionBlocked    = "redaction_blocked"
	SourceBlockerSnapshotMissing     = "source_snapshot_missing"
	SourceBlockerSnapshotFileChanged = "source_snapshot_file_changed"
	SourceBlockerRipgrepMissing      = "ripgrep_unavailable"
	SourceBlockerUnknownRepository   = "unknown_repository"
	SourceBlockerAmbiguousRepository = "alias_ambiguous"

	DefaultSourceSnapshotFreshnessMaxAgeSeconds int64 = 900

	SourceFreshnessStatusFresh         = "fresh"
	SourceFreshnessStatusDirtyWorktree = "dirty_worktree"
	SourceFreshnessStatusPartial       = "partial"
	SourceFreshnessStatusBlocked       = "blocked"
	SourceFreshnessStatusStaleByAge    = "stale_by_age"
	SourceFreshnessStatusDrifted       = "drifted"

	SourceFreshnessCodeDirtyWorktree = "source_snapshot_dirty_worktree"
	SourceFreshnessCodeDrifted       = "source_snapshot_drifted"
	SourceFreshnessCodeStale         = "source_snapshot_stale"
	SourceFreshnessCodeUnavailable   = "source_snapshot_unavailable"
)

type SourceSnapshotInput struct {
	ProjectID           string
	RepoIDs             []string
	IncludeDisabled     bool
	IncludeFileMetadata bool
	MaxFilesPerRepo     int
}

type SourceSnapshotResult struct {
	SourceSnapshotID string
	ProjectID        string
	SnapshotKind     string
	Status           string
	Repositories     []RepositorySnapshotResult
	Blockers         []SourceBlocker
	FreshnessReport  SourceFreshnessReport `json:"freshness_report"`
}

type RepositorySnapshotResult struct {
	RepoID            string
	Role              string
	LocalPath         string
	DefaultBranch     string
	GitStatus         RepositoryGitStatus
	RecentCommit      *RecentCommit
	FileCount         int
	IncludedFileCount int
	Freshness         RepositoryFreshnessReport `json:"freshness,omitempty"`
}

type SourceFreshnessReport struct {
	Status              string                      `json:"status"`
	ReusableForHandoff  bool                        `json:"reusable_for_handoff"`
	SourceSnapshotID    string                      `json:"source_snapshot_id"`
	GeneratedAt         string                      `json:"generated_at"`
	SnapshotCreatedAt   string                      `json:"snapshot_created_at"`
	SnapshotCompletedAt string                      `json:"snapshot_completed_at,omitempty"`
	AgeSeconds          int64                       `json:"age_seconds"`
	MaxAgeSeconds       int64                       `json:"max_age_seconds"`
	RepositoryReports   []RepositoryFreshnessReport `json:"repository_reports"`
	Warnings            []SourceBlocker             `json:"warnings,omitempty"`
	Blockers            []SourceBlocker             `json:"blockers,omitempty"`
	NextActions         []SourceFreshnessNextAction `json:"next_actions,omitempty"`
}

type RepositoryFreshnessReport struct {
	RepoID              string          `json:"repo_id"`
	Status              string          `json:"status"`
	ReusableForHandoff  bool            `json:"reusable_for_handoff"`
	CapturedBranch      string          `json:"captured_branch,omitempty"`
	CurrentBranch       string          `json:"current_branch,omitempty"`
	CapturedHeadSHA     string          `json:"captured_head_sha,omitempty"`
	CurrentHeadSHA      string          `json:"current_head_sha,omitempty"`
	CapturedDirty       bool            `json:"captured_dirty"`
	CurrentDirty        bool            `json:"current_dirty"`
	CapturedChangeCount int             `json:"captured_change_count"`
	CurrentChangeCount  int             `json:"current_change_count"`
	GitStatusAvailable  bool            `json:"git_status_available"`
	Warnings            []SourceBlocker `json:"warnings,omitempty"`
	Blockers            []SourceBlocker `json:"blockers,omitempty"`
}

type SourceFreshnessNextAction struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type SourceBlocker struct {
	RepoID      string   `json:"repo_id,omitempty"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Recoverable bool     `json:"recoverable,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	NextActions []string `json:"next_actions,omitempty"`
}

type RepositoryGitStatus struct {
	RepoID             string
	CurrentBranch      string
	HeadSHA            string
	Dirty              bool
	StagedCount        int
	UnstagedCount      int
	UntrackedCount     int
	ChangedFileCount   int
	PorcelainHash      string
	GitStatusAvailable bool
	GitError           string
}

type RecentCommit struct {
	RepoID      string
	CommitSHA   string
	AuthorName  string
	AuthorEmail string
	AuthorDate  string
	Subject     string
}

type ChangedFile struct {
	RepoID string
	Path   string
	Status string
	Staged bool
}

type BoundedDiff struct {
	RepoID          string
	Mode            string
	Content         string
	ContentHash     string
	Truncated       bool
	MaxBytes        int
	RedactionStatus string
}

type FileInventoryInput struct {
	ProjectID        string
	SourceSnapshotID string
	RepoIDs          []string
	IncludeExcluded  bool
	IncludeDisabled  bool
	MaxResults       int
}

type SourceFileRecord struct {
	ProjectID        string
	RepoID           string
	SourceSnapshotID string
	Path             string
	SizeBytes        int64
	ContentHash      string
	HashAlgorithm    string
	Tracked          bool
	Included         bool
	ExclusionReason  string
	RedactionStatus  string
	IndexedAt        string
}

type FileInventoryResult struct {
	ProjectID        string
	SourceSnapshotID string
	Files            []SourceFileRecord
	Truncated        bool
	Blockers         []SourceBlocker
	GeneratedAt      string
	FreshnessReport  SourceFreshnessReport `json:"freshness_report"`
}

type SourceSearchInput struct {
	ProjectID        string
	SourceSnapshotID string
	RepoIDs          []string
	Pattern          string
	Literal          bool
	CaseSensitive    bool
	ContextLines     int
	MaxResults       int
	MaxBytes         int
	IncludeExcluded  bool
}

type SourceSearchMatch struct {
	ProjectID        string
	RepoID           string
	SourceSnapshotID string
	Path             string
	LineStart        int
	LineEnd          int
	Snippet          string
	SnippetHash      string
	ContentHash      string
	RedactionStatus  string
	Truncated        bool
	GeneratedAt      string
}

type SourceSearchResult struct {
	ProjectID        string
	SourceSnapshotID string
	Matches          []SourceSearchMatch
	Truncated        bool
	Blockers         []SourceBlocker
	GeneratedAt      string
	FreshnessReport  SourceFreshnessReport `json:"freshness_report"`
}

type BoundedFileReadInput struct {
	ProjectID        string
	SourceSnapshotID string
	RepoID           string
	Path             string
	LineStart        int
	LineEnd          int
	MaxBytes         int
}

type BoundedFileReadResult struct {
	ProjectID        string
	RepoID           string
	SourceSnapshotID string
	Path             string
	LineStart        int
	LineEnd          int
	Content          string
	ContentHash      string
	CurrentHash      string
	SnippetHash      string
	RedactionStatus  string
	Truncated        bool
	GeneratedAt      string
	Blockers         []SourceBlocker
	FreshnessReport  SourceFreshnessReport `json:"freshness_report"`
}
