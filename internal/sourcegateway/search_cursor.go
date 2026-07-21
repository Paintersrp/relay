package sourcegateway

import (
	"context"
	"strconv"

	"relay/internal/app/operations"
)

func searchFingerprint(authority operations.SourceReadAuthority, mode SearchMode, literal []byte, prefixes []canonicalSearchPrefix, budget SearchBudget) string {
	parts := append([]string{}, revisionFingerprint(authority)...)
	parts = append(parts, "search", SearchOrderVersion, string(mode), string(literal))
	for _, prefix := range prefixes {
		parts = append(parts, string(prefix.bytes))
	}
	parts = append(parts, strconv.FormatInt(budget.ExaminedObjects, 10), strconv.FormatInt(budget.ExaminedBytes, 10))
	return requestFingerprint(parts...)
}

func searchCursorPayload(authority operations.SourceReadAuthority, fingerprint string, identity PathIdentity, blobOID string, phase searchPhase, nextOffset, ordinal, totalSize int64, totalSizeKnown bool) cursorPayload {
	value := fidelityCursorBase(authority, "search", fingerprint)
	value.AfterPath = referenceFromIdentity(identity)
	value.PathID = identity.PathID
	value.ObjectOID = blobOID
	value.NextOffset = nextOffset
	value.NextIndex = ordinal
	value.SearchPhase = phase
	value.SearchObjectSize = totalSize
	value.SearchObjectSizeKnown = totalSizeKnown
	return value
}

func (s *Service) decodeSearchCursor(ctx context.Context, authority operations.SourceReadAuthority, fingerprint, token string) (searchResume, error) {
	value, err := s.cursors.Decode(token)
	if err != nil || !fidelityCursorMatches(value, authority, "search", fingerprint) || !validSearchPhase(value.SearchPhase) || value.AfterPath.PathID == "" || value.PathID == "" || value.PathID != value.AfterPath.PathID || value.ObjectOID == "" || value.NextOffset < 0 || value.NextIndex < 0 || value.LastCommitOID != "" || value.LastEntryID != "" || value.TextLineOpen {
		return searchResume{}, &Error{Code: CodeInvalidCursor}
	}
	path, err := s.resolvePathReference(ctx, authority, value.AfterPath, false)
	if err != nil || pathID(path) != value.PathID {
		return searchResume{}, &Error{Code: CodeInvalidCursor}
	}
	if value.SearchObjectSizeKnown && value.NextOffset > value.SearchObjectSize {
		return searchResume{}, &Error{Code: CodeInvalidCursor}
	}
	if value.SearchPhase == searchPhaseTextValidation && value.NextIndex != 0 {
		return searchResume{}, &Error{Code: CodeInvalidCursor}
	}
	if value.SearchPhase == searchPhaseLiteralScan && value.NextIndex > value.NextOffset {
		return searchResume{}, &Error{Code: CodeInvalidCursor}
	}
	return searchResume{path: path, pathID: value.PathID, blobOID: value.ObjectOID, phase: value.SearchPhase, nextOffset: value.NextOffset, ordinal: value.NextIndex, totalSize: value.SearchObjectSize, totalSizeKnown: value.SearchObjectSizeKnown}, nil
}
