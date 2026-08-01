//go:build darwin || freebsd || netbsd

package fsatomic

import "os"

func RenameNoReplace(string, string) error { return os.ErrInvalid }

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
