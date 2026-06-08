package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Repo struct {
	ID                        int64  `json:"id"`
	Name                      string `json:"name"`
	Path                      string `json:"path"`
	DefaultValidationCommands string `json:"default_validation_commands"`
	CreatedAt                 string `json:"created_at"`
	UpdatedAt                 string `json:"updated_at"`
}

type Run struct {
	ID               int64  `json:"id"`
	RepoID           int64  `json:"repo_id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	RecommendedModel string `json:"recommended_model"`
	SelectedModel    string `json:"selected_model"`
	BranchName       string `json:"branch_name"`
	BaseCommit       string `json:"base_commit"`
	HeadCommit       string `json:"head_commit"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type Artifact struct {
	ID        int64  `json:"id"`
	RunID     int64  `json:"run_id"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	MimeType  string `json:"mime_type"`
	CreatedAt string `json:"created_at"`
}

type Event struct {
	ID           int64  `json:"id"`
	RunID        int64  `json:"run_id"`
	Level        string `json:"level"`
	Message      string `json:"message"`
	MetadataJSON string `json:"metadata_json"`
	CreatedAt    string `json:"created_at"`
}

type Check struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	DetailsJSON string `json:"details_json"`
	CreatedAt   string `json:"created_at"`
}

type Store struct {
	db  *sql.DB
	log *slog.Logger
}

func Open(dbPath string, log *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	db.SetMaxOpenConns(1)

	return &Store{db: db, log: log}, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

// Repos

func (s *Store) CreateRepo(name, path string) (*Repo, error) {
	r := &Repo{}
	err := s.db.QueryRow(
		`INSERT INTO repos (name, path) VALUES (?, ?) RETURNING id, name, path, default_validation_commands, created_at, updated_at`,
		name, path,
	).Scan(&r.ID, &r.Name, &r.Path, &r.DefaultValidationCommands, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) GetRepo(id int64) (*Repo, error) {
	r := &Repo{}
	err := s.db.QueryRow(
		`SELECT id, name, path, default_validation_commands, created_at, updated_at FROM repos WHERE id = ?`, id,
	).Scan(&r.ID, &r.Name, &r.Path, &r.DefaultValidationCommands, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) ListRepos() ([]Repo, error) {
	rows, err := s.db.Query(`SELECT id, name, path, default_validation_commands, created_at, updated_at FROM repos ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var repos []Repo
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.Name, &r.Path, &r.DefaultValidationCommands, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *Store) GetRepoByName(name string) (*Repo, error) {
	r := &Repo{}
	err := s.db.QueryRow(
		`SELECT id, name, path, default_validation_commands, created_at, updated_at FROM repos WHERE name = ?`, name,
	).Scan(&r.ID, &r.Name, &r.Path, &r.DefaultValidationCommands, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// Runs

func (s *Store) CreateRun(repoID int64, title, status, recommendedModel, selectedModel, branchName string) (*Run, error) {
	r := &Run{}
	err := s.db.QueryRow(
		`INSERT INTO runs (repo_id, title, status, recommended_model, selected_model, branch_name) VALUES (?, ?, ?, ?, ?, ?) RETURNING id, repo_id, title, status, recommended_model, selected_model, branch_name, base_commit, head_commit, created_at, updated_at`,
		repoID, title, status, recommendedModel, selectedModel, branchName,
	).Scan(&r.ID, &r.RepoID, &r.Title, &r.Status, &r.RecommendedModel, &r.SelectedModel, &r.BranchName, &r.BaseCommit, &r.HeadCommit, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) GetRun(id int64) (*Run, error) {
	r := &Run{}
	err := s.db.QueryRow(
		`SELECT id, repo_id, title, status, recommended_model, selected_model, branch_name, base_commit, head_commit, created_at, updated_at FROM runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.RepoID, &r.Title, &r.Status, &r.RecommendedModel, &r.SelectedModel, &r.BranchName, &r.BaseCommit, &r.HeadCommit, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (s *Store) ListRecentRuns(limit int) ([]Run, error) {
	rows, err := s.db.Query(`SELECT id, repo_id, title, status, recommended_model, selected_model, branch_name, base_commit, head_commit, created_at, updated_at FROM runs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.RepoID, &r.Title, &r.Status, &r.RecommendedModel, &r.SelectedModel, &r.BranchName, &r.BaseCommit, &r.HeadCommit, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) UpdateRunStatus(id int64, status string) (*Run, error) {
	r := &Run{}
	err := s.db.QueryRow(
		`UPDATE runs SET status = ?, updated_at = datetime('now') WHERE id = ? RETURNING id, repo_id, title, status, recommended_model, selected_model, branch_name, base_commit, head_commit, created_at, updated_at`,
		status, id,
	).Scan(&r.ID, &r.RepoID, &r.Title, &r.Status, &r.RecommendedModel, &r.SelectedModel, &r.BranchName, &r.BaseCommit, &r.HeadCommit, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// Artifacts

func (s *Store) CreateArtifact(runID int64, kind, path, mimeType string) (*Artifact, error) {
	a := &Artifact{}
	err := s.db.QueryRow(
		`INSERT INTO artifacts (run_id, kind, path, mime_type) VALUES (?, ?, ?, ?) RETURNING id, run_id, kind, path, mime_type, created_at`,
		runID, kind, path, mimeType,
	).Scan(&a.ID, &a.RunID, &a.Kind, &a.Path, &a.MimeType, &a.CreatedAt)
	return a, err
}

func (s *Store) GetArtifact(id int64) (*Artifact, error) {
	a := &Artifact{}
	err := s.db.QueryRow(
		`SELECT id, run_id, kind, path, mime_type, created_at FROM artifacts WHERE id = ?`, id,
	).Scan(&a.ID, &a.RunID, &a.Kind, &a.Path, &a.MimeType, &a.CreatedAt)
	return a, err
}

func (s *Store) ListArtifactsByRun(runID int64) ([]Artifact, error) {
	rows, err := s.db.Query(`SELECT id, run_id, kind, path, mime_type, created_at FROM artifacts WHERE run_id = ? ORDER BY created_at DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artifacts []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.RunID, &a.Kind, &a.Path, &a.MimeType, &a.CreatedAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

// Events

func (s *Store) CreateEvent(runID int64, level, message string) (*Event, error) {
	e := &Event{}
	err := s.db.QueryRow(
		`INSERT INTO events (run_id, level, message) VALUES (?, ?, ?) RETURNING id, run_id, level, message, metadata_json, created_at`,
		runID, level, message,
	).Scan(&e.ID, &e.RunID, &e.Level, &e.Message, &e.MetadataJSON, &e.CreatedAt)
	return e, err
}

func (s *Store) ListEventsByRun(runID int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, run_id, level, message, metadata_json, created_at FROM events WHERE run_id = ? ORDER BY created_at DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.RunID, &e.Level, &e.Message, &e.MetadataJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Checks

func (s *Store) CreateCheck(runID int64, kind, status, summary, detailsJSON string) (*Check, error) {
	c := &Check{}
	err := s.db.QueryRow(
		`INSERT INTO checks (run_id, kind, status, summary, details_json) VALUES (?, ?, ?, ?, ?) RETURNING id, run_id, kind, status, summary, details_json, created_at`,
		runID, kind, status, summary, detailsJSON,
	).Scan(&c.ID, &c.RunID, &c.Kind, &c.Status, &c.Summary, &c.DetailsJSON, &c.CreatedAt)
	return c, err
}

func (s *Store) ListChecksByRun(runID int64) ([]Check, error) {
	rows, err := s.db.Query(`SELECT id, run_id, kind, status, summary, details_json, created_at FROM checks WHERE run_id = ? ORDER BY created_at DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checks []Check
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.ID, &c.RunID, &c.Kind, &c.Status, &c.Summary, &c.DetailsJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}

func (s *Store) DeleteChecksByRunKind(runID int64, kind string) error {
	_, err := s.db.Exec(`DELETE FROM checks WHERE run_id = ? AND kind = ?`, runID, kind)
	return err
}

func (s *Store) GetChecksByRunKind(runID int64, kind string) ([]Check, error) {
	rows, err := s.db.Query(`SELECT id, run_id, kind, status, summary, details_json, created_at FROM checks WHERE run_id = ? AND kind = ? ORDER BY created_at DESC`, runID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var checks []Check
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.ID, &c.RunID, &c.Kind, &c.Status, &c.Summary, &c.DetailsJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}
