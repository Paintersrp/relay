//go:build linux

// Package fsatomic supplies descriptor-rooted source-index filesystem
// operations. Cleanup is deliberately narrow: only canonical generation
// objects and attributable build attempts may be removed.
package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func RemoveOwnedGeneration(indexRoot, generationID string) error {
	if !validHex(generationID, 64) {
		return os.ErrInvalid
	}
	return removeOwnedChild(indexRoot, "generations", generationID)
}

func RemoveOwnedGenerationAttempt(indexRoot, generationID, nonce string) error {
	if !validHex(generationID, 64) || !validHex(nonce, 32) {
		return os.ErrInvalid
	}
	if err := validateOwnedAttemptNames(indexRoot, generationID); err != nil {
		return err
	}
	if err := removeOwnedChild(indexRoot, "staging", generationID+"-"+nonce); err != nil {
		return err
	}
	return removePrivateAttempt(indexRoot, generationID, nonce)
}

func RemoveAllOwnedGenerationAttempts(indexRoot, generationID string) error {
	if !validHex(generationID, 64) {
		return os.ErrInvalid
	}
	return removeOwnedStagingAttempts(indexRoot, generationID)
}

func removeOwnedChild(indexRoot, parentName, child string) error {
	if !filepath.IsAbs(indexRoot) || filepath.Clean(indexRoot) != indexRoot {
		return os.ErrInvalid
	}
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
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
	if err := removeDirectory(fd, true, map[[2]uint64]bool{}); err != nil {
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
	return errors.Join(unix.Fsync(parent), unix.Fsync(root))
}

func removeOwnedStagingAttempts(indexRoot, generationID string) error {
	if !filepath.IsAbs(indexRoot) || filepath.Clean(indexRoot) != indexRoot {
		return os.ErrInvalid
	}
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
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
	defer unix.Close(parent)
	entries, err := listDirectory(parent)
	if err != nil {
		return err
	}
	prefix := generationID + "-"
	private := ".relay-build-" + generationID + "-"
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, prefix):
			if !validHex(strings.TrimPrefix(name, prefix), 32) {
				return os.ErrInvalid
			}
		case strings.HasPrefix(name, private):
			rest := strings.TrimPrefix(name, private)
			parts := strings.Split(rest, "-")
			if len(parts) != 2 || !validHex(parts[0], 32) || !validPrivateSuffix(parts[1]) {
				return os.ErrInvalid
			}
		default:
			continue
		}
		if err := removeOwnedChild(indexRoot, "staging", name); err != nil {
			return err
		}
	}
	return errors.Join(unix.Fsync(parent), unix.Fsync(root))
}

func removePrivateAttempt(indexRoot, generationID, nonce string) error {
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
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
	defer unix.Close(parent)
	entries, err := listDirectory(parent)
	if err != nil {
		return err
	}
	prefix := ".relay-build-" + generationID + "-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		rest := strings.TrimPrefix(entry.Name(), prefix)
		parts := strings.Split(rest, "-")
		if len(parts) != 2 || !validHex(parts[0], 32) || !validPrivateSuffix(parts[1]) {
			return os.ErrInvalid
		}
		if parts[0] == nonce {
			if err := removeOwnedChild(indexRoot, "staging", entry.Name()); err != nil {
				return err
			}
		}
	}
	return errors.Join(unix.Fsync(parent), unix.Fsync(root))
}

func validateOwnedAttemptNames(indexRoot, generationID string) error {
	root, err := unix.Openat2(unix.AT_FDCWD, indexRoot, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
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
	defer unix.Close(parent)
	entries, err := listDirectory(parent)
	if err != nil {
		return err
	}
	canonical := generationID + "-"
	private := ".relay-build-" + generationID + "-"
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, canonical) && !validHex(strings.TrimPrefix(name, canonical), 32) {
			return os.ErrInvalid
		}
		if strings.HasPrefix(name, private) {
			parts := strings.Split(strings.TrimPrefix(name, private), "-")
			if len(parts) != 2 || !validHex(parts[0], 32) || !validPrivateSuffix(parts[1]) {
				return os.ErrInvalid
			}
		}
	}
	return nil
}

func listDirectory(fd int) ([]os.DirEntry, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(dup), "directory")
	entries, readErr := f.ReadDir(-1)
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	return entries, closeErr
}

func removeDirectory(fd int, root bool, seen map[[2]uint64]bool) error {
	var self unix.Stat_t
	if err := unix.Fstat(fd, &self); err != nil {
		return err
	}
	key := [2]uint64{uint64(self.Dev), uint64(self.Ino)}
	if seen[key] {
		return os.ErrInvalid
	}
	seen[key] = true
	entries, err := listDirectory(fd)
	if err != nil {
		return err
	}
	if root {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	} else {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for i, entry := range entries {
			if !canonicalShardName(entry.Name()) {
				return os.ErrInvalid
			}
			sequence, err := strconv.Atoi(entry.Name()[:6])
			if err != nil || sequence != i {
				return os.ErrInvalid
			}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		var inspected unix.Stat_t
		if err := unix.Fstatat(fd, name, &inspected, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		mode := inspected.Mode & unix.S_IFMT
		if root {
			if mode == unix.S_IFDIR && name != "shards" || mode == unix.S_IFREG && name != "generation.json" && name != "coverage.json" && name != "manifest.json" || mode != unix.S_IFDIR && mode != unix.S_IFREG {
				return os.ErrInvalid
			}
		} else if mode != unix.S_IFREG || !canonicalShardName(name) {
			return os.ErrInvalid
		}
		if mode == unix.S_IFDIR {
			child, err := openVerifiedDirectory(fd, name, &inspected)
			if err != nil {
				return err
			}
			err = removeDirectory(child, false, seen)
			closeErr := unix.Close(child)
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if err := unlinkOwnedDirectory(fd, name, &inspected); err != nil {
				return err
			}
			continue
		}
		if inspected.Nlink != 1 {
			return os.ErrInvalid
		}
		var current unix.Stat_t
		if err := unix.Fstatat(fd, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if current.Dev != inspected.Dev || current.Ino != inspected.Ino || current.Mode != inspected.Mode || current.Nlink != inspected.Nlink {
			return os.ErrInvalid
		}
		key := [2]uint64{uint64(current.Dev), uint64(current.Ino)}
		if seen[key] {
			return os.ErrInvalid
		}
		seen[key] = true
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return err
		}
	}
	if err := requireEmpty(fd); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

func requireEmpty(fd int) error {
	entries, err := listDirectory(fd)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return os.ErrInvalid
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

func openVerifiedDirectory(parent int, name string, inspected *unix.Stat_t) (int, error) {
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return -1, err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if opened.Mode&unix.S_IFMT != unix.S_IFDIR || inspected != nil && (opened.Dev != inspected.Dev || opened.Ino != inspected.Ino) {
		unix.Close(fd)
		return -1, os.ErrInvalid
	}
	return fd, nil
}

func canonicalShardName(name string) bool {
	if len(name) != 12 || !strings.HasSuffix(name, ".zoekt") {
		return false
	}
	for _, c := range name[:6] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validPrivateSuffix(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
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
