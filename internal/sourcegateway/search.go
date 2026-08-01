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

type searchCandidateSource interface {
	Next(context.Context) (searchCandidate, bool, error)
}

// searchCandidate deliberately carries only producer-owned path bytes.
type searchCandidate struct {
	Path []byte
}

type searchTraversalCandidate struct {
	path  []byte
	entry sourcevault.RetainedTreeEntry
}

type searchCandidateHeap []searchTraversalCandidate

func (h searchCandidateHeap) Len() int { return len(h) }
func (h searchCandidateHeap) Less(left, right int) bool {
	return bytes.Compare(h[left].path, h[right].path) < 0
}
func (h searchCandidateHeap) Swap(left, right int) { h[left], h[right] = h[right], h[left] }
func (h *searchCandidateHeap) Push(value any)      { *h = append(*h, value.(searchTraversalCandidate)) }
func (h *searchCandidateHeap) Pop() any {
	values := *h
	last := len(values) - 1
	value := values[last]
	*h = values[:last]
	return value
}

type retainedTreeSearchCandidateSource struct {
	service    *Service
	authority  operations.SourceReadAuthority
	prefixes   []canonicalSearchPrefix
	candidates searchCandidateHeap
}

func newRetainedTreeSearchCandidateSource(ctx context.Context, service *Service, authority operations.SourceReadAuthority, prefixes []canonicalSearchPrefix) (*retainedTreeSearchCandidateSource, error) {
	entries, err := service.readTree(ctx, authority, authority.Relationship.TreeOID)
	if err != nil {
		return nil, err
	}
	values := searchCandidateHeap{}
	heap.Init(&values)
	for _, entry := range entries {
		heap.Push(&values, searchTraversalCandidate{path: append([]byte(nil), entry.Name...), entry: entry})
	}
	return &retainedTreeSearchCandidateSource{service: service, authority: authority, prefixes: prefixes, candidates: values}, nil
}

func (i *retainedTreeSearchCandidateSource) Next(ctx context.Context) (searchCandidate, bool, error) {
	for i.candidates.Len() > 0 {
		current := heap.Pop(&i.candidates).(searchTraversalCandidate)
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
				heap.Push(&i.candidates, searchTraversalCandidate{path: joinPath(current.path, entry.Name), entry: entry})
			}
		case "blob":
			if searchPathSelected(current.path, i.prefixes) {
				return searchCandidate{Path: append([]byte(nil), current.path...)}, true, nil
			}
		case "commit":
			continue
		default:
			return searchCandidate{}, false, &Error{Code: CodeIntegrityFailure}
		}
	}
	return searchCandidate{}, false, nil
}

type preparedSearch struct {
	request       SearchRequest
	literal       []byte
	authority     operations.SourceReadAuthority
	prefixes      []canonicalSearchPrefix
	queryID       string
	filterID      string
	fingerprint   string
	resume        searchResume
	resumePending bool
}

type verifiedSearchCandidate struct {
	Path     []byte
	Identity PathIdentity
	Mode     string
	BlobOID  string
}

type searchCandidateState struct {
	phase          searchPhase
	nextOffset     int64
	ordinal        int64
	totalSize      int64
	totalSizeKnown bool
}

func (s *Service) prepareSearch(ctx context.Context, request SearchRequest) (preparedSearch, error) {
	literal, err := validateSearchRequest(request)
	if err != nil {
		return preparedSearch{}, err
	}
	authority, err := s.resolveRevisionAuthority(ctx, request.PacketID, request.SurfaceContract, request.OperationID, request.RepositoryKey, request.Revision)
	if err != nil {
		return preparedSearch{}, err
	}
	prefixes, err := s.canonicalSearchPrefixes(ctx, authority, request.Prefixes)
	if err != nil {
		return preparedSearch{}, err
	}
	prepared := preparedSearch{request: request, literal: literal, authority: authority, prefixes: prefixes, queryID: searchQueryID(request.Mode, literal), filterID: searchFilterID(prefixes), fingerprint: searchFingerprint(authority, request.Mode, literal, prefixes, request.Budget)}
	if request.Cursor != "" {
		prepared.resume, err = s.decodeSearchCursor(ctx, authority, prepared.fingerprint, request.Cursor)
		if err != nil {
			return preparedSearch{}, err
		}
		if request.Mode == SearchModeByteLiteral && prepared.resume.phase != searchPhaseLiteralScan {
			return preparedSearch{}, &Error{Code: CodeInvalidCursor}
		}
		prepared.resumePending = true
	}
	return prepared, nil
}

func (s *Service) verifySearchCandidate(ctx context.Context, authority operations.SourceReadAuthority, candidate searchCandidate) (verifiedSearchCandidate, error) {
	entry, err := s.resolvePathEntry(ctx, authority, candidate.Path)
	if err != nil {
		return verifiedSearchCandidate{}, err
	}
	if entry.ObjectType != "blob" {
		return verifiedSearchCandidate{}, &Error{Code: CodeObjectMismatch}
	}
	identity, err := s.makePathIdentity(ctx, authority, candidate.Path)
	if err != nil {
		return verifiedSearchCandidate{}, err
	}
	return verifiedSearchCandidate{Path: append([]byte(nil), candidate.Path...), Identity: identity, Mode: entry.Mode, BlobOID: entry.ObjectOID}, nil
}

func (s *Service) Search(ctx context.Context, request SearchRequest) (SearchResult, error) {
	prepared, err := s.prepareSearch(ctx, request)
	if err != nil {
		return SearchResult{}, err
	}
	source, err := newRetainedTreeSearchCandidateSource(ctx, s, prepared.authority, prepared.prefixes)
	if err != nil {
		return SearchResult{}, err
	}
	return s.executePreparedSearch(ctx, prepared, source)
}

// executePreparedSearch is the canonical authoritative matcher for ordered,
// path-only candidate sources.
func (s *Service) executePreparedSearch(ctx context.Context, prepared preparedSearch, source searchCandidateSource) (SearchResult, error) {
	result := SearchResult{Source: fidelitySourceIdentity(prepared.authority), Mode: prepared.request.Mode, QueryID: prepared.queryID, FilterID: prepared.filterID, Matches: []SearchMatch{}}
	var previous []byte
	var err error
	for {
		candidate, ok, nextErr := source.Next(ctx)
		if nextErr != nil {
			return SearchResult{}, nextErr
		}
		if !ok {
			if prepared.resumePending {
				return SearchResult{}, &Error{Code: CodeInvalidCursor}
			}
			result.Completion = SearchCompletionComplete
			return result, nil
		}
		if !validatePath(candidate.Path, false) || !searchPathSelected(candidate.Path, prepared.prefixes) || previous != nil && bytes.Compare(candidate.Path, previous) <= 0 {
			return SearchResult{}, &Error{Code: CodeIntegrityFailure}
		}
		previous = append(previous[:0], candidate.Path...)
		if prepared.resumePending {
			comparison := bytes.Compare(candidate.Path, prepared.resume.path)
			if comparison < 0 {
				continue
			}
			if comparison > 0 {
				return SearchResult{}, &Error{Code: CodeInvalidCursor}
			}
		}
		verified, verifyErr := s.verifySearchCandidate(ctx, prepared.authority, candidate)
		if verifyErr != nil {
			return SearchResult{}, verifyErr
		}
		state := searchCandidateState{phase: searchPhaseLiteralScan}
		if prepared.request.Mode == SearchModeTextLiteral {
			state.phase = searchPhaseTextValidation
		}
		if prepared.resumePending {
			if verified.Identity.PathID != prepared.resume.pathID || verified.BlobOID != prepared.resume.blobOID {
				return SearchResult{}, &Error{Code: CodeInvalidCursor}
			}
			state = searchCandidateState{phase: prepared.resume.phase, nextOffset: prepared.resume.nextOffset, ordinal: prepared.resume.ordinal, totalSize: prepared.resume.totalSize, totalSizeKnown: prepared.resume.totalSizeKnown}
			prepared.resumePending = false
		}
		var complete bool
		result, complete, err = s.matchRetainedSearchCandidate(ctx, prepared, verified, state, result)
		if err != nil || !complete {
			return result, err
		}
	}
}

func (s *Service) matchRetainedSearchCandidate(ctx context.Context, prepared preparedSearch, candidate verifiedSearchCandidate, state searchCandidateState, result SearchResult) (SearchResult, bool, error) {
	if state.phase == searchPhaseLiteralScan && state.totalSizeKnown && state.nextOffset >= state.totalSize {
		return result, true, nil
	}
	if result.ExaminedObjects >= prepared.request.Budget.ExaminedObjects {
		minimumNextBytes := int64(len(prepared.literal))
		if state.phase == searchPhaseTextValidation {
			minimumNextBytes = utf8.UTFMax
		}
		return s.finishSearchCandidateIncomplete(prepared, result, candidate, state, SearchCompletionBudgetIncomplete, true, prepared.request.Budget.ExaminedBytes-result.ExaminedBytes < minimumNextBytes)
	}
	result.ExaminedObjects++
	if state.phase == searchPhaseTextValidation {
		for {
			remaining := prepared.request.Budget.ExaminedBytes - result.ExaminedBytes
			if remaining < utf8.UTFMax {
				state.ordinal = 0
				return s.finishSearchCandidateIncomplete(prepared, result, candidate, state, SearchCompletionBudgetIncomplete, false, true)
			}
			validation, err := s.validateSearchText(ctx, prepared.authority, candidate.BlobOID, state.nextOffset, remaining, state.totalSize, state.totalSizeKnown)
			if err != nil {
				return SearchResult{}, false, err
			}
			result.ExaminedBytes += validation.examined
			state.totalSize = validation.totalSize
			state.totalSizeKnown = validation.totalSizeKnown
			if validation.invalid {
				break
			}
			state.nextOffset = validation.nextOffset
			if validation.complete {
				state.phase = searchPhaseLiteralScan
				state.nextOffset = 0
				break
			}
		}
		if state.phase == searchPhaseTextValidation {
			return result, true, nil
		}
	}
	for {
		remaining := prepared.request.Budget.ExaminedBytes - result.ExaminedBytes
		if remaining < int64(len(prepared.literal)) {
			state.phase = searchPhaseLiteralScan
			return s.finishSearchCandidateIncomplete(prepared, result, candidate, state, SearchCompletionBudgetIncomplete, false, true)
		}
		page, err := s.vault.ReadRetainedBlobRange(ctx, sourcevault.ReadRetainedBlobRangeRequest{Relationship: prepared.authority.Relationship, BlobOID: candidate.BlobOID, Offset: state.nextOffset, Limit: int64(len(prepared.literal))})
		if err != nil {
			return SearchResult{}, false, mapVaultError(err)
		}
		if page.BlobOID != candidate.BlobOID || page.Offset != state.nextOffset || page.TotalSize < 0 || page.Offset+int64(len(page.Bytes)) > page.TotalSize || int64(len(page.Bytes)) > int64(len(prepared.literal)) {
			return SearchResult{}, false, &Error{Code: CodeObjectMismatch}
		}
		if state.totalSizeKnown && page.TotalSize != state.totalSize {
			return SearchResult{}, false, &Error{Code: CodeObjectMismatch}
		}
		state.totalSize = page.TotalSize
		state.totalSizeKnown = true
		result.ExaminedBytes += int64(len(page.Bytes))
		if int64(len(page.Bytes)) < int64(len(prepared.literal)) {
			if page.Offset+int64(len(page.Bytes)) != page.TotalSize {
				return SearchResult{}, false, &Error{Code: CodeObjectMismatch}
			}
			return result, true, nil
		}
		if bytes.Equal(page.Bytes, prepared.literal) {
			result.Matches = append(result.Matches, SearchMatch{MatchID: searchMatchID(prepared.authority, prepared.request.Mode, prepared.queryID, prepared.filterID, candidate.Identity.PathID, candidate.BlobOID, state.nextOffset, int64(len(prepared.literal)), state.ordinal), Path: candidate.Identity, FileMode: candidate.Mode, BlobOID: candidate.BlobOID, ByteOffset: state.nextOffset, MatchLength: int64(len(prepared.literal)), OccurrenceOrdinal: state.ordinal})
			state.ordinal++
			state.nextOffset++
			if len(result.Matches) == prepared.request.Limit {
				return s.finishSearchCandidateIncomplete(prepared, result, candidate, state, SearchCompletionPageIncomplete, false, false)
			}
		} else {
			state.nextOffset++
		}
	}
}

func (s *Service) finishSearchCandidateIncomplete(prepared preparedSearch, result SearchResult, candidate verifiedSearchCandidate, state searchCandidateState, completion SearchCompletion, objectExhausted, byteExhausted bool) (SearchResult, bool, error) {
	value, err := s.finishSearchIncomplete(prepared.authority, prepared.fingerprint, result, candidate.Identity, candidate.BlobOID, state.phase, state.nextOffset, state.ordinal, state.totalSize, state.totalSizeKnown, completion, objectExhausted, byteExhausted)
	return value, false, err
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
	scan := scanTextEligibility(page.Bytes, page.Offset+int64(len(page.Bytes)) == page.TotalSize)
	if scan.ineligible {
		return searchTextValidation{nextOffset: offset + int64(scan.consumed), examined: int64(len(page.Bytes)), totalSize: totalSize, totalSizeKnown: totalSizeKnown, invalid: true}, nil
	}
	if scan.consumed == 0 && offset < page.TotalSize {
		return searchTextValidation{}, &Error{Code: CodeIntegrityFailure}
	}
	next := offset + int64(scan.consumed)
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
