package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type Service struct {
	store Store
}

func New(store Store) (*Service, error) {
	if store == nil {
		return nil, appError(ErrorStoreUnavailable)
	}
	return &Service{store: store}, nil
}

func (s *Service) Resolve(ctx context.Context, key MutationKey, fingerprint semanticidentity.Fingerprint) (Resolution, error) {
	if err := validateKeyAndFingerprint(key, fingerprint); err != nil {
		return Resolution{}, err
	}
	row, ok, err := s.store.GetMCPMutationResultOptional(ctx, storeKey(key))
	if err != nil {
		return Resolution{}, appError(ErrorStoreUnavailable)
	}
	if !ok {
		return Resolution{Kind: ResolutionMiss}, nil
	}
	return resolveRow(key, fingerprint, row)
}

func (s *Service) RecordSuccess(ctx context.Context, input RecordSuccessInput, mutate DomainMutation) (StoredResult, bool, error) {
	var result StoredResult
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		result, err = s.RecordSuccessInTx(ctx, tx, input, mutate)
		return err
	})
	if err == nil {
		return result, false, nil
	}
	if IsConcurrentWinner(err) {
		return s.ResolveAfterRollback(ctx, input, err)
	}
	var value *Error
	if errors.As(err, &value) {
		return StoredResult{}, false, err
	}
	return StoredResult{}, false, appError(ErrorTransactionIntegration)
}

func (s *Service) RecordSuccessInTx(ctx context.Context, tx *workflowstore.Tx, input RecordSuccessInput, mutate DomainMutation) (StoredResult, error) {
	if tx == nil || mutate == nil {
		return StoredResult{}, appError(ErrorTransactionIntegration)
	}
	if err := validateKeyAndFingerprint(input.Key, input.Fingerprint); err != nil {
		return StoredResult{}, err
	}
	currentManifest, ok := registry.SurfaceManifestSHA256(input.Key.SurfaceContractID)
	if !ok || input.SurfaceManifestSHA256 != currentManifest {
		return StoredResult{}, appError(ErrorInvalidSemanticIdentity)
	}

	row, found, err := tx.GetMCPMutationResultOptional(ctx, storeKey(input.Key))
	if err != nil {
		return StoredResult{}, appError(ErrorTransactionIntegration)
	}
	if found {
		resolution, err := resolveRow(input.Key, input.Fingerprint, row)
		if err != nil {
			return StoredResult{}, err
		}
		switch resolution.Kind {
		case ResolutionReplay:
			return StoredResult{}, appError(ErrorConcurrentWinner)
		case ResolutionConflict:
			return StoredResult{}, appError(ErrorMutationConflict)
		default:
			return StoredResult{}, appError(ErrorTransactionIntegration)
		}
	}

	identity, err := mutate(ctx, tx)
	if err != nil {
		return StoredResult{}, err
	}
	encoded, err := semanticidentity.EncodeResultIdentity(input.Key.SurfaceContractID, input.Key.Tool, identity)
	if err != nil {
		return StoredResult{}, appError(ErrorInvalidResultIdentity)
	}
	created, err := tx.CreateMCPMutationResult(ctx, workflowstore.CreateMCPMutationResultParams{
		SurfaceContractID:       string(input.Key.SurfaceContractID),
		ToolName:                string(input.Key.Tool),
		MutationID:              input.Key.MutationID,
		SurfaceManifestSHA256:   input.SurfaceManifestSHA256,
		SemanticIdentityVersion: input.Fingerprint.SemanticIdentityVersion(),
		SemanticRequestSHA256:   input.Fingerprint.SemanticRequestSHA256(),
		ResultKind:              string(encoded.Kind),
		ResultIdentityJSON:      string(encoded.JSON),
		ResultSHA256:            encoded.SHA256,
	})
	if err != nil {
		if isUniqueMutationKeyError(err) {
			return StoredResult{}, appError(ErrorConcurrentWinner)
		}
		return StoredResult{}, appError(ErrorTransactionIntegration)
	}
	result, err := verifiedStoredResult(input.Key, input.Fingerprint, created)
	if err != nil {
		return StoredResult{}, err
	}
	return result, nil
}

func (s *Service) ResolveAfterRollback(ctx context.Context, input RecordSuccessInput, transactionErr error) (StoredResult, bool, error) {
	if !IsConcurrentWinner(transactionErr) {
		return StoredResult{}, false, transactionErr
	}
	resolution, err := s.Resolve(ctx, input.Key, input.Fingerprint)
	if err != nil {
		return StoredResult{}, false, err
	}
	switch resolution.Kind {
	case ResolutionReplay:
		return resolution.Result, true, nil
	case ResolutionConflict:
		return StoredResult{}, false, appError(ErrorMutationConflict)
	default:
		return StoredResult{}, false, appError(ErrorTransactionIntegration)
	}
}

func resolveRow(key MutationKey, fingerprint semanticidentity.Fingerprint, row workflowstore.MCPMutationResult) (Resolution, error) {
	if row.SurfaceContractID != string(key.SurfaceContractID) || row.ToolName != string(key.Tool) || row.MutationID != key.MutationID {
		return Resolution{}, appError(ErrorCorruptStoredResult)
	}
	currentManifest, ok := registry.SurfaceManifestSHA256(key.SurfaceContractID)
	if !ok || row.SurfaceManifestSHA256 != currentManifest || row.SemanticIdentityVersion == "" || row.SemanticRequestSHA256 == "" {
		return Resolution{}, appError(ErrorCorruptStoredResult)
	}
	if row.SemanticIdentityVersion != fingerprint.SemanticIdentityVersion() || row.SemanticRequestSHA256 != fingerprint.SemanticRequestSHA256() {
		return Resolution{Kind: ResolutionConflict}, nil
	}
	result, err := verifiedStoredResult(key, fingerprint, row)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Kind: ResolutionReplay, Result: result}, nil
}

func verifiedStoredResult(key MutationKey, fingerprint semanticidentity.Fingerprint, row workflowstore.MCPMutationResult) (StoredResult, error) {
	if row.SurfaceContractID != string(key.SurfaceContractID) || row.ToolName != string(key.Tool) || row.MutationID != key.MutationID || row.SemanticIdentityVersion != fingerprint.SemanticIdentityVersion() || row.SemanticRequestSHA256 != fingerprint.SemanticRequestSHA256() || strings.TrimSpace(row.CommittedAt) == "" {
		return StoredResult{}, appError(ErrorCorruptStoredResult)
	}
	raw := []byte(row.ResultIdentityJSON)
	if len(raw) < 2 || len(raw) > semanticidentity.MaxResultIdentityBytes || raw[0] != '{' {
		return StoredResult{}, appError(ErrorCorruptStoredResult)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != row.ResultSHA256 {
		return StoredResult{}, appError(ErrorCorruptStoredResult)
	}
	identity, err := semanticidentity.DecodeResultIdentity(key.SurfaceContractID, key.Tool, semanticidentity.ResultKind(row.ResultKind), raw)
	if err != nil {
		return StoredResult{}, appError(ErrorCorruptStoredResult)
	}
	encoded, err := semanticidentity.EncodeResultIdentity(key.SurfaceContractID, key.Tool, identity)
	if err != nil || encoded.Kind != semanticidentity.ResultKind(row.ResultKind) || encoded.SHA256 != row.ResultSHA256 || !bytes.Equal(encoded.JSON, raw) {
		return StoredResult{}, appError(ErrorCorruptStoredResult)
	}
	return StoredResult{
		ResultKind:         encoded.Kind,
		ResultIdentity:     identity,
		ResultIdentityJSON: append([]byte(nil), raw...),
		ResultSHA256:       row.ResultSHA256,
		CommittedAt:        row.CommittedAt,
	}, nil
}

func validateKeyAndFingerprint(key MutationKey, fingerprint semanticidentity.Fingerprint) error {
	if _, ok := registry.SurfaceManifestSHA256(key.SurfaceContractID); !ok {
		return appError(ErrorUnknownSurfaceContract)
	}
	if !registry.IsStateChangingToolForSurface(key.SurfaceContractID, string(key.Tool)) {
		return appError(ErrorUnknownMutationTool)
	}
	if registry.ValidateMutationID(key.MutationID) != nil {
		return appError(ErrorInvalidMutationID)
	}
	version, ok := registry.SemanticProjectionVersion(string(key.Tool))
	if !ok || fingerprint.SurfaceContractID() != key.SurfaceContractID || fingerprint.Tool() != key.Tool || fingerprint.SemanticIdentityVersion() != version || !validSHA256(fingerprint.SemanticRequestSHA256()) {
		return appError(ErrorInvalidSemanticIdentity)
	}
	return nil
}

func storeKey(key MutationKey) workflowstore.MCPMutationKey {
	return workflowstore.MCPMutationKey{
		SurfaceContractID: string(key.SurfaceContractID),
		ToolName:          string(key.Tool),
		MutationID:        key.MutationID,
	}
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func isUniqueMutationKeyError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed: mcp_mutation_results.surface_contract_id, mcp_mutation_results.tool_name, mcp_mutation_results.mutation_id") ||
		strings.Contains(message, "mcp_mutation_results.surface_contract_id, mcp_mutation_results.tool_name, mcp_mutation_results.mutation_id")
}
