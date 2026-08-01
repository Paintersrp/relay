//go:build linux || darwin || freebsd || netbsd

package zoektread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/query"

	"relay/internal/sourceindex/zoektbuild"
)

func TestSearchUsesCaseSensitiveContentSubstring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "000000.zoekt")
	generation := strings.Repeat("a", 64)
	meta := Metadata{RepositoryName: "relay-generation/" + generation, Branch: "relay-revision", Version: strings.Repeat("b", 40), IndexOptions: strings.Repeat("c", 64), Values: map[string]string{"relay_generation_id": generation}}
	if err := zoektbuild.Write(path, generation, 0, zoektbuild.Metadata{RepositoryName: meta.RepositoryName, Branch: meta.Branch, Version: meta.Version, IndexOptions: meta.IndexOptions, Values: meta.Values}, []zoektbuild.Document{{Name: "literal-in-name", Content: []byte("lowercase content")}, {Name: "content", Content: []byte("Needle needle")}}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(f, generation, 0, meta)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Search(context.Background(), "Needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 1 || got.Matches[0].FileName != "content" {
		t.Fatalf("matches = %#v", got.Matches)
	}
	got, err = r.Search(context.Background(), "needle", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 0 {
		t.Fatalf("case-sensitive content matches = %#v", got.Matches)
	}
}

func TestSearchRequiresPositiveLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "000000.zoekt")
	generation := strings.Repeat("a", 64)
	meta := Metadata{RepositoryName: "relay-generation/" + generation, Branch: "relay-revision", Version: strings.Repeat("b", 40), IndexOptions: strings.Repeat("c", 64), Values: map[string]string{"relay_generation_id": generation}}
	if err := zoektbuild.Write(path, generation, 0, zoektbuild.Metadata{RepositoryName: meta.RepositoryName, Branch: meta.Branch, Version: meta.Version, IndexOptions: meta.IndexOptions, Values: meta.Values}, []zoektbuild.Document{{Name: "content", Content: []byte("Needle needle")}}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(f, generation, 0, meta)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, limit := range []int{0, -1} {
		if _, err := r.Search(context.Background(), "Needle", limit); !errors.Is(err, ErrInvalid) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

// fakeSearcher captures the exact options and serves canned results.
type fakeSearcher struct {
	options *zoekt.SearchOptions
	result  *zoekt.SearchResult
	err     error
}

func (f *fakeSearcher) Search(_ context.Context, _ query.Q, opts *zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	f.options = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
func (f *fakeSearcher) List(context.Context, query.Q, *zoekt.ListOptions) (*zoekt.RepoList, error) {
	return nil, nil
}
func (f *fakeSearcher) Close()         {}
func (f *fakeSearcher) String() string { return "fake" }

func filenameResult(name, repository, version string, branches []string) *zoekt.SearchResult {
	return &zoekt.SearchResult{
		Files: []zoekt.FileMatch{{
			FileName: name, Repository: repository, Version: version, Branches: branches,
			LineMatches: []zoekt.LineMatch{{Line: []byte(name), FileName: true}},
		}},
		Stats: zoekt.Stats{FileCount: 1, MatchCount: 1},
	}
}

func TestSearchSetsEveryRequiredOptionExplicitly(t *testing.T) {
	searcher := &fakeSearcher{result: &zoekt.SearchResult{}}
	r := &Reader{searcher: searcher}
	if _, err := r.Search(context.Background(), "abc", 5); err != nil {
		t.Fatal(err)
	}
	o := searcher.options
	if o == nil {
		t.Fatal("no options captured")
	}
	for name, value := range map[string]int{
		"ShardMaxMatchCount":     o.ShardMaxMatchCount,
		"TotalMaxMatchCount":     o.TotalMaxMatchCount,
		"ShardRepoMaxMatchCount": o.ShardRepoMaxMatchCount,
		"MaxDocDisplayCount":     o.MaxDocDisplayCount,
		"MaxMatchDisplayCount":   o.MaxMatchDisplayCount,
	} {
		if value != 5 {
			t.Fatalf("%s = %d, want 5", name, value)
		}
	}
	if o.Whole || o.ChunkMatches || o.NumContextLines != 0 || o.DebugScore {
		t.Fatalf("unexpected display options: %#v", o)
	}
	if o.ShardMaxMatchCount <= 0 {
		t.Fatal("search options must stay positive for a zero-document generation")
	}
}

func TestSearchRetainsCompletionEvidence(t *testing.T) {
	searcher := &fakeSearcher{result: &zoekt.SearchResult{Stats: zoekt.Stats{
		Crashes: 1, FilesSkipped: 2, ShardsSkipped: 3, ShardsSkippedFilter: 4, FileCount: 5, MatchCount: 6, FlushReason: zoekt.FlushReasonMaxSize,
	}}}
	r := &Reader{searcher: searcher}
	got, err := r.Search(context.Background(), "abc", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Crashes != 1 || got.FilesSkipped != 2 || got.ShardsSkipped != 3 || got.ShardsSkippedFilter != 4 || got.FileCount != 5 || got.MatchCount != 6 || got.FlushReason != FlushReasonMaxSize {
		t.Fatalf("completion evidence = %#v", got)
	}
}

func TestSearchRejectsUnexpectedFilenameResultPayload(t *testing.T) {
	base := func(files []zoekt.FileMatch) *Reader {
		return &Reader{searcher: &fakeSearcher{result: &zoekt.SearchResult{Files: files, Stats: zoekt.Stats{FileCount: len(files), MatchCount: len(files)}}}}
	}
	valid := func() zoekt.FileMatch {
		return zoekt.FileMatch{FileName: "a.txt", Repository: "relay-generation/x", Version: strings.Repeat("b", 40), Branches: []string{"relay-revision"}, LineMatches: []zoekt.LineMatch{{Line: []byte("a.txt"), FileName: true}}}
	}
	for _, tc := range []struct {
		name string
		file zoekt.FileMatch
	}{
		{"content line match", func() zoekt.FileMatch {
			f := valid()
			f.LineMatches = []zoekt.LineMatch{{Line: []byte("content line"), FileName: false}}
			return f
		}()},
		{"multiple line matches", func() zoekt.FileMatch {
			f := valid()
			f.LineMatches = append(f.LineMatches, zoekt.LineMatch{Line: []byte("a.txt"), FileName: true})
			return f
		}()},
		{"no line match", func() zoekt.FileMatch {
			f := valid()
			f.LineMatches = nil
			return f
		}()},
		{"line does not match filename", func() zoekt.FileMatch {
			f := valid()
			f.LineMatches = []zoekt.LineMatch{{Line: []byte("other"), FileName: true}}
			return f
		}()},
		{"chunk matches", func() zoekt.FileMatch {
			f := valid()
			f.ChunkMatches = []zoekt.ChunkMatch{{Ranges: []zoekt.Range{{Start: zoekt.Location{ByteOffset: 0}, End: zoekt.Location{ByteOffset: 1}}}}}
			return f
		}()},
		{"whole file content", func() zoekt.FileMatch {
			f := valid()
			f.Content = []byte("whole file")
			return f
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := base([]zoekt.FileMatch{tc.file}).Search(context.Background(), "abc", 5); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want invalid", err)
			}
		})
	}
	t.Run("valid filename result retained", func(t *testing.T) {
		r := base([]zoekt.FileMatch{valid()})
		got, err := r.Search(context.Background(), "abc", 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Matches) != 1 || got.Matches[0].FileName != "a.txt" || got.Matches[0].Repository != "relay-generation/x" || got.Matches[0].Version != strings.Repeat("b", 40) || len(got.Matches[0].Branches) != 1 || got.Matches[0].Branches[0] != "relay-revision" {
			t.Fatalf("matches = %#v", got.Matches)
		}
	})
}

func TestSearchRejectsNilResultAndUnknownFlushReason(t *testing.T) {
	if _, err := (&Reader{searcher: &fakeSearcher{result: nil}}).Search(context.Background(), "abc", 5); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil result error = %v", err)
	}
	if _, err := (&Reader{searcher: &fakeSearcher{result: &zoekt.SearchResult{Stats: zoekt.Stats{FlushReason: zoekt.FlushReason(8)}}}}).Search(context.Background(), "abc", 5); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown flush error = %v", err)
	}
	if _, err := (&Reader{searcher: nil}).Search(context.Background(), "abc", 5); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil reader error = %v", err)
	}
}

func TestSearchCompletionStatsMatchFiles(t *testing.T) {
	file := filenameResult("a.txt", "relay-generation/x", strings.Repeat("b", 40), []string{"relay-revision"})
	r := &Reader{searcher: &fakeSearcher{result: file}}
	got, err := r.Search(context.Background(), "abc", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.FileCount != 1 || got.MatchCount != 1 || len(got.Matches) != 1 {
		t.Fatalf("completion = %#v", got)
	}
}
