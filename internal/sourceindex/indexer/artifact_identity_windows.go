//go:build windows

package indexer

import (
	"errors"
	"os"
)

type fileIdentity struct{}

// os.FileInfo does not expose stable Windows file IDs and link counts. Reject
// verification rather than accepting artifacts whose identity cannot be proven.
func artifactFileIdentity(os.FileInfo) (fileIdentity, error) {
	return fileIdentity{}, errors.New("unreliable file identity")
}
