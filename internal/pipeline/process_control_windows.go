//go:build windows

package pipeline

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

type defaultProcessController struct{}

func (defaultProcessController) PrepareCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return nil
}

func (defaultProcessController) Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error) {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ProcessIdentity{}, ErrProcessUnverifiable
	}
	createdAt, err := windowsProcessCreationTime(cmd.Process.Pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	return ProcessIdentity{
		PID:       cmd.Process.Pid,
		GroupID:   cmd.Process.Pid,
		StartedAt: createdAt.UTC().Format(time.RFC3339Nano),
		Platform:  runtime.GOOS,
	}, nil
}

func (defaultProcessController) IsRunning(identity ProcessIdentity) (bool, error) {
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) error {
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		return err
	}
	pid := strconv.Itoa(identity.PID)
	_ = exec.Command("taskkill.exe", "/PID", pid, "/T").Run()
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		running, err := defaultProcessController{}.IsRunning(identity)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		return err
	}
	if err := exec.Command("taskkill.exe", "/PID", pid, "/T", "/F").Run(); err != nil {
		return err
	}
	return nil
}

func verifyWindowsProcessIdentity(identity ProcessIdentity) error {
	if identity.PID <= 0 || identity.StartedAt == "" {
		return ErrProcessUnverifiable
	}
	createdAt, err := windowsProcessCreationTime(identity.PID)
	if err != nil {
		return err
	}
	expected, err := time.Parse(time.RFC3339Nano, identity.StartedAt)
	if err != nil {
		return ErrProcessUnverifiable
	}
	if !createdAt.Equal(expected) {
		return ErrProcessIdentityMismatch
	}
	return nil
}

func windowsProcessCreationTime(pid int) (time.Time, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return time.Time{}, ErrProcessNotRunning
		}
		return time.Time{}, fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(handle)

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, fmt.Errorf("get process times: %w", err)
	}
	return time.Unix(0, creation.Nanoseconds()).UTC(), nil
}
