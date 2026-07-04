package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/events"
	"relay/internal/store"
	"relay/internal/store/generated"
)

// Service owns run read/use-case logic and run lifecycle workflow operations.
type Service struct {
	store           *store.Store
	log             *slog.Logger
	eventHub        *events.Hub
	ownerInstanceID string
}

// NewService constructs a run app service. eventHub is required for executor
// dispatch during ExecuteRun.
func NewService(st *store.Store, log *slog.Logger, eventHub *events.Hub) *Service {
	return &Service{store: st, log: log, eventHub: eventHub}
}

func (s *Service) SetExecutorOwnerInstanceID(ownerInstanceID string) {
	s.ownerInstanceID = ownerInstanceID
}

// ListRuns preserves GET /api/runs behavior: list the 100 most recent runs with
// repo names and load full run details for each.
func (s *Service) ListRuns(ctx context.Context, limit int) ([]RunDetails, error) {
	_ = ctx
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.store.ListRecentRunsWithRepo(limit)
	if err != nil {
		return nil, err
	}
	result := make([]RunDetails, 0, len(rows))
	for _, row := range rows {
		run := generated.Run{
			ID:               row.ID,
			RepoID:           row.RepoID,
			Title:            row.Title,
			Status:           row.Status,
			RecommendedModel: row.RecommendedModel,
			SelectedModel:    row.SelectedModel,
			BranchName:       row.BranchName,
			BaseCommit:       row.BaseCommit,
			HeadCommit:       row.HeadCommit,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		}
		result = append(result, s.loadRunDetails(run, row.RepoName))
	}
	return result, nil
}

// GetRun preserves GET /api/runs/{id} behavior.
func (s *Service) GetRun(ctx context.Context, id int64) (RunDetails, error) {
	_ = ctx
	run, err := s.store.GetRun(id)
	if err != nil {
		return RunDetails{}, err
	}
	repoName := "Unknown Repo"
	if repo, err := s.store.GetRepo(run.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}
	return s.loadRunDetails(*run, repoName), nil
}

// ListEvents returns events for a run.
func (s *Service) ListEvents(ctx context.Context, runID int64) ([]store.Event, error) {
	_ = ctx
	return s.store.ListEventsByRun(runID)
}

// RunExists reports whether a run exists.
func (s *Service) RunExists(ctx context.Context, runID int64) (bool, error) {
	_ = ctx
	if _, err := s.store.GetRun(runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListRunArtifactViews validates the run exists and returns artifact views.
func (s *Service) ListRunArtifactViews(ctx context.Context, runID int64) ([]ArtifactView, error) {
	_ = ctx
	if _, err := s.store.GetRun(runID); err != nil {
		return nil, err
	}
	arts, err := s.store.ListArtifactsByRun(runID)
	if err != nil {
		return nil, err
	}
	return s.buildArtifactViews(arts), nil
}

// ErrArtifactNotFound indicates no artifact exists for the requested run/kind.
var ErrArtifactNotFound = errors.New("artifact not found")

// GetRunArtifactContent preserves GET /api/runs/{id}/artifacts/{kind} behavior:
// select the last artifact of the kind and read its bytes.
func (s *Service) GetRunArtifactContent(ctx context.Context, runID int64, kind string) (ArtifactContent, error) {
	_ = ctx
	arts, err := s.store.ListArtifactsByRunKind(runID, kind)
	if err != nil || len(arts) == 0 {
		return ArtifactContent{}, ErrArtifactNotFound
	}
	art := arts[len(arts)-1]
	data, err := os.ReadFile(art.Path)
	if err != nil {
		return ArtifactContent{}, err
	}
	return ArtifactContent{Data: data}, nil
}

// loadRunDetails loads all run-associated data for presentation.
func (s *Service) loadRunDetails(run store.Run, repoName string) RunDetails {
	arts, _ := s.store.ListArtifactsByRun(run.ID)
	checks, _ := s.store.ListChecksByRun(run.ID)
	evts, _ := s.store.ListEventsByRun(run.ID)
	latestExec, _ := s.store.GetLatestAgentExecutionByRun(run.ID)

	worktree := ""
	for _, art := range arts {
		if art.Kind == "run_config" {
			if data, err := os.ReadFile(art.Path); err == nil {
				var cfg map[string]interface{}
				if err := json.Unmarshal(data, &cfg); err == nil {
					if wt, ok := cfg["worktree"].(string); ok {
						worktree = wt
					}
				}
			}
		}
	}

	details := RunDetails{
		Run:             run,
		RepoName:        repoName,
		ArtifactViews:   s.buildArtifactViews(arts),
		Checks:          checks,
		Events:          evts,
		LatestExecution: latestExec,
		Worktree:        worktree,
	}

	if run.PlanRowID.Valid {
		if plan, err := s.store.GetPlan(run.PlanRowID.Int64); err == nil && plan != nil {
			details.Plan = plan
		}
	}
	if run.PlanPassRowID.Valid {
		if pass, err := s.store.GetPlanPass(run.PlanPassRowID.Int64); err == nil && pass != nil {
			details.Pass = pass
			if plan, err := s.store.GetPlan(pass.PlanRowID); err == nil && plan != nil {
				details.PassPlan = plan
			}
		}
	}

	if row, err := s.store.GetRunSubmissionProvenanceByRun(run.ID); err == nil && row != nil {
		details.Provenance = row
		if row.ContextPacketID != "" {
			if packet, err := s.store.GetContextPacketByID(row.ContextPacketID); err == nil && packet != nil {
				details.ContextPacket = packet
			}
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Warn("failed to load run provenance", slog.Int64("run_id", run.ID), slog.String("error", err.Error()))
	}

	return details
}

// buildArtifactViews computes size hint and preview for each artifact.
func (s *Service) buildArtifactViews(arts []store.Artifact) []ArtifactView {
	views := make([]ArtifactView, 0, len(arts))
	for _, art := range arts {
		views = append(views, ArtifactView{
			ID:        art.ID,
			Kind:      art.Kind,
			Path:      art.Path,
			CreatedAt: art.CreatedAt,
			SizeHint:  fileSizeHint(art.Path),
			Preview:   artifactPreview(art.MimeType, art.Path),
		})
	}
	return views
}

func fileSizeHint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	size := info.Size()
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func artifactPreview(mimeType, path string) string {
	if mimeType != "text/plain" && mimeType != "application/json" && mimeType != "text/markdown" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 500 {
		return string(data[:500]) + "..."
	}
	return string(data)
}

// resolveRepo finds or creates a repo by name or path, mirroring the legacy
// intake/api repo resolution behavior.
func (s *Service) resolveRepo(repoNameOrPath string) (*store.Repo, error) {
	if repoNameOrPath == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if repo, err := s.store.GetRepoByName(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	if repo, err := s.store.GetRepoByPath(repoNameOrPath); err == nil && repo != nil {
		return repo, nil
	}
	baseName := filepath.Base(repoNameOrPath)
	if repo, err := s.store.GetRepoByName(baseName); err == nil && repo != nil {
		return repo, nil
	}
	normalized := filepath.Clean(repoNameOrPath)
	if repo, err := s.store.GetRepoByPath(normalized); err == nil && repo != nil {
		return repo, nil
	}
	if repos, err := s.store.ListRepos(); err == nil {
		for _, r := range repos {
			if strings.EqualFold(r.Name, repoNameOrPath) || strings.EqualFold(r.Name, baseName) {
				rCopy := r
				return &rCopy, nil
			}
		}
	}
	return s.store.CreateRepo(baseName, repoNameOrPath)
}
