//go:build !linux && !darwin && !freebsd && !netbsd

package indexer

import (
	"errors"
	"os"
)

type FileIdentity struct {
	Device uint64
	Inode  uint64
}

// os.FileInfo does not expose stable file IDs and link counts on this
// platform. Reject verification rather than accepting artifacts whose
// identity cannot be proven.
func artifactFileIdentity(os.FileInfo) (FileIdentity, error) {
	return FileIdentity{}, errors.New("unreliable file identity")
}
