package store

import (
	"context"

	"relay/internal/store/generated"
)

type LocalAudit = generated.LocalAudit

type CreateLocalAuditParams struct {
	AuditID          string
	ProjectRowID     int64
	ProjectID        string
	Mode             string
	Title            string
	Status           string
	PlanID           string
	PassID           string
	SourceSnapshotID string
	ContextPacketID  string
	ManifestPath     string
	PacketPath       string
	InputSummaryPath string
	BlockersJSON     string
	WarningsJSON     string
	CompletedAt      string
}

func (s *Store) CreateLocalAudit(params CreateLocalAuditParams) (*LocalAudit, error) {
	row, err := s.queries.CreateLocalAudit(context.Background(), generated.CreateLocalAuditParams{
		AuditID:          params.AuditID,
		ProjectRowID:     params.ProjectRowID,
		ProjectID:        params.ProjectID,
		Mode:             params.Mode,
		Title:            params.Title,
		Status:           params.Status,
		PlanID:           params.PlanID,
		PassID:           params.PassID,
		SourceSnapshotID: params.SourceSnapshotID,
		ContextPacketID:  params.ContextPacketID,
		ManifestPath:     params.ManifestPath,
		PacketPath:       params.PacketPath,
		InputSummaryPath: params.InputSummaryPath,
		BlockersJson:     params.BlockersJSON,
		WarningsJson:     params.WarningsJSON,
		CompletedAt:      params.CompletedAt,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetLocalAuditByAuditID(auditID string) (*LocalAudit, error) {
	row, err := s.queries.GetLocalAuditByAuditID(context.Background(), auditID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListLocalAuditsByProject(projectID string, limit int64) ([]LocalAudit, error) {
	return s.queries.ListLocalAuditsByProject(context.Background(), generated.ListLocalAuditsByProjectParams{
		ProjectID: projectID,
		Limit:     limit,
	})
}

func (s *Store) ListLocalAuditsByProjectAndMode(projectID string, mode string, limit int64) ([]LocalAudit, error) {
	return s.queries.ListLocalAuditsByProjectAndMode(context.Background(), generated.ListLocalAuditsByProjectAndModeParams{
		ProjectID: projectID,
		Mode:      mode,
		Limit:     limit,
	})
}
