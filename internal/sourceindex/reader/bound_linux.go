//go:build linux

package reader

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"

	"relay/internal/sourceindex/indexer"
)

// unixFS anchors every reader filesystem operation to open descriptors with
// openat2 constraints equivalent to RESOLVE_BENEATH, RESOLVE_NO_SYMLINKS,
// RESOLVE_NO_MAGICLINKS, and RESOLVE_NO_XDEV.
type unixFS struct{}

func defaultDirFS() dirFS { return unixFS{} }

var resolveHard uint64 = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV

func openHard(dirfd int, name string, extra uint64) (int, error) {
	return unix.Openat2(dirfd, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY|unix.O_CLOEXEC) | extra,
		Resolve: resolveHard,
	})
}

func (unixFS) OpenRoot(path string) (dirHandle, indexer.FileIdentity, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return 0, indexer.FileIdentity{}, osErr(err)
	}
	id, err := fstatIdentity(fd)
	if err != nil {
		unix.Close(fd)
		return 0, indexer.FileIdentity{}, err
	}
	return dirHandle(fd), id, nil
}

func (unixFS) OpenChild(parent dirHandle, name string) (dirHandle, indexer.FileIdentity, error) {
	fd, err := openHard(int(parent), name, uint64(unix.O_DIRECTORY))
	if err != nil {
		return 0, indexer.FileIdentity{}, osErr(err)
	}
	id, err := fstatIdentity(fd)
	if err != nil {
		unix.Close(fd)
		return 0, indexer.FileIdentity{}, err
	}
	return dirHandle(fd), id, nil
}

func (unixFS) Identity(dir dirHandle) (indexer.FileIdentity, error) {
	return fstatIdentity(int(dir))
}

func (unixFS) ReadFile(dir dirHandle, name string, limit int64) ([]byte, indexer.FileIdentity, error) {
	fd, err := openHard(int(dir), name, 0)
	if err != nil {
		return nil, indexer.FileIdentity{}, osErr(err)
	}
	b, id, err := readBounded(fd, limit)
	if err != nil {
		unix.Close(fd)
		return nil, indexer.FileIdentity{}, err
	}
	if err := unix.Close(fd); err != nil {
		return nil, indexer.FileIdentity{}, err
	}
	return b, id, nil
}

func (unixFS) OpenFile(dir dirHandle, name string) (*os.File, indexer.FileIdentity, error) {
	fd, err := openHard(int(dir), name, 0)
	if err != nil {
		return nil, indexer.FileIdentity{}, osErr(err)
	}
	id, err := fstatFileIdentity(fd)
	if err != nil {
		unix.Close(fd)
		return nil, indexer.FileIdentity{}, err
	}
	return os.NewFile(uintptr(fd), name), id, nil
}

func (unixFS) List(dir dirHandle) ([]dirEntry, error) {
	dup, err := unix.Dup(int(dir))
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(dup), "directory")
	infos, err := f.Readdir(-1)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	entries := make([]dirEntry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, dirEntry{Name: info.Name(), Mode: info.Mode()})
	}
	return entries, nil
}

func (unixFS) Close(dir dirHandle) error {
	return osErr(unix.Close(int(dir)))
}

// fstatIdentity requires a directory and records its device and inode identity.
func fstatIdentity(fd int) (indexer.FileIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return indexer.FileIdentity{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return indexer.FileIdentity{}, os.ErrInvalid
	}
	return indexer.FileIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino)}, nil
}

// fstatFileIdentity requires a regular file with link count one and records
// its device and inode identity.
func fstatFileIdentity(fd int) (indexer.FileIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return indexer.FileIdentity{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return indexer.FileIdentity{}, os.ErrInvalid
	}
	return indexer.FileIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino)}, nil
}

// readBounded reads at most limit bytes from an already-open regular file,
// requiring a regular file with link count one.
func readBounded(fd int, limit int64) ([]byte, indexer.FileIdentity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, indexer.FileIdentity{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return nil, indexer.FileIdentity{}, os.ErrInvalid
	}
	if st.Size > limit {
		return nil, indexer.FileIdentity{}, os.ErrInvalid
	}
	b := make([]byte, st.Size)
	var read int
	for int64(read) < st.Size {
		n, err := unix.Read(fd, b[read:])
		if n > 0 {
			read += n
		}
		if err != nil {
			return nil, indexer.FileIdentity{}, err
		}
		if n == 0 {
			break
		}
	}
	if int64(read) != st.Size {
		return nil, indexer.FileIdentity{}, io.ErrUnexpectedEOF
	}
	return b, indexer.FileIdentity{Device: uint64(st.Dev), Inode: uint64(st.Ino)}, nil
}

func osErr(err error) error {
	if errors.Is(err, unix.ENOENT) {
		return os.ErrNotExist
	}
	if err != nil {
		return err
	}
	return nil
}
