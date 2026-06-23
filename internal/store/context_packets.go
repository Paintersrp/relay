package store

import (
	"context"

	"relay/internal/store/generated"
)

type ContextPacket = generated.ContextPacket
type ContextPacketSource = generated.ContextPacketSource

type CreateContextPacketParams struct {
	ContextPacketID    string
	ProjectID          string
	PlanID             string
	PassID             string
	TaskSlug           string
	SourceSnapshotID   string
	Status             string
	PacketJSONPath     string
	PacketMarkdownPath string
	CoverageReportPath string
	SourceCount        int64
	CoveredSeedCount   int64
	BlockedSeedCount   int64
	MissingSeedCount   int64
	Truncated          int64
	BlockersJSON       string
}

type CreateContextPacketSourceParams struct {
	ContextPacketRowID int64
	SourceID           string
	SourceType         string
	ProjectID          string
	RepoID             string
	SourceSnapshotID   string
	Path               string
	LineStart          int64
	LineEnd            int64
	ContentHash        string
	SnippetHash        string
	RedactionStatus    string
	Truncated          int64
	GeneratedAt        string
	Reason             string
}

func (s *Store) CreateContextPacket(params CreateContextPacketParams) (*ContextPacket, error) {
	row, err := s.queries.CreateContextPacket(context.Background(), generated.CreateContextPacketParams{
		ContextPacketID:    params.ContextPacketID,
		ProjectID:          params.ProjectID,
		PlanID:             params.PlanID,
		PassID:             params.PassID,
		TaskSlug:           params.TaskSlug,
		SourceSnapshotID:   params.SourceSnapshotID,
		Status:             params.Status,
		PacketJsonPath:     params.PacketJSONPath,
		PacketMarkdownPath: params.PacketMarkdownPath,
		CoverageReportPath: params.CoverageReportPath,
		SourceCount:        params.SourceCount,
		CoveredSeedCount:   params.CoveredSeedCount,
		BlockedSeedCount:   params.BlockedSeedCount,
		MissingSeedCount:   params.MissingSeedCount,
		Truncated:          params.Truncated,
		BlockersJson:       params.BlockersJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetContextPacketByID(contextPacketID string) (*ContextPacket, error) {
	row, err := s.queries.GetContextPacketByID(context.Background(), contextPacketID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListContextPacketsByProject(projectID string) ([]ContextPacket, error) {
	return s.queries.ListContextPacketsByProject(context.Background(), projectID)
}

func (s *Store) CreateContextPacketSource(params CreateContextPacketSourceParams) (*ContextPacketSource, error) {
	row, err := s.queries.CreateContextPacketSource(context.Background(), generated.CreateContextPacketSourceParams{
		ContextPacketRowID: params.ContextPacketRowID,
		SourceID:           params.SourceID,
		SourceType:         params.SourceType,
		ProjectID:          params.ProjectID,
		RepoID:             params.RepoID,
		SourceSnapshotID:   params.SourceSnapshotID,
		Path:               params.Path,
		LineStart:          params.LineStart,
		LineEnd:            params.LineEnd,
		ContentHash:        params.ContentHash,
		SnippetHash:        params.SnippetHash,
		RedactionStatus:    params.RedactionStatus,
		Truncated:          params.Truncated,
		GeneratedAt:        params.GeneratedAt,
		Reason:             params.Reason,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListContextPacketSources(contextPacketRowID int64) ([]ContextPacketSource, error) {
	return s.queries.ListContextPacketSources(context.Background(), contextPacketRowID)
}
