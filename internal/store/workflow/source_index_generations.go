package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/sourceindex"
)

type SourceIndexGenerationState string

const (
	SourceIndexGenerationPending  SourceIndexGenerationState = "pending"
	SourceIndexGenerationBuilding SourceIndexGenerationState = "building"
	SourceIndexGenerationReady    SourceIndexGenerationState = "ready"
	SourceIndexGenerationFailed   SourceIndexGenerationState = "failed"
	SourceIndexGenerationRetired  SourceIndexGenerationState = "retired"
)

var (
	ErrInvalidSourceIndexGeneration           = errors.New("invalid source-index generation")
	ErrSourceIndexGenerationNotFound          = errors.New("source-index generation not found")
	ErrSourceIndexGenerationIntegrity         = errors.New("source-index generation integrity failure")
	ErrSourceIndexGenerationLifecycleConflict = errors.New("source-index generation lifecycle conflict")
)

type SourceIndexGeneration struct {
	ID                       int64
	GenerationID             string
	Identity                 sourceindex.GenerationIdentity
	State                    SourceIndexGenerationState
	AttemptCount             int64
	GenerationManifestSHA256 string
	CoverageManifestSHA256   string
	ArtifactManifestSHA256   string
	FailureCode              string
	FailureMessage           string
	CreatedAt                string
	UpdatedAt                string
	BuildingStartedAt        string
	ReadyAt                  string
	FailedAt                 string
	RetiredAt                string
}

// ActiveSourceIndexAuthority is the durable source authority needed to use a
// ready source-index generation. The closure is joined by the exact vault,
// commit, and tree identity and must have an active retention.
type ActiveSourceIndexAuthority struct {
	VaultRowID   int64
	VaultID      string
	ClosureRowID int64
	CommitOID    string
	TreeOID      string
}

type CreateOrResolveSourceIndexGenerationParams struct {
	Identity sourceindex.GenerationIdentity
}
type MarkSourceIndexGenerationReadyParams struct {
	GenerationID             string
	GenerationManifestSHA256 string
	CoverageManifestSHA256   string
	ArtifactManifestSHA256   string
}
type MarkSourceIndexGenerationFailedParams struct{ GenerationID, FailureCode, FailureMessage string }

const sourceIndexGenerationColumns = `id, generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state, attempt_count, generation_manifest_sha256, coverage_manifest_sha256, artifact_manifest_sha256, failure_code, failure_message, created_at, updated_at, building_started_at, ready_at, failed_at, retired_at`

func (s *Store) CreateOrResolveSourceIndexGeneration(ctx context.Context, params CreateOrResolveSourceIndexGenerationParams) (SourceIndexGeneration, bool, error) {
	if s == nil {
		return SourceIndexGeneration{}, false, ErrInvalidSourceIndexGeneration
	}
	id, err := sourceindex.GenerationID(params.Identity)
	if err != nil {
		return SourceIndexGeneration{}, false, fmt.Errorf("%w: %v", ErrInvalidSourceIndexGeneration, err)
	}
	value, err := s.getSourceIndexGenerationByExactIdentity(ctx, params.Identity)
	if err == nil {
		if value.GenerationID != id || value.Identity != params.Identity {
			return SourceIndexGeneration{}, false, fmt.Errorf("%w: existing identity differs", ErrSourceIndexGenerationIntegrity)
		}
		return value, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SourceIndexGeneration{}, false, err
	}
	value, err = scanSourceIndexGeneration(s.db.QueryRowContext(ctx, `INSERT INTO source_index_generations (generation_id, identity_version, vault_id, commit_oid, tree_oid, engine, engine_revision, build_contract_version, build_options_sha256, state) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending') ON CONFLICT(generation_id) DO NOTHING RETURNING `+sourceIndexGenerationColumns, id, params.Identity.Version, params.Identity.VaultID, params.Identity.CommitOID, params.Identity.TreeOID, params.Identity.Engine, params.Identity.EngineRevision, params.Identity.BuildContractVersion, params.Identity.BuildOptionsSHA256))
	if err == nil {
		return value, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		identityValue, identityErr := s.getSourceIndexGenerationByExactIdentity(ctx, params.Identity)
		if identityErr == nil {
			if identityValue.GenerationID != id || identityValue.Identity != params.Identity {
				return SourceIndexGeneration{}, false, fmt.Errorf("%w: existing identity differs", ErrSourceIndexGenerationIntegrity)
			}
			return identityValue, false, nil
		}
		if !errors.Is(identityErr, sql.ErrNoRows) {
			return SourceIndexGeneration{}, false, identityErr
		}
		return SourceIndexGeneration{}, false, err
	}
	value, err = s.GetSourceIndexGeneration(ctx, id)
	if err != nil {
		return SourceIndexGeneration{}, false, err
	}
	if value.Identity != params.Identity {
		return SourceIndexGeneration{}, false, fmt.Errorf("%w: existing identity differs", ErrSourceIndexGenerationIntegrity)
	}
	return value, false, nil
}

func (s *Store) GetSourceIndexGeneration(ctx context.Context, generationID string) (SourceIndexGeneration, error) {
	if s == nil || !validLowerHex(generationID, 64) {
		return SourceIndexGeneration{}, ErrInvalidSourceIndexGeneration
	}
	value, err := scanSourceIndexGeneration(s.db.QueryRowContext(ctx, `SELECT `+sourceIndexGenerationColumns+` FROM source_index_generations WHERE generation_id = ?`, generationID))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceIndexGeneration{}, fmt.Errorf("%w: %s", ErrSourceIndexGenerationNotFound, generationID)
	}
	return value, err
}

func (s *Store) GetSourceIndexGenerationByIdentity(ctx context.Context, identity sourceindex.GenerationIdentity) (SourceIndexGeneration, error) {
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		return SourceIndexGeneration{}, fmt.Errorf("%w: %v", ErrInvalidSourceIndexGeneration, err)
	}
	value, err := s.getSourceIndexGenerationByExactIdentity(ctx, identity)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceIndexGeneration{}, fmt.Errorf("%w: %s", ErrSourceIndexGenerationNotFound, id)
	}
	if err != nil {
		return SourceIndexGeneration{}, err
	}
	if value.GenerationID != id || value.Identity != identity {
		return SourceIndexGeneration{}, fmt.Errorf("%w: existing identity differs", ErrSourceIndexGenerationIntegrity)
	}
	return value, nil
}

func (s *Store) getSourceIndexGenerationByExactIdentity(ctx context.Context, identity sourceindex.GenerationIdentity) (SourceIndexGeneration, error) {
	return scanSourceIndexGeneration(s.db.QueryRowContext(ctx, `SELECT `+sourceIndexGenerationColumns+` FROM source_index_generations WHERE identity_version = ? AND vault_id = ? AND commit_oid = ? AND tree_oid = ? AND engine = ? AND engine_revision = ? AND build_contract_version = ? AND build_options_sha256 = ?`, identity.Version, identity.VaultID, identity.CommitOID, identity.TreeOID, identity.Engine, identity.EngineRevision, identity.BuildContractVersion, identity.BuildOptionsSHA256))
}

func (s *Store) ListSourceIndexGenerationsByState(ctx context.Context, state SourceIndexGenerationState) ([]SourceIndexGeneration, error) {
	if s == nil || !validSourceIndexGenerationState(state) {
		return nil, ErrInvalidSourceIndexGeneration
	}
	return s.listSourceIndexGenerations(ctx, ` WHERE state = ?`, state)
}

func (s *Store) ListSourceIndexGenerations(ctx context.Context) ([]SourceIndexGeneration, error) {
	if s == nil {
		return nil, ErrInvalidSourceIndexGeneration
	}
	return s.listSourceIndexGenerations(ctx, "")
}

func (s *Store) listSourceIndexGenerations(ctx context.Context, condition string, args ...any) ([]SourceIndexGeneration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sourceIndexGenerationColumns+` FROM source_index_generations`+condition+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]SourceIndexGeneration, 0)
	for rows.Next() {
		value, err := scanSourceIndexGeneration(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) ListActiveSourceIndexAuthorities(ctx context.Context) ([]ActiveSourceIndexAuthority, error) {
	if s == nil {
		return nil, ErrInvalidSourceIndexGeneration
	}
	rows, err := s.db.QueryContext(ctx, `SELECT v.id, v.vault_id, MIN(c.id), c.commit_oid, c.tree_oid
FROM source_vaults AS v
JOIN source_vault_closures AS c ON c.vault_row_id = v.id AND c.state = 'ready'
JOIN source_vault_retentions AS r ON r.closure_row_id = c.id AND r.state = 'active'
GROUP BY v.id, v.vault_id, c.commit_oid, c.tree_oid
ORDER BY v.vault_id, c.commit_oid, c.tree_oid, v.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ActiveSourceIndexAuthority, 0)
	for rows.Next() {
		var value ActiveSourceIndexAuthority
		if err := rows.Scan(&value.VaultRowID, &value.VaultID, &value.ClosureRowID, &value.CommitOID, &value.TreeOID); err != nil {
			return nil, err
		}
		if value.VaultRowID < 1 || value.ClosureRowID < 1 || strings.TrimSpace(value.VaultID) != value.VaultID || value.VaultID == "" || !validLowerHex(value.CommitOID, 40) || !validLowerHex(value.TreeOID, 40) {
			return nil, ErrSourceIndexGenerationIntegrity
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// IsSourceIndexAuthorityActive is the authoritative build eligibility check.
// It deliberately validates both the requested identity and the persisted rows
// so malformed durable state fails closed.
func (s *Store) IsSourceIndexAuthorityActive(ctx context.Context, identity sourceindex.GenerationIdentity) (bool, error) {
	if s == nil {
		return false, ErrInvalidSourceIndexGeneration
	}
	if _, err := sourceindex.GenerationID(identity); err != nil {
		return false, fmt.Errorf("%w: identity", ErrInvalidSourceIndexGeneration)
	}
	var vaultRowID, closureRowID int64
	var vaultID, commitOID, treeOID string
	err := s.db.QueryRowContext(ctx, `SELECT v.id, v.vault_id, c.id, c.commit_oid, c.tree_oid
FROM source_vaults AS v
JOIN source_vault_closures AS c ON c.vault_row_id = v.id AND c.state = 'ready'
JOIN source_vault_retentions AS r ON r.closure_row_id = c.id AND r.state = 'active'
WHERE v.vault_id = ? AND c.commit_oid = ? AND c.tree_oid = ?
ORDER BY c.id LIMIT 1`, identity.VaultID, identity.CommitOID, identity.TreeOID).Scan(&vaultRowID, &vaultID, &closureRowID, &commitOID, &treeOID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if vaultRowID < 1 || closureRowID < 1 || vaultID != identity.VaultID || !validLowerHex(commitOID, 40) || !validLowerHex(treeOID, 40) || commitOID != identity.CommitOID || treeOID != identity.TreeOID {
		return false, ErrSourceIndexGenerationIntegrity
	}
	return true, nil
}

func (s *Store) BeginSourceIndexGenerationBuild(ctx context.Context, generationID string) (SourceIndexGeneration, error) {
	return s.transitionSourceIndexGeneration(ctx, generationID, `UPDATE source_index_generations SET state = 'building', attempt_count = attempt_count + 1, building_started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state = 'pending' RETURNING `+sourceIndexGenerationColumns)
}

func (s *Store) MarkSourceIndexGenerationReady(ctx context.Context, params MarkSourceIndexGenerationReadyParams) (SourceIndexGeneration, error) {
	if !validLowerHex(params.GenerationID, 64) || !validLowerHex(params.GenerationManifestSHA256, 64) || !validLowerHex(params.CoverageManifestSHA256, 64) || !validLowerHex(params.ArtifactManifestSHA256, 64) {
		return SourceIndexGeneration{}, ErrInvalidSourceIndexGeneration
	}
	value, err := scanSourceIndexGeneration(s.db.QueryRowContext(ctx, `UPDATE source_index_generations SET state = 'ready', generation_manifest_sha256 = ?, coverage_manifest_sha256 = ?, artifact_manifest_sha256 = ?, ready_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state = 'building' RETURNING `+sourceIndexGenerationColumns, params.GenerationManifestSHA256, params.CoverageManifestSHA256, params.ArtifactManifestSHA256, params.GenerationID))
	return sourceIndexGenerationTransitionResult(ctx, s, params.GenerationID, value, err)
}

func (s *Store) MarkSourceIndexGenerationFailed(ctx context.Context, params MarkSourceIndexGenerationFailedParams) (SourceIndexGeneration, error) {
	if !validLowerHex(params.GenerationID, 64) || !validFailureValue(params.FailureCode, 128) || !validFailureValue(params.FailureMessage, 4096) {
		return SourceIndexGeneration{}, ErrInvalidSourceIndexGeneration
	}
	value, err := scanSourceIndexGeneration(s.db.QueryRowContext(ctx, `UPDATE source_index_generations SET state = 'failed', failure_code = ?, failure_message = ?, failed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state = 'building' RETURNING `+sourceIndexGenerationColumns, params.FailureCode, params.FailureMessage, params.GenerationID))
	return sourceIndexGenerationTransitionResult(ctx, s, params.GenerationID, value, err)
}

func (s *Store) RetrySourceIndexGeneration(ctx context.Context, generationID string) (SourceIndexGeneration, error) {
	return s.transitionSourceIndexGeneration(ctx, generationID, `UPDATE source_index_generations SET state = 'pending', failure_code = NULL, failure_message = NULL, building_started_at = NULL, failed_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state = 'failed' RETURNING `+sourceIndexGenerationColumns)
}

func (s *Store) RetireSourceIndexGeneration(ctx context.Context, generationID string) (SourceIndexGeneration, error) {
	return s.transitionSourceIndexGeneration(ctx, generationID, `UPDATE source_index_generations SET state = 'retired', retired_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state IN ('pending', 'failed', 'ready') RETURNING `+sourceIndexGenerationColumns)
}

func (s *Store) ReactivateSourceIndexGeneration(ctx context.Context, generationID string) (SourceIndexGeneration, error) {
	return s.transitionSourceIndexGeneration(ctx, generationID, `UPDATE source_index_generations SET state = 'pending', generation_manifest_sha256 = NULL, coverage_manifest_sha256 = NULL, artifact_manifest_sha256 = NULL, failure_code = NULL, failure_message = NULL, building_started_at = NULL, ready_at = NULL, failed_at = NULL, retired_at = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE generation_id = ? AND state = 'retired' RETURNING `+sourceIndexGenerationColumns)
}

func (s *Store) transitionSourceIndexGeneration(ctx context.Context, generationID, query string) (SourceIndexGeneration, error) {
	if s == nil || !validLowerHex(generationID, 64) {
		return SourceIndexGeneration{}, ErrInvalidSourceIndexGeneration
	}
	value, err := scanSourceIndexGeneration(s.db.QueryRowContext(ctx, query, generationID))
	return sourceIndexGenerationTransitionResult(ctx, s, generationID, value, err)
}

func sourceIndexGenerationTransitionResult(ctx context.Context, s *Store, generationID string, value SourceIndexGeneration, err error) (SourceIndexGeneration, error) {
	if !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	_, getErr := s.GetSourceIndexGeneration(ctx, generationID)
	if errors.Is(getErr, ErrSourceIndexGenerationNotFound) {
		return SourceIndexGeneration{}, getErr
	}
	if getErr != nil {
		return SourceIndexGeneration{}, getErr
	}
	return SourceIndexGeneration{}, fmt.Errorf("%w: %s", ErrSourceIndexGenerationLifecycleConflict, generationID)
}

func scanSourceIndexGeneration(row rowScanner) (SourceIndexGeneration, error) {
	var value SourceIndexGeneration
	var generation, coverage, artifact, code, message, building, ready, failed, retired sql.NullString
	err := row.Scan(&value.ID, &value.GenerationID, &value.Identity.Version, &value.Identity.VaultID, &value.Identity.CommitOID, &value.Identity.TreeOID, &value.Identity.Engine, &value.Identity.EngineRevision, &value.Identity.BuildContractVersion, &value.Identity.BuildOptionsSHA256, &value.State, &value.AttemptCount, &generation, &coverage, &artifact, &code, &message, &value.CreatedAt, &value.UpdatedAt, &building, &ready, &failed, &retired)
	if err != nil {
		return SourceIndexGeneration{}, err
	}
	value.GenerationManifestSHA256, value.CoverageManifestSHA256, value.ArtifactManifestSHA256 = generation.String, coverage.String, artifact.String
	value.FailureCode, value.FailureMessage = code.String, message.String
	value.BuildingStartedAt, value.ReadyAt, value.FailedAt, value.RetiredAt = building.String, ready.String, failed.String, retired.String
	calculated, err := sourceindex.GenerationID(value.Identity)
	if err != nil || calculated != value.GenerationID || !validSourceIndexGenerationRecord(value) {
		return SourceIndexGeneration{}, fmt.Errorf("%w: malformed persisted row", ErrSourceIndexGenerationIntegrity)
	}
	return value, nil
}

func validSourceIndexGenerationState(value SourceIndexGenerationState) bool {
	return value == SourceIndexGenerationPending || value == SourceIndexGenerationBuilding || value == SourceIndexGenerationReady || value == SourceIndexGenerationFailed || value == SourceIndexGenerationRetired
}
func validFailureValue(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len([]byte(value)) <= limit
}
func validSourceIndexGenerationRecord(v SourceIndexGeneration) bool {
	manifests := v.GenerationManifestSHA256 != "" && v.CoverageManifestSHA256 != "" && v.ArtifactManifestSHA256 != ""
	noManifests := v.GenerationManifestSHA256 == "" && v.CoverageManifestSHA256 == "" && v.ArtifactManifestSHA256 == ""
	if !validSourceIndexGenerationState(v.State) || v.AttemptCount < 0 || !validLowerHex(v.GenerationID, 64) || !validLowerHex(v.Identity.BuildOptionsSHA256, 64) || (v.GenerationManifestSHA256 != "" && !validLowerHex(v.GenerationManifestSHA256, 64)) || (v.CoverageManifestSHA256 != "" && !validLowerHex(v.CoverageManifestSHA256, 64)) || (v.ArtifactManifestSHA256 != "" && !validLowerHex(v.ArtifactManifestSHA256, 64)) {
		return false
	}
	failure := v.FailureCode != "" && v.FailureMessage != "" && validFailureValue(v.FailureCode, 128) && validFailureValue(v.FailureMessage, 4096)
	noFailure := v.FailureCode == "" && v.FailureMessage == ""
	switch v.State {
	case SourceIndexGenerationPending:
		return noManifests && noFailure && v.BuildingStartedAt == "" && v.ReadyAt == "" && v.FailedAt == "" && v.RetiredAt == ""
	case SourceIndexGenerationBuilding:
		return noManifests && noFailure && v.BuildingStartedAt != "" && v.ReadyAt == "" && v.FailedAt == "" && v.RetiredAt == ""
	case SourceIndexGenerationReady:
		return manifests && noFailure && v.BuildingStartedAt != "" && v.ReadyAt != "" && v.FailedAt == "" && v.RetiredAt == ""
	case SourceIndexGenerationFailed:
		return noManifests && failure && v.BuildingStartedAt != "" && v.ReadyAt == "" && v.FailedAt != "" && v.RetiredAt == ""
	case SourceIndexGenerationRetired:
		return v.RetiredAt != "" && ((noManifests && noFailure && v.BuildingStartedAt == "" && v.ReadyAt == "" && v.FailedAt == "") || (manifests && noFailure && v.BuildingStartedAt != "" && v.ReadyAt != "" && v.FailedAt == "") || (noManifests && failure && v.BuildingStartedAt != "" && v.ReadyAt == "" && v.FailedAt != ""))
	}
	return false
}
