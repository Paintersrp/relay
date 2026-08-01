//go:build windows

package indexer

import "golang.org/x/sys/windows"

// MoveFile fails when the destination exists; unlike MoveFileEx it never
// requests replacement semantics.
func exposeNoReplace(source, target string) error {
	return windows.MoveFile(windows.StringToUTF16Ptr(source), windows.StringToUTF16Ptr(target))
}
