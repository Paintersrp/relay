//go:build windows

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
type Result struct {
	Matches                              []Match
	Crashes, FilesSkipped, ShardsSkipped int
	Flush                                bool
}
type Reader struct{}

func Open(*os.File, string, int, Metadata) (*Reader, error)         { return nil, ErrUnsupported }
func (*Reader) Search(context.Context, string, int) (Result, error) { return Result{}, ErrUnsupported }
func (*Reader) Close()                                              {}
