package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"relay/internal/store/generated"

	_ "modernc.org/sqlite"
)

type Repo = generated.Repo
type Run = generated.Run
type Artifact = generated.Artifact
type Event = generated.Event
type Check = generated.Check
type DashboardRun = generated.ListRecentRunsWithRepoRow

type Store struct {
	db      *sql.DB
	queries *generated.Queries
	log     *slog.Logger
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

	return &Store{
		db:      db,
		queries: generated.New(db),
		log:     log,
	}, nil
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Close() error { return s.db.Close() }

// Repos

func (s *Store) CreateRepo(name, path string) (*Repo, error) {
	repo, err := s.queries.CreateRepo(context.Background(), generated.CreateRepoParams{
		Name:                      name,
		Path:                      path,
		DefaultValidationCommands: "[]",
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) GetRepo(id int64) (*Repo, error) {
	repo, err := s.queries.GetRepo(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) ListRepos() ([]Repo, error) {
	return s.queries.ListRepos(context.Background())
}

func (s *Store) GetRepoByName(name string) (*Repo, error) {
	repo, err := s.queries.GetRepoByName(context.Background(), name)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

// Runs

func (s *Store) CreateRun(repoID int64, title, status, recommendedModel, selectedModel, branchName string) (*Run, error) {
	run, err := s.queries.CreateRun(context.Background(), generated.CreateRunParams{
		RepoID:           repoID,
		Title:            title,
		Status:           status,
		RecommendedModel: recommendedModel,
		SelectedModel:    selectedModel,
		BranchName:       branchName,
		BaseCommit:       "",
		HeadCommit:       "",
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetRun(id int64) (*Run, error) {
	run, err := s.queries.GetRun(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) ListRecentRuns(limit int) ([]Run, error) {
	return s.queries.ListRecentRuns(context.Background(), int64(limit))
}

func (s *Store) ListRecentRunsWithRepo(limit int) ([]DashboardRun, error) {
	return s.queries.ListRecentRunsWithRepo(context.Background(), int64(limit))
}

func (s *Store) UpdateRunStatus(id int64, status string) (*Run, error) {
	run, err := s.queries.UpdateRunStatus(context.Background(), generated.UpdateRunStatusParams{
		Status: status,
		ID:     id,
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// Artifacts

func (s *Store) CreateArtifact(runID int64, kind, path, mimeType string) (*Artifact, error) {
	a, err := s.queries.CreateArtifact(context.Background(), generated.CreateArtifactParams{
		RunID:    runID,
		Kind:     kind,
		Path:     path,
		MimeType: mimeType,
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) GetArtifact(id int64) (*Artifact, error) {
	a, err := s.queries.GetArtifact(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ListArtifactsByRun(runID int64) ([]Artifact, error) {
	return s.queries.ListArtifactsByRun(context.Background(), runID)
}

// Events

func (s *Store) CreateEvent(runID int64, level, message string) (*Event, error) {
	e, err := s.queries.CreateEvent(context.Background(), generated.CreateEventParams{
		RunID:        runID,
		Level:        level,
		Message:      message,
		MetadataJson: "{}",
	})
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListEventsByRun(runID int64) ([]Event, error) {
	return s.queries.ListEventsByRun(context.Background(), runID)
}

// Checks

func (s *Store) CreateCheck(runID int64, kind, status, summary, detailsJSON string) (*Check, error) {
	c, err := s.queries.CreateCheck(context.Background(), generated.CreateCheckParams{
		RunID:       runID,
		Kind:        kind,
		Status:      status,
		Summary:     summary,
		DetailsJson: detailsJSON,
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListChecksByRun(runID int64) ([]Check, error) {
	return s.queries.ListChecksByRun(context.Background(), runID)
}

func (s *Store) DeleteChecksByRunKind(runID int64, kind string) error {
	return s.queries.DeleteChecksByRunKind(context.Background(), generated.DeleteChecksByRunKindParams{
		RunID: runID,
		Kind:  kind,
	})
}

func (s *Store) GetChecksByRunKind(runID int64, kind string) ([]Check, error) {
	return s.queries.GetChecksByRunKind(context.Background(), generated.GetChecksByRunKindParams{
		RunID: runID,
		Kind:  kind,
	})
}
