//go:build !windows

package pipeline

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type defaultProcessController struct{}

func (defaultProcessController) PrepareCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func (defaultProcessController) Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error) {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ProcessIdentity{}, ErrProcessUnverifiable
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	fingerprint, err := unixProcessStartFingerprint(cmd.Process.Pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	return ProcessIdentity{
		PID:       cmd.Process.Pid,
		GroupID:   pgid,
		StartedAt: fingerprint,
		Platform:  runtime.GOOS,
	}, nil
}

func (defaultProcessController) IsRunning(identity ProcessIdentity) (bool, error) {
	if err := verifyUnixProcessIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) error {
	if err := verifyUnixProcessIdentity(identity); err != nil {
		return err
	}
	groupID := identity.GroupID
	if groupID <= 0 {
		groupID = identity.PID
	}
	if err := syscall.Kill(-groupID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
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
	if err := verifyUnixProcessIdentity(identity); err != nil {
		return err
	}
	if err := syscall.Kill(-groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func verifyUnixProcessIdentity(identity ProcessIdentity) error {
	if identity.PID <= 0 || identity.StartedAt == "" {
		return ErrProcessUnverifiable
	}
	current, err := unixProcessStartFingerprint(identity.PID)
	if err != nil {
		return err
	}
	if current != identity.StartedAt {
		return ErrProcessIdentityMismatch
	}
	return nil
}

func unixProcessStartFingerprint(pid int) (string, error) {
	if runtime.GOOS != "linux" {
		return "", ErrProcessUnverifiable
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrProcessNotRunning
		}
		return "", err
	}
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 >= len(text) {
		return "", fmt.Errorf("%w: malformed proc stat", ErrProcessUnverifiable)
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("%w: missing proc starttime", ErrProcessUnverifiable)
	}
	return fields[19], nil
}
