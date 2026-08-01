//go:build linux || darwin || freebsd || netbsd

package indexer

import (
	"errors"
	"os"
	"syscall"
)

type fileIdentity struct {
	device uint64
	inode  uint64
}

func artifactFileIdentity(info os.FileInfo) (fileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fileIdentity{}, errors.New("unreliable file identity")
	}
	return fileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}
