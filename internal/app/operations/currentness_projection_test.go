package operations

import (
	"database/sql"
	"testing"

	featureapp "relay/internal/app/features"
)

func TestProjectCurrentnessPreservesTypedBlockingState(t *testing.T) {
	decision := featureapp.FeatureCurrentnessDecision{
		Readiness:              featureapp.FeatureStale,
		StaleOwner:             "source_closure",
		HistoricalIdentity:     "closure-packet:7/revision:3/authority:4/source:9",
		BlockedOperation:       "package|run|audit",
		Effect:                 "new progression is blocked",
		RecoveryCategory:       "restore_current_source_closure",
		WorkspaceID:            "workspace-checkout",
		WorkspaceVersion:       8,
		ClosurePacketRowID:     sql.NullInt64{Int64: 7, Valid: true},
		ClosureRevisionRowID:   sql.NullInt64{Int64: 3, Valid: true},
		AuthorityRevisionRowID: sql.NullInt64{Int64: 4, Valid: true},
		CandidateID:            "candidate-1",
		Basis:                  "current authority source closure is not ready",
	}

	projection := projectCurrentness(decision)
	if projection.Readiness != featureapp.FeatureStale || projection.StaleOwner != decision.StaleOwner || projection.BlockedOperation != decision.BlockedOperation || projection.Effect != decision.Effect || projection.RecoveryCategory != decision.RecoveryCategory {
		t.Fatalf("currentness blocking projection = %#v", projection)
	}
	if projection.WorkspaceID != decision.WorkspaceID || projection.WorkspaceVersion != decision.WorkspaceVersion || projection.ClosurePacketRowID == nil || *projection.ClosurePacketRowID != 7 || projection.AuthorityRevisionRowID == nil || *projection.AuthorityRevisionRowID != 4 {
		t.Fatalf("currentness identity projection = %#v", projection)
	}
}
