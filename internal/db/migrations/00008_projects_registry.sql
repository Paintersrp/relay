-- +goose Up
CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    default_repository_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE project_repositories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_row_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('primary', 'reference', 'contracts', 'docs')),
    local_path TEXT NOT NULL,
    remote_label TEXT NOT NULL DEFAULT '',
    remote_url TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT 'main',
    allowed_roots_json TEXT NOT NULL DEFAULT '[]',
    ignored_globs_json TEXT NOT NULL DEFAULT '[]',
    max_file_size_bytes INTEGER NOT NULL DEFAULT 262144 CHECK (max_file_size_bytes >= 1024),
    include_untracked INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(project_row_id, repo_id)
);

CREATE INDEX IF NOT EXISTS idx_projects_status ON projects(status);
CREATE INDEX IF NOT EXISTS idx_project_repositories_project_row_id ON project_repositories(project_row_id);
CREATE INDEX IF NOT EXISTS idx_project_repositories_role ON project_repositories(role);
CREATE INDEX IF NOT EXISTS idx_project_repositories_enabled ON project_repositories(enabled);

-- +goose Down
DROP TABLE IF EXISTS project_repositories;
DROP TABLE IF EXISTS projects;
