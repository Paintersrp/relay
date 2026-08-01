//go:build windows

package supervisor

import "os"

func executable(os.FileInfo) bool { return true }
