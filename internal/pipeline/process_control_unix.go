//go:build linux

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
	if err := verifyLeaderIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			groupID := identity.GroupID
			if groupID <= 0 {
				groupID = identity.PID
			}
			present, probeErr := processGroupPresent(groupID)
			if probeErr != nil {
				return false, probeErr
			}
			if present {
				return false, fmt.Errorf("%w: leader absent but process group %d is still present", ErrProcessUnverifiable, groupID)
			}
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	if err := verifyLeaderIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			groupID := identity.GroupID
			if groupID <= 0 {
				groupID = identity.PID
			}
			present, probeErr := processGroupPresent(groupID)
			if probeErr != nil {
				return ProcessTerminationResult{}, probeErr
			}
			if present {
				return ProcessTerminationResult{}, fmt.Errorf("%w: leader absent but process group %d is still present", ErrProcessUnverifiable, groupID)
			}
			return ProcessTerminationResult{VerifiedAbsent: true, AlreadyAbsent: true}, nil
		}
		return ProcessTerminationResult{}, err
	}
	groupID := identity.GroupID
	if groupID <= 0 {
		groupID = identity.PID
	}
	return terminateVerifiedProcessGroup(groupID, gracefulTimeout)
}

func terminateVerifiedProcessGroup(groupID int, gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	if err := syscall.Kill(-groupID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return ProcessTerminationResult{}, err
	}
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		running, err := processGroupPresent(groupID)
		if err != nil {
			return ProcessTerminationResult{}, err
		}
		if !running {
			return ProcessTerminationResult{VerifiedAbsent: true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return ProcessTerminationResult{}, err
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		running, err := processGroupPresent(groupID)
		if err != nil {
			return ProcessTerminationResult{}, err
		}
		if !running {
			return ProcessTerminationResult{VerifiedAbsent: true, Forced: true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ProcessTerminationResult{Forced: true}, fmt.Errorf("%w: process group %d still present after forced termination", ErrProcessUnverifiable, groupID)
}

func verifyLeaderIdentity(identity ProcessIdentity) error {
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

func processGroupPresent(groupID int) (bool, error) {
	if groupID <= 0 {
		return false, ErrProcessUnverifiable
	}
	if err := syscall.Kill(-groupID, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		if errors.Is(err, syscall.EPERM) {
			return true, nil
		}
		return true, err
	}
	return true, nil
}
