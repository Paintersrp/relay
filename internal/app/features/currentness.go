package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

type FeatureCurrentnessReadiness string

const (
	FeatureCurrent = FeatureCurrentnessReadiness("current")
	FeatureStale   = FeatureCurrentnessReadiness("stale")
	FeatureLegacy  = FeatureCurrentnessReadiness("legacy_insufficient")
)

type FeatureCurrentnessDecision struct {
	Readiness              FeatureCurrentnessReadiness
	StaleOwner             string
	HistoricalIdentity     string
	BlockedOperation       string
	Effect                 string
	RecoveryCategory       string
	WorkspaceID            string
	WorkspaceVersion       int64
	ClosurePacketRowID     sql.NullInt64
	ClosureRevisionRowID   sql.NullInt64
	AuthorityRevisionRowID sql.NullInt64
	CandidateID            string
	Basis                  string
}

// CurrentnessReader is the read surface required by the Feature currentness
// contract. Store and Tx both implement it so downstream owner boundaries can
// recheck the same semantics atomically.
type CurrentnessReader interface {
	GetFeatureWorkspaceByWorkspaceID(context.Context, string) (workflowstore.FeatureWorkspace, error)
	GetDiscoveryLifecycleAdoption(context.Context, int64) (workflowstore.DiscoveryLifecycleAdoption, error)
	GetDiscoveryClosurePacketByRowID(context.Context, int64) (workflowstore.DiscoveryClosurePacket, error)
	GetFeatureWorkspaceAuthorityRevisionByRowID(context.Context, int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error)
	GetSourceVaultClosureByRowID(context.Context, int64) (workflowstore.SourceVaultClosure, error)
}

func currentnessDecision(workspaceID string, workspaceVersion int64, readiness FeatureCurrentnessReadiness) FeatureCurrentnessDecision {
	return FeatureCurrentnessDecision{Readiness: readiness, WorkspaceID: workspaceID, WorkspaceVersion: workspaceVersion}
}

func blockCurrentness(decision *FeatureCurrentnessDecision, owner, operation, effect, recovery, basis, historical string) {
	decision.Readiness = FeatureStale
	decision.StaleOwner = owner
	decision.BlockedOperation = operation
	decision.Effect = effect
	decision.RecoveryCategory = recovery
	decision.Basis = basis
	decision.HistoricalIdentity = historical
}

// EvaluateCurrentness is the sole Feature semantic decision point for
// progression currentness. It is deliberately read-only.
func (s *Service) EvaluateCurrentness(ctx context.Context, workspaceID string) (FeatureCurrentnessDecision, error) {
	if s == nil || s.store == nil {
		return FeatureCurrentnessDecision{}, ErrInvalidAuthorityRequest
	}
	return EvaluateCurrentness(ctx, s.store, workspaceID)
}

// EvaluateCurrentness evaluates the Feature-owned contract against either the
// Store or the caller's transaction. It never creates or changes lifecycle
// state; callers decide how to map a blocked decision to their domain error.
func EvaluateCurrentness(ctx context.Context, reader CurrentnessReader, workspaceID string) (FeatureCurrentnessDecision, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if reader == nil || workspaceID == "" {
		return FeatureCurrentnessDecision{}, ErrInvalidAuthorityRequest
	}
	workspace, err := reader.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return FeatureCurrentnessDecision{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return FeatureCurrentnessDecision{}, err
	}
	decision := currentnessDecision(workspace.WorkspaceID, workspace.Version, FeatureCurrent)
	decision.ClosurePacketRowID = workspace.CurrentDiscoveryClosurePacketRowID
	decision.ClosureRevisionRowID = workspace.CurrentDiscoveryRevisionRowID
	decision.AuthorityRevisionRowID = workspace.CurrentAuthorityRevisionRowID

	if _, adoptionErr := reader.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); errors.Is(adoptionErr, sql.ErrNoRows) {
		decision.Readiness = FeatureLegacy
		decision.StaleOwner = "legacy_workspace"
		decision.BlockedOperation = "progression"
		decision.Effect = "no closure, candidate, approval, or authority may authorize new work"
		decision.RecoveryCategory = "adopt_discovery_lifecycle"
		decision.HistoricalIdentity = fmt.Sprintf("workspace:%s/version:%d", workspace.WorkspaceID, workspace.Version)
		decision.Basis = "legacy workspace without discovery adoption"
		return decision, nil
	} else if adoptionErr != nil {
		return FeatureCurrentnessDecision{}, adoptionErr
	}

	if !workspace.CurrentDiscoveryRevisionRowID.Valid || !workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		blockCurrentness(&decision, "discovery_closure", "candidate_admission|approval|promotion|package|run|audit", "new progression is blocked", "close_current_discovery", "current closure packet and revision are incomplete", "closure:none")
		return decision, nil
	}
	packet, err := reader.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil || packet.WorkspaceRowID != workspace.ID || packet.ClosingRevisionRowID != workspace.CurrentDiscoveryRevisionRowID.Int64 {
		blockCurrentness(&decision, "discovery_closure", "candidate_admission|approval|promotion|package|run|audit", "new progression is blocked", "replace_current_closure", "closure packet does not match current discovery revision", fmt.Sprintf("closure-packet:%d/revision:%d", workspace.CurrentDiscoveryClosurePacketRowID.Int64, workspace.CurrentDiscoveryRevisionRowID.Int64))
		return decision, nil
	}
	decision.HistoricalIdentity = fmt.Sprintf("closure-packet:%d/revision:%d", packet.ID, packet.ClosingRevisionRowID)
	decision.Basis = fmt.Sprintf("current closure packet %d binds revision %d", packet.ID, packet.ClosingRevisionRowID)

	if !workspace.CurrentAuthorityRevisionRowID.Valid {
		// No authority is valid for the first planning candidate in a workspace,
		// but downstream package/run owners still require an authority revision.
		return decision, nil
	}
	authority, err := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid {
		blockCurrentness(&decision, "authority", "candidate_admission|approval|promotion|package|run|audit", "new progression is blocked", "publish_current_authority", "current authority pointer is invalid", fmt.Sprintf("closure-packet:%d/revision:%d/authority:%d", packet.ID, packet.ClosingRevisionRowID, workspace.CurrentAuthorityRevisionRowID.Int64))
		return decision, nil
	}
	closure, err := reader.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady {
		blockCurrentness(&decision, "source_closure", "candidate_admission|approval|promotion|package|run|audit", "new progression is blocked", "restore_current_source_closure", "current authority source closure is not ready", fmt.Sprintf("closure-packet:%d/revision:%d/authority:%d/source:%d", packet.ID, packet.ClosingRevisionRowID, authority.ID, authority.SourceClosureRowID.Int64))
		return decision, nil
	}
	decision.HistoricalIdentity = fmt.Sprintf("closure-packet:%d/revision:%d/authority:%d/source:%d", packet.ID, packet.ClosingRevisionRowID, authority.ID, closure.ID)
	decision.Basis = fmt.Sprintf("current closure packet %d, authority revision %d, and ready source closure %d match workspace pointers", packet.ID, authority.ID, closure.ID)
	return decision, nil
}

func (s *Service) Currentness(ctx context.Context, workspaceID string) (FeatureCurrentnessDecision, error) {
	return s.EvaluateCurrentness(ctx, workspaceID)
}
