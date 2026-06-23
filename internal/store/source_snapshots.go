package store

import (
	"context"

	"relay/internal/store/generated"
)

type SourceSnapshot = generated.SourceSnapshot
type SourceSnapshotRepository = generated.SourceSnapshotRepository
type SourceSnapshotFile = generated.SourceSnapshotFile

type CreateSourceSnapshotParams struct {
	SourceSnapshotID string
	ProjectRowID     int64
	ProjectID        string
	SnapshotKind     string
	Status           string
	CompletedAt      string
	SummaryJSON      string
}

type UpdateSourceSnapshotStatusParams struct {
	SourceSnapshotID string
	SnapshotKind     string
	Status           string
	CompletedAt      string
	SummaryJSON      string
}

type CreateSourceSnapshotRepositoryParams struct {
	SourceSnapshotRowID    int64
	ProjectRepositoryRowID int64
	RepoID                 string
	Role                   string
	LocalPath              string
	DefaultBranch          string
	CurrentBranch          string
	HeadSHA                string
	Dirty                  int64
	StagedCount            int64
	UnstagedCount          int64
	UntrackedCount         int64
	ChangedFileCount       int64
	GitStatusAvailable     int64
	GitError               string
	StatusPorcelainHash    string
}

type CreateSourceSnapshotFileParams struct {
	SourceSnapshotRepositoryRowID int64
	Path                          string
	SizeBytes                     int64
	ContentHash                   string
	HashAlgorithm                 string
	Tracked                       int64
	Included                      int64
	ExclusionReason               string
	RedactionStatus               string
}

func (s *Store) CreateSourceSnapshot(params CreateSourceSnapshotParams) (*SourceSnapshot, error) {
	row, err := s.queries.CreateSourceSnapshot(context.Background(), generated.CreateSourceSnapshotParams{
		SourceSnapshotID: params.SourceSnapshotID,
		ProjectRowID:     params.ProjectRowID,
		ProjectID:        params.ProjectID,
		SnapshotKind:     params.SnapshotKind,
		Status:           params.Status,
		CompletedAt:      params.CompletedAt,
		SummaryJson:      params.SummaryJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetSourceSnapshotByID(sourceSnapshotID string) (*SourceSnapshot, error) {
	row, err := s.queries.GetSourceSnapshotByID(context.Background(), sourceSnapshotID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListSourceSnapshotsByProject(projectRowID int64) ([]SourceSnapshot, error) {
	return s.queries.ListSourceSnapshotsByProject(context.Background(), projectRowID)
}

func (s *Store) UpdateSourceSnapshotStatus(params UpdateSourceSnapshotStatusParams) (*SourceSnapshot, error) {
	row, err := s.queries.UpdateSourceSnapshotStatus(context.Background(), generated.UpdateSourceSnapshotStatusParams{
		SourceSnapshotID: params.SourceSnapshotID,
		SnapshotKind:     params.SnapshotKind,
		Status:           params.Status,
		CompletedAt:      params.CompletedAt,
		SummaryJson:      params.SummaryJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetLatestSourceSnapshotForProject(projectRowID int64) (*SourceSnapshot, error) {
	row, err := s.queries.GetLatestSourceSnapshotForProject(context.Background(), projectRowID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) CreateSourceSnapshotRepository(params CreateSourceSnapshotRepositoryParams) (*SourceSnapshotRepository, error) {
	row, err := s.queries.CreateSourceSnapshotRepository(context.Background(), generated.CreateSourceSnapshotRepositoryParams{
		SourceSnapshotRowID:    params.SourceSnapshotRowID,
		ProjectRepositoryRowID: params.ProjectRepositoryRowID,
		RepoID:                 params.RepoID,
		Role:                   params.Role,
		LocalPath:              params.LocalPath,
		DefaultBranch:          params.DefaultBranch,
		CurrentBranch:          params.CurrentBranch,
		HeadSha:                params.HeadSHA,
		Dirty:                  params.Dirty,
		StagedCount:            params.StagedCount,
		UnstagedCount:          params.UnstagedCount,
		UntrackedCount:         params.UntrackedCount,
		ChangedFileCount:       params.ChangedFileCount,
		GitStatusAvailable:     params.GitStatusAvailable,
		GitError:               params.GitError,
		StatusPorcelainHash:    params.StatusPorcelainHash,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListSourceSnapshotRepositories(sourceSnapshotRowID int64) ([]SourceSnapshotRepository, error) {
	return s.queries.ListSourceSnapshotRepositories(context.Background(), sourceSnapshotRowID)
}

func (s *Store) CreateSourceSnapshotFile(params CreateSourceSnapshotFileParams) (*SourceSnapshotFile, error) {
	row, err := s.queries.CreateSourceSnapshotFile(context.Background(), generated.CreateSourceSnapshotFileParams{
		SourceSnapshotRepositoryRowID: params.SourceSnapshotRepositoryRowID,
		Path:                          params.Path,
		SizeBytes:                     params.SizeBytes,
		ContentHash:                   params.ContentHash,
		HashAlgorithm:                 params.HashAlgorithm,
		Tracked:                       params.Tracked,
		Included:                      params.Included,
		ExclusionReason:               params.ExclusionReason,
		RedactionStatus:               params.RedactionStatus,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListSourceSnapshotFiles(sourceSnapshotRepositoryRowID int64) ([]SourceSnapshotFile, error) {
	return s.queries.ListSourceSnapshotFiles(context.Background(), sourceSnapshotRepositoryRowID)
}
