package sourcegateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	"relay/internal/sourcevault"
)

type hybridReaderFake struct {
	descriptor reader.Descriptor
	indexed    []reader.Candidate
	fallback   []reader.Candidate
	err        error
	fallbacks  int
	onFallback func()
}

func (f *hybridReaderFake) Descriptor() reader.Descriptor { return f.descriptor }
func (f *hybridReaderFake) IndexedTextCandidates(ctx context.Context, _ string) ([]reader.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.indexed, nil
}
func (f *hybridReaderFake) FallbackCandidates() []reader.Candidate {
	f.fallbacks++
	if f.onFallback != nil {
		f.onFallback()
	}
	return f.fallback
}

func hybridTestDescriptor(t *testing.T) reader.Descriptor {
	t.Helper()
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", strings.Repeat("1", 40), strings.Repeat("2", 40), options)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return reader.Descriptor{GenerationID: id, Identity: identity, GenerationManifestSHA256: strings.Repeat("3", 64), CoverageManifestSHA256: strings.Repeat("4", 64), ArtifactManifestSHA256: strings.Repeat("5", 64)}
}

func hybridPaths(t *testing.T, source searchCandidateSource) [][]byte {
	t.Helper()
	var paths [][]byte
	for {
		candidate, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return paths
		}
		paths = append(paths, candidate.Path)
	}
}

func TestHybridTextCandidateSourceValidatesAndMergesPaths(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	fake := &hybridReaderFake{descriptor: descriptor, indexed: []reader.Candidate{{Path: []byte("a")}, {Path: []byte("z")}}, fallback: []reader.Candidate{{Path: []byte("b")}, {Path: []byte{'m', 0xff}}}}
	source, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, "abc", []canonicalSearchPrefix{{bytes: []byte{}}})
	if err != nil {
		t.Fatal(err)
	}
	paths := hybridPaths(t, source)
	if got := []string{string(paths[0]), string(paths[1]), string(paths[2]), string(paths[3])}; strings.Join(got, ",") != "a,b,m\xff,z" {
		t.Fatalf("merged paths = %q", got)
	}
}

func TestHybridTextCandidateSourceFailsClosed(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	tests := []struct {
		name     string
		indexed  []reader.Candidate
		fallback []reader.Candidate
	}{
		{name: "indexed non utf8", indexed: []reader.Candidate{{Path: []byte{'a', 0xff}}}},
		{name: "indexed duplicate", indexed: []reader.Candidate{{Path: []byte("a")}, {Path: []byte("a")}}},
		{name: "fallback descending", fallback: []reader.Candidate{{Path: []byte("z")}, {Path: []byte("a")}}},
		{name: "cross stream duplicate", indexed: []reader.Candidate{{Path: []byte("a")}}, fallback: []reader.Candidate{{Path: []byte("a")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &hybridReaderFake{descriptor: descriptor, indexed: test.indexed, fallback: test.fallback}
			_, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, "abc", []canonicalSearchPrefix{{bytes: []byte{}}})
			if !errors.Is(err, reader.ErrGenerationIntegrity) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHybridTextCandidateSourceValidatesUnselectedPaths(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	prefixes := []canonicalSearchPrefix{{bytes: []byte("selected")}}
	tests := []struct {
		name     string
		indexed  []reader.Candidate
		fallback []reader.Candidate
	}{
		{name: "indexed duplicate", indexed: []reader.Candidate{{Path: []byte("other")}, {Path: []byte("other")}}},
		{name: "fallback descending", fallback: []reader.Candidate{{Path: []byte("z")}, {Path: []byte("a")}}},
		{name: "invalid fallback path", fallback: []reader.Candidate{{Path: []byte("../other")}}},
		{name: "cross stream duplicate", indexed: []reader.Candidate{{Path: []byte("other")}}, fallback: []reader.Candidate{{Path: []byte("other")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &hybridReaderFake{descriptor: descriptor, indexed: test.indexed, fallback: test.fallback}
			if _, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, "abc", prefixes); !errors.Is(err, reader.ErrGenerationIntegrity) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHybridTextCandidateSourceRejectsNilReaderAndCancellation(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	var nilReader indexedSearchCandidateReader
	if _, err := newHybridTextSearchCandidateSource(context.Background(), nilReader, descriptor, "abc", nil); !errors.Is(err, reader.ErrGenerationIntegrity) {
		t.Fatalf("nil reader error = %v", err)
	}
	var typedNil *hybridReaderFake
	if _, err := newHybridTextSearchCandidateSource(context.Background(), typedNil, descriptor, "abc", nil); !errors.Is(err, reader.ErrGenerationIntegrity) {
		t.Fatalf("typed nil reader error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fake := &hybridReaderFake{descriptor: descriptor, indexed: []reader.Candidate{{Path: []byte("a")}}, onFallback: cancel}
	if _, err := newHybridTextSearchCandidateSource(ctx, fake, descriptor, "abc", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled materialization error = %v", err)
	}
}

func TestExecutePreparedSearchRejectsNilCandidateSource(t *testing.T) {
	service := &Service{}
	if _, err := service.executePreparedSearch(context.Background(), preparedSearch{}, nil); ErrorCode(err) != CodeIntegrityFailure {
		t.Fatalf("error = %v", err)
	}
	var typedNil *sliceSearchCandidateSource
	if _, err := service.executePreparedSearch(context.Background(), preparedSearch{}, typedNil); ErrorCode(err) != CodeIntegrityFailure {
		t.Fatalf("typed nil source error = %v", err)
	}
}

func TestHybridTextCandidateSourceRejectsIneligibleOrFailedQueryAtomically(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	fake := &hybridReaderFake{descriptor: descriptor}
	for _, literal := range []string{"", "a", "ab", string([]byte{0xff})} {
		if _, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, literal, nil); !errors.Is(err, reader.ErrQueryIneligible) {
			t.Fatalf("literal %q error = %v", literal, err)
		}
	}
	fake.err = reader.ErrQueryIncomplete
	if _, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, "abc", nil); !errors.Is(err, reader.ErrQueryIncomplete) || fake.fallbacks != 0 {
		t.Fatalf("error = %v fallbacks = %d", err, fake.fallbacks)
	}
}

func TestHybridTextCandidateSourceRejectsDescriptorMismatch(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	bad := descriptor
	bad.ArtifactManifestSHA256 = strings.Repeat("f", 64)
	fake := &hybridReaderFake{descriptor: descriptor}
	if _, err := newHybridTextSearchCandidateSource(context.Background(), fake, bad, "abc", nil); !errors.Is(err, reader.ErrGenerationIntegrity) {
		t.Fatalf("error = %v", err)
	}
}

func TestSliceSearchCandidateSourceOwnsPathsAndReturnsFreshBytes(t *testing.T) {
	paths := [][]byte{[]byte("a")}
	source := newSliceSearchCandidateSource(paths)
	paths[0][0] = 'z'
	first, ok, err := source.Next(context.Background())
	if err != nil || !ok || string(first.Path) != "a" {
		t.Fatalf("first=%q ok=%v err=%v", first.Path, ok, err)
	}

	owned := [][]byte{[]byte("b"), []byte("c")}
	ownedSource := newOwnedSliceSearchCandidateSource(owned)
	first, _, _ = ownedSource.Next(context.Background())
	first.Path[0] = 'z'
	second, ok, err := ownedSource.Next(context.Background())
	if err != nil || !ok || string(second.Path) != "c" || string(owned[0]) != "b" {
		t.Fatalf("second=%q owned=%q ok=%v err=%v", second.Path, owned, ok, err)
	}
}

func TestHybridTextCandidateSourceReaderFailuresAreAtomic(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	for _, want := range []error{context.Canceled, context.DeadlineExceeded, reader.ErrGenerationUnavailable, reader.ErrGenerationIntegrity, reader.ErrQueryIncomplete, reader.ErrClosed, reader.ErrUnsupportedPlatform} {
		t.Run(want.Error(), func(t *testing.T) {
			fake := &hybridReaderFake{descriptor: descriptor, err: want, fallback: []reader.Candidate{{Path: []byte("fallback")}}}
			source, err := newHybridTextSearchCandidateSource(context.Background(), fake, descriptor, "abc", []canonicalSearchPrefix{{bytes: []byte{}}})
			if source != nil || !errors.Is(err, want) || fake.fallbacks != 0 {
				t.Fatalf("source=%v error=%v fallbacks=%d", source, err, fake.fallbacks)
			}
		})
	}
}

func TestHybridAuthoritativeMatchesEqualRetainedTreeScanner(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	exactBlob := strings.Repeat("3", 40)
	falseBlob := strings.Repeat("4", 40)
	fallbackBlob := strings.Repeat("5", 40)
	rawBlob := strings.Repeat("6", 40)
	literal := "猫犬鳥"
	rawPath := []byte{'z', 0xff}
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("a-exact"), Mode: "100755", ObjectType: "blob", ObjectOID: exactBlob},
			{Name: []byte("b-false-positive"), Mode: "100644", ObjectType: "blob", ObjectOID: falseBlob},
			{Name: []byte("c-fallback"), Mode: "100644", ObjectType: "blob", ObjectOID: fallbackBlob},
			{Name: rawPath, Mode: "100644", ObjectType: "blob", ObjectOID: rawBlob},
		}},
		blobs: map[string][]byte{
			exactBlob:    []byte("x" + literal + literal),
			falseBlob:    []byte("index metadata is not authority"),
			fallbackBlob: []byte(literal),
			rawBlob:      []byte("x" + literal),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, vault, authority)
	prepared, err := service.prepareSearch(context.Background(), textSearchRequest(literal))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := hybridTestDescriptor(t)
	hybrid, err := newHybridTextSearchCandidateSource(context.Background(), &hybridReaderFake{
		descriptor: descriptor,
		indexed:    []reader.Candidate{{Path: []byte("a-exact")}, {Path: []byte("b-false-positive")}},
		fallback:   []reader.Candidate{{Path: []byte("c-fallback")}, {Path: rawPath}},
	}, descriptor, literal, prepared.prefixes)
	if err != nil {
		t.Fatal(err)
	}
	hybridResult, err := service.executePreparedSearch(context.Background(), prepared, hybrid)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := newRetainedTreeSearchCandidateSource(context.Background(), service, authority, prepared.prefixes)
	if err != nil {
		t.Fatal(err)
	}
	scannerResult, err := service.executePreparedSearch(context.Background(), prepared, scanner)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hybridResult.Matches, scannerResult.Matches) || hybridResult.Completion != scannerResult.Completion {
		t.Fatalf("hybrid=%#v scanner=%#v", hybridResult, scannerResult)
	}
	if len(hybridResult.Matches) != 4 || hybridResult.Matches[0].FileMode != "100755" || hybridResult.Matches[0].BlobOID != exactBlob || hybridResult.Matches[0].ByteOffset != 1 || hybridResult.Matches[1].ByteOffset != 10 || hybridResult.Matches[3].Path.DisplayValid {
		t.Fatalf("authoritative matches=%#v", hybridResult.Matches)
	}
}

type hybridPathSource struct {
	paths [][]byte
	next  int
}

func (s *hybridPathSource) Next(context.Context) (searchCandidate, bool, error) {
	if s.next == len(s.paths) {
		return searchCandidate{}, false, nil
	}
	path := s.paths[s.next]
	s.next++
	return searchCandidate{Path: append([]byte(nil), path...)}, true, nil
}

func TestExecutePreparedSearchRejectsInvalidCandidateStreams(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID},
			{Name: []byte("b"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID},
		}},
		blobs: map[string][]byte{blobOID: []byte("x")}, nodes: map[string]sourcevault.RetainedCommitNode{},
	}, authority)
	invalid := []struct {
		name  string
		paths [][]byte
	}{
		{name: "empty", paths: [][]byte{[]byte{}}},
		{name: "absolute", paths: [][]byte{[]byte("/a")}},
		{name: "nul", paths: [][]byte{[]byte{'a', 0}}},
		{name: "empty component", paths: [][]byte{[]byte("a//b")}},
		{name: "dot", paths: [][]byte{[]byte("a/./b")}},
		{name: "dot dot", paths: [][]byte{[]byte("a/../b")}},
		{name: "duplicate", paths: [][]byte{[]byte("a"), []byte("a")}},
		{name: "descending", paths: [][]byte{[]byte("b"), []byte("a")}},
		{name: "outside prefix", paths: [][]byte{[]byte("b")}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			prefix := []byte{}
			if test.name == "outside prefix" {
				prefix = []byte("a")
			}
			prepared := preparedSearch{authority: authority, prefixes: []canonicalSearchPrefix{{bytes: prefix}}, literal: []byte("z"), request: byteSearchRequest([]byte("z"))}
			_, err := service.executePreparedSearch(context.Background(), prepared, &hybridPathSource{paths: test.paths})
			if ErrorCode(err) != CodeIntegrityFailure {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
