package sourcegateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
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
