//go:build linux || darwin || freebsd || netbsd

package indexer

import (
	"errors"
	"os"
	"syscall"
)

// FileIdentity identifies one regular file by device and inode.
type FileIdentity struct {
	Device uint64
	Inode  uint64
}

func artifactFileIdentity(info os.FileInfo) (FileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return FileIdentity{}, errors.New("unreliable file identity")
	}
	return FileIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}
