//go:build windows

package supervisor

import "os/exec"

func configureProcess(*exec.Cmd) {}
func terminateProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
