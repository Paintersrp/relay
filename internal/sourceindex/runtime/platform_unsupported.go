//go:build !linux

package sourceindexruntime

func runtimeSupported() bool { return false }
