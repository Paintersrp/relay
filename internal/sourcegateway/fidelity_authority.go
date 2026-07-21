package sourcegateway

import (
	"context"
	"strconv"
	"strings"

	"relay/internal/app/operations"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func (s *Service) resolveRevisionAuthority(ctx context.Context, packetID string, surface registry.SurfaceContractID, operation registry.OperationID, repositoryKey string, revision RevisionReference) (operations.SourceReadAuthority, error) {
	anchorName := strings.TrimSpace(revision.AnchorName)
	if anchorName != revision.AnchorName || s == nil || strings.TrimSpace(packetID) != packetID || packetID == "" || surface == "" || operation == "" || strings.TrimSpace(repositoryKey) != repositoryKey || repositoryKey == "" {
		return operations.SourceReadAuthority{}, &Error{Code: CodeInvalidRequest}
	}
	value, err := s.authorities.ResolveSourceReadAuthority(ctx, operations.ResolveSourceReadAuthorityRequest{PacketID: packetID, SurfaceContract: surface, OperationID: operation, RepositoryKey: repositoryKey, AnchorName: anchorName})
	if err != nil {
		return operations.SourceReadAuthority{}, mapAuthorityError(err)
	}
	expected := "repository:" + repositoryKey + ":primary"
	if anchorName != "" {
		expected = "repository:" + repositoryKey + ":anchor:" + anchorName
	}
	if value.Summary.PacketID != packetID || value.Summary.SurfaceContract != surface || value.Summary.OperationID != operation || value.RepositoryKey != repositoryKey || value.PacketRowID <= 0 || value.PublicationID == "" || value.DependencyKey != expected || value.AnchorName != anchorName || value.Relationship.ID <= 0 || value.Relationship.DependencyClass != workflowstore.OperationPacketDependencyRepositoryVault || value.Relationship.DependencyKey != expected || !validLowerHex(value.Relationship.CommitOID, 40) || !validLowerHex(value.Relationship.TreeOID, 40) {
		return operations.SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	return value, nil
}

func (s *Service) fidelityVault() (FidelityVaultReader, error) {
	if s == nil || s.vault == nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	value, ok := s.vault.(FidelityVaultReader)
	if !ok {
		return nil, &Error{Code: CodeInternalFailure}
	}
	return value, nil
}

func fidelitySourceIdentity(value operations.SourceReadAuthority) SourceIdentity {
	return SourceIdentity{PacketID: value.Summary.PacketID, PacketSHA256: value.Summary.PacketSHA256, LifecycleState: value.Summary.LifecycleState, SurfaceContract: value.Summary.SurfaceContract, OperationID: value.Summary.OperationID, ProjectID: value.Summary.ProjectID, RepositoryKey: value.RepositoryKey, DependencyKey: value.DependencyKey, AnchorName: value.AnchorName, PublicationID: value.PublicationID, VaultRelationshipRowID: value.Relationship.ID, CommitOID: value.Relationship.CommitOID, TreeOID: value.Relationship.TreeOID}
}
func revisionFingerprint(value operations.SourceReadAuthority) []string {
	return []string{value.Summary.PacketID, value.Summary.PacketSHA256, string(value.Summary.SurfaceContract), string(value.Summary.OperationID), value.Summary.ProjectID, strconv.FormatInt(value.PacketRowID, 10), value.RepositoryKey, value.DependencyKey, value.AnchorName, value.PublicationID, strconv.FormatInt(value.Relationship.ID, 10), value.Relationship.CommitOID, value.Relationship.TreeOID}
}
func pairFingerprint(before, after operations.SourceReadAuthority, values ...string) string {
	parts := append([]string{}, revisionFingerprint(before)...)
	parts = append(parts, revisionFingerprint(after)...)
	parts = append(parts, values...)
	return requestFingerprint(parts...)
}
