package repos

import (
	"context"
	"log/slog"

	"relay/internal/store"
)

type ScanSummary struct {
	RootsScanned int
	ReposFound   int
	ReposSaved   int
	Warnings     []string
}

type repoStore interface {
	ListEnabledRepoRoots() ([]store.RepoRoot, error)
	TouchRepoRootScanned(id int64) (*store.RepoRoot, error)
	UpsertDiscoveredRepo(name, path string) (*store.Repo, error)
}

type Service struct {
	store repoStore
	log   *slog.Logger
}

func NewService(s *store.Store, log *slog.Logger) *Service {
	return &Service{store: s, log: log}
}

func (svc *Service) ScanEnabledRoots(ctx context.Context) ScanSummary {
	summary := ScanSummary{}

	roots, err := svc.store.ListEnabledRepoRoots()
	if err != nil {
		svc.log.Error("list enabled repo roots", "error", err)
		summary.Warnings = append(summary.Warnings, "failed to list roots: "+err.Error())
		return summary
	}

	for _, root := range roots {
		summary.RootsScanned++
		scanResult := Discover(root.Path, 3)
		summary.ReposFound += len(scanResult.Repos)
		summary.Warnings = append(summary.Warnings, scanResult.Warnings...)

		for _, repo := range scanResult.Repos {
			_, err := svc.store.UpsertDiscoveredRepo(repo.Name, repo.Path)
			if err != nil {
				svc.log.Warn("upsert discovered repo", "name", repo.Name, "path", repo.Path, "error", err)
				summary.Warnings = append(summary.Warnings, "failed to save repo "+repo.Path+": "+err.Error())
			} else {
				summary.ReposSaved++
			}
		}

		if _, err := svc.store.TouchRepoRootScanned(root.ID); err != nil {
			svc.log.Warn("touch repo root scanned", "id", root.ID, "error", err)
		}
	}

	return summary
}
