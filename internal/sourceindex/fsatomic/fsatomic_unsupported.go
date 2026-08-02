//go:build !linux && !windows

package fsatomic

import "os"

func RenameNoReplace(string, string) error                      { return os.ErrInvalid }
func RemoveOwnedGeneration(string, string) error                { return os.ErrInvalid }
func RemoveOwnedGenerationAttempt(string, string, string) error { return os.ErrInvalid }
func RemoveAllOwnedGenerationAttempts(string, string) error     { return os.ErrInvalid }

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
