//go:build linux

// Package fsatomic supplies the shared durable no-replace operations used by
// source-index staging and publication.
package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// RemoveOwnedGeneration removes one exact generation directory through
// descriptors rooted at indexRoot. Missing paths mean no owned generation can
// remain.
func RemoveOwnedGeneration(indexRoot, generationID string) error {
	if !validHex(generationID, 64) {
		return os.ErrInvalid
	}
	return removeOwnedChild(indexRoot, "generations", generationID)
}

// RemoveOwnedGenerationStaging removes one exact generation staging directory
// through descriptors rooted at indexRoot.
func RemoveOwnedGenerationStaging(indexRoot, generationID string, nonce ...string) error {
	if !validHex(generationID, 64) || len(nonce) > 1 || len(nonce) == 1 && !validHex(nonce[0], 32) {
		return os.ErrInvalid
	}
	if len(nonce) == 1 {
		return removeOwnedChild(indexRoot, "staging", generationID+"-"+nonce[0])
	}
	return removeOwnedStagingAttempts(indexRoot, generationID)
}

// RemoveOwnedStaging is retained for the supervisor's existing staging
// cleanup boundary. New callers should use RemoveOwnedGenerationStaging.
func RemoveOwnedStaging(indexRoot, child string) error {
	if child == "" || strings.ContainsAny(child, `/\`) || child == "." || child == ".." {
		return os.ErrInvalid
	}
	return removeOwnedChild(indexRoot, "staging", child)
}

func removeOwnedChild(indexRoot, parentName, child string) error {
	if !filepath.IsAbs(indexRoot) || filepath.Clean(indexRoot) != indexRoot {
		return os.ErrInvalid
	}
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(root)
	parent, err := openVerifiedDirectory(root, parentName, nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := openVerifiedDirectory(parent, child, nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	var owned unix.Stat_t
	if err := unix.Fstat(fd, &owned); err != nil || owned.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(fd)
		if err == nil {
			err = os.ErrInvalid
		}
		return err
	}
	if err := validateCanonicalDirectory(fd); err != nil {
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
	if err := unix.Fstatat(parent, child, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if current.Dev != owned.Dev || current.Ino != owned.Ino || current.Mode&unix.S_IFMT != unix.S_IFDIR {
		return os.ErrInvalid
	}
	if err := unix.Unlinkat(parent, child, unix.AT_REMOVEDIR); err != nil {
		return err
	}
	if err := unix.Fsync(parent); err != nil {
		return err
	}
	return unix.Fsync(root)
}

func removeOwnedStagingAttempts(indexRoot, generationID string) error {
	if !filepath.IsAbs(indexRoot) || filepath.Clean(indexRoot) != indexRoot {
		return os.ErrInvalid
	}
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(root)
	parent, err := openVerifiedDirectory(root, "staging", nil)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(parent), "staging")
	entries, readErr := f.ReadDir(-1)
	closeErr := f.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	prefix, private := generationID+"-", ".relay-build-"+generationID+"-"
	for _, entry := range entries {
		name := entry.Name()
		if ownedStagingName(name, prefix, private) {
			if err := removeOwnedChild(indexRoot, "staging", name); err != nil {
				return err
			}
		}
	}
	return nil
}
func ownedStagingName(name, prefix, private string) bool {
	if strings.HasPrefix(name, prefix) {
		return validHex(strings.TrimPrefix(name, prefix), 32)
	}
	if !strings.HasPrefix(name, private) {
		return false
	}
	rest := strings.TrimPrefix(name, private)
	if len(rest) <= 33 || !validHex(rest[:32], 32) || rest[32] != '-' {
		return false
	}
	return rest[33:] != ""
}

// validateCanonicalDirectory refuses arbitrary content even when it sits below
// a correctly named owned directory. Interrupted staging may omit members.
func validateCanonicalDirectory(fd int) error {
	readFD, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(readFD), "owned")
	entries, err := f.ReadDir(-1)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	seen := map[[2]uint64]bool{}
	for _, entry := range entries {
		name := entry.Name()
		var stat unix.Stat_t
		if err := unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		key := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
		if seen[key] {
			return os.ErrInvalid
		}
		seen[key] = true
		mode := stat.Mode & unix.S_IFMT
		if mode == unix.S_IFREG {
			if stat.Nlink != 1 || (name != "generation.json" && name != "coverage.json" && name != "manifest.json") {
				return os.ErrInvalid
			}
			continue
		}
		if mode != unix.S_IFDIR || name != "shards" {
			return os.ErrInvalid
		}
		child, err := openVerifiedDirectory(fd, name, &stat)
		if err != nil {
			return err
		}
		err = validateShards(child)
		closeErr := unix.Close(child)
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func validateShards(fd int) error {
	readFD, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(readFD), "shards")
	entries, err := f.ReadDir(-1)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	seen := map[[2]uint64]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 12 || !strings.HasSuffix(name, ".zoekt") {
			return os.ErrInvalid
		}
		for _, c := range name[:6] {
			if c < '0' || c > '9' {
				return os.ErrInvalid
			}
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		key := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
		if seen[key] || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
			return os.ErrInvalid
		}
		seen[key] = true
	}
	return nil
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
			if err := unlinkOwnedDirectory(fd, name, &stat); err != nil {
				return err
			}
			continue
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
			return os.ErrInvalid
		}
		var current unix.Stat_t
		if err := unix.Fstatat(fd, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if current.Dev != stat.Dev || current.Ino != stat.Ino || current.Mode != stat.Mode || current.Nlink != stat.Nlink {
			return os.ErrInvalid
		}
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return err
		}
	}
	return nil
}

func unlinkOwnedDirectory(parent int, name string, inspected *unix.Stat_t) error {
	var current unix.Stat_t
	if err := unix.Fstatat(parent, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != inspected.Dev || current.Ino != inspected.Ino || current.Mode&unix.S_IFMT != unix.S_IFDIR {
		return os.ErrInvalid
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
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

func validHex(s string, length int) bool {
	if len(s) != length {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
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
