package operations

import (
	"context"
	"database/sql"

	featureapp "relay/internal/app/features"
)

// CurrentnessProjection is an operation-facing read model of the Feature
// decision. The operation owner exposes it but does not calculate or mutate
// Feature semantics.
type CurrentnessProjection struct {
	Readiness              featureapp.FeatureCurrentnessReadiness `json:"readiness"`
	StaleOwner             string                                 `json:"staleOwner,omitempty"`
	HistoricalIdentity     string                                 `json:"historicalIdentity,omitempty"`
	BlockedOperation       string                                 `json:"blockedOperation,omitempty"`
	Effect                 string                                 `json:"effect,omitempty"`
	RecoveryCategory       string                                 `json:"recoveryCategory,omitempty"`
	WorkspaceID            string                                 `json:"workspaceId"`
	WorkspaceVersion       int64                                  `json:"workspaceVersion"`
	ClosurePacketRowID     *int64                                 `json:"closurePacketRowId,omitempty"`
	ClosureRevisionRowID   *int64                                 `json:"closureRevisionRowId,omitempty"`
	AuthorityRevisionRowID *int64                                 `json:"authorityRevisionRowId,omitempty"`
	CandidateID            string                                 `json:"candidateId,omitempty"`
	Basis                  string                                 `json:"basis,omitempty"`
}

func projectCurrentness(value featureapp.FeatureCurrentnessDecision) CurrentnessProjection {
	return CurrentnessProjection{
		Readiness:              value.Readiness,
		StaleOwner:             value.StaleOwner,
		HistoricalIdentity:     value.HistoricalIdentity,
		BlockedOperation:       value.BlockedOperation,
		Effect:                 value.Effect,
		RecoveryCategory:       value.RecoveryCategory,
		WorkspaceID:            value.WorkspaceID,
		WorkspaceVersion:       value.WorkspaceVersion,
		ClosurePacketRowID:     nullableInt64Pointer(value.ClosurePacketRowID),
		ClosureRevisionRowID:   nullableInt64Pointer(value.ClosureRevisionRowID),
		AuthorityRevisionRowID: nullableInt64Pointer(value.AuthorityRevisionRowID),
		CandidateID:            value.CandidateID,
		Basis:                  value.Basis,
	}
}

func nullableInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copy := value.Int64
	return &copy
}

func (s *PackageWorkflowService) packageCurrentness(ctx context.Context, workspaceRowID int64) (*CurrentnessProjection, error) {
	if workspaceRowID < 1 {
		return nil, nil
	}
	workspace, err := s.store.GetFeatureWorkspaceByRowID(ctx, workspaceRowID)
	if err != nil {
		return nil, err
	}
	decision, err := featureapp.EvaluateCurrentness(ctx, s.store, workspace.WorkspaceID)
	if err != nil {
		return nil, err
	}
	projection := projectCurrentness(decision)
	return &projection, nil
}
