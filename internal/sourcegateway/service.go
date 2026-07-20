package sourcegateway

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"relay/internal/app/operations"
	"relay/internal/operations/registry"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type Service struct {
	authorities AuthorityResolver
	vault       VaultReader
	selectors   SelectorStore
	cursors     CursorCodec
}

func NewService(authorities AuthorityResolver, vault VaultReader, selectors SelectorStore, cursors CursorCodec) (*Service, error) {
	if authorities == nil || vault == nil || selectors == nil || cursors == nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	return &Service{authorities: authorities, vault: vault, selectors: selectors, cursors: cursors}, nil
}
func (s *Service) resolveAuthority(ctx context.Context, packetID string, surface registry.SurfaceContractID, operation registry.OperationID, repositoryKey string) (operations.SourceReadAuthority, error) {
	if s == nil || strings.TrimSpace(packetID) != packetID || packetID == "" || surface == "" || operation == "" || strings.TrimSpace(repositoryKey) != repositoryKey || repositoryKey == "" {
		return operations.SourceReadAuthority{}, &Error{Code: CodeInvalidRequest}
	}
	value, err := s.authorities.ResolveSourceReadAuthority(ctx, operations.ResolveSourceReadAuthorityRequest{PacketID: packetID, SurfaceContract: surface, OperationID: operation, RepositoryKey: repositoryKey})
	if err != nil {
		return operations.SourceReadAuthority{}, mapAuthorityError(err)
	}
	if value.Summary.PacketID != packetID || value.Summary.SurfaceContract != surface || value.Summary.OperationID != operation || value.RepositoryKey != repositoryKey || value.PacketRowID <= 0 || value.PublicationID == "" || value.Relationship.ID <= 0 || !validLowerHex(value.Relationship.CommitOID, 40) || !validLowerHex(value.Relationship.TreeOID, 40) {
		return operations.SourceReadAuthority{}, &Error{Code: CodeRetainedAuthorityUnavailable}
	}
	return value, nil
}
func sourceIdentity(value operations.SourceReadAuthority) SourceIdentity {
	return SourceIdentity{PacketID: value.Summary.PacketID, PacketSHA256: value.Summary.PacketSHA256, LifecycleState: value.Summary.LifecycleState, SurfaceContract: value.Summary.SurfaceContract, OperationID: value.Summary.OperationID, ProjectID: value.Summary.ProjectID, RepositoryKey: value.RepositoryKey, DependencyKey: value.DependencyKey, AnchorName: value.AnchorName, PublicationID: value.PublicationID, VaultRelationshipRowID: value.Relationship.ID, CommitOID: value.Relationship.CommitOID, TreeOID: value.Relationship.TreeOID}
}
func (s *Service) makePathIdentity(ctx context.Context, authority operations.SourceReadAuthority, path []byte) (PathIdentity, error) {
	if !validatePath(path, true) {
		return PathIdentity{}, &Error{Code: CodeInvalidRequest}
	}
	digest := pathID(path)
	display, displayValid := displayPath(path)
	result := PathIdentity{Version: PathIdentityVersion, PathID: digest, ByteLength: int64(len(path)), Display: display, DisplayValid: displayValid}
	if len(path) <= MaxInlinePathBytes {
		result.InlineBase64 = canonicalInline(path)
		return result, nil
	}
	selector := selectorID(authority, digest, path)
	stored, err := s.selectors.CreateOrGetSourcePathSelector(ctx, workflowstore.CreateOrGetSourcePathSelectorParams{SelectorID: selector, PacketRowID: authority.PacketRowID, PacketID: authority.Summary.PacketID, SurfaceContractID: string(authority.Summary.SurfaceContract), OperationID: string(authority.Summary.OperationID), ProjectID: authority.Summary.ProjectID, RepositoryKey: authority.RepositoryKey, PublicationID: authority.PublicationID, VaultRelationshipRowID: authority.Relationship.ID, CommitOID: authority.Relationship.CommitOID, TreeOID: authority.Relationship.TreeOID, PathID: digest, PathBytes: append([]byte(nil), path...)})
	if err != nil || !selectorMatchesAuthority(stored, authority, digest, path) {
		return PathIdentity{}, &Error{Code: CodeInternalFailure}
	}
	result.SelectorID = stored.SelectorID
	return result, nil
}
func (s *Service) resolvePathReference(ctx context.Context, authority operations.SourceReadAuthority, reference PathReference, allowRoot bool) ([]byte, error) {
	if reference.PathID == "" && reference.InlineBase64 == "" && reference.SelectorID == "" {
		if allowRoot {
			return []byte{}, nil
		}
		return nil, &Error{Code: CodeInvalidRequest}
	}
	if !validLowerHex(reference.PathID, 64) || (reference.InlineBase64 == "") == (reference.SelectorID == "") {
		return nil, &Error{Code: CodeInvalidSelector}
	}
	if reference.InlineBase64 != "" {
		if len(reference.InlineBase64) > 4*((MaxInlinePathBytes+2)/3) {
			return nil, &Error{Code: CodeInvalidSelector}
		}
		value, ok := decodeCanonicalInline(reference.InlineBase64)
		if !ok || len(value) > MaxInlinePathBytes || !validatePath(value, allowRoot) || pathID(value) != reference.PathID {
			return nil, &Error{Code: CodeInvalidSelector}
		}
		return value, nil
	}
	stored, err := s.selectors.GetSourcePathSelector(ctx, reference.SelectorID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &Error{Code: CodeInvalidSelector}
	}
	if err != nil {
		return nil, &Error{Code: CodeInternalFailure}
	}
	if stored.PathByteLength <= MaxInlinePathBytes || !selectorMatchesAuthority(stored, authority, reference.PathID, stored.PathBytes) || !validatePath(stored.PathBytes, allowRoot) {
		return nil, &Error{Code: CodeInvalidSelector}
	}
	return append([]byte(nil), stored.PathBytes...), nil
}
func selectorMatchesAuthority(value workflowstore.SourcePathSelector, authority operations.SourceReadAuthority, digest string, path []byte) bool {
	return value.SelectorID == selectorID(authority, digest, path) && value.PacketRowID == authority.PacketRowID && value.PacketID == authority.Summary.PacketID && value.SurfaceContractID == string(authority.Summary.SurfaceContract) && value.OperationID == string(authority.Summary.OperationID) && value.ProjectID == authority.Summary.ProjectID && value.RepositoryKey == authority.RepositoryKey && value.PublicationID == authority.PublicationID && value.VaultRelationshipRowID == authority.Relationship.ID && value.CommitOID == authority.Relationship.CommitOID && value.TreeOID == authority.Relationship.TreeOID && value.PathID == digest && value.PathByteLength == int64(len(path)) && string(value.PathBytes) == string(path)
}
func mapAuthorityError(err error) error {
	switch operations.ErrorCode(err) {
	case operations.CodePacketNotFound, operations.CodePacketNotReady:
		return &Error{Code: CodePacketUnavailable}
	case operations.CodePacketRouteMismatch:
		return &Error{Code: CodeRouteMismatch}
	case operations.CodeRepositoryAuthorityUnavailable:
		return &Error{Code: CodeRepositoryUnavailable}
	case operations.CodeRetainedAuthorityUnavailable, operations.CodePacketArtifactMismatch, operations.CodeAuthorityPublicationConflict, operations.CodeAuthorityPublicationFailure:
		return &Error{Code: CodeRetainedAuthorityUnavailable}
	default:
		return &Error{Code: CodeInternalFailure}
	}
}
func mapVaultError(err error) error {
	switch sourcevault.ErrorCode(err) {
	case sourcevault.CodeInvalidRequest:
		return &Error{Code: CodeInvalidRequest}
	case sourcevault.CodeVaultUnavailable, sourcevault.CodeRetentionConflict, sourcevault.CodeStateConflict, sourcevault.CodeCleanupBlocked:
		return &Error{Code: CodeRetainedAuthorityUnavailable}
	case sourcevault.CodeObjectMismatch:
		return &Error{Code: CodeObjectMismatch}
	case sourcevault.CodeObjectUnavailable, sourcevault.CodeSourceObjectUnavailable:
		return &Error{Code: CodeObjectUnavailable}
	default:
		return &Error{Code: CodeInternalFailure}
	}
}
func cursorMatchesAuthority(value cursorPayload, authority operations.SourceReadAuthority, fingerprint, kind string) bool {
	return value.Version == CursorVersion && value.Kind == kind && value.PacketID == authority.Summary.PacketID && value.PacketSHA256 == authority.Summary.PacketSHA256 && value.SurfaceContract == string(authority.Summary.SurfaceContract) && value.OperationID == string(authority.Summary.OperationID) && value.ProjectID == authority.Summary.ProjectID && value.RepositoryKey == authority.RepositoryKey && value.PublicationID == authority.PublicationID && value.VaultRelationshipRowID == authority.Relationship.ID && value.CommitOID == authority.Relationship.CommitOID && value.TreeOID == authority.Relationship.TreeOID && value.RequestFingerprint == fingerprint
}
func treeFingerprint(authority operations.SourceReadAuthority, directoryID string, recursive bool, limit int) string {
	return requestFingerprint("tree", authority.Summary.PacketID, string(authority.Summary.SurfaceContract), string(authority.Summary.OperationID), authority.Summary.ProjectID, authority.RepositoryKey, authority.PublicationID, strconv.FormatInt(authority.Relationship.ID, 10), authority.Relationship.CommitOID, authority.Relationship.TreeOID, directoryID, strconv.FormatBool(recursive), strconv.Itoa(limit))
}
func blobFingerprint(authority operations.SourceReadAuthority, pathDigest string, offset, limit int64) string {
	return requestFingerprint("blob", authority.Summary.PacketID, string(authority.Summary.SurfaceContract), string(authority.Summary.OperationID), authority.Summary.ProjectID, authority.RepositoryKey, authority.PublicationID, strconv.FormatInt(authority.Relationship.ID, 10), authority.Relationship.CommitOID, authority.Relationship.TreeOID, pathDigest, strconv.FormatInt(offset, 10), strconv.FormatInt(limit, 10))
}
