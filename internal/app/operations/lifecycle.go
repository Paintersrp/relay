package operations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/app/idempotency"
	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func NewLifecycleService(deps LifecycleDependencies) (*LifecycleService, error) {
	if deps.Store == nil || deps.Repositories == nil || deps.Vaults == nil || deps.Publications == nil || deps.FileFetcher == nil || deps.IDs == nil || deps.Clock == nil {
		return nil, fmt.Errorf("complete operation packet lifecycle dependencies are required")
	}
	mutationService := deps.Idempotency
	if mutationService == nil {
		var err error
		mutationService, err = idempotency.New(deps.Store)
		if err != nil {
			return nil, fmt.Errorf("complete operation packet lifecycle dependencies are required")
		}
	}
	reader := deps.PacketReader
	if reader == nil {
		var err error
		reader, err = NewServiceWithDependencies(deps.Store, deps.IDs, deps.Clock)
		if err != nil {
			return nil, fmt.Errorf("complete operation packet lifecycle dependencies are required")
		}
	}
	return &LifecycleService{
		store:        deps.Store,
		repositories: deps.Repositories,
		vaults:       deps.Vaults,
		publications: deps.Publications,
		idempotency:  mutationService,
		fetcher:      deps.FileFetcher,
		packets:      reader,
		ids:          deps.IDs,
		clock:        deps.Clock,
	}, nil
}

func (s *LifecycleService) Create(ctx context.Context, input CreateLifecycleInput) (CreateLifecycleResult, error) {
	fingerprint, _, record, err := s.mutationAuthority(input.MutationID, input.Identity)
	if err != nil {
		return CreateLifecycleResult{}, err
	}
	resolved, err := s.publications.Resolve(ctx, AuthorityPublicationResolveInput{
		RequestIdentity: input.Identity,
		Idempotency:     record,
	})
	if err != nil {
		return CreateLifecycleResult{}, err
	}
	if resolved.Kind == AuthorityPublicationResolutionReplay {
		identity, ok := resolved.Result.Mutation.ResultIdentity.(semanticidentity.CreateOperationPacketResult)
		if !ok {
			return CreateLifecycleResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		view, err := s.packets.Get(ctx, identity.Packet.Summary.PacketID)
		if err != nil || !packetViewMatchesResult(view, identity.Packet) {
			return CreateLifecycleResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		return CreateLifecycleResult{Packet: view, Mutation: resolved.Result.Mutation, Replay: true}, nil
	}

	prepared, err := s.prepareCreate(ctx, input, fingerprint)
	if err != nil {
		return CreateLifecycleResult{}, normalizeLifecycleError(err)
	}
	result, err := s.publications.Publish(ctx, AuthorityPublicationInput{
		PacketID:           prepared.PacketID,
		RequestIdentity:    input.Identity,
		PacketArtifactID:   prepared.PacketArtifactID,
		PacketMediaType:    prepared.Snapshot.MediaType(),
		PacketBytes:        prepared.Snapshot.Bytes(),
		RetainedArtifacts:  prepared.RetainedArtifacts,
		Bindings:           prepared.Bindings,
		VaultRelationships: prepared.VaultRelationships,
		Idempotency:        record,
		Mutation: func(ctx context.Context, tx *workflowstore.Tx, publication PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			document := prepared.Snapshot.Document()
			created, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
				PacketID:                 prepared.PacketID,
				PacketSHA256:             prepared.Snapshot.SHA256(),
				SchemaVersion:            packet.SchemaVersion,
				Role:                     string(document.Role),
				OperationID:              string(document.OperationID),
				SurfaceContractID:        string(document.SurfaceContract),
				ProjectID:                document.Project.ProjectID,
				ReadinessState:           packet.ReadinessReady,
				CoordinatedPublicationID: sql.NullString{String: publication.PublicationID, Valid: true},
				CreatedAt:                document.CreatedAt,
				PacketArtifactRowID:      publication.PacketArtifactRowID,
			})
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			identity := semanticidentity.CreateOperationPacketResult{
				Packet:                operationPacketViewIdentityFromPublication(created, prepared.PacketArtifactID, publication.PacketArtifact, nil),
				SurfaceManifestSHA256: document.SurfaceManifestSHA256,
				Complete:              true,
			}
			return created, identity, nil
		},
	})
	if err != nil {
		return CreateLifecycleResult{}, normalizeLifecycleError(err)
	}
	view, err := s.packets.Get(ctx, result.Packet.PacketID)
	if err != nil {
		return CreateLifecycleResult{}, err
	}
	return CreateLifecycleResult{Packet: view, Mutation: result.Mutation, Replay: result.Replay}, nil
}

func (s *LifecycleService) Refresh(ctx context.Context, input RefreshLifecycleInput) (RefreshLifecycleResult, error) {
	if strings.TrimSpace(input.PriorPacketID) != input.PriorPacketID || input.PriorPacketID == "" || input.Identity.ExpectedPacketID != input.PriorPacketID {
		return RefreshLifecycleResult{}, &Error{Code: CodePacketRouteMismatch}
	}
	fingerprint, _, record, err := s.mutationAuthority(input.MutationID, input.Identity)
	if err != nil {
		return RefreshLifecycleResult{}, err
	}
	resolved, err := s.publications.Resolve(ctx, AuthorityPublicationResolveInput{
		PriorPacketID:   input.PriorPacketID,
		RequestIdentity: input.Identity,
		Idempotency:     record,
	})
	if err != nil {
		return RefreshLifecycleResult{}, err
	}
	if resolved.Kind == AuthorityPublicationResolutionReplay {
		identity, ok := resolved.Result.Mutation.ResultIdentity.(semanticidentity.RefreshOperationPacketResult)
		if !ok {
			return RefreshLifecycleResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		prior, err := s.packets.Get(ctx, identity.PriorPacket.PacketID)
		if err != nil || !packetSummaryMatchesResult(prior.Summary, identity.PriorPacket) {
			return RefreshLifecycleResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		view, err := s.packets.Get(ctx, identity.Packet.Summary.PacketID)
		if err != nil || !packetViewMatchesResult(view, identity.Packet) {
			return RefreshLifecycleResult{}, &Error{Code: CodeAuthorityPublicationConflict}
		}
		return RefreshLifecycleResult{Prior: prior.Summary, Packet: view, Mutation: resolved.Result.Mutation, Replay: true}, nil
	}

	priorView, err := s.packets.Get(ctx, input.PriorPacketID)
	if err != nil {
		return RefreshLifecycleResult{}, err
	}
	if priorView.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || priorView.Summary.ReplacementPacket != nil || priorView.Summary.SupersededAt != nil || priorView.Summary.ClosedAt != nil {
		return RefreshLifecycleResult{}, lifecycleViewError(priorView.Summary)
	}
	prepared, err := s.prepareRefresh(ctx, input, fingerprint, priorView)
	if err != nil {
		return RefreshLifecycleResult{}, normalizeLifecycleError(err)
	}
	result, err := s.publications.Publish(ctx, AuthorityPublicationInput{
		PacketID:           prepared.PacketID,
		PriorPacketID:      input.PriorPacketID,
		RequestIdentity:    input.Identity,
		PacketArtifactID:   prepared.PacketArtifactID,
		PacketMediaType:    prepared.Snapshot.MediaType(),
		PacketBytes:        prepared.Snapshot.Bytes(),
		RetainedArtifacts:  prepared.RetainedArtifacts,
		Bindings:           prepared.Bindings,
		VaultRelationships: prepared.VaultRelationships,
		Idempotency:        record,
		Mutation: func(ctx context.Context, tx *workflowstore.Tx, publication PublicationMutationInput) (workflowstore.OperationPacket, semanticidentity.ResultIdentity, error) {
			current, err := tx.GetOperationPacketByPacketID(ctx, input.PriorPacketID)
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			if current.LifecycleState != workflowstore.OperationPacketLifecycleActive || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid || current.ClosedAt.Valid {
				return workflowstore.OperationPacket{}, nil, refreshConflictInTx(ctx, tx, current)
			}
			artifact, err := tx.GetOperationPacketArtifact(ctx, current.ID)
			if err != nil || artifact.SHA256 != current.PacketSHA256 {
				return workflowstore.OperationPacket{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
			}
			dependencies, err := tx.ListOperationPacketRetentionDependencies(ctx, current.ID)
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			if err := validateRequiredDependencies(dependencies, artifact.ArtifactID); err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			document := prepared.Snapshot.Document()
			if current.PacketID != document.PriorPacket.PacketID || current.PacketSHA256 != document.PriorPacket.PacketSHA256 || current.Role != string(document.Role) || current.OperationID != string(document.OperationID) || current.SurfaceContractID != string(document.SurfaceContract) || current.ProjectID != document.Project.ProjectID {
				return workflowstore.OperationPacket{}, nil, &Error{Code: CodePacketRouteMismatch}
			}
			replacement, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{
				PacketID:                 prepared.PacketID,
				PacketSHA256:             prepared.Snapshot.SHA256(),
				SchemaVersion:            packet.SchemaVersion,
				Role:                     string(document.Role),
				OperationID:              string(document.OperationID),
				SurfaceContractID:        string(document.SurfaceContract),
				ProjectID:                document.Project.ProjectID,
				ReadinessState:           packet.ReadinessReady,
				PriorPacketRowID:         sql.NullInt64{Int64: current.ID, Valid: true},
				CoordinatedPublicationID: sql.NullString{String: publication.PublicationID, Valid: true},
				CreatedAt:                document.CreatedAt,
				PacketArtifactRowID:      publication.PacketArtifactRowID,
			})
			if err != nil {
				return workflowstore.OperationPacket{}, nil, err
			}
			superseded, err := tx.SupersedeOperationPacket(ctx, workflowstore.SupersedeOperationPacketParams{
				PacketID:               current.PacketID,
				ReplacementPacketRowID: replacement.ID,
				SupersededAt:           document.CreatedAt,
			})
			if err != nil {
				return workflowstore.OperationPacket{}, nil, refreshConflictInTx(ctx, tx, current)
			}
			identity := semanticidentity.RefreshOperationPacketResult{
				PriorPacket:           operationPacketSummaryIdentity(superseded, &replacement),
				Packet:                operationPacketViewIdentityFromPublication(replacement, prepared.PacketArtifactID, publication.PacketArtifact, nil),
				SurfaceManifestSHA256: document.SurfaceManifestSHA256,
				Complete:              true,
			}
			return replacement, identity, nil
		},
	})
	if err != nil {
		return RefreshLifecycleResult{}, normalizeLifecycleError(err)
	}
	prior, err := s.packets.Get(ctx, input.PriorPacketID)
	if err != nil {
		return RefreshLifecycleResult{}, err
	}
	view, err := s.packets.Get(ctx, result.Packet.PacketID)
	if err != nil {
		return RefreshLifecycleResult{}, err
	}
	return RefreshLifecycleResult{Prior: prior.Summary, Packet: view, Mutation: result.Mutation, Replay: result.Replay}, nil
}

func (s *LifecycleService) Close(ctx context.Context, input CloseLifecycleInput) (CloseLifecycleResult, error) {
	fingerprint, key, record, err := s.mutationAuthority(input.MutationID, input.Identity)
	if err != nil {
		return CloseLifecycleResult{}, err
	}
	resolution, err := s.idempotency.Resolve(ctx, key, fingerprint)
	if err != nil {
		return CloseLifecycleResult{}, err
	}
	if resolution.Kind == idempotency.ResolutionConflict {
		return CloseLifecycleResult{}, &idempotency.Error{Code: idempotency.ErrorMutationConflict}
	}
	if resolution.Kind == idempotency.ResolutionReplay {
		return s.verifyCloseResult(ctx, resolution.Result, true)
	}

	view, err := s.packets.Get(ctx, input.Identity.ExpectedPacketID)
	if err != nil {
		return CloseLifecycleResult{}, err
	}
	if view.Summary.SurfaceContract != input.Identity.SurfaceContract {
		return CloseLifecycleResult{}, &Error{Code: CodePacketRouteMismatch}
	}
	if view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || view.Summary.ReplacementPacket != nil || view.Summary.SupersededAt != nil || view.Summary.ClosedAt != nil {
		return CloseLifecycleResult{}, lifecycleViewError(view.Summary)
	}
	packetRow, err := s.store.GetOperationPacketByPacketID(ctx, view.Summary.PacketID)
	if err != nil {
		return CloseLifecycleResult{}, internalFailure()
	}
	artifact, err := s.store.GetOperationPacketArtifact(ctx, packetRow.ID)
	if err != nil {
		return CloseLifecycleResult{}, retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
	}
	stored, replay, err := s.idempotency.RecordSuccess(ctx, record, func(ctx context.Context, tx *workflowstore.Tx) (semanticidentity.ResultIdentity, error) {
		current, err := tx.GetOperationPacketByPacketID(ctx, view.Summary.PacketID)
		if err != nil {
			return nil, err
		}
		if current.SurfaceContractID != string(input.Identity.SurfaceContract) {
			return nil, &Error{Code: CodePacketRouteMismatch}
		}
		if current.LifecycleState != workflowstore.OperationPacketLifecycleActive || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid || current.ClosedAt.Valid {
			return nil, lifecyclePacketError(ctx, tx, current)
		}
		currentArtifact, err := tx.GetOperationPacketArtifact(ctx, current.ID)
		if err != nil || currentArtifact.ID != artifact.ID || currentArtifact.ArtifactID != artifact.ArtifactID || currentArtifact.SHA256 != artifact.SHA256 || currentArtifact.SizeBytes != artifact.SizeBytes || currentArtifact.MediaType != artifact.MediaType || current.PacketSHA256 != currentArtifact.SHA256 {
			return nil, retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
		}
		dependencies, err := tx.ListOperationPacketRetentionDependencies(ctx, current.ID)
		if err != nil {
			return nil, err
		}
		if err := validateRequiredDependencies(dependencies, currentArtifact.ArtifactID); err != nil {
			return nil, err
		}
		closed, err := tx.CloseOperationPacket(ctx, workflowstore.CloseOperationPacketParams{PacketID: current.PacketID, ClosedAt: canonicalTime(s.clock.Now())})
		if err != nil {
			return nil, lifecyclePacketError(ctx, tx, current)
		}
		return semanticidentity.CloseOperationPacketResult{Packet: operationPacketSummaryIdentity(closed, nil), Complete: true}, nil
	})
	if err != nil {
		return CloseLifecycleResult{}, err
	}
	return s.verifyCloseResult(ctx, stored, replay)
}

func (s *LifecycleService) mutationAuthority(mutationID string, identity semanticidentity.RequestIdentity) (semanticidentity.Fingerprint, idempotency.MutationKey, idempotency.RecordSuccessInput, error) {
	fingerprint, err := semanticidentity.BuildFingerprint(identity)
	if err != nil {
		return semanticidentity.Fingerprint{}, idempotency.MutationKey{}, idempotency.RecordSuccessInput{}, &idempotency.Error{Code: idempotency.ErrorInvalidSemanticIdentity}
	}
	key := idempotency.MutationKey{SurfaceContractID: identity.SurfaceContractID(), Tool: identity.MutationTool(), MutationID: mutationID}
	manifest, ok := registry.SurfaceManifestSHA256(key.SurfaceContractID)
	if !ok {
		return semanticidentity.Fingerprint{}, idempotency.MutationKey{}, idempotency.RecordSuccessInput{}, &idempotency.Error{Code: idempotency.ErrorUnknownSurfaceContract}
	}
	return fingerprint, key, idempotency.RecordSuccessInput{Key: key, SurfaceManifestSHA256: manifest, Fingerprint: fingerprint}, nil
}

func (s *LifecycleService) verifyCloseResult(ctx context.Context, stored idempotency.StoredResult, replay bool) (CloseLifecycleResult, error) {
	identity, ok := stored.ResultIdentity.(semanticidentity.CloseOperationPacketResult)
	if !ok {
		return CloseLifecycleResult{}, &idempotency.Error{Code: idempotency.ErrorCorruptStoredResult}
	}
	view, err := s.packets.Get(ctx, identity.Packet.PacketID)
	if err != nil || !packetSummaryMatchesResult(view.Summary, identity.Packet) || view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleClosed {
		return CloseLifecycleResult{}, &idempotency.Error{Code: idempotency.ErrorCorruptStoredResult}
	}
	return CloseLifecycleResult{Packet: view.Summary, Mutation: stored, Replay: replay}, nil
}

func refreshConflictInTx(ctx context.Context, tx *workflowstore.Tx, current workflowstore.OperationPacket) error {
	if current.LifecycleState == workflowstore.OperationPacketLifecycleClosed || current.ClosedAt.Valid {
		return &Error{Code: CodePacketClosed}
	}
	if current.LifecycleState == workflowstore.OperationPacketLifecycleSuperseded || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid {
		replacement, err := tx.GetOperationPacketReplacement(ctx, current.ID)
		if err == nil {
			return &Error{Code: CodePacketRefreshConflict, Replacement: replacementIdentity(replacement)}
		}
		return &Error{Code: CodePacketRefreshConflict}
	}
	return &Error{Code: CodePacketRefreshConflict}
}

func lifecyclePacketError(ctx context.Context, tx *workflowstore.Tx, current workflowstore.OperationPacket) error {
	if current.LifecycleState == workflowstore.OperationPacketLifecycleClosed || current.ClosedAt.Valid {
		return &Error{Code: CodePacketClosed}
	}
	if current.LifecycleState == workflowstore.OperationPacketLifecycleSuperseded || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid {
		replacement, err := tx.GetOperationPacketReplacement(ctx, current.ID)
		if err == nil {
			return &Error{Code: CodePacketSuperseded, Replacement: replacementIdentity(replacement)}
		}
		return &Error{Code: CodePacketSuperseded}
	}
	return &Error{Code: CodePacketNotReady}
}

func lifecycleViewError(value PacketSummary) error {
	if value.LifecycleState == workflowstore.OperationPacketLifecycleClosed || value.ClosedAt != nil {
		return &Error{Code: CodePacketClosed}
	}
	if value.LifecycleState == workflowstore.OperationPacketLifecycleSuperseded || value.ReplacementPacket != nil || value.SupersededAt != nil {
		return &Error{Code: CodePacketSuperseded, Replacement: value.ReplacementPacket}
	}
	return &Error{Code: CodePacketNotReady}
}

func operationPacketViewIdentityFromPublication(value workflowstore.OperationPacket, artifactID string, artifact workflowartifacts.File, replacement *workflowstore.OperationPacket) semanticidentity.OperationPacketViewIdentity {
	return semanticidentity.OperationPacketViewIdentity{
		Summary:  operationPacketSummaryIdentity(value, replacement),
		Document: semanticidentity.PacketDocumentIdentity{ArtifactID: artifactID, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256},
	}
}

func operationPacketViewIdentity(value workflowstore.OperationPacket, artifact workflowstore.OperationPacketArtifact, replacement *workflowstore.OperationPacket) semanticidentity.OperationPacketViewIdentity {
	return semanticidentity.OperationPacketViewIdentity{
		Summary:  operationPacketSummaryIdentity(value, replacement),
		Document: semanticidentity.PacketDocumentIdentity{ArtifactID: artifact.ArtifactID, MediaType: artifact.MediaType, SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256},
	}
}

func operationPacketSummaryIdentity(value workflowstore.OperationPacket, replacement *workflowstore.OperationPacket) semanticidentity.PacketSummaryIdentity {
	result := semanticidentity.PacketSummaryIdentity{
		PacketID:          value.PacketID,
		PacketSHA256:      value.PacketSHA256,
		SchemaVersion:     value.SchemaVersion,
		Role:              value.Role,
		OperationID:       registry.OperationID(value.OperationID),
		SurfaceContractID: registry.SurfaceContractID(value.SurfaceContractID),
		ProjectID:         value.ProjectID,
		ReadinessState:    value.ReadinessState,
		LifecycleState:    value.LifecycleState,
	}
	if value.SupersededAt.Valid {
		copy := value.SupersededAt.String
		result.SupersededAt = &copy
	}
	if value.ClosedAt.Valid {
		copy := value.ClosedAt.String
		result.ClosedAt = &copy
	}
	if replacement != nil {
		result.ReplacementPacket = &semanticidentity.ReplacementPacketIdentity{
			PacketID:          replacement.PacketID,
			PacketSHA256:      replacement.PacketSHA256,
			Role:              replacement.Role,
			OperationID:       registry.OperationID(replacement.OperationID),
			SurfaceContractID: registry.SurfaceContractID(replacement.SurfaceContractID),
		}
	}
	return result
}

func packetViewMatchesResult(value PacketView, expected semanticidentity.OperationPacketViewIdentity) bool {
	return packetSummaryMatchesResult(value.Summary, expected.Summary) &&
		value.DocumentMediaType == expected.Document.MediaType &&
		value.DocumentSizeBytes == expected.Document.SizeBytes &&
		value.Summary.PacketSHA256 == expected.Document.SHA256
}

func packetSummaryMatchesResult(value PacketSummary, expected semanticidentity.PacketSummaryIdentity) bool {
	if value.PacketID != expected.PacketID || value.PacketSHA256 != expected.PacketSHA256 || value.SchemaVersion != expected.SchemaVersion || string(value.Role) != expected.Role || value.OperationID != expected.OperationID || value.SurfaceContract != expected.SurfaceContractID || value.ProjectID != expected.ProjectID || value.ReadinessState != expected.ReadinessState || value.LifecycleState != expected.LifecycleState || !optionalStringEqual(value.SupersededAt, expected.SupersededAt) || !optionalStringEqual(value.ClosedAt, expected.ClosedAt) {
		return false
	}
	if value.ReplacementPacket == nil || expected.ReplacementPacket == nil {
		return value.ReplacementPacket == nil && expected.ReplacementPacket == nil
	}
	return value.ReplacementPacket.PacketID == expected.ReplacementPacket.PacketID && value.ReplacementPacket.PacketSHA256 == expected.ReplacementPacket.PacketSHA256 && string(value.ReplacementPacket.Role) == expected.ReplacementPacket.Role && value.ReplacementPacket.OperationID == expected.ReplacementPacket.OperationID && value.ReplacementPacket.SurfaceContract == expected.ReplacementPacket.SurfaceContractID
}

func optionalStringEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func normalizeLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	var operationErr *Error
	var mutationErr *idempotency.Error
	var fileErr *fileacquisition.Error
	var vaultErr *sourcevault.Error
	if errors.As(err, &operationErr) || errors.As(err, &mutationErr) || errors.As(err, &fileErr) || errors.As(err, &vaultErr) {
		return err
	}
	if errors.Is(err, workflowrepos.ErrRepositoryUnconfigured) || errors.Is(err, workflowrepos.ErrInvalidExplicitCommit) || errors.Is(err, workflowrepos.ErrRepositoryObject) || errors.Is(err, workflowrepos.ErrDirtyProjectWorktree) || errors.Is(err, workflowrepos.ErrGovernanceUnavailable) {
		return &Error{Code: CodeRepositoryAuthorityUnavailable}
	}
	return internalFailure()
}
