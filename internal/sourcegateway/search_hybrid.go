package sourcegateway

import (
	"bytes"
	"context"
	"unicode/utf8"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
)

// indexedSearchCandidateReader is deliberately limited to verified descriptor
// and path candidates. Source Gateway never receives index metadata or hits.
type indexedSearchCandidateReader interface {
	Descriptor() reader.Descriptor
	IndexedTextCandidates(context.Context, string) ([]reader.Candidate, error)
	FallbackCandidates() []reader.Candidate
}

type sliceSearchCandidateSource struct {
	paths [][]byte
	next  int
}

func newSliceSearchCandidateSource(paths [][]byte) *sliceSearchCandidateSource {
	owned := make([][]byte, len(paths))
	for i := range paths {
		owned[i] = append([]byte(nil), paths[i]...)
	}
	return &sliceSearchCandidateSource{paths: owned}
}

func (s *sliceSearchCandidateSource) Next(ctx context.Context) (searchCandidate, bool, error) {
	if err := ctx.Err(); err != nil {
		return searchCandidate{}, false, err
	}
	if s.next == len(s.paths) {
		return searchCandidate{}, false, nil
	}
	path := append([]byte(nil), s.paths[s.next]...)
	s.next++
	return searchCandidate{Path: path}, true, nil
}

func newHybridTextSearchCandidateSource(ctx context.Context, index indexedSearchCandidateReader, expected reader.Descriptor, literal string, prefixes []canonicalSearchPrefix) (searchCandidateSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validReaderDescriptor(expected) || index.Descriptor() != expected {
		return nil, generationIntegrityError()
	}
	if literal == "" || !utf8.ValidString(literal) || utf8.RuneCountInString(literal) < 3 {
		return nil, reader.ErrQueryIneligible
	}
	indexed, err := index.IndexedTextCandidates(ctx, literal)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fallback := index.FallbackCandidates()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	indexedPaths, err := validateHybridCandidates(indexed, prefixes, true)
	if err != nil {
		return nil, err
	}
	fallbackPaths, err := validateHybridCandidates(fallback, prefixes, false)
	if err != nil {
		return nil, err
	}
	merged, err := mergeHybridCandidates(indexedPaths, fallbackPaths)
	if err != nil {
		return nil, err
	}
	return newSliceSearchCandidateSource(merged), nil
}

func generationIntegrityError() error { return reader.ErrGenerationIntegrity }

func validReaderDescriptor(d reader.Descriptor) bool {
	if !validLowerHex64(d.GenerationID) || !validLowerHex64(d.GenerationManifestSHA256) || !validLowerHex64(d.CoverageManifestSHA256) || !validLowerHex64(d.ArtifactManifestSHA256) {
		return false
	}
	id, err := sourceindex.GenerationID(d.Identity)
	return err == nil && id == d.GenerationID
}

func validLowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}

func validateHybridCandidates(candidates []reader.Candidate, prefixes []canonicalSearchPrefix, requireUTF8 bool) ([][]byte, error) {
	paths := make([][]byte, 0, len(candidates))
	var previous []byte
	for _, candidate := range candidates {
		path := append([]byte(nil), candidate.Path...)
		if !validatePath(path, false) || requireUTF8 && !utf8.Valid(path) || previous != nil && bytes.Compare(path, previous) <= 0 {
			return nil, generationIntegrityError()
		}
		previous = path
		if searchPathSelected(path, prefixes) {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func mergeHybridCandidates(indexed, fallback [][]byte) ([][]byte, error) {
	paths := make([][]byte, 0, len(indexed)+len(fallback))
	for len(indexed) > 0 && len(fallback) > 0 {
		comparison := bytes.Compare(indexed[0], fallback[0])
		if comparison == 0 {
			return nil, generationIntegrityError()
		}
		if comparison < 0 {
			paths = append(paths, indexed[0])
			indexed = indexed[1:]
		} else {
			paths = append(paths, fallback[0])
			fallback = fallback[1:]
		}
	}
	paths = append(paths, indexed...)
	paths = append(paths, fallback...)
	return paths, nil
}
