//go:build !linux && !darwin && !freebsd && !netbsd

// Package zoektbuild isolates the pinned Zoekt platform limitation.
package zoektbuild

import (
	"errors"
	"os"
)

type Document struct {
	Name    string
	Content []byte
}
type Metadata struct {
	RepositoryName, Branch, Version, IndexOptions string
	Values                                        map[string]string
}

var unsupported = errors.New("pinned zoekt index builder is unavailable on this platform")

func Write(string, string, int, Metadata, []Document) error     { return unsupported }
func Verify(string, string, int, Metadata) error                { return unsupported }
func Documents(string, string, int, Metadata) ([]string, error) { return nil, unsupported }
func VerifyFile(*os.File, string, int, Metadata) error          { return unsupported }
func DocumentsFile(*os.File, string, int, Metadata) ([]string, error) {
	return nil, unsupported
}
