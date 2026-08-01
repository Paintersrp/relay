//go:build !linux && !darwin && !freebsd && !netbsd

package reader

import (
	"errors"
	"os"

	"relay/internal/sourceindex/indexer"
)

// closedFS fails closed: platforms without a descriptor-bound implementation
// can never anchor a generation.
type closedFS struct{}

func defaultDirFS() dirFS { return closedFS{} }

func (closedFS) OpenRoot(string) (dirHandle, indexer.FileIdentity, error) {
	return 0, indexer.FileIdentity{}, ErrUnsupportedPlatform
}
func (closedFS) OpenChild(dirHandle, string) (dirHandle, indexer.FileIdentity, error) {
	return 0, indexer.FileIdentity{}, ErrUnsupportedPlatform
}
func (closedFS) Identity(dirHandle) (indexer.FileIdentity, error) {
	return indexer.FileIdentity{}, ErrUnsupportedPlatform
}
func (closedFS) ReadFile(dirHandle, string, int64) ([]byte, indexer.FileIdentity, error) {
	return nil, indexer.FileIdentity{}, ErrUnsupportedPlatform
}
func (closedFS) OpenFile(dirHandle, string) (*os.File, indexer.FileIdentity, error) {
	return nil, indexer.FileIdentity{}, ErrUnsupportedPlatform
}
func (closedFS) List(dirHandle) ([]dirEntry, error) {
	return nil, ErrUnsupportedPlatform
}
func (closedFS) Close(dirHandle) error {
	return errors.New("closed filesystem")
}
