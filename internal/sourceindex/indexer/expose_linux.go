//go:build linux

package indexer

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func exposeNoReplace(source, target string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err == nil {
		return nil
	} else if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		// link(2) does not support directories, so systems without renameat2
		// cannot safely meet the no-replace publication contract.
		return os.ErrInvalid
	} else {
		return err
	}
}
