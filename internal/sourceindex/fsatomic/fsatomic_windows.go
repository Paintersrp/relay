//go:build windows

package fsatomic

import (
	"os"

	"golang.org/x/sys/windows"
)

func RenameNoReplace(source, target string) error {
	return windows.MoveFile(windows.StringToUTF16Ptr(source), windows.StringToUTF16Ptr(target))
}
func RemoveOwnedGeneration(string, string) error                   { return os.ErrInvalid }
func RemoveOwnedGenerationStaging(string, string, ...string) error { return os.ErrInvalid }
func RemoveOwnedStaging(string, string) error                      { return os.ErrInvalid }

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
