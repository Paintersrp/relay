package workflowstore

import (
	"context"
	"database/sql"

	workflowgenerated "relay/internal/store/workflowgenerated"
)

// FeatureWorkspaceInvestigation is immutable durable evidence for an
// investigation. Source authority is represented only by its closure row.
type FeatureWorkspaceInvestigation struct {
	ID                    int64
	InvestigationID       string
	WorkspaceRowID        int64
	TicketRowID           sql.NullInt64
	Sequence              int64
	InvestigationKind     string
	ArtifactRowID         sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	ArtifactSHA256        string
	SourceClosureRowID    sql.NullInt64
	CreatedAt             string
}

type CreateFeatureWorkspaceInvestigationParams struct {
	InvestigationID       string
	WorkspaceRowID        int64
	TicketRowID           sql.NullInt64
	Sequence              int64
	InvestigationKind     string
	ArtifactRowID         sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	ArtifactSHA256        string
	SourceClosureRowID    sql.NullInt64
}

func (s *Store) GetFeatureWorkspaceInvestigationByID(ctx context.Context, investigationID string) (FeatureWorkspaceInvestigation, error) {
	value, err := workflowgenerated.New(s.db).GetFeatureWorkspaceInvestigationByID(ctx, investigationID)
	return featureWorkspaceInvestigationFromGenerated(value), err
}

func (tx *Tx) CreateFeatureWorkspaceInvestigation(ctx context.Context, params CreateFeatureWorkspaceInvestigationParams) (FeatureWorkspaceInvestigation, error) {
	value, err := workflowgenerated.New(tx.tx).CreateFeatureWorkspaceInvestigation(ctx, workflowgenerated.CreateFeatureWorkspaceInvestigationParams{
		InvestigationID:       params.InvestigationID,
		WorkspaceRowID:        params.WorkspaceRowID,
		TicketRowID:           params.TicketRowID,
		Sequence:              params.Sequence,
		InvestigationKind:     params.InvestigationKind,
		ArtifactRowID:         params.ArtifactRowID,
		RetainedArtifactRowID: params.RetainedArtifactRowID,
		ArtifactSha256:        params.ArtifactSHA256,
		SourceClosureRowID:    params.SourceClosureRowID,
	})
	return featureWorkspaceInvestigationFromGenerated(value), err
}

func featureWorkspaceInvestigationFromGenerated(value workflowgenerated.FeatureWorkspaceInvestigation) FeatureWorkspaceInvestigation {
	return FeatureWorkspaceInvestigation{
		ID:                    value.ID,
		InvestigationID:       value.InvestigationID,
		WorkspaceRowID:        value.WorkspaceRowID,
		TicketRowID:           value.TicketRowID,
		Sequence:              value.Sequence,
		InvestigationKind:     value.InvestigationKind,
		ArtifactRowID:         value.ArtifactRowID,
		RetainedArtifactRowID: value.RetainedArtifactRowID,
		ArtifactSHA256:        value.ArtifactSha256,
		SourceClosureRowID:    value.SourceClosureRowID,
		CreatedAt:             value.CreatedAt,
	}
}
