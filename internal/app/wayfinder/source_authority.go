// Package wayfinder owns application-level feature investigation state.
package wayfinder

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidInvestigation       = errors.New("invalid investigation closure request")
	ErrStaleSourceBase            = errors.New("source base is stale")
	ErrRetainedClosureUnavailable = errors.New("retained investigation closure is unavailable")
)

// SourceAuthority is the bounded source-retention contract for investigations.
// It deliberately exposes only durable identities, never vault paths or object
// browsing capabilities.
type SourceAuthority interface {
	CreateInvestigationClosure(context.Context, CreateInvestigationClosureInput) (RetainedClosureIdentity, error)
	ReplaceInvestigationClosure(context.Context, RetainedClosureIdentity, CreateInvestigationClosureInput) (RetainedClosureIdentity, error)
	ReadInvestigationClosure(context.Context, RetainedClosureIdentity) (RetainedClosureIdentity, error)
	ReleaseInvestigationClosure(context.Context, RetainedClosureIdentity) error
}

type InvestigationArtifactReference struct {
	ArtifactRowID         sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	SHA256                string
}

type CreateInvestigationClosureInput struct {
	InvestigationID string
	WorkspaceRowID  int64
	TicketRowID     sql.NullInt64
	Sequence        int64
	Artifact        InvestigationArtifactReference
	SourceBase      workflowrepos.ResolvedRevision
}

// RetainedClosureIdentity is sufficient to bind later application work to the
// exact commit/tree source authority. It is intentionally free of vault IDs,
// vault paths, and raw-object access.
type RetainedClosureIdentity struct {
	InvestigationID  string
	RetentionID      string
	ClosureID        string
	RepositoryTarget string
	CommitOID        string
	TreeOID          string
}

type sourceAuthorityVault interface {
	ImportClosure(context.Context, sourcevault.ImportRequest) (sourcevault.ImportResult, error)
	PrepareInvestigationRetention(context.Context, string, string) (sourcevault.PreparedInvestigationRetention, error)
	RetainPreparedInvestigationInTx(context.Context, *workflowstore.Tx, sourcevault.PreparedInvestigationRetention) (workflowstore.SourceVaultRetention, error)
}

type sourceAuthority struct {
	store *workflowstore.Store
	vault sourceAuthorityVault
}

func NewSourceAuthority(store *workflowstore.Store, vault *sourcevault.Manager) (SourceAuthority, error) {
	return newSourceAuthority(store, vault)
}

func newSourceAuthority(store *workflowstore.Store, vault sourceAuthorityVault) (*sourceAuthority, error) {
	if store == nil || vault == nil {
		return nil, ErrInvalidInvestigation
	}
	return &sourceAuthority{store: store, vault: vault}, nil
}

func (s *sourceAuthority) CreateInvestigationClosure(ctx context.Context, input CreateInvestigationClosureInput) (RetainedClosureIdentity, error) {
	prepared, err := s.prepare(ctx, input)
	if err != nil {
		return RetainedClosureIdentity{}, err
	}
	return s.persist(ctx, input, prepared, RetainedClosureIdentity{})
}

func (s *sourceAuthority) ReplaceInvestigationClosure(ctx context.Context, previous RetainedClosureIdentity, input CreateInvestigationClosureInput) (RetainedClosureIdentity, error) {
	if !validRetainedClosureIdentity(previous) || input.InvestigationID == previous.InvestigationID {
		return RetainedClosureIdentity{}, ErrInvalidInvestigation
	}
	if _, err := s.ReadInvestigationClosure(ctx, previous); err != nil {
		return RetainedClosureIdentity{}, err
	}
	prepared, err := s.prepare(ctx, input)
	if err != nil {
		return RetainedClosureIdentity{}, err
	}
	return s.persist(ctx, input, prepared, previous)
}

func (s *sourceAuthority) ReadInvestigationClosure(ctx context.Context, identity RetainedClosureIdentity) (RetainedClosureIdentity, error) {
	if !validRetainedClosureIdentity(identity) {
		return RetainedClosureIdentity{}, ErrInvalidInvestigation
	}
	investigation, err := s.store.GetFeatureWorkspaceInvestigationByID(ctx, identity.InvestigationID)
	if errors.Is(err, sql.ErrNoRows) {
		return RetainedClosureIdentity{}, ErrRetainedClosureUnavailable
	}
	if err != nil || !investigation.SourceClosureRowID.Valid {
		return RetainedClosureIdentity{}, unavailable(err)
	}
	retention, err := s.store.GetSourceVaultRetentionByRetentionID(ctx, identity.RetentionID)
	if err != nil || retention.State != workflowstore.SourceVaultRetentionStateActive || retention.OwnerClass != workflowstore.SourceVaultOwnerArtifact || retention.OwnerIdentity != identity.InvestigationID || retention.ClosureRowID != investigation.SourceClosureRowID.Int64 {
		return RetainedClosureIdentity{}, unavailable(err)
	}
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, investigation.SourceClosureRowID.Int64)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady || closure.ClosureID != identity.ClosureID {
		return RetainedClosureIdentity{}, unavailable(err)
	}
	if closure.CommitOID != identity.CommitOID || closure.TreeOID != identity.TreeOID {
		return RetainedClosureIdentity{}, ErrStaleSourceBase
	}
	vault, err := s.store.GetSourceVaultByRowID(ctx, closure.VaultRowID)
	if err != nil {
		return RetainedClosureIdentity{}, unavailable(err)
	}
	if vault.RepoTarget != identity.RepositoryTarget {
		return RetainedClosureIdentity{}, ErrStaleSourceBase
	}
	return identity, nil
}

func (s *sourceAuthority) ReleaseInvestigationClosure(ctx context.Context, identity RetainedClosureIdentity) error {
	if _, err := s.ReadInvestigationClosure(ctx, identity); err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		retention, err := tx.GetSourceVaultRetentionByRetentionID(ctx, identity.RetentionID)
		if err != nil || retention.State != workflowstore.SourceVaultRetentionStateActive || retention.OwnerClass != workflowstore.SourceVaultOwnerArtifact || retention.OwnerIdentity != identity.InvestigationID {
			return ErrRetainedClosureUnavailable
		}
		_, err = tx.ReleaseSourceVaultRetention(ctx, workflowstore.ReleaseSourceVaultRetentionParams{
			RetentionID: identity.RetentionID,
			ReleasedAt:  canonicalTime(),
		})
		return err
	})
}

func (s *sourceAuthority) prepare(ctx context.Context, input CreateInvestigationClosureInput) (sourcevault.PreparedInvestigationRetention, error) {
	if !validCreateInvestigationInput(input) {
		return sourcevault.PreparedInvestigationRetention{}, ErrInvalidInvestigation
	}
	imported, err := s.vault.ImportClosure(ctx, sourcevault.ImportRequest{Revision: input.SourceBase})
	if err != nil {
		if sourcevault.ErrorCode(err) == sourcevault.CodeStaleConfiguredAuthority {
			return sourcevault.PreparedInvestigationRetention{}, ErrStaleSourceBase
		}
		return sourcevault.PreparedInvestigationRetention{}, fmt.Errorf("retain investigation source authority: %w", err)
	}
	prepared, err := s.vault.PrepareInvestigationRetention(ctx, imported.Closure.ClosureID, input.InvestigationID)
	if err != nil {
		return sourcevault.PreparedInvestigationRetention{}, fmt.Errorf("prepare investigation source authority: %w", err)
	}
	return prepared, nil
}

func (s *sourceAuthority) persist(ctx context.Context, input CreateInvestigationClosureInput, prepared sourcevault.PreparedInvestigationRetention, previous RetainedClosureIdentity) (RetainedClosureIdentity, error) {
	var result RetainedClosureIdentity
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if previous.InvestigationID != "" {
			current, err := tx.GetSourceVaultRetentionByRetentionID(ctx, previous.RetentionID)
			if err != nil || current.State != workflowstore.SourceVaultRetentionStateActive || current.OwnerClass != workflowstore.SourceVaultOwnerArtifact || current.OwnerIdentity != previous.InvestigationID {
				return ErrRetainedClosureUnavailable
			}
			if _, err := tx.ReleaseSourceVaultRetention(ctx, workflowstore.ReleaseSourceVaultRetentionParams{RetentionID: previous.RetentionID, ReleasedAt: canonicalTime()}); err != nil {
				return err
			}
		}
		retention, err := s.vault.RetainPreparedInvestigationInTx(ctx, tx, prepared)
		if err != nil {
			return err
		}
		investigation, err := tx.CreateFeatureWorkspaceInvestigation(ctx, workflowstore.CreateFeatureWorkspaceInvestigationParams{
			InvestigationID:       input.InvestigationID,
			WorkspaceRowID:        input.WorkspaceRowID,
			TicketRowID:           input.TicketRowID,
			Sequence:              input.Sequence,
			InvestigationKind:     "source",
			ArtifactRowID:         input.Artifact.ArtifactRowID,
			RetainedArtifactRowID: input.Artifact.RetainedArtifactRowID,
			ArtifactSHA256:        input.Artifact.SHA256,
			SourceClosureRowID:    sql.NullInt64{Int64: prepared.Closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		if investigation.SourceClosureRowID.Int64 != prepared.Closure.ID {
			return ErrRetainedClosureUnavailable
		}
		result = RetainedClosureIdentity{
			InvestigationID:  input.InvestigationID,
			RetentionID:      retention.RetentionID,
			ClosureID:        prepared.Closure.ClosureID,
			RepositoryTarget: prepared.Vault.RepoTarget,
			CommitOID:        prepared.Closure.CommitOID,
			TreeOID:          prepared.Closure.TreeOID,
		}
		return nil
	})
	if err != nil {
		return RetainedClosureIdentity{}, err
	}
	return result, nil
}

func validCreateInvestigationInput(input CreateInvestigationClosureInput) bool {
	return strings.HasPrefix(input.InvestigationID, "investigation-") && strings.TrimSpace(input.InvestigationID) == input.InvestigationID && input.WorkspaceRowID > 0 && input.Sequence > 0 && validArtifact(input.Artifact) && validResolvedRevision(input.SourceBase)
}

func validRetainedClosureIdentity(value RetainedClosureIdentity) bool {
	return strings.HasPrefix(value.InvestigationID, "investigation-") && strings.TrimSpace(value.InvestigationID) == value.InvestigationID && strings.HasPrefix(value.RetentionID, "retention-") && strings.TrimSpace(value.RetentionID) == value.RetentionID && strings.HasPrefix(value.ClosureID, "closure-") && strings.TrimSpace(value.ClosureID) == value.ClosureID && value.RepositoryTarget != "" && strings.TrimSpace(value.RepositoryTarget) == value.RepositoryTarget && validOID(value.CommitOID) && validOID(value.TreeOID)
}

func validArtifact(value InvestigationArtifactReference) bool {
	return (value.ArtifactRowID.Valid != value.RetainedArtifactRowID.Valid) && validOID256(value.SHA256)
}

func validResolvedRevision(value workflowrepos.ResolvedRevision) bool {
	return value.RepositoryTarget.RepoTarget != "" && strings.TrimSpace(value.RepositoryTarget.RepoTarget) == value.RepositoryTarget.RepoTarget && validOID(value.CommitOID) && validOID(value.TreeOID)
}

func validOID(value string) bool {
	return len(value) == 40 && strings.ToLower(value) == value && validHex(value)
}

func validOID256(value string) bool {
	return len(value) == 64 && strings.ToLower(value) == value && validHex(value)
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func unavailable(err error) error {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return ErrRetainedClosureUnavailable
	}
	return fmt.Errorf("read retained investigation closure: %w", err)
}

func canonicalTime() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
