//go:build !windows

package supervisor

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// A surviving group proves descendants still own inherited descriptors.
func terminateResidualProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, 0) == nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
