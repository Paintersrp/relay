package sourcegateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"relay/internal/app/operations"
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
	queries    int
	closes     int
	closeErr   error
}

func (f *hybridReaderFake) Descriptor() reader.Descriptor { return f.descriptor }
func (f *hybridReaderFake) IndexedTextCandidates(ctx context.Context, _ string) ([]reader.Candidate, error) {
	f.queries++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.indexed, nil
}
func (f *hybridReaderFake) Close() error { f.closes++; return f.closeErr }

type searchIndexProviderFake struct {
	handle    SearchIndexHandle
	err       error
	opens     int
	authority operations.SourceReadAuthority
}

func (f *searchIndexProviderFake) OpenSearchIndex(_ context.Context, authority operations.SourceReadAuthority) (SearchIndexHandle, error) {
	f.opens++
	f.authority = authority
	return f.handle, f.err
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

func TestPublicSearchRoutesAndBindsBackend(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	treeOID := strings.Repeat("2", 40)
	blobOID := strings.Repeat("6", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{treeOID: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("abcabc")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, treeOID, "", 1))
	descriptor := hybridTestDescriptor(t)
	handle := &hybridReaderFake{descriptor: descriptor, indexed: []reader.Candidate{{Path: []byte("a.txt")}}}
	provider := &searchIndexProviderFake{handle: handle}
	service.searchIndex = provider
	request := SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeTextLiteral, TextLiteral: "abc", Limit: 1, Budget: SearchBudget{ExaminedObjects: 10, ExaminedBytes: 64}}
	result, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if provider.opens != 1 || provider.authority.Relationship.CommitOID != commitOID || handle.queries != 1 || handle.closes != 1 || len(result.Matches) != 1 {
		t.Fatalf("opens=%d authority=%#v queries=%d closes=%d matches=%#v", provider.opens, provider.authority, handle.queries, handle.closes, result.Matches)
	}
	payload, err := service.cursors.Decode(result.Cursor)
	if err != nil || payload.Version != SearchCursorVersion || payload.SearchBackend != string(searchBackendIndexed) || payload.SearchGenerationID != descriptor.GenerationID {
		t.Fatalf("cursor=%#v error=%v", payload, err)
	}
	request.Cursor = result.Cursor
	continued, err := service.Search(context.Background(), request)
	if err != nil || provider.opens != 2 || handle.closes != 2 || len(continued.Matches) != 1 || continued.Matches[0].ByteOffset != 3 {
		t.Fatalf("continued=%#v error=%v opens=%d closes=%d", continued, err, provider.opens, handle.closes)
	}
}

func TestPublicSearchScannerRoutesDoNotOpenIndex(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	treeOID := strings.Repeat("2", 40)
	blobOID := strings.Repeat("6", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{treeOID: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("abc")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, treeOID, "", 1))
	provider := &searchIndexProviderFake{}
	service.searchIndex = provider
	requests := []SearchRequest{
		{Mode: SearchModeByteLiteral, ByteLiteral: []byte("a")},
		{Mode: SearchModeTextLiteral, TextLiteral: "a"},
		{Mode: SearchModeTextLiteral, TextLiteral: "ab"},
	}
	for _, request := range requests {
		request.PacketID, request.SurfaceContract, request.OperationID, request.RepositoryKey = "opkt-fidelity", "planner-authoring.v1", "planner.requirements", "relay"
		request.Limit, request.Budget = 1, SearchBudget{ExaminedObjects: 10, ExaminedBytes: 64}
		if _, err := service.Search(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if provider.opens != 0 {
		t.Fatalf("provider opens = %d", provider.opens)
	}
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

type hybridErrAfterChecks struct {
	checks int
	failAt int
	err    error
}

func (c *hybridErrAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *hybridErrAfterChecks) Done() <-chan struct{}       { return nil }
func (c *hybridErrAfterChecks) Err() error {
	c.checks++
	if c.checks >= c.failAt {
		return c.err
	}
	return nil
}
func (c *hybridErrAfterChecks) Value(any) any { return nil }

func TestHybridMaterializationCancellationIsAtomic(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	cases := []struct {
		name     string
		indexed  []reader.Candidate
		fallback []reader.Candidate
		failAt   int
	}{
		{name: "indexed validation", indexed: []reader.Candidate{{Path: []byte("a")}}, failAt: 5},
		{name: "fallback validation", fallback: []reader.Candidate{{Path: []byte("a")}}, failAt: 5},
		{name: "canonical merge", indexed: []reader.Candidate{{Path: []byte("a")}}, fallback: []reader.Candidate{{Path: []byte("b")}}, failAt: 9},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := &hybridErrAfterChecks{failAt: test.failAt, err: context.Canceled}
			fake := &hybridReaderFake{descriptor: descriptor, indexed: test.indexed, fallback: test.fallback}
			source, err := newHybridTextSearchCandidateSource(ctx, fake, descriptor, "abc", []canonicalSearchPrefix{{bytes: []byte{}}})
			if source != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("source=%v err=%v checks=%d", source, err, ctx.checks)
			}
		})
	}
}

func TestHybridCandidateEmissionCancellationReturnsNoCandidate(t *testing.T) {
	descriptor := hybridTestDescriptor(t)
	source, err := newHybridTextSearchCandidateSource(context.Background(), &hybridReaderFake{
		descriptor: descriptor,
		indexed:    []reader.Candidate{{Path: []byte("a")}},
	}, descriptor, "abc", []canonicalSearchPrefix{{bytes: []byte{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &hybridErrAfterChecks{failAt: 1, err: context.DeadlineExceeded}
	candidate, ok, err := source.Next(ctx)
	if ok || !errors.Is(err, context.DeadlineExceeded) || candidate.Path != nil {
		t.Fatalf("candidate=%#v ok=%v err=%v", candidate, ok, err)
	}
}

func hybridTextPage(t *testing.T, service *Service, request SearchRequest, descriptor reader.Descriptor, indexed, fallback []reader.Candidate) SearchResult {
	t.Helper()
	prepared, err := service.prepareSearch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if request.Cursor == "" {
		prepared.backend = indexedSearchBinding(descriptor)
	}
	source, err := newHybridTextSearchCandidateSource(context.Background(), &hybridReaderFake{descriptor: descriptor, indexed: indexed, fallback: fallback}, descriptor, string(prepared.literal), prepared.prefixes)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.executePreparedSearch(context.Background(), prepared, source)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHybridOverlappingPaginationReconstructsSource(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}},
		blobs: map[string][]byte{blobOID: []byte("aaaaa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}, authority)
	descriptor := hybridTestDescriptor(t)
	request := textSearchRequest("aaa")
	request.Limit = 1
	request.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 64}
	indexed := []reader.Candidate{{Path: []byte("a.txt")}}
	var matches []SearchMatch
	for page := 0; page < 8; page++ {
		result := hybridTextPage(t, service, request, descriptor, indexed, nil)
		matches = append(matches, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			if result.Cursor != "" || len(result.Matches) != 0 {
				t.Fatalf("terminal page=%#v", result)
			}
			break
		}
		if result.Cursor == "" || len(result.Matches) != 1 {
			t.Fatalf("incomplete page=%#v", result)
		}
		request.Cursor = result.Cursor
		if page == 7 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(matches) != 3 {
		t.Fatalf("matches=%#v", matches)
	}
	for i, match := range matches {
		if match.ByteOffset != int64(i) || match.OccurrenceOrdinal != int64(i) || match.MatchLength != 3 || match.Path.PathID != pathID([]byte("a.txt")) || match.BlobOID != blobOID || match.MatchID == "" {
			t.Fatalf("match %d=%#v", i, match)
		}
		if i > 0 && match.MatchID == matches[i-1].MatchID {
			t.Fatalf("duplicate match identity at %d", i)
		}
	}
}

func TestHybridObjectBudgetContinuationResumesExactCandidate(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	firstBlob := strings.Repeat("3", 40)
	secondBlob := strings.Repeat("4", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: firstBlob},
			{Name: []byte("b.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: secondBlob},
		}},
		blobs: map[string][]byte{firstBlob: []byte("needle"), secondBlob: []byte("needle")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}, authority)
	descriptor := hybridTestDescriptor(t)
	request := textSearchRequest("needle")
	request.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 128}
	indexed := []reader.Candidate{{Path: []byte("a.txt")}, {Path: []byte("b.txt")}}
	first := hybridTextPage(t, service, request, descriptor, indexed, nil)
	if first.Completion != SearchCompletionBudgetIncomplete || !first.ObjectBudgetExhausted || first.ByteBudgetExhausted || first.Cursor == "" || len(first.Matches) != 1 || first.Matches[0].Path.PathID != pathID([]byte("a.txt")) {
		t.Fatalf("first=%#v", first)
	}
	cursor, err := service.cursors.Decode(first.Cursor)
	path, ok := decodeCanonicalInline(cursor.AfterPath.InlineBase64)
	if err != nil || !ok || string(path) != "b.txt" {
		t.Fatalf("cursor=%#v err=%v", cursor, err)
	}
	request.Cursor = first.Cursor
	second := hybridTextPage(t, service, request, descriptor, indexed, nil)
	if second.Completion != SearchCompletionComplete || second.Cursor != "" || len(second.Matches) != 1 || second.Matches[0].Path.PathID != pathID([]byte("b.txt")) {
		t.Fatalf("second=%#v", second)
	}
}

func TestHybridByteBudgetZeroMatchContinuation(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}},
		blobs: map[string][]byte{blobOID: []byte("zzzzzz")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}, authority)
	descriptor := hybridTestDescriptor(t)
	request := textSearchRequest("aaa")
	request.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}
	indexed := []reader.Candidate{{Path: []byte("a.txt")}}
	var all []SearchMatch
	seenIncomplete := false
	seenPartialValidation := false
	seenLiteralScanExhaustion := false
	for page := 0; page < 16; page++ {
		result := hybridTextPage(t, service, request, descriptor, indexed, nil)
		if len(result.Matches) != 0 || result.Cursor == "" && result.Completion != SearchCompletionComplete {
			t.Fatalf("page=%d result=%#v", page, result)
		}
		all = append(all, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			if result.Cursor != "" || result.ByteBudgetExhausted {
				t.Fatalf("terminal result=%#v", result)
			}
			break
		}
		if result.Completion != SearchCompletionBudgetIncomplete || !result.ByteBudgetExhausted {
			t.Fatalf("incomplete result=%#v", result)
		}
		cursor, err := service.cursors.Decode(result.Cursor)
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 && cursor.SearchPhase != searchPhaseTextValidation {
			t.Fatalf("first cursor phase=%q", cursor.SearchPhase)
		}
		if page == 0 {
			seenPartialValidation = cursor.SearchPhase == searchPhaseTextValidation && cursor.NextOffset > 0
		}
		if cursor.SearchPhase == searchPhaseLiteralScan {
			seenLiteralScanExhaustion = true
		}
		seenIncomplete = true
		request.Cursor = result.Cursor
		if page == 15 {
			t.Fatal("zero-match continuation did not terminate")
		}
	}
	if !seenIncomplete || !seenPartialValidation || !seenLiteralScanExhaustion || len(all) != 0 {
		t.Fatalf("incomplete=%v partial=%v literal=%v matches=%#v", seenIncomplete, seenPartialValidation, seenLiteralScanExhaustion, all)
	}
}

func TestHybridResumeIntegrityRejectsReconstructedState(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	oldBlob := strings.Repeat("3", 40)
	newBlob := strings.Repeat("4", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	service := newFidelityService(t, &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: newBlob}}},
		blobs: map[string][]byte{newBlob: []byte("needle")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}, authority)
	base := preparedSearch{authority: authority, prefixes: []canonicalSearchPrefix{{bytes: []byte{}}}, literal: []byte("needle"), request: textSearchRequest("needle"), resumePending: true}
	cases := []struct {
		name   string
		paths  []reader.Candidate
		resume searchResume
	}{
		{name: "resume path absent", paths: []reader.Candidate{{Path: []byte("a.txt")}, {Path: []byte("c.txt")}}, resume: searchResume{path: []byte("b.txt"), pathID: pathID([]byte("b.txt")), blobOID: oldBlob, phase: searchPhaseLiteralScan, totalSizeKnown: true, totalSize: 6}},
		{name: "resume path after canonical source", paths: []reader.Candidate{{Path: []byte("b.txt")}}, resume: searchResume{path: []byte("a.txt"), pathID: pathID([]byte("a.txt")), blobOID: oldBlob, phase: searchPhaseLiteralScan, totalSizeKnown: true, totalSize: 6}},
		{name: "blob changed", paths: []reader.Candidate{{Path: []byte("a.txt")}}, resume: searchResume{path: []byte("a.txt"), pathID: pathID([]byte("a.txt")), blobOID: oldBlob, phase: searchPhaseLiteralScan, totalSizeKnown: true, totalSize: 6}},
		{name: "path identity changed", paths: []reader.Candidate{{Path: []byte("a.txt")}}, resume: searchResume{path: []byte("a.txt"), pathID: pathID([]byte("b.txt")), blobOID: newBlob, phase: searchPhaseLiteralScan, totalSizeKnown: true, totalSize: 6}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			prepared := base
			prepared.resume = test.resume
			source, err := newHybridTextSearchCandidateSource(context.Background(), &hybridReaderFake{descriptor: hybridTestDescriptor(t), indexed: test.paths}, hybridTestDescriptor(t), "needle", prepared.prefixes)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.executePreparedSearch(context.Background(), prepared, source); ErrorCode(err) != CodeInvalidCursor {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
