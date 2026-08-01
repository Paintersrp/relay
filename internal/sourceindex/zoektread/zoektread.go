//go:build linux || darwin || freebsd || netbsd

// Package zoektread isolates Zoekt's unstable reader APIs.
package zoektread

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"time"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
)

var ErrInvalid = errors.New("invalid zoekt shard")
var ErrUnsupported = errors.New("zoekt reading unsupported")

func Supported() bool { return true }

type Metadata struct {
	RepositoryName, Branch, Version, IndexOptions string
	Values                                        map[string]string
}
type Match struct {
	FileName, Repository, Version string
	Branches                      []string
}

// FlushReason mirrors the pinned Zoekt completion reasons.
type FlushReason uint8

const (
	FlushReasonNone FlushReason = iota
	FlushReasonTimerExpired
	FlushReasonFinalFlush
	FlushReasonMaxSize
)

type Result struct {
	Matches                              []Match
	Crashes, FilesSkipped, ShardsSkipped int
	ShardsSkippedFilter                  int
	FlushReason                          FlushReason
	FileCount, MatchCount                int
}
type Reader struct{ searcher zoekt.Searcher }

func shardID(generation string, sequence int) string {
	s := sha256.Sum256([]byte(generation + ":" + fmtInt(sequence)))
	return hex.EncodeToString(s[:])[:20]
}
func fmtInt(v int) string {
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 12)
	for v > 0 {
		b = append(b, byte('0'+v%10))
		v /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// Open consumes f only after metadata was read from the exact opened descriptor.
func Open(f *os.File, generation string, sequence int, want Metadata) (*Reader, error) {
	indexFile, err := index.NewIndexFile(f)
	if err != nil {
		return nil, err
	}
	repos, md, err := index.ReadMetadata(indexFile)
	if err != nil {
		indexFile.Close()
		return nil, err
	}
	if md == nil || md.ID != shardID(generation, sequence) || !md.IndexTime.Equal(time.Unix(0, 0).UTC()) || len(repos) != 1 {
		indexFile.Close()
		return nil, ErrInvalid
	}
	r := repos[0]
	if r.Tombstone || r.Name != want.RepositoryName || r.IndexOptions != want.IndexOptions || len(r.Branches) != 1 || r.Branches[0].Name != want.Branch || r.Branches[0].Version != want.Version || len(r.Metadata) != len(want.Values) {
		indexFile.Close()
		return nil, ErrInvalid
	}
	for k, v := range want.Values {
		if r.Metadata[k] != v {
			indexFile.Close()
			return nil, ErrInvalid
		}
	}
	s, err := index.NewSearcher(indexFile)
	if err != nil {
		indexFile.Close()
		return nil, err
	}
	return &Reader{searcher: s}, nil
}

func (r *Reader) Search(ctx context.Context, literal string, limit int) (Result, error) {
	if r == nil || r.searcher == nil || limit < 1 {
		return Result{}, ErrInvalid
	}
	q := &query.Type{Type: query.TypeFileName, Child: &query.Substring{Pattern: literal, CaseSensitive: true, Content: true}}
	o := &zoekt.SearchOptions{ShardMaxMatchCount: limit, TotalMaxMatchCount: limit, ShardRepoMaxMatchCount: limit, MaxDocDisplayCount: limit, MaxMatchDisplayCount: limit, Whole: false, ChunkMatches: false, NumContextLines: 0, DebugScore: false}
	result, err := r.searcher.Search(ctx, q, o)
	if err != nil {
		return Result{}, err
	}
	if result == nil {
		return Result{}, ErrInvalid
	}
	out := Result{Crashes: result.Crashes, FilesSkipped: result.FilesSkipped, ShardsSkipped: result.ShardsSkipped, ShardsSkippedFilter: result.ShardsSkippedFilter, FileCount: result.FileCount, MatchCount: result.MatchCount}
	switch result.FlushReason {
	case 0:
		out.FlushReason = FlushReasonNone
	case zoekt.FlushReasonFinalFlush:
		out.FlushReason = FlushReasonFinalFlush
	case zoekt.FlushReasonTimerExpired:
		out.FlushReason = FlushReasonTimerExpired
	case zoekt.FlushReasonMaxSize:
		out.FlushReason = FlushReasonMaxSize
	default:
		return Result{}, ErrInvalid
	}
	for _, f := range result.Files {
		if err := validFilenameResult(&f); err != nil {
			return Result{}, err
		}
		out.Matches = append(out.Matches, Match{FileName: f.FileName, Repository: f.Repository, Version: f.Version, Branches: append([]string(nil), f.Branches...)})
	}
	return out, nil
}

// validFilenameResult establishes the exact expected TypeFileName result shape
// for the pinned Zoekt revision: exactly one filename LineMatch whose line is
// the filename, and no content, chunk, or whole-file payload. Any deviation
// means filename-result mode was not honored.
func validFilenameResult(f *zoekt.FileMatch) error {
	if len(f.ChunkMatches) != 0 || len(f.Content) != 0 {
		return ErrInvalid
	}
	if len(f.LineMatches) != 1 || !f.LineMatches[0].FileName {
		return ErrInvalid
	}
	if !bytes.Equal(f.LineMatches[0].Line, []byte(f.FileName)) {
		return ErrInvalid
	}
	return nil
}

func (r *Reader) Close() error {
	if r != nil && r.searcher != nil {
		r.searcher.Close()
		r.searcher = nil
	}
	return nil
}
