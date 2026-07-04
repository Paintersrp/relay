//go:build !windows && !linux

package pipeline

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

type defaultProcessController struct{}

func (defaultProcessController) PrepareCommand(cmd *exec.Cmd) error {
	return fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}

func (defaultProcessController) Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error) {
	return ProcessIdentity{}, fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}

func (defaultProcessController) IsRunning(identity ProcessIdentity) (bool, error) {
	return false, fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	return ProcessTerminationResult{}, fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}
