package sourcegateway

import (
	"bytes"
	"container/heap"
	"context"
	"sort"
	"unicode/utf8"

	"relay/internal/app/operations"
	"relay/internal/sourcevault"
)

type searchCandidate struct {
	path  []byte
	entry sourcevault.RetainedTreeEntry
}

type searchCandidateHeap []searchCandidate

func (h searchCandidateHeap) Len() int { return len(h) }
func (h searchCandidateHeap) Less(left, right int) bool {
	return bytes.Compare(h[left].path, h[right].path) < 0
}
func (h searchCandidateHeap) Swap(left, right int) { h[left], h[right] = h[right], h[left] }
func (h *searchCandidateHeap) Push(value any)      { *h = append(*h, value.(searchCandidate)) }
func (h *searchCandidateHeap) Pop() any {
	values := *h
	last := len(values) - 1
	value := values[last]
	*h = values[:last]
	return value
}

type searchCandidateIterator struct {
	service    *Service
	authority  operations.SourceReadAuthority
	prefixes   []canonicalSearchPrefix
	candidates searchCandidateHeap
}

func newSearchCandidateIterator(ctx context.Context, service *Service, authority operations.SourceReadAuthority, prefixes []canonicalSearchPrefix) (*searchCandidateIterator, error) {
	entries, err := service.readTree(ctx, authority, authority.Relationship.TreeOID)
	if err != nil {
		return nil, err
	}
	values := searchCandidateHeap{}
	heap.Init(&values)
	for _, entry := range entries {
		heap.Push(&values, searchCandidate{path: append([]byte(nil), entry.Name...), entry: entry})
	}
	return &searchCandidateIterator{service: service, authority: authority, prefixes: prefixes, candidates: values}, nil
}

func (i *searchCandidateIterator) next(ctx context.Context) (searchCandidate, bool, error) {
	for i.candidates.Len() > 0 {
		current := heap.Pop(&i.candidates).(searchCandidate)
		switch current.entry.ObjectType {
		case "tree":
			if !searchDirectoryIntersects(current.path, i.prefixes) {
				continue
			}
			entries, err := i.service.readTree(ctx, i.authority, current.entry.ObjectOID)
			if err != nil {
				return searchCandidate{}, false, err
			}
			for _, entry := range entries {
				heap.Push(&i.candidates, searchCandidate{path: joinPath(current.path, entry.Name), entry: entry})
			}
		case "blob":
			if searchPathSelected(current.path, i.prefixes) {
				current.path = append([]byte(nil), current.path...)
				return current, true, nil
			}
		case "commit":
			continue
		default:
			return searchCandidate{}, false, &Error{Code: CodeIntegrityFailure}
		}
	}
	return searchCandidate{}, false, nil
}

func (s *Service) Search(ctx context.Context, request SearchRequest) (SearchResult, error) {
	literal, err := validateSearchRequest(request)
	if err != nil {
		return SearchResult{}, err
	}
	authority, err := s.resolveRevisionAuthority(ctx, request.PacketID, request.SurfaceContract, request.OperationID, request.RepositoryKey, request.Revision)
	if err != nil {
		return SearchResult{}, err
	}
	prefixes, err := s.canonicalSearchPrefixes(ctx, authority, request.Prefixes)
	if err != nil {
		return SearchResult{}, err
	}
	queryID := searchQueryID(request.Mode, literal)
	filterID := searchFilterID(prefixes)
	fingerprint := searchFingerprint(authority, request.Mode, literal, prefixes, request.Budget)
	var resume searchResume
	resumePending := false
	if request.Cursor != "" {
		resume, err = s.decodeSearchCursor(ctx, authority, fingerprint, request.Cursor)
		if err != nil {
			return SearchResult{}, err
		}
		if request.Mode == SearchModeByteLiteral && resume.phase != searchPhaseLiteralScan {
			return SearchResult{}, &Error{Code: CodeInvalidCursor}
		}
		resumePending = true
	}
	iterator, err := newSearchCandidateIterator(ctx, s, authority, prefixes)
	if err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{Source: fidelitySourceIdentity(authority), Mode: request.Mode, QueryID: queryID, FilterID: filterID, Matches: []SearchMatch{}}
	for {
		candidate, ok, nextErr := iterator.next(ctx)
		if nextErr != nil {
			return SearchResult{}, nextErr
		}
		if !ok {
			if resumePending {
				return SearchResult{}, &Error{Code: CodeInvalidCursor}
			}
			result.Completion = SearchCompletionComplete
			return result, nil
		}
		identity, identityErr := s.makePathIdentity(ctx, authority, candidate.path)
		if identityErr != nil {
			return SearchResult{}, identityErr
		}
		phase := searchPhaseLiteralScan
		nextOffset := int64(0)
		ordinal := int64(0)
		totalSize := int64(0)
		totalSizeKnown := false
		if request.Mode == SearchModeTextLiteral {
			phase = searchPhaseTextValidation
		}
		if resumePending {
			comparison := bytes.Compare(candidate.path, resume.path)
			if comparison < 0 {
				continue
			}
			if comparison > 0 || identity.PathID != resume.pathID || candidate.entry.ObjectOID != resume.blobOID {
				return SearchResult{}, &Error{Code: CodeInvalidCursor}
			}
			phase = resume.phase
			nextOffset = resume.nextOffset
			ordinal = resume.ordinal
			totalSize = resume.totalSize
			totalSizeKnown = resume.totalSizeKnown
			resumePending = false
		}
		// A cursor at the end of a known blob represents a fully decided
		// candidate. It must not consume a renewed object budget again.
		if !resumePending && phase == searchPhaseLiteralScan && totalSizeKnown && nextOffset >= totalSize {
			continue
		}
		if result.ExaminedObjects >= request.Budget.ExaminedObjects {
			minimumNextBytes := int64(len(literal))
			if phase == searchPhaseTextValidation {
				minimumNextBytes = utf8.UTFMax
			}
			return s.finishSearchIncomplete(authority, fingerprint, result, identity, candidate.entry.ObjectOID, phase, nextOffset, ordinal, totalSize, totalSizeKnown, SearchCompletionBudgetIncomplete, true, request.Budget.ExaminedBytes-result.ExaminedBytes < minimumNextBytes)
		}
		result.ExaminedObjects++
		if phase == searchPhaseTextValidation {
			for {
				remaining := request.Budget.ExaminedBytes - result.ExaminedBytes
				if remaining < utf8.UTFMax {
					return s.finishSearchIncomplete(authority, fingerprint, result, identity, candidate.entry.ObjectOID, phase, nextOffset, 0, totalSize, totalSizeKnown, SearchCompletionBudgetIncomplete, false, true)
				}
				validation, validationErr := s.validateSearchText(ctx, authority, candidate.entry.ObjectOID, nextOffset, remaining, totalSize, totalSizeKnown)
				if validationErr != nil {
					return SearchResult{}, validationErr
				}
				result.ExaminedBytes += validation.examined
				totalSize = validation.totalSize
				totalSizeKnown = validation.totalSizeKnown
				if validation.invalid {
					break
				}
				nextOffset = validation.nextOffset
				if validation.complete {
					phase = searchPhaseLiteralScan
					nextOffset = 0
					break
				}
			}
			if phase == searchPhaseTextValidation {
				continue
			}
		}
		for {
			remaining := request.Budget.ExaminedBytes - result.ExaminedBytes
			if remaining < int64(len(literal)) {
				return s.finishSearchIncomplete(authority, fingerprint, result, identity, candidate.entry.ObjectOID, searchPhaseLiteralScan, nextOffset, ordinal, totalSize, totalSizeKnown, SearchCompletionBudgetIncomplete, false, true)
			}
			page, readErr := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: authority.Relationship, BlobOID: candidate.entry.ObjectOID, Offset: nextOffset, Limit: int64(len(literal))})
			if readErr != nil {
				return SearchResult{}, mapVaultError(readErr)
			}
			if page.BlobOID != candidate.entry.ObjectOID || page.Offset != nextOffset || page.TotalSize < 0 || page.Offset+int64(len(page.Bytes)) > page.TotalSize || int64(len(page.Bytes)) > int64(len(literal)) {
				return SearchResult{}, &Error{Code: CodeObjectMismatch}
			}
			if totalSizeKnown && page.TotalSize != totalSize {
				return SearchResult{}, &Error{Code: CodeObjectMismatch}
			}
			totalSize = page.TotalSize
			totalSizeKnown = true
			result.ExaminedBytes += int64(len(page.Bytes))
			if int64(len(page.Bytes)) < int64(len(literal)) {
				if page.Offset+int64(len(page.Bytes)) != page.TotalSize {
					return SearchResult{}, &Error{Code: CodeObjectMismatch}
				}
				break
			}
			if bytes.Equal(page.Bytes, literal) {
				result.Matches = append(result.Matches, SearchMatch{MatchID: searchMatchID(authority, request.Mode, queryID, filterID, identity.PathID, candidate.entry.ObjectOID, nextOffset, int64(len(literal)), ordinal), Path: identity, FileMode: candidate.entry.Mode, BlobOID: candidate.entry.ObjectOID, ByteOffset: nextOffset, MatchLength: int64(len(literal)), OccurrenceOrdinal: ordinal})
				ordinal++
				nextOffset++
				if len(result.Matches) == request.Limit {
					return s.finishSearchIncomplete(authority, fingerprint, result, identity, candidate.entry.ObjectOID, searchPhaseLiteralScan, nextOffset, ordinal, totalSize, totalSizeKnown, SearchCompletionPageIncomplete, false, false)
				}
			} else {
				nextOffset++
			}
		}
	}
}

type searchTextValidation struct {
	nextOffset     int64
	examined       int64
	totalSize      int64
	totalSizeKnown bool
	complete       bool
	invalid        bool
}

func (s *Service) validateSearchText(ctx context.Context, authority operations.SourceReadAuthority, blobOID string, offset, remaining, totalSize int64, totalSizeKnown bool) (searchTextValidation, error) {
	limit := remaining
	if limit > textValidationChunkBytes {
		limit = textValidationChunkBytes
	}
	page, err := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: authority.Relationship, BlobOID: blobOID, Offset: offset, Limit: limit})
	if err != nil {
		return searchTextValidation{}, mapVaultError(err)
	}
	if page.BlobOID != blobOID || page.Offset != offset || page.TotalSize < 0 || page.Offset+int64(len(page.Bytes)) > page.TotalSize || int64(len(page.Bytes)) > limit {
		return searchTextValidation{}, &Error{Code: CodeObjectMismatch}
	}
	if totalSizeKnown && page.TotalSize != totalSize {
		return searchTextValidation{}, &Error{Code: CodeObjectMismatch}
	}
	totalSize = page.TotalSize
	totalSizeKnown = true
	if len(page.Bytes) == 0 {
		if offset != page.TotalSize {
			return searchTextValidation{}, &Error{Code: CodeObjectMismatch}
		}
		return searchTextValidation{nextOffset: offset, totalSize: totalSize, totalSizeKnown: totalSizeKnown, complete: true}, nil
	}
	processed := 0
	for processed < len(page.Bytes) {
		remainingBytes := page.Bytes[processed:]
		if !utf8.FullRune(remainingBytes) {
			break
		}
		value, size := utf8.DecodeRune(remainingBytes)
		if value == utf8.RuneError && size == 1 {
			return searchTextValidation{nextOffset: offset + int64(processed), examined: int64(len(page.Bytes)), totalSize: totalSize, totalSizeKnown: totalSizeKnown, invalid: true}, nil
		}
		processed += size
	}
	if processed < len(page.Bytes) && offset+int64(len(page.Bytes)) == page.TotalSize {
		return searchTextValidation{nextOffset: offset + int64(processed), examined: int64(len(page.Bytes)), totalSize: totalSize, totalSizeKnown: totalSizeKnown, invalid: true}, nil
	}
	if processed == 0 && offset < page.TotalSize {
		return searchTextValidation{}, &Error{Code: CodeIntegrityFailure}
	}
	next := offset + int64(processed)
	return searchTextValidation{nextOffset: next, examined: int64(len(page.Bytes)), totalSize: totalSize, totalSizeKnown: totalSizeKnown, complete: next == page.TotalSize}, nil
}

func validateSearchRequest(request SearchRequest) ([]byte, error) {
	if request.Limit <= 0 || request.Limit > MaxSearchPageMatches || len(request.Prefixes) > MaxTreePageEntries || request.Budget.ExaminedObjects <= 0 || request.Budget.ExaminedBytes <= 0 {
		return nil, &Error{Code: CodeInvalidRange}
	}
	var literal []byte
	switch request.Mode {
	case SearchModeTextLiteral:
		if request.TextLiteral == "" || len(request.ByteLiteral) != 0 || !utf8.ValidString(request.TextLiteral) {
			return nil, &Error{Code: CodeInvalidRequest}
		}
		literal = []byte(request.TextLiteral)
	case SearchModeByteLiteral:
		if request.TextLiteral != "" || len(request.ByteLiteral) == 0 {
			return nil, &Error{Code: CodeInvalidRequest}
		}
		literal = append([]byte(nil), request.ByteLiteral...)
	default:
		return nil, &Error{Code: CodeInvalidRequest}
	}
	if len(literal) > MaxSearchLiteralBytes {
		return nil, &Error{Code: CodeInvalidRange}
	}
	minimum := int64(len(literal))
	if minimum < utf8.UTFMax {
		minimum = utf8.UTFMax
	}
	if request.Budget.ExaminedBytes < minimum {
		return nil, &Error{Code: CodeInvalidRange}
	}
	return append([]byte(nil), literal...), nil
}

func (s *Service) canonicalSearchPrefixes(ctx context.Context, authority operations.SourceReadAuthority, references []PathReference) ([]canonicalSearchPrefix, error) {
	if len(references) == 0 {
		identity, err := s.makePathIdentity(ctx, authority, []byte{})
		if err != nil {
			return nil, err
		}
		return []canonicalSearchPrefix{{bytes: []byte{}, identity: identity}}, nil
	}
	prefixes := make([]canonicalSearchPrefix, 0, len(references))
	for _, reference := range references {
		value, err := s.resolvePathReference(ctx, authority, reference, true)
		if err != nil {
			return nil, err
		}
		identity, err := s.makePathIdentity(ctx, authority, value)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, canonicalSearchPrefix{bytes: append([]byte(nil), value...), identity: identity})
	}
	sort.Slice(prefixes, func(left, right int) bool { return bytes.Compare(prefixes[left].bytes, prefixes[right].bytes) < 0 })
	for index := 1; index < len(prefixes); index++ {
		if bytes.Equal(prefixes[index-1].bytes, prefixes[index].bytes) {
			return nil, &Error{Code: CodeInvalidRequest}
		}
	}
	return prefixes, nil
}

func searchPathSelected(path []byte, prefixes []canonicalSearchPrefix) bool {
	for _, prefix := range prefixes {
		if pathHasComponentPrefix(path, prefix.bytes) {
			return true
		}
	}
	return false
}

func searchDirectoryIntersects(directory []byte, prefixes []canonicalSearchPrefix) bool {
	for _, prefix := range prefixes {
		if pathHasComponentPrefix(directory, prefix.bytes) || pathHasComponentPrefix(prefix.bytes, directory) {
			return true
		}
	}
	return false
}

func pathHasComponentPrefix(path, prefix []byte) bool {
	if len(prefix) == 0 {
		return true
	}
	if bytes.Equal(path, prefix) {
		return true
	}
	return len(path) > len(prefix) && bytes.Equal(path[:len(prefix)], prefix) && path[len(prefix)] == '/'
}

func (s *Service) finishSearchIncomplete(authority operations.SourceReadAuthority, fingerprint string, result SearchResult, identity PathIdentity, blobOID string, phase searchPhase, nextOffset, ordinal, totalSize int64, totalSizeKnown bool, completion SearchCompletion, objectExhausted, byteExhausted bool) (SearchResult, error) {
	if completion != SearchCompletionPageIncomplete && completion != SearchCompletionBudgetIncomplete {
		return SearchResult{}, &Error{Code: CodeInternalFailure}
	}
	value := searchCursorPayload(authority, fingerprint, identity, blobOID, phase, nextOffset, ordinal, totalSize, totalSizeKnown)
	token, err := s.cursors.Encode(value)
	if err != nil {
		return SearchResult{}, err
	}
	result.Completion = completion
	result.ObjectBudgetExhausted = objectExhausted
	result.ByteBudgetExhausted = byteExhausted
	result.Cursor = token
	return result, nil
}
