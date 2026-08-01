//go:build linux

// Package fsatomic supplies the shared durable no-replace operations used by
// source-index staging and publication.
package fsatomic

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// RemoveOwnedStaging removes child only through descriptors rooted at indexRoot.
// Missing root, staging, or child means no owned staging can remain.
func RemoveOwnedStaging(indexRoot, child string) error {
	root, err := unix.Open(indexRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(root)
	staging, err := unix.Openat(root, "staging", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(staging)
	fd, err := openVerifiedDirectory(staging, child, nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	var owned unix.Stat_t
	if err := unix.Fstat(fd, &owned); err != nil {
		unix.Close(fd)
		return err
	}
	if err := removeDirectory(fd); err != nil {
		unix.Close(fd)
		return err
	}
	if err := unix.Close(fd); err != nil {
		return err
	}
	var current unix.Stat_t
	if err := unix.Fstatat(staging, child, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != owned.Dev || current.Ino != owned.Ino || current.Mode&unix.S_IFMT != unix.S_IFDIR {
		return os.ErrInvalid
	}
	return unix.Unlinkat(staging, child, unix.AT_REMOVEDIR)
}

func removeDirectory(fd int) error {
	readFD, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(readFD), "staging")
	entries, err := f.ReadDir(-1)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		var stat unix.Stat_t
		if err := unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			child, err := openVerifiedDirectory(fd, name, &stat)
			if err != nil {
				return err
			}
			err = removeDirectory(child)
			closeErr := unix.Close(child)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if err := unix.Unlinkat(fd, name, unix.AT_REMOVEDIR); err != nil {
				return err
			}
			continue
		}
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return err
		}
	}
	return nil
}

// openVerifiedDirectory binds traversal to an opened object.  openat2 prevents
// path escape, symlinks, magic links, and mounts; fstat then binds an optional
// inspected entry to that descriptor before recursion can continue.
func openVerifiedDirectory(parent int, name string, inspected *unix.Stat_t) (int, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return -1, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFDIR || (inspected != nil && (opened.Dev != inspected.Dev || opened.Ino != inspected.Ino)) {
		unix.Close(fd)
		return -1, os.ErrInvalid
	}
	return fd, nil
}

func RenameNoReplace(source, target string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err == nil {
		return nil
	} else if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return os.ErrInvalid
	} else {
		return err
	}
}

func SyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
