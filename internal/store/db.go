package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	relaydb "relay/internal/db"
	"relay/internal/store/generated"

	_ "modernc.org/sqlite"
)

type Repo = generated.Repo
type RepoRoot = generated.RepoRoot
type Run = generated.Run
type Artifact = generated.Artifact
type Event = generated.Event
type Check = generated.Check
type AgentExecution = generated.AgentExecution
type ValidationExecution = generated.ValidationExecution
type DashboardRun = generated.ListRecentRunsWithRepoRow
type Plan = generated.Plan
type PlanPass = generated.PlanPass
type ProjectContextRecord = generated.ProjectContextRecord

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

	if err := relaydb.AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("auto-migrate: %w", err)
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

func (s *Store) GetRepoByPath(path string) (*Repo, error) {
	repo, err := s.queries.GetRepoByPath(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) UpsertDiscoveredRepo(name, path string) (*Repo, error) {
	repo, err := s.queries.UpsertDiscoveredRepo(context.Background(), generated.UpsertDiscoveredRepoParams{
		Name: name,
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (s *Store) ListReposByName() ([]Repo, error) {
	return s.queries.ListReposByName(context.Background())
}

// Repo Roots

func (s *Store) CreateRepoRoot(path string) (*RepoRoot, error) {
	root, err := s.queries.CreateRepoRoot(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &root, nil
}

func (s *Store) ListRepoRoots() ([]RepoRoot, error) {
	return s.queries.ListRepoRoots(context.Background())
}

func (s *Store) ListEnabledRepoRoots() ([]RepoRoot, error) {
	return s.queries.ListEnabledRepoRoots(context.Background())
}

func (s *Store) SetRepoRootEnabled(id int64, enabled bool) (*RepoRoot, error) {
	value := int64(0)
	if enabled {
		value = 1
	}
	root, err := s.queries.SetRepoRootEnabled(context.Background(), generated.SetRepoRootEnabledParams{
		ID:      id,
		Enabled: value,
	})
	if err != nil {
		return nil, err
	}
	return &root, nil
}

func (s *Store) DeleteRepoRoot(id int64) error {
	return s.queries.DeleteRepoRoot(context.Background(), id)
}

func (s *Store) TouchRepoRootScanned(id int64) (*RepoRoot, error) {
	root, err := s.queries.TouchRepoRootScanned(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &root, nil
}

func (s *Store) EnsureDefaultRepoRoots(paths []string) error {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := s.CreateRepoRoot(path); err != nil {
			return err
		}
	}
	return nil
}

// Runs

const DefaultExecutorAdapter = "opencode_go"

func (s *Store) CreateRun(repoID int64, title, status, recommendedModel, selectedModel, branchName string) (*Run, error) {
	return s.CreateRunWithExecutorAdapter(repoID, title, status, recommendedModel, selectedModel, DefaultExecutorAdapter, branchName)
}

func (s *Store) CreateRunWithExecutorAdapter(repoID int64, title, status, recommendedModel, selectedModel, executorAdapter, branchName string) (*Run, error) {
	return s.CreateRunWithAssociation(repoID, title, status, recommendedModel, selectedModel, executorAdapter, branchName, sql.NullInt64{}, sql.NullInt64{})
}

func (s *Store) CreateRunWithAssociation(
	repoID int64,
	title, status, recommendedModel, selectedModel, executorAdapter, branchName string,
	planRowID, planPassRowID sql.NullInt64,
) (*Run, error) {
	if executorAdapter == "" {
		executorAdapter = DefaultExecutorAdapter
	}
	run, err := s.queries.CreateRun(context.Background(), generated.CreateRunParams{
		RepoID:           repoID,
		Title:            title,
		Status:           status,
		RecommendedModel: recommendedModel,
		SelectedModel:    selectedModel,
		ExecutorAdapter:  executorAdapter,
		BranchName:       branchName,
		BaseCommit:       "",
		HeadCommit:       "",
		PlanRowID:        planRowID,
		PlanPassRowID:    planPassRowID,
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

func (s *Store) ListRunsByPlan(planRowID int64) ([]Run, error) {
	return s.queries.ListRunsByPlan(context.Background(), sql.NullInt64{Int64: planRowID, Valid: true})
}

func (s *Store) ListRunsByPlanPass(planPassRowID int64) ([]Run, error) {
	return s.queries.ListRunsByPlanPass(context.Background(), sql.NullInt64{Int64: planPassRowID, Valid: true})
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

func (s *Store) UpdateRunBranch(id int64, branchName, baseCommit, headCommit string) (*Run, error) {
	run, err := s.queries.UpdateRunBranch(context.Background(), generated.UpdateRunBranchParams{
		BranchName: branchName,
		BaseCommit: baseCommit,
		HeadCommit: headCommit,
		ID:         id,
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) UpdateRunModel(id int64, recommendedModel, selectedModel string) (*Run, error) {
	run, err := s.queries.UpdateRunModel(context.Background(), generated.UpdateRunModelParams{
		RecommendedModel: recommendedModel,
		SelectedModel:    selectedModel,
		ID:               id,
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) UpdateRunRepo(id int64, repoID int64) (*Run, error) {
	run, err := s.queries.UpdateRunRepo(context.Background(), generated.UpdateRunRepoParams{
		RepoID: repoID,
		ID:     id,
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) UpdateRunExecutorAdapter(id int64, executorAdapter string) (*Run, error) {
	if executorAdapter == "" {
		executorAdapter = DefaultExecutorAdapter
	}
	run, err := s.queries.UpdateRunExecutorAdapter(context.Background(), generated.UpdateRunExecutorAdapterParams{
		ExecutorAdapter: executorAdapter,
		ID:              id,
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) GetPlanByPlanID(planID string) (*Plan, error) {
	plan, err := s.queries.GetPlanByPlanID(context.Background(), planID)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Store) GetPlan(id int64) (*Plan, error) {
	plan, err := s.queries.GetPlan(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Store) ListPlans(limit int64) ([]Plan, error) {
	return s.queries.ListPlans(context.Background(), limit)
}

func (s *Store) ListPlansByStatus(status string, limit int64) ([]Plan, error) {
	return s.queries.ListPlansByStatus(context.Background(), generated.ListPlansByStatusParams{
		Status: status,
		Limit:  limit,
	})
}

func (s *Store) GetPlanPass(id int64) (*PlanPass, error) {
	pass, err := s.queries.GetPlanPass(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &pass, nil
}

func (s *Store) GetPlanPassByPassID(planRowID int64, passID string) (*PlanPass, error) {
	pass, err := s.queries.GetPlanPassByPassID(context.Background(), generated.GetPlanPassByPassIDParams{
		PlanRowID: planRowID,
		PassID:    passID,
	})
	if err != nil {
		return nil, err
	}
	return &pass, nil
}

func (s *Store) ListPlanPassesByPlan(planRowID int64) ([]PlanPass, error) {
	return s.queries.ListPlanPassesByPlan(context.Background(), planRowID)
}

func (s *Store) UpdatePlanPassStatus(id int64, status string) (*PlanPass, error) {
	pass, err := s.queries.UpdatePlanPassStatus(context.Background(), generated.UpdatePlanPassStatusParams{
		Status: status,
		ID:     id,
	})
	if err != nil {
		return nil, err
	}
	return &pass, nil
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

func (s *Store) DeleteArtifactsByRunKind(runID int64, kind string) error {
	return s.queries.DeleteArtifactsByRunKind(context.Background(), generated.DeleteArtifactsByRunKindParams{
		RunID: runID,
		Kind:  kind,
	})
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

func (s *Store) ListArtifactsByRunKind(runID int64, kind string) ([]Artifact, error) {
	return s.queries.ListArtifactsByRunKind(context.Background(), generated.ListArtifactsByRunKindParams{
		RunID: runID,
		Kind:  kind,
	})
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

// Agent Executions

func (s *Store) CreateAgentExecution(runID int64, provider, status, commandPreview string) (*AgentExecution, error) {
	exec, err := s.queries.CreateAgentExecution(context.Background(), generated.CreateAgentExecutionParams{
		RunID:          runID,
		Provider:       provider,
		Status:         status,
		CommandPreview: commandPreview,
	})
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *Store) GetAgentExecution(id int64) (*AgentExecution, error) {
	exec, err := s.queries.GetAgentExecution(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *Store) ListAgentExecutionsByRun(runID int64) ([]AgentExecution, error) {
	return s.queries.ListAgentExecutionsByRun(context.Background(), runID)
}

func (s *Store) GetLatestAgentExecutionByRun(runID int64) (*AgentExecution, error) {
	exec, err := s.queries.GetLatestAgentExecutionByRun(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *Store) UpdateAgentExecutionStatus(
	id int64,
	status string,
	exitCode *int64,
	startedAt, finishedAt *string,
	stdoutPath, stderrPath, combinedPath, resultPath *string,
	errMsg *string,
) (*AgentExecution, error) {
	exitCodeVal := sql.NullInt64{}
	if exitCode != nil {
		exitCodeVal = sql.NullInt64{Int64: *exitCode, Valid: true}
	}

	startedAtVal := sql.NullString{}
	if startedAt != nil {
		startedAtVal = sql.NullString{String: *startedAt, Valid: true}
	}

	finishedAtVal := sql.NullString{}
	if finishedAt != nil {
		finishedAtVal = sql.NullString{String: *finishedAt, Valid: true}
	}

	stdoutVal := sql.NullString{}
	if stdoutPath != nil {
		stdoutVal = sql.NullString{String: *stdoutPath, Valid: true}
	}

	stderrVal := sql.NullString{}
	if stderrPath != nil {
		stderrVal = sql.NullString{String: *stderrPath, Valid: true}
	}

	combinedVal := sql.NullString{}
	if combinedPath != nil {
		combinedVal = sql.NullString{String: *combinedPath, Valid: true}
	}

	resultVal := sql.NullString{}
	if resultPath != nil {
		resultVal = sql.NullString{String: *resultPath, Valid: true}
	}

	errorVal := sql.NullString{}
	if errMsg != nil {
		errorVal = sql.NullString{String: *errMsg, Valid: true}
	}

	exec, err := s.queries.UpdateAgentExecutionStatus(context.Background(), generated.UpdateAgentExecutionStatusParams{
		ID:                   id,
		Status:               status,
		ExitCode:             exitCodeVal,
		StartedAt:            startedAtVal,
		FinishedAt:           finishedAtVal,
		StdoutArtifactPath:   stdoutVal,
		StderrArtifactPath:   stderrVal,
		CombinedArtifactPath: combinedVal,
		ResultArtifactPath:   resultVal,
		Error:                errorVal,
	})
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

// Validation Executions

func (s *Store) TryCreateValidationExecution(runID int64) (int64, bool, error) {
	result, err := s.db.ExecContext(context.Background(),
		`INSERT INTO validation_executions (run_id, status, started_at, updated_at)
		 SELECT ?, 'starting', datetime('now'), datetime('now')
		 WHERE NOT EXISTS (
		     SELECT 1 FROM validation_executions
		     WHERE run_id = ? AND status IN ('starting', 'running')
		 )`, runID, runID)
	if err != nil {
		return 0, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if rows == 0 {
		return 0, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (s *Store) MarkValidationExecutionRunning(id int64) error {
	return s.queries.MarkValidationExecutionRunning(context.Background(), id)
}

func (s *Store) FinishValidationExecution(id int64, status string, errText string) error {
	errVal := sql.NullString{}
	if errText != "" {
		errVal = sql.NullString{String: errText, Valid: true}
	}
	return s.queries.UpdateValidationExecutionStatus(context.Background(), generated.UpdateValidationExecutionStatusParams{
		ID:     id,
		Status: status,
		Error:  errVal,
	})
}

func (s *Store) GetActiveValidationExecutionByRun(runID int64) (*ValidationExecution, error) {
	exec, err := s.queries.GetActiveValidationExecutionByRun(context.Background(), runID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &exec, nil
}

func (s *Store) MarkStaleValidationExecutionError(runID int64, cutoff time.Time) error {
	cutoffStr := cutoff.UTC().Format("2006-01-02 15:04:05")
	return s.queries.MarkStaleValidationExecutionsError(context.Background(), generated.MarkStaleValidationExecutionsErrorParams{
		RunID:     runID,
		UpdatedAt: cutoffStr,
		Error:     sql.NullString{String: "stale: execution exceeded time limit", Valid: true},
	})
}
