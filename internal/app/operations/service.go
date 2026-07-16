package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	workflowartifacts "relay/internal/artifacts/workflow"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type defaultIDGenerator struct{}

func (defaultIDGenerator) PacketID() string   { return workflowstore.NewOperationPacketID() }
func (defaultIDGenerator) ArtifactID() string { return workflowstore.NewArtifactID() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
	clock Clock
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithDependencies(store, defaultIDGenerator{}, systemClock{})
}
func NewServiceWithDependencies(store *workflowstore.Store, ids IDGenerator, clock Clock) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	if ids == nil {
		return nil, fmt.Errorf("operation packet ID generator is required")
	}
	if clock == nil {
		return nil, fmt.Errorf("operation packet clock is required")
	}
	return &Service{store: store, ids: ids, clock: clock}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (PacketView, error) {
	if input.Document.PriorPacket != nil {
		return PacketView{}, &Error{Code: CodeInvalidPacketDocument}
	}
	prepared, err := s.prepare(input.Document, nil)
	if err != nil {
		return PacketView{}, err
	}
	batch, staged, err := s.stage(prepared)
	if err != nil {
		return PacketView{}, internalFailure()
	}
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(ctx, workflowstore.CreateOperationPacketArtifactParams{ArtifactID: prepared.ArtifactID, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		if err != nil {
			return err
		}
		document := prepared.Snapshot.Document()
		created, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{PacketID: prepared.PacketID, PacketSHA256: prepared.Snapshot.SHA256(), SchemaVersion: packet.SchemaVersion, Role: string(document.Role), OperationID: string(document.OperationID), SurfaceContractID: string(document.SurfaceContract), ProjectID: document.Project.ProjectID, ReadinessState: packet.ReadinessReady, CreatedAt: document.CreatedAt, PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{PacketRowID: created.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}})
		return err
	})
	if err != nil {
		return PacketView{}, internalFailure()
	}
	return s.Get(ctx, prepared.PacketID)
}

func (s *Service) Refresh(ctx context.Context, input RefreshInput) (PacketView, error) {
	priorID := strings.TrimSpace(input.PriorPacketID)
	if priorID == "" {
		return PacketView{}, &Error{Code: CodePacketNotFound}
	}
	prior, err := s.loadPacket(ctx, priorID)
	if err != nil {
		return PacketView{}, err
	}
	if err := lifecycleMutationError(ctx, s.store, prior); err != nil {
		return PacketView{}, err
	}
	priorArtifact, _, err := s.loadVerifiedPacketDocument(ctx, prior)
	if err != nil {
		return PacketView{}, err
	}
	prepared, err := s.prepare(input.Document, &packet.PriorPacketIdentity{PacketID: prior.PacketID, PacketSHA256: prior.PacketSHA256})
	if err != nil {
		return PacketView{}, err
	}
	document := prepared.Snapshot.Document()
	if document.Role != registry.Role(prior.Role) || document.OperationID != registry.OperationID(prior.OperationID) || document.SurfaceContract != registry.SurfaceContractID(prior.SurfaceContractID) || document.Project.ProjectID != prior.ProjectID {
		return PacketView{}, &Error{Code: CodePacketRouteMismatch}
	}
	batch, staged, err := s.stage(prepared)
	if err != nil {
		return PacketView{}, internalFailure()
	}
	var authorityErr error
	err = s.store.CommitArtifactBatch(ctx, batch, func(tx *workflowstore.Tx) error {
		current, err := tx.GetOperationPacketByPacketID(ctx, priorID)
		if err != nil {
			return err
		}
		if current.LifecycleState != workflowstore.OperationPacketLifecycleActive || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid || current.ClosedAt.Valid {
			return sql.ErrNoRows
		}
		dependencies, err := tx.ListOperationPacketRetentionDependencies(ctx, current.ID)
		if err != nil {
			return err
		}
		if err := validateRequiredDependencies(dependencies, priorArtifact.ArtifactID); err != nil {
			authorityErr = err
			return err
		}
		artifact, err := tx.CreateOperationPacketArtifact(ctx, workflowstore.CreateOperationPacketArtifactParams{ArtifactID: prepared.ArtifactID, Kind: staged.Kind, RelativePath: staged.RelativePath, MediaType: staged.MediaType, SHA256: staged.SHA256, SizeBytes: staged.SizeBytes})
		if err != nil {
			return err
		}
		replacement, err := tx.CreateOperationPacket(ctx, workflowstore.CreateOperationPacketParams{PacketID: prepared.PacketID, PacketSHA256: prepared.Snapshot.SHA256(), SchemaVersion: packet.SchemaVersion, Role: string(document.Role), OperationID: string(document.OperationID), SurfaceContractID: string(document.SurfaceContract), ProjectID: document.Project.ProjectID, ReadinessState: packet.ReadinessReady, PriorPacketRowID: sql.NullInt64{Int64: current.ID, Valid: true}, CreatedAt: document.CreatedAt, PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		if _, err := tx.AttachOperationPacketDependency(ctx, workflowstore.AttachOperationPacketDependencyParams{PacketRowID: replacement.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}}); err != nil {
			return err
		}
		_, err = tx.SupersedeOperationPacket(ctx, workflowstore.SupersedeOperationPacketParams{PacketID: current.PacketID, ReplacementPacketRowID: replacement.ID, SupersededAt: document.CreatedAt})
		return err
	})
	if err != nil {
		if authorityErr != nil {
			return PacketView{}, authorityErr
		}
		return PacketView{}, s.refreshFailure(ctx, priorID)
	}
	return s.Get(ctx, prepared.PacketID)
}

func (s *Service) Close(ctx context.Context, input CloseInput) (PacketSummary, error) {
	packetID := strings.TrimSpace(input.PacketID)
	if packetID == "" {
		return PacketSummary{}, &Error{Code: CodePacketNotFound}
	}
	current, err := s.loadPacket(ctx, packetID)
	if err != nil {
		return PacketSummary{}, err
	}
	if err := lifecycleMutationError(ctx, s.store, current); err != nil {
		return PacketSummary{}, err
	}
	artifact, _, err := s.loadVerifiedPacketDocument(ctx, current)
	if err != nil {
		return PacketSummary{}, err
	}
	closedAt := canonicalTime(s.clock.Now())
	var authorityErr error
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetOperationPacketByPacketID(ctx, packetID)
		if err != nil {
			return err
		}
		if current.LifecycleState != workflowstore.OperationPacketLifecycleActive || current.ReplacementPacketRowID.Valid || current.SupersededAt.Valid || current.ClosedAt.Valid {
			return sql.ErrNoRows
		}
		dependencies, err := tx.ListOperationPacketRetentionDependencies(ctx, current.ID)
		if err != nil {
			return err
		}
		if err := validateRequiredDependencies(dependencies, artifact.ArtifactID); err != nil {
			authorityErr = err
			return err
		}
		_, err = tx.CloseOperationPacket(ctx, workflowstore.CloseOperationPacketParams{PacketID: packetID, ClosedAt: closedAt})
		return err
	})
	if err != nil {
		if authorityErr != nil {
			return PacketSummary{}, authorityErr
		}
		current, loadErr := s.loadPacket(ctx, packetID)
		if loadErr != nil {
			return PacketSummary{}, loadErr
		}
		if lifecycleErr := lifecycleMutationError(ctx, s.store, current); lifecycleErr != nil {
			return PacketSummary{}, lifecycleErr
		}
		return PacketSummary{}, internalFailure()
	}
	view, err := s.Get(ctx, packetID)
	return view.Summary, err
}

func (s *Service) Get(ctx context.Context, packetID string) (PacketView, error) {
	value, err := s.loadPacket(ctx, strings.TrimSpace(packetID))
	if err != nil {
		return PacketView{}, err
	}
	if value.ReadinessState != workflowstore.OperationPacketReadinessReady {
		return PacketView{}, &Error{Code: CodePacketNotReady}
	}
	artifact, data, err := s.loadVerifiedPacketDocument(ctx, value)
	if err != nil {
		return PacketView{}, err
	}
	replacement, err := s.replacement(ctx, value)
	if err != nil {
		return PacketView{}, internalFailure()
	}
	return PacketView{Summary: summary(value, replacement), DocumentMediaType: artifact.MediaType, DocumentSizeBytes: artifact.SizeBytes, DocumentBytes: append([]byte(nil), data...)}, nil
}

func (s *Service) AuthorizeMutation(ctx context.Context, request MutationRequest) (MutationAuthorization, error) {
	value, err := s.loadPacket(ctx, strings.TrimSpace(request.PacketID))
	if err != nil {
		return MutationAuthorization{}, err
	}
	if err := lifecycleMutationError(ctx, s.store, value); err != nil {
		return MutationAuthorization{}, err
	}
	if value.ReadinessState != workflowstore.OperationPacketReadinessReady {
		return MutationAuthorization{}, &Error{Code: CodePacketNotReady}
	}
	if _, _, err := s.loadVerifiedPacketDocument(ctx, value); err != nil {
		return MutationAuthorization{}, err
	}
	if request.SurfaceContract != registry.SurfaceContractID(value.SurfaceContractID) || request.OperationID != registry.OperationID(value.OperationID) {
		return MutationAuthorization{}, &Error{Code: CodePacketRouteMismatch}
	}
	operation, ok := registry.Lookup(registry.OperationID(value.OperationID))
	if !ok || !containsAction(operation.AllowedNonSourceActions, request.Action) {
		return MutationAuthorization{}, &Error{Code: CodePacketActionNotAllowed}
	}
	for _, dependency := range request.RequiredDependencies {
		if _, err := s.authorizeDependency(ctx, value, dependency.Class, dependency.Key); err != nil {
			return MutationAuthorization{}, err
		}
	}
	replacement, err := s.replacement(ctx, value)
	if err != nil {
		return MutationAuthorization{}, internalFailure()
	}
	return MutationAuthorization{Summary: summary(value, replacement), Allowed: true}, nil
}
func (s *Service) AuthorizeRead(ctx context.Context, request ReadRequest) (ReadAuthorization, error) {
	value, err := s.loadPacket(ctx, strings.TrimSpace(request.PacketID))
	if err != nil {
		return ReadAuthorization{}, err
	}
	if _, _, err := s.loadVerifiedPacketDocument(ctx, value); err != nil {
		return ReadAuthorization{}, err
	}
	dependency, err := s.authorizeDependency(ctx, value, request.DependencyClass, request.DependencyKey)
	if err != nil {
		return ReadAuthorization{}, err
	}
	replacement, err := s.replacement(ctx, value)
	if err != nil {
		return ReadAuthorization{}, internalFailure()
	}
	return ReadAuthorization{Summary: summary(value, replacement), DependencyClass: dependency.DependencyClass, DependencyKey: dependency.DependencyKey, OwnerIdentity: dependency.OwnerIdentity.String}, nil
}

func (s *Service) prepare(document packet.Document, prior *packet.PriorPacketIdentity) (PreparedPacket, error) {
	document.CreatedAt = canonicalTime(s.clock.Now())
	if prior == nil {
		document.PriorPacket = nil
	} else {
		copy := *prior
		document.PriorPacket = &copy
	}
	snapshot, err := packet.NewSnapshot(document)
	if err != nil {
		return PreparedPacket{}, &Error{Code: CodeInvalidPacketDocument}
	}
	return PreparedPacket{PacketID: s.ids.PacketID(), ArtifactID: s.ids.ArtifactID(), Snapshot: snapshot}, nil
}
func (s *Service) stage(prepared PreparedPacket) (*workflowartifacts.Batch, workflowartifacts.File, error) {
	batch, err := s.store.ArtifactStore().Begin("operation-packets/" + prepared.PacketID)
	if err != nil {
		return nil, workflowartifacts.File{}, err
	}
	staged, err := batch.Stage("operation_packet_document", "operation-packet.json", packet.MediaType, prepared.Snapshot.Bytes())
	if err != nil {
		_ = batch.Rollback()
		return nil, workflowartifacts.File{}, err
	}
	if staged.SHA256 != prepared.Snapshot.SHA256() || staged.SizeBytes != prepared.Snapshot.SizeBytes() {
		_ = batch.Rollback()
		return nil, workflowartifacts.File{}, fmt.Errorf("staged operation packet identity mismatch")
	}
	return batch, staged, nil
}
func (s *Service) loadPacket(ctx context.Context, packetID string) (workflowstore.OperationPacket, error) {
	if packetID == "" {
		return workflowstore.OperationPacket{}, &Error{Code: CodePacketNotFound}
	}
	value, err := s.store.GetOperationPacketByPacketID(ctx, packetID)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.OperationPacket{}, &Error{Code: CodePacketNotFound}
	}
	if err != nil {
		return workflowstore.OperationPacket{}, internalFailure()
	}
	return value, nil
}
func (s *Service) replacement(ctx context.Context, value workflowstore.OperationPacket) (*ReplacementPacketIdentity, error) {
	if !value.ReplacementPacketRowID.Valid {
		return nil, nil
	}
	replacement, err := s.store.GetOperationPacketReplacement(ctx, value.ID)
	if err != nil {
		return nil, err
	}
	return replacementIdentity(replacement), nil
}
func (s *Service) authorizeDependency(ctx context.Context, packetValue workflowstore.OperationPacket, class, key string) (workflowstore.OperationPacketRetentionDependency, error) {
	if class == "" || key == "" {
		return workflowstore.OperationPacketRetentionDependency{}, retainedAuthorityError(class)
	}
	value, err := s.store.GetOperationPacketRetentionDependency(ctx, packetValue.ID, class, key)
	if err != nil || !value.Required || !value.Attached || !value.Retained || !value.OwnerIdentity.Valid || value.OwnerIdentity.String == "" {
		return workflowstore.OperationPacketRetentionDependency{}, retainedAuthorityError(class)
	}
	return value, nil
}
func (s *Service) loadVerifiedPacketDocument(ctx context.Context, value workflowstore.OperationPacket) (workflowstore.OperationPacketArtifact, []byte, error) {
	artifact, err := s.store.GetOperationPacketArtifact(ctx, value.ID)
	if err != nil {
		return workflowstore.OperationPacketArtifact{}, nil, retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
	}
	dependencies, err := s.store.ListOperationPacketRetentionDependencies(ctx, value.ID)
	if err != nil {
		return workflowstore.OperationPacketArtifact{}, nil, internalFailure()
	}
	if err := validateRequiredDependencies(dependencies, artifact.ArtifactID); err != nil {
		return workflowstore.OperationPacketArtifact{}, nil, err
	}
	data, err := s.readArtifact(artifact)
	if err != nil {
		return workflowstore.OperationPacketArtifact{}, nil, &Error{Code: CodePacketArtifactMismatch}
	}
	if artifact.SHA256 != value.PacketSHA256 || artifact.MediaType != packet.MediaType || int64(len(data)) != artifact.SizeBytes {
		return workflowstore.OperationPacketArtifact{}, nil, &Error{Code: CodePacketArtifactMismatch}
	}
	return artifact, data, nil
}

func validateRequiredDependencies(values []workflowstore.OperationPacketRetentionDependency, packetArtifactID string) error {
	packetDocumentFound := false
	for _, value := range values {
		if value.DependencyClass == workflowstore.OperationPacketDependencyPacketDocument && value.DependencyKey == packetArtifactID {
			packetDocumentFound = true
			if !value.Required || !value.Attached || !value.Retained || !value.OwnerIdentity.Valid || value.OwnerIdentity.String != packetArtifactID {
				return retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
			}
		}
		if value.Required && (!value.Attached || !value.Retained || !value.OwnerIdentity.Valid || strings.TrimSpace(value.OwnerIdentity.String) == "") {
			return retainedAuthorityError(value.DependencyClass)
		}
	}
	if !packetDocumentFound {
		return retainedAuthorityError(workflowstore.OperationPacketDependencyPacketDocument)
	}
	return nil
}
func (s *Service) readArtifact(artifact workflowstore.OperationPacketArtifact) ([]byte, error) {
	root := s.store.ArtifactStore().Root()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(artifact.RelativePath)))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact path escapes root")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := packet.VerifyBytes(data, artifact.SHA256, artifact.SizeBytes); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != artifact.SHA256 {
		return nil, fmt.Errorf("artifact digest mismatch")
	}
	return data, nil
}
func (s *Service) refreshFailure(ctx context.Context, priorID string) error {
	current, err := s.loadPacket(ctx, priorID)
	if err != nil {
		return err
	}
	if current.LifecycleState == workflowstore.OperationPacketLifecycleSuperseded {
		replacement, replacementErr := s.replacement(ctx, current)
		if replacementErr != nil {
			return internalFailure()
		}
		return &Error{Code: CodePacketRefreshConflict, Replacement: replacement}
	}
	if current.LifecycleState == workflowstore.OperationPacketLifecycleClosed {
		return &Error{Code: CodePacketClosed}
	}
	return &Error{Code: CodePacketRefreshConflict}
}
func lifecycleMutationError(ctx context.Context, store *workflowstore.Store, value workflowstore.OperationPacket) error {
	switch value.LifecycleState {
	case workflowstore.OperationPacketLifecycleActive:
		return nil
	case workflowstore.OperationPacketLifecycleSuperseded:
		replacement, err := store.GetOperationPacketReplacement(ctx, value.ID)
		if err != nil {
			return internalFailure()
		}
		return &Error{Code: CodePacketSuperseded, Replacement: replacementIdentity(replacement)}
	case workflowstore.OperationPacketLifecycleClosed:
		return &Error{Code: CodePacketClosed}
	default:
		return &Error{Code: CodePacketNotReady}
	}
}
func replacementIdentity(value workflowstore.OperationPacketReplacement) *ReplacementPacketIdentity {
	return &ReplacementPacketIdentity{PacketID: value.PacketID, PacketSHA256: value.PacketSHA256, Role: registry.Role(value.Role), OperationID: registry.OperationID(value.OperationID), SurfaceContract: registry.SurfaceContractID(value.SurfaceContractID)}
}
func summary(value workflowstore.OperationPacket, replacement *ReplacementPacketIdentity) PacketSummary {
	result := PacketSummary{PacketID: value.PacketID, PacketSHA256: value.PacketSHA256, SchemaVersion: value.SchemaVersion, Role: registry.Role(value.Role), OperationID: registry.OperationID(value.OperationID), SurfaceContract: registry.SurfaceContractID(value.SurfaceContractID), ProjectID: value.ProjectID, ReadinessState: value.ReadinessState, LifecycleState: value.LifecycleState, ReplacementPacket: replacement}
	if value.SupersededAt.Valid {
		v := value.SupersededAt.String
		result.SupersededAt = &v
	}
	if value.ClosedAt.Valid {
		v := value.ClosedAt.String
		result.ClosedAt = &v
	}
	return result
}
func containsAction(values []registry.AllowedAction, target registry.AllowedAction) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func canonicalTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
func retainedAuthorityError(class string) error {
	return &Error{Code: CodeRetainedAuthorityUnavailable, MissingDependencyClass: class}
}
func internalFailure() error { return &Error{Code: CodeInternalFailure} }
