package sourcegateway

import (
	"context"

	"relay/internal/sourcevault"
)

func (s *Service) ReadBlob(ctx context.Context, request ReadBlobRequest) (ReadBlobResult, error) {
	if request.Offset < 0 || request.Limit <= 0 || request.Limit > MaxBlobPageBytes {
		return ReadBlobResult{}, &Error{Code: CodeInvalidRange}
	}
	authority, err := s.resolveAuthority(ctx, request.PacketID, request.SurfaceContract, request.OperationID, request.RepositoryKey)
	if err != nil {
		return ReadBlobResult{}, err
	}
	path, err := s.resolvePathReference(ctx, authority, request.Path, false)
	if err != nil {
		return ReadBlobResult{}, err
	}
	identity, err := s.makePathIdentity(ctx, authority, path)
	if err != nil {
		return ReadBlobResult{}, err
	}
	fingerprint := blobFingerprint(authority, identity.PathID, request.Offset, request.Limit)
	actualOffset := request.Offset
	if request.Cursor != "" {
		cursor, decodeErr := s.cursors.Decode(request.Cursor)
		if decodeErr != nil || !cursorMatchesAuthority(cursor, authority, fingerprint, "blob") || cursor.AfterPath.PathID != "" || cursor.NextOffset < request.Offset {
			return ReadBlobResult{}, &Error{Code: CodeInvalidCursor}
		}
		actualOffset = cursor.NextOffset
	}
	entry, err := s.resolvePathEntry(ctx, authority, path)
	if err != nil {
		return ReadBlobResult{}, err
	}
	if entry.ObjectType != "blob" {
		return ReadBlobResult{}, &Error{Code: CodeObjectMismatch}
	}
	page, err := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: authority.Relationship, BlobOID: entry.ObjectOID, Offset: actualOffset, Limit: request.Limit})
	if err != nil {
		mapped := mapVaultError(err)
		if ErrorCode(mapped) == CodeInvalidRequest {
			mapped = &Error{Code: CodeInvalidRange}
		}
		return ReadBlobResult{}, mapped
	}
	if page.BlobOID != entry.ObjectOID || page.Offset != actualOffset || page.TotalSize < 0 || int64(len(page.Bytes)) > request.Limit || page.Offset+int64(len(page.Bytes)) > page.TotalSize {
		return ReadBlobResult{}, &Error{Code: CodeObjectMismatch}
	}
	complete := page.Offset+int64(len(page.Bytes)) == page.TotalSize
	result := ReadBlobResult{Source: sourceIdentity(authority), Path: identity, Mode: entry.Mode, ObjectType: entry.ObjectType, ObjectOID: entry.ObjectOID, Offset: page.Offset, ReturnedLength: int64(len(page.Bytes)), TotalSize: page.TotalSize, Bytes: append([]byte(nil), page.Bytes...), Complete: complete}
	if !complete {
		result.Cursor, err = s.cursors.Encode(cursorPayload{Version: CursorVersion, Kind: "blob", PacketID: authority.Summary.PacketID, PacketSHA256: authority.Summary.PacketSHA256, SurfaceContract: string(authority.Summary.SurfaceContract), OperationID: string(authority.Summary.OperationID), ProjectID: authority.Summary.ProjectID, RepositoryKey: authority.RepositoryKey, PublicationID: authority.PublicationID, VaultRelationshipRowID: authority.Relationship.ID, CommitOID: authority.Relationship.CommitOID, TreeOID: authority.Relationship.TreeOID, RequestFingerprint: fingerprint, NextOffset: page.Offset + int64(len(page.Bytes))})
		if err != nil {
			return ReadBlobResult{}, err
		}
	}
	return result, nil
}
