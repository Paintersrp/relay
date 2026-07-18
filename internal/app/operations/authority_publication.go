package operations

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"

	"relay/internal/app/idempotency"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

var ErrAuthorityPublication = &Error{Code: CodeAuthorityPublicationFailure}

type PublicationArtifactInput struct {
	ArtifactID   string
	Kind         string
	RelativePath string
	MediaType    string
	Bytes        []byte
	SourcePath   string
}

type PublicationBindingInput struct {
	Sequence        int64
	DependencyClass string
	DependencyKey   string
	ArtifactID      string
}

type PublicationVaultInput struct {
	ClosureID       string
	DependencyClass string
	DependencyKey   string
}

type PublicationMutationInput struct {
	PublicationID       string
	PacketArtifactRowID int64
	PacketArtifact      workflowartifacts.File
}

type AuthorityPublicationMutation func(context.Context, *workflowstore.Tx, PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error)

type AuthorityPublicationInput struct {
	PacketID           string
	PriorPacketID      string
	RequestIdentity    semanticidentity.RequestIdentity
	PacketArtifactID   string
	PacketMediaType    string
	PacketBytes        []byte
	RetainedArtifacts  []PublicationArtifactInput
	Bindings           []PublicationBindingInput
	VaultRelationships []PublicationVaultInput
	Idempotency        idempotency.RecordSuccessInput
	Mutation           AuthorityPublicationMutation
}

type AuthorityPublicationResult struct {
	Publication workflowstore.OperationPacketPublication
	Packet      workflowstore.OperationPacket
	Mutation    idempotency.StoredResult
	Replay      bool
}

type AuthorityPublicationService struct {
	store       *workflowstore.Store
	vaults      *sourcevault.Manager
	idempotency *idempotency.Service
}

func NewAuthorityPublicationService(store *workflowstore.Store, vaults *sourcevault.Manager) (*AuthorityPublicationService, error) {
	if store == nil || vaults == nil {
		return nil, ErrAuthorityPublication
	}
	mutationService, err := idempotency.New(store)
	if err != nil {
		return nil, ErrAuthorityPublication
	}
	return &AuthorityPublicationService{store: store, vaults: vaults, idempotency: mutationService}, nil
}

func (s *AuthorityPublicationService) Publish(ctx context.Context, input AuthorityPublicationInput) (AuthorityPublicationResult, error) {
	if err := validateAuthorityPublicationInput(input); err != nil {
		return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(err)
	}
	publicationID := workflowstore.NewOperationPacketPublicationID()
	preparedVaults := make([]sourcevault.PreparedRetention, 0, len(input.VaultRelationships))
	for _, value := range input.VaultRelationships {
		ownerIdentity, err := workflowstore.SourceVaultRetentionOwnerIdentity(input.PacketID, value.DependencyClass, value.DependencyKey)
		if err != nil {
			return AuthorityPublicationResult{}, ErrAuthorityPublication
		}
		prepared, err := s.vaults.PrepareRetention(ctx, sourcevault.RetainRequest{
			ClosureID:       value.ClosureID,
			OwnerClass:      workflowstore.SourceVaultOwnerOperationPacket,
			OwnerIdentity:   ownerIdentity,
			PacketID:        input.PacketID,
			DependencyClass: value.DependencyClass,
			DependencyKey:   value.DependencyKey,
		})
		if err != nil {
			return AuthorityPublicationResult{}, err
		}
		preparedVaults = append(preparedVaults, prepared)
	}

	batch, err := s.store.ArtifactStore().BeginPublication(publicationID)
	if err != nil {
		return AuthorityPublicationResult{}, ErrAuthorityPublication
	}
	packetFile, err := batch.Stage("operation_packet_document", "operation-packet.json", input.PacketMediaType, input.PacketBytes)
	if err != nil {
		_ = batch.Rollback()
		return AuthorityPublicationResult{}, ErrAuthorityPublication
	}
	stagedRetained := make(map[string]workflowartifacts.File, len(input.RetainedArtifacts))
	for _, value := range input.RetainedArtifacts {
		var file workflowartifacts.File
		if value.SourcePath != "" {
			file, err = batch.StageFile(value.Kind, value.RelativePath, value.MediaType, value.SourcePath)
		} else {
			file, err = batch.Stage(value.Kind, value.RelativePath, value.MediaType, value.Bytes)
		}
		if err != nil {
			_ = batch.Rollback()
			return AuthorityPublicationResult{}, ErrAuthorityPublication
		}
		stagedRetained[value.ArtifactID] = file
	}
	expectations := workflowartifacts.PublicationExpectations{
		RetainedArtifactCount:  int64(len(input.RetainedArtifacts)),
		BindingCount:           int64(1 + len(input.Bindings)),
		DependencyCount:        int64(1 + len(input.Bindings) + len(input.VaultRelationships)),
		VaultRelationshipCount: int64(len(input.VaultRelationships)),
	}
	if _, err := batch.Seal(expectations); err != nil {
		_ = batch.Rollback()
		return AuthorityPublicationResult{}, ErrAuthorityPublication
	}

	var result AuthorityPublicationResult
	err = s.store.CommitOperationPacketPublication(ctx, batch, func(tx *workflowstore.Tx) error {
		packetArtifact, err := tx.CreateOperationPacketArtifact(ctx, workflowstore.CreateOperationPacketArtifactParams{
			ArtifactID:   input.PacketArtifactID,
			Kind:         packetFile.Kind,
			RelativePath: packetFile.RelativePath,
			MediaType:    packetFile.MediaType,
			SHA256:       packetFile.SHA256,
			SizeBytes:    packetFile.SizeBytes,
		})
		if err != nil {
			return err
		}
		retainedRows := make(map[string]workflowstore.OperationPacketRetainedArtifact, len(input.RetainedArtifacts))
		for _, value := range input.RetainedArtifacts {
			file := stagedRetained[value.ArtifactID]
			row, err := tx.CreateOperationPacketRetainedArtifact(ctx, workflowstore.CreateOperationPacketRetainedArtifactParams{
				PublicationID: publicationID,
				ArtifactID:    value.ArtifactID,
				Kind:          value.Kind,
				RelativePath:  file.RelativePath,
				MediaType:     file.MediaType,
				SHA256:        file.SHA256,
				SizeBytes:     file.SizeBytes,
			})
			if err != nil {
				return err
			}
			retainedRows[value.ArtifactID] = row
		}

		stored, err := s.idempotency.RecordSuccessInTx(ctx, tx, input.Idempotency, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
			packet, identity, err := input.Mutation(ctx, tx, PublicationMutationInput{
				PublicationID:       publicationID,
				PacketArtifactRowID: packetArtifact.ID,
				PacketArtifact:      packetFile,
			})
			if err != nil {
				return nil, err
			}
			if packet.PacketID != input.PacketID || !packet.CoordinatedPublicationID.Valid || packet.CoordinatedPublicationID.String != publicationID || packet.PacketArtifactRowID != packetArtifact.ID || packet.PacketSHA256 != packetArtifact.SHA256 {
				return nil, ErrAuthorityPublication
			}
			result.Packet = packet
			return identity, nil
		})
		if err != nil {
			return err
		}
		result.Mutation = stored
		mutationRow, err := tx.GetMCPMutationResult(ctx, workflowstore.MCPMutationKey{
			SurfaceContractID: string(input.Idempotency.Key.SurfaceContractID),
			ToolName:          string(input.Idempotency.Key.Tool),
			MutationID:        input.Idempotency.Key.MutationID,
		})
		if err != nil {
			return err
		}
		var priorPacket *workflowstore.OperationPacket
		if input.PriorPacketID != "" {
			value, err := tx.GetOperationPacketByPacketID(ctx, input.PriorPacketID)
			if err != nil {
				return err
			}
			priorPacket = &value
		}
		if err := validatePublicationMutationAuthority(input, result.Packet, packetArtifact, priorPacket, mutationRow); err != nil {
			return err
		}

		if _, err := tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{
			PacketRowID:     result.Packet.ID,
			DependencyClass: workflowstore.OperationPacketDependencyPacketDocument,
			DependencyKey:   packetArtifact.ArtifactID,
			Required:        true,
			Attached:        true,
			Retained:        true,
			OwnerIdentity:   sql.NullString{String: packetArtifact.ArtifactID, Valid: true},
		}); err != nil {
			return err
		}
		if _, err := tx.CreateOperationPacketArtifactBinding(ctx, workflowstore.CreateOperationPacketArtifactBindingParams{
			PublicationID:       publicationID,
			PacketRowID:         result.Packet.ID,
			Sequence:            0,
			DependencyClass:     workflowstore.OperationPacketDependencyPacketDocument,
			DependencyKey:       packetArtifact.ArtifactID,
			PacketArtifactRowID: sql.NullInt64{Int64: packetArtifact.ID, Valid: true},
		}); err != nil {
			return err
		}
		for _, value := range input.Bindings {
			retained, ok := retainedRows[value.ArtifactID]
			if !ok {
				return ErrAuthorityPublication
			}
			if _, err := tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{
				PacketRowID:     result.Packet.ID,
				DependencyClass: value.DependencyClass,
				DependencyKey:   value.DependencyKey,
				Required:        true,
				Attached:        true,
				Retained:        true,
				OwnerIdentity:   sql.NullString{String: retained.ArtifactID, Valid: true},
			}); err != nil {
				return err
			}
			if _, err := tx.CreateOperationPacketArtifactBinding(ctx, workflowstore.CreateOperationPacketArtifactBindingParams{
				PublicationID:         publicationID,
				PacketRowID:           result.Packet.ID,
				Sequence:              value.Sequence,
				DependencyClass:       value.DependencyClass,
				DependencyKey:         value.DependencyKey,
				RetainedArtifactRowID: sql.NullInt64{Int64: retained.ID, Valid: true},
			}); err != nil {
				return err
			}
		}
		for _, prepared := range preparedVaults {
			retention, err := s.vaults.RetainPreparedInTx(ctx, tx, prepared)
			if err != nil {
				return err
			}
			if _, err := tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{
				PacketRowID:     result.Packet.ID,
				DependencyClass: prepared.DependencyClass,
				DependencyKey:   prepared.DependencyKey,
				Required:        true,
				Attached:        true,
				Retained:        true,
				OwnerIdentity:   sql.NullString{String: prepared.OwnerIdentity, Valid: true},
			}); err != nil {
				return err
			}
			if _, err := tx.CreateOperationPacketVaultRelationship(ctx, workflowstore.CreateOperationPacketVaultRelationshipParams{
				PublicationID:   publicationID,
				PacketRowID:     result.Packet.ID,
				DependencyClass: prepared.DependencyClass,
				DependencyKey:   prepared.DependencyKey,
				OwnerIdentity:   prepared.OwnerIdentity,
				RetentionRowID:  retention.ID,
				ClosureRowID:    prepared.Closure.ID,
				VaultRowID:      prepared.Vault.ID,
				CommitOID:       prepared.Closure.CommitOID,
				TreeOID:         prepared.Closure.TreeOID,
			}); err != nil {
				return err
			}
		}
		publication, err := tx.CreateOperationPacketPublication(ctx, workflowstore.CreateOperationPacketPublicationParams{
			PublicationID:                  publicationID,
			PacketRowID:                    result.Packet.ID,
			PacketArtifactRowID:            packetArtifact.ID,
			MutationResultRowID:            mutationRow.ID,
			Namespace:                      batch.Namespace(),
			ManifestSHA256:                 batch.ManifestSHA256(),
			ExpectedRetainedArtifactCount:  expectations.RetainedArtifactCount,
			ExpectedBindingCount:           expectations.BindingCount,
			ExpectedDependencyCount:        expectations.DependencyCount,
			ExpectedVaultRelationshipCount: expectations.VaultRelationshipCount,
		})
		if err != nil {
			return err
		}
		result.Publication = publication
		return nil
	})
	if err != nil {
		if idempotency.IsConcurrentWinner(err) {
			stored, replay, recoveryErr := s.idempotency.ResolveAfterRollback(ctx, input.Idempotency, err)
			if recoveryErr != nil {
				return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(recoveryErr)
			}
			return s.resolveCommittedPublicationWinner(ctx, input, stored, replay)
		}
		return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(err)
	}
	return result, nil
}

func validatePublicationMutationAuthority(input AuthorityPublicationInput, packet workflowstore.OperationPacket, artifact workflowstore.OperationPacketArtifact, prior *workflowstore.OperationPacket, mutation workflowstore.MCPMutationResult) error {
	return validatePublicationMutationAuthorityMode(input, packet, artifact, prior, mutation, true)
}

func validateCommittedPublicationWinnerAuthority(input AuthorityPublicationInput, packet workflowstore.OperationPacket, artifact workflowstore.OperationPacketArtifact, prior *workflowstore.OperationPacket, mutation workflowstore.MCPMutationResult) error {
	return validatePublicationMutationAuthorityMode(input, packet, artifact, prior, mutation, false)
}

func validatePublicationMutationAuthorityMode(input AuthorityPublicationInput, packet workflowstore.OperationPacket, artifact workflowstore.OperationPacketArtifact, prior *workflowstore.OperationPacket, mutation workflowstore.MCPMutationResult, requireCandidateIdentity bool) error {
	tool := input.Idempotency.Key.Tool
	surface := input.Idempotency.Key.SurfaceContractID
	operation, ok := registry.Lookup(registry.OperationID(packet.OperationID))
	projectionVersion, versionOK := registry.SemanticProjectionVersion(string(tool))
	if !ok || !versionOK ||
		packet.PacketArtifactRowID != artifact.ID ||
		packet.PacketSHA256 != artifact.SHA256 ||
		(requireCandidateIdentity && (packet.PacketID != input.PacketID || artifact.ArtifactID != input.PacketArtifactID)) ||
		string(operation.Role) != packet.Role ||
		string(operation.SurfaceContract) != packet.SurfaceContractID ||
		packet.SurfaceContractID != string(surface) ||
		input.Idempotency.Fingerprint.SurfaceContractID() != surface ||
		input.Idempotency.Fingerprint.Tool() != tool ||
		!registry.IsStateChangingToolForSurface(surface, string(tool)) ||
		mutation.SurfaceContractID != packet.SurfaceContractID ||
		mutation.ToolName != string(tool) ||
		mutation.MutationID != input.Idempotency.Key.MutationID ||
		mutation.SurfaceManifestSHA256 != input.Idempotency.SurfaceManifestSHA256 ||
		mutation.SemanticIdentityVersion != projectionVersion ||
		mutation.SemanticIdentityVersion != input.Idempotency.Fingerprint.SemanticIdentityVersion() ||
		mutation.SemanticRequestSHA256 != input.Idempotency.Fingerprint.SemanticRequestSHA256() {
		return &Error{Code: CodeAuthorityPublicationConflict}
	}
	identity, err := semanticidentity.DecodeResultIdentity(
		surface,
		tool,
		semanticidentity.ResultKind(mutation.ResultKind),
		[]byte(mutation.ResultIdentityJSON),
	)
	if err != nil {
		return &Error{Code: CodeAuthorityPublicationConflict}
	}
	switch tool {
	case registry.MutationToolCreateOperationPacket:
		request, requestOK := input.RequestIdentity.(semanticidentity.CreateOperationPacket)
		result, resultOK := identity.(semanticidentity.CreateOperationPacketResult)
		if !requestOK || !resultOK || request.OperationID != registry.OperationID(packet.OperationID) || request.ProjectID != packet.ProjectID || request.SurfaceContract != surface ||
			input.PriorPacketID != "" || prior != nil || packet.PriorPacketRowID.Valid || packet.LifecycleState != workflowstore.OperationPacketLifecycleActive ||
			packet.ReplacementPacketRowID.Valid || packet.SupersededAt.Valid || packet.ClosedAt.Valid ||
			result.SurfaceManifestSHA256 != input.Idempotency.SurfaceManifestSHA256 ||
			!operationPacketViewIdentityMatches(result.Packet, packet, artifact, nil) {
			return &Error{Code: CodeAuthorityPublicationConflict}
		}
	case registry.MutationToolRefreshOperationPacket:
		request, requestOK := input.RequestIdentity.(semanticidentity.RefreshOperationPacket)
		result, resultOK := identity.(semanticidentity.RefreshOperationPacketResult)
		if !requestOK || !resultOK || prior == nil || input.PriorPacketID == "" || request.ExpectedPacketID != input.PriorPacketID || prior.PacketID != input.PriorPacketID || request.SurfaceContract != surface ||
			!packet.PriorPacketRowID.Valid || packet.PriorPacketRowID.Int64 != prior.ID || packet.LifecycleState != workflowstore.OperationPacketLifecycleActive ||
			packet.ReplacementPacketRowID.Valid || packet.SupersededAt.Valid || packet.ClosedAt.Valid ||
			prior.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || !prior.SupersededAt.Valid || prior.ClosedAt.Valid ||
			prior.Role != packet.Role || prior.OperationID != packet.OperationID || prior.SurfaceContractID != packet.SurfaceContractID || prior.ProjectID != packet.ProjectID ||
			!prior.ReplacementPacketRowID.Valid || prior.ReplacementPacketRowID.Int64 != packet.ID ||
			result.SurfaceManifestSHA256 != input.Idempotency.SurfaceManifestSHA256 ||
			!operationPacketSummaryIdentityMatches(result.PriorPacket, *prior, &packet) ||
			!operationPacketViewIdentityMatches(result.Packet, packet, artifact, nil) {
			return &Error{Code: CodeAuthorityPublicationConflict}
		}
	default:
		return &Error{Code: CodeAuthorityPublicationConflict}
	}
	return nil
}

func operationPacketViewIdentityMatches(value semanticidentity.OperationPacketViewIdentity, packet workflowstore.OperationPacket, artifact workflowstore.OperationPacketArtifact, replacement *workflowstore.OperationPacket) bool {
	return operationPacketSummaryIdentityMatches(value.Summary, packet, replacement) &&
		value.Document.ArtifactID == artifact.ArtifactID &&
		value.Document.MediaType == artifact.MediaType &&
		value.Document.SizeBytes == artifact.SizeBytes &&
		value.Document.SHA256 == artifact.SHA256
}

func operationPacketSummaryIdentityMatches(value semanticidentity.PacketSummaryIdentity, packet workflowstore.OperationPacket, replacement *workflowstore.OperationPacket) bool {
	if value.PacketID != packet.PacketID ||
		value.PacketSHA256 != packet.PacketSHA256 ||
		value.SchemaVersion != packet.SchemaVersion ||
		value.Role != packet.Role ||
		string(value.OperationID) != packet.OperationID ||
		string(value.SurfaceContractID) != packet.SurfaceContractID ||
		value.ProjectID != packet.ProjectID ||
		value.ReadinessState != packet.ReadinessState ||
		value.LifecycleState != packet.LifecycleState ||
		!operationPacketOptionalStringMatches(value.SupersededAt, packet.SupersededAt) ||
		!operationPacketOptionalStringMatches(value.ClosedAt, packet.ClosedAt) {
		return false
	}
	if packet.ReplacementPacketRowID.Valid {
		return replacement != nil &&
			replacement.ID == packet.ReplacementPacketRowID.Int64 &&
			value.ReplacementPacket != nil &&
			value.ReplacementPacket.PacketID == replacement.PacketID &&
			value.ReplacementPacket.PacketSHA256 == replacement.PacketSHA256 &&
			value.ReplacementPacket.Role == replacement.Role &&
			string(value.ReplacementPacket.OperationID) == replacement.OperationID &&
			string(value.ReplacementPacket.SurfaceContractID) == replacement.SurfaceContractID
	}
	return replacement == nil && value.ReplacementPacket == nil
}

func operationPacketOptionalStringMatches(value *string, stored sql.NullString) bool {
	if !stored.Valid {
		return value == nil
	}
	return value != nil && *value == stored.String
}

func (s *AuthorityPublicationService) resolveCommittedPublicationWinner(ctx context.Context, input AuthorityPublicationInput, stored idempotency.StoredResult, replay bool) (AuthorityPublicationResult, error) {
	integrity, err := s.store.GetOperationPacketPublicationIntegrityByMutationKey(ctx, workflowstore.MCPMutationKey{
		SurfaceContractID: string(input.Idempotency.Key.SurfaceContractID),
		ToolName:          string(input.Idempotency.Key.Tool),
		MutationID:        input.Idempotency.Key.MutationID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return AuthorityPublicationResult{}, &Error{Code: CodeAuthorityPublicationConflict}
	}
	if err != nil {
		return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(err)
	}
	var prior *workflowstore.OperationPacket
	if input.PriorPacketID != "" {
		value, err := s.store.GetOperationPacketByPacketID(ctx, input.PriorPacketID)
		if errors.Is(err, sql.ErrNoRows) {
			return AuthorityPublicationResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		if err != nil {
			return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(err)
		}
		prior = &value
	}
	if err := validateCommittedPublicationWinnerAuthority(input, integrity.Packet, integrity.PacketArtifact, prior, integrity.MutationResult); err != nil {
		return AuthorityPublicationResult{}, err
	}
	if integrity.MutationResult.ResultKind != string(stored.ResultKind) ||
		integrity.MutationResult.ResultSHA256 != stored.ResultSHA256 ||
		integrity.MutationResult.CommittedAt != stored.CommittedAt ||
		!bytes.Equal([]byte(integrity.MutationResult.ResultIdentityJSON), stored.ResultIdentityJSON) {
		return AuthorityPublicationResult{}, &Error{Code: CodeAuthorityPublicationConflict}
	}
	verified, err := s.store.ArtifactStore().VerifyPublication(integrity.Publication.PublicationID)
	if err != nil ||
		verified.ManifestSHA256 != integrity.Publication.ManifestSHA256 ||
		verified.Manifest.Namespace != integrity.Publication.Namespace ||
		verifyPublicationIntegrity(verified.Manifest, integrity) != nil {
		return AuthorityPublicationResult{}, ErrAuthorityPublication
	}
	for _, relationship := range integrity.VaultRelationships {
		if err := s.vaults.VerifyActiveRetentionEdge(ctx, relationship); err != nil {
			return AuthorityPublicationResult{}, normalizeAuthorityPublicationError(err)
		}
	}
	return AuthorityPublicationResult{
		Publication: integrity.Publication,
		Packet:      integrity.Packet,
		Mutation:    stored,
		Replay:      replay,
	}, nil
}

func normalizeAuthorityPublicationError(err error) error {
	if err == nil {
		return nil
	}
	var operationErr *Error
	if errors.As(err, &operationErr) {
		return err
	}
	var vaultErr *sourcevault.Error
	if errors.As(err, &vaultErr) {
		return err
	}
	var mutationErr *idempotency.Error
	if errors.As(err, &mutationErr) {
		return err
	}
	return ErrAuthorityPublication
}

func (s *AuthorityPublicationService) Reconcile(ctx context.Context) error {
	artifacts := s.store.ArtifactStore()
	if err := artifacts.RemovePublicationStagingResidue(); err != nil {
		return ErrAuthorityPublication
	}
	directoryIDs, err := artifacts.ListPublicationIDs()
	if err != nil {
		return ErrAuthorityPublication
	}
	publications, err := s.store.ListOperationPacketPublications(ctx)
	if err != nil {
		return ErrAuthorityPublication
	}
	committed := make(map[string]workflowstore.OperationPacketPublication, len(publications))
	for _, publication := range publications {
		committed[publication.PublicationID] = publication
	}
	for _, publicationID := range directoryIDs {
		if _, ok := committed[publicationID]; ok {
			continue
		}
		if err := artifacts.RemoveUncommittedPublication(publicationID); err != nil {
			return ErrAuthorityPublication
		}
	}
	for _, publication := range publications {
		verified, err := artifacts.VerifyPublication(publication.PublicationID)
		if err != nil || verified.ManifestSHA256 != publication.ManifestSHA256 || verified.Manifest.Namespace != publication.Namespace {
			return ErrAuthorityPublication
		}
		integrity, err := s.store.GetOperationPacketPublicationIntegrity(ctx, publication.PublicationID)
		if err != nil || verifyPublicationIntegrity(verified.Manifest, integrity) != nil {
			return ErrAuthorityPublication
		}
		for _, relationship := range integrity.VaultRelationships {
			if err := s.vaults.VerifyActiveRetentionEdge(ctx, relationship); err != nil {
				return ErrAuthorityPublication
			}
		}
	}
	return nil
}

func validateAuthorityPublicationInput(input AuthorityPublicationInput) error {
	if input.PacketID == "" || input.PacketArtifactID == "" || input.PacketMediaType == "" || len(input.PacketBytes) == 0 || input.Mutation == nil || input.RequestIdentity == nil {
		return ErrAuthorityPublication
	}
	fingerprint, err := semanticidentity.BuildFingerprint(input.RequestIdentity)
	if err != nil ||
		fingerprint.SurfaceContractID() != input.Idempotency.Fingerprint.SurfaceContractID() ||
		fingerprint.Tool() != input.Idempotency.Fingerprint.Tool() ||
		fingerprint.SemanticIdentityVersion() != input.Idempotency.Fingerprint.SemanticIdentityVersion() ||
		fingerprint.SemanticRequestSHA256() != input.Idempotency.Fingerprint.SemanticRequestSHA256() ||
		fingerprint.SurfaceContractID() != input.Idempotency.Key.SurfaceContractID ||
		fingerprint.Tool() != input.Idempotency.Key.Tool {
		return &Error{Code: CodeAuthorityPublicationConflict}
	}
	switch request := input.RequestIdentity.(type) {
	case semanticidentity.CreateOperationPacket:
		if input.Idempotency.Key.Tool != registry.MutationToolCreateOperationPacket || input.PriorPacketID != "" || request.SurfaceContract != input.Idempotency.Key.SurfaceContractID {
			return &Error{Code: CodeAuthorityPublicationConflict}
		}
	case semanticidentity.RefreshOperationPacket:
		if input.Idempotency.Key.Tool != registry.MutationToolRefreshOperationPacket || input.PriorPacketID == "" || strings.TrimSpace(input.PriorPacketID) != input.PriorPacketID || request.ExpectedPacketID != input.PriorPacketID || request.SurfaceContract != input.Idempotency.Key.SurfaceContractID {
			return &Error{Code: CodeAuthorityPublicationConflict}
		}
	default:
		return &Error{Code: CodeAuthorityPublicationConflict}
	}
	artifacts := make(map[string]struct{}, len(input.RetainedArtifacts))
	for _, value := range input.RetainedArtifacts {
		if value.ArtifactID == "" || value.Kind == "" || value.RelativePath == "" || value.MediaType == "" || (value.SourcePath == "" && value.Bytes == nil) {
			return ErrAuthorityPublication
		}
		if _, duplicate := artifacts[value.ArtifactID]; duplicate {
			return ErrAuthorityPublication
		}
		artifacts[value.ArtifactID] = struct{}{}
	}
	sequences := make(map[int64]struct{})
	sequences[0] = struct{}{}
	edges := make(map[string]struct{})
	for _, value := range input.Bindings {
		if value.Sequence < 1 || value.DependencyClass == "" || value.DependencyKey == "" {
			return ErrAuthorityPublication
		}
		if _, ok := artifacts[value.ArtifactID]; !ok {
			return ErrAuthorityPublication
		}
		if _, duplicate := sequences[value.Sequence]; duplicate {
			return ErrAuthorityPublication
		}
		sequences[value.Sequence] = struct{}{}
		key := value.DependencyClass + "\x00" + value.DependencyKey
		if _, duplicate := edges[key]; duplicate {
			return ErrAuthorityPublication
		}
		edges[key] = struct{}{}
	}
	for _, value := range input.VaultRelationships {
		if value.ClosureID == "" || value.DependencyClass == "" || value.DependencyKey == "" {
			return ErrAuthorityPublication
		}
		key := value.DependencyClass + "\x00" + value.DependencyKey
		if _, duplicate := edges[key]; duplicate {
			return ErrAuthorityPublication
		}
		edges[key] = struct{}{}
	}
	return nil
}

func verifyPublicationIntegrity(manifest workflowartifacts.PublicationManifest, integrity workflowstore.OperationPacketPublicationIntegrity) error {
	publication := integrity.Publication
	if publication.State != workflowstore.OperationPacketPublicationStateCommitted || !integrity.Packet.CoordinatedPublicationID.Valid || integrity.Packet.CoordinatedPublicationID.String != publication.PublicationID || integrity.Packet.PacketArtifactRowID != integrity.PacketArtifact.ID || integrity.Packet.PacketSHA256 != integrity.PacketArtifact.SHA256 || publication.PacketArtifactRowID != integrity.PacketArtifact.ID || publication.MutationResultRowID != integrity.MutationResult.ID {
		return ErrAuthorityPublication
	}
	if int64(len(integrity.RetainedArtifacts)) != publication.ExpectedRetainedArtifactCount || int64(len(integrity.Bindings)) != publication.ExpectedBindingCount || int64(len(integrity.Dependencies)) != publication.ExpectedDependencyCount || int64(len(integrity.VaultRelationships)) != publication.ExpectedVaultRelationshipCount {
		return ErrAuthorityPublication
	}
	if manifest.Expectations.RetainedArtifactCount != publication.ExpectedRetainedArtifactCount || manifest.Expectations.BindingCount != publication.ExpectedBindingCount || manifest.Expectations.DependencyCount != publication.ExpectedDependencyCount || manifest.Expectations.VaultRelationshipCount != publication.ExpectedVaultRelationshipCount {
		return ErrAuthorityPublication
	}
	files := make(map[string]workflowartifacts.PublicationManifestFile, len(manifest.Files))
	for _, value := range manifest.Files {
		files[manifest.Namespace+"/"+value.RelativePath] = value
	}
	packetFile, ok := files[integrity.PacketArtifact.RelativePath]
	if !ok || packetFile.SHA256 != integrity.PacketArtifact.SHA256 || packetFile.SizeBytes != integrity.PacketArtifact.SizeBytes || packetFile.MediaType != integrity.PacketArtifact.MediaType {
		return ErrAuthorityPublication
	}
	for _, retained := range integrity.RetainedArtifacts {
		file, ok := files[retained.RelativePath]
		if !ok || file.Kind != retained.Kind || file.MediaType != retained.MediaType || file.SHA256 != retained.SHA256 || file.SizeBytes != retained.SizeBytes {
			return ErrAuthorityPublication
		}
	}
	if len(files) != 1+len(integrity.RetainedArtifacts) {
		return ErrAuthorityPublication
	}
	return nil
}
