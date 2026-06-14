//go:build !windows

package handlers

import (
	"fmt"
	"syscall"
)

func probeProcessAlivePlatform(pid int) processProbeResult {
	err := syscall.Kill(pid, 0)
	switch err {
	case nil, syscall.EPERM:
		return processProbeResult{Alive: true, Known: true}
	case syscall.ESRCH:
		return processProbeResult{Alive: false, Known: true}
	default:
		return processProbeResult{Known: false, Error: fmt.Sprintf("kill probe failed: %v", err)}
	}
}
