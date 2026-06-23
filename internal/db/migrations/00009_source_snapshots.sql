-- +goose Up
CREATE TABLE source_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_snapshot_id TEXT NOT NULL UNIQUE,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    snapshot_kind TEXT NOT NULL CHECK (snapshot_kind IN ('clean_commit', 'dirty_worktree', 'mixed', 'unavailable')),
    status TEXT NOT NULL CHECK (status IN ('created', 'partial', 'blocked')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    completed_at TEXT NOT NULL DEFAULT '',
    summary_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE source_snapshot_repositories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_snapshot_row_id INTEGER NOT NULL REFERENCES source_snapshots(id) ON DELETE CASCADE,
    project_repository_row_id INTEGER NOT NULL REFERENCES project_repositories(id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL,
    role TEXT NOT NULL,
    local_path TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    current_branch TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    dirty INTEGER NOT NULL DEFAULT 0,
    staged_count INTEGER NOT NULL DEFAULT 0,
    unstaged_count INTEGER NOT NULL DEFAULT 0,
    untracked_count INTEGER NOT NULL DEFAULT 0,
    changed_file_count INTEGER NOT NULL DEFAULT 0,
    git_status_available INTEGER NOT NULL DEFAULT 0,
    git_error TEXT NOT NULL DEFAULT '',
    status_porcelain_hash TEXT NOT NULL DEFAULT '',
    indexed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE source_snapshot_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_snapshot_repository_row_id INTEGER NOT NULL REFERENCES source_snapshot_repositories(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT NOT NULL DEFAULT '',
    hash_algorithm TEXT NOT NULL DEFAULT 'sha256',
    tracked INTEGER NOT NULL DEFAULT 1,
    included INTEGER NOT NULL DEFAULT 1,
    exclusion_reason TEXT NOT NULL DEFAULT '',
    redaction_status TEXT NOT NULL DEFAULT 'not_scanned',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(source_snapshot_repository_row_id, path)
);

CREATE INDEX IF NOT EXISTS idx_source_snapshots_source_snapshot_id ON source_snapshots(source_snapshot_id);
CREATE INDEX IF NOT EXISTS idx_source_snapshots_project_row_id ON source_snapshots(project_row_id);
CREATE INDEX IF NOT EXISTS idx_source_snapshot_repositories_source_snapshot_row_id ON source_snapshot_repositories(source_snapshot_row_id);
CREATE INDEX IF NOT EXISTS idx_source_snapshot_repositories_repo_id ON source_snapshot_repositories(repo_id);
CREATE INDEX IF NOT EXISTS idx_source_snapshot_files_source_snapshot_repository_row_id ON source_snapshot_files(source_snapshot_repository_row_id);
CREATE INDEX IF NOT EXISTS idx_source_snapshot_files_path ON source_snapshot_files(path);

-- +goose Down
DROP TABLE IF EXISTS source_snapshot_files;
DROP TABLE IF EXISTS source_snapshot_repositories;
DROP TABLE IF EXISTS source_snapshots;
