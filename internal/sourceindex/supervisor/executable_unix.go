//go:build !windows

package supervisor

import "os"

func executable(info os.FileInfo) bool { return info.Mode().Perm()&0111 != 0 }
