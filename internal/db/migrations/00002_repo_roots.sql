-- +goose Up
ALTER TABLE repos ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE repos ADD COLUMN discovered_at TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN last_seen_at TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX idx_repos_path_unique_nonempty ON repos(path) WHERE path <> '';

CREATE TABLE repo_roots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    last_scanned_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX idx_repo_roots_path ON repo_roots(path);

INSERT OR IGNORE INTO repo_roots (path, enabled)
VALUES ('D:/Code', 1);

-- +goose Down
DROP TABLE IF EXISTS repo_roots;
DROP INDEX IF EXISTS idx_repos_path_unique_nonempty;
