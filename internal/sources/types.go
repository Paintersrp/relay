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
	SourceBlockerAmbiguousRepository = "ambiguous_repository"
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
}
