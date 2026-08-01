//go:build darwin || freebsd || netbsd

package indexer

import "os"

// These platforms have no atomic directory rename-without-replacement API.
func exposeNoReplace(string, string) error { return os.ErrInvalid }
