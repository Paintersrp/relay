package sources

const (
	SnapshotKindCleanCommit   = "clean_commit"
	SnapshotKindDirtyWorktree = "dirty_worktree"
	SnapshotKindMixed         = "mixed"
	SnapshotKindUnavailable   = "unavailable"

	SnapshotStatusCreated = "created"
	SnapshotStatusPartial = "partial"
	SnapshotStatusBlocked = "blocked"

	DiffModeWorktree = "worktree"
	DiffModeStaged   = "staged"

	RedactionStatusNotNeeded  = "not_needed"
	RedactionStatusRedacted   = "redacted"
	RedactionStatusBlocked    = "blocked"
	RedactionStatusNotScanned = "not_scanned"
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
	RepoID  string
	Code    string
	Message string
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
