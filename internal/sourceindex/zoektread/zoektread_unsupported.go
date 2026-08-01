//go:build !linux && !darwin && !freebsd && !netbsd

// Package zoektread isolates the pinned Zoekt platform limitation.
package zoektread

import (
	"context"
	"errors"
	"os"
)

var ErrInvalid = errors.New("invalid zoekt shard")
var ErrUnsupported = errors.New("zoekt reading unsupported")

func Supported() bool { return false }

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
type Reader struct{}

func Open(*os.File, string, int, Metadata) (*Reader, error) { return nil, ErrUnsupported }
func (*Reader) Search(context.Context, string, int) (Result, error) {
	return Result{}, ErrUnsupported
}
func (*Reader) Close() error { return nil }
