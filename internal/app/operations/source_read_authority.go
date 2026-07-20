package operations

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type ResolveSourceReadAuthorityRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	AnchorName      string
}

type SourceReadAuthority struct {
	Summary       PacketSummary
	PacketRowID   int64
	PublicationID string
	RepositoryKey string
	DependencyKey string
	AnchorName    string
	Relationship  workflowstore.OperationPacketVaultRelationship
}

func (s *Service) ResolveSourceReadAuthority(ctx context.Context, request ResolveSourceReadAuthorityRequest) (SourceReadAuthority, error) {
	packetID := strings.TrimSpace(request.PacketID)
	repositoryKey := strings.TrimSpace(request.RepositoryKey)
	anchorName := strings.TrimSpace(request.AnchorName)
	if s == nil || packetID == "" || packetID != request.PacketID || repositoryKey == "" || repositoryKey != request.RepositoryKey || anchorName != request.AnchorName || request.SurfaceContract == "" || request.OperationID == "" {
		return SourceReadAuthority{}, &Error{Code: CodeRepositoryAuthorityUnavailable}
	}
	packet, err := s.Get(ctx, packetID)
	if err != nil {
		return SourceReadAuthority{}, err
	}
	if packet.Summary.SurfaceContract != request.SurfaceContract || packet.Summary.OperationID != request.OperationID {
		return SourceReadAuthority{}, &Error{Code: CodePacketRouteMismatch}
	}
	publication, err := s.store.GetOperationPacketPublicationByPacketID(ctx, packetID)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceReadAuthority{}, &Error{Code: CodeRepositoryAuthorityUnavailable}
	}
	if err != nil {
		return SourceReadAuthority{}, &Error{Code: CodeInternalFailure}
	}
	integrity, err := s.store.GetOperationPacketPublicationIntegrity(ctx, publication.PublicationID)
	if err != nil {
		return SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	verified, err := s.store.ArtifactStore().VerifyPublication(publication.PublicationID)
	if err != nil || verified.ManifestSHA256 != integrity.Publication.ManifestSHA256 || verified.Manifest.Namespace != integrity.Publication.Namespace || verifyPublicationIntegrity(verified.Manifest, integrity) != nil {
		return SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	if integrity.Packet.PacketID != packetID || integrity.Packet.ID != publication.PacketRowID || integrity.Publication.PublicationID != publication.PublicationID || integrity.Publication.State != workflowstore.OperationPacketPublicationStateCommitted {
		return SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	dependencyKey := sourceReadDependencyKey(repositoryKey, anchorName)
	relationship, ok := oneSourceRelationship(integrity.VaultRelationships, dependencyKey)
	if !ok || relationship.PacketRowID != integrity.Packet.ID || relationship.PublicationID != publication.PublicationID {
		return SourceReadAuthority{}, &Error{Code: CodeRepositoryAuthorityUnavailable}
	}
	if !matchingRetainedDependency(integrity.Dependencies, dependencyKey, relationship.OwnerIdentity) {
		return SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	return SourceReadAuthority{Summary: packet.Summary, PacketRowID: integrity.Packet.ID, PublicationID: publication.PublicationID, RepositoryKey: repositoryKey, DependencyKey: dependencyKey, AnchorName: anchorName, Relationship: relationship}, nil
}

func sourceReadDependencyKey(repositoryKey, anchorName string) string {
	if anchorName == "" {
		return "repository:" + repositoryKey + ":primary"
	}
	return "repository:" + repositoryKey + ":anchor:" + anchorName
}

func oneSourceRelationship(values []workflowstore.OperationPacketVaultRelationship, dependencyKey string) (workflowstore.OperationPacketVaultRelationship, bool) {
	var result workflowstore.OperationPacketVaultRelationship
	found := false
	for _, value := range values {
		if value.DependencyClass != workflowstore.OperationPacketDependencyRepositoryVault || value.DependencyKey != dependencyKey {
			continue
		}
		if found {
			return workflowstore.OperationPacketVaultRelationship{}, false
		}
		result = value
		found = true
	}
	return result, found
}

func matchingRetainedDependency(values []workflowstore.OperationPacketRetentionDependency, dependencyKey, ownerIdentity string) bool {
	found := false
	for _, value := range values {
		if value.DependencyClass != workflowstore.OperationPacketDependencyRepositoryVault || value.DependencyKey != dependencyKey {
			continue
		}
		if found || !value.Required || !value.Attached || !value.Retained || !value.OwnerIdentity.Valid || value.OwnerIdentity.String != ownerIdentity {
			return false
		}
		found = true
	}
	return found
}
