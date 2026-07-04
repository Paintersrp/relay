//go:build linux

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type defaultProcessController struct{}

type unixOwnedProcess struct {
	cmd      *exec.Cmd
	identity ProcessIdentity
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	released bool
}

func (c defaultProcessController) StartOwned(ctx context.Context, spec CommandSpec) (OwnedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Dir = spec.WorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	identity, err := c.identity(cmd)
	if err != nil {
		owned := &unixOwnedProcess{
			cmd: cmd,
			identity: ProcessIdentity{
				PID:      cmd.Process.Pid,
				GroupID:  cmd.Process.Pid,
				Platform: runtime.GOOS,
			},
			stdout: stdout,
			stderr: stderr,
		}
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		waitErr := waitForCommandBounded(cmd, 2*time.Second)
		cleanup := ProcessTerminationResult{Forced: true}
		if waitErr == nil {
			cleanup.VerifiedAbsent = true
		}
		return owned, &OwnedStartError{
			Cause:         err,
			NativeStarted: true,
			Identity:      owned.identity,
			Cleanup:       cleanup,
			CleanupError:  waitErr,
		}
	}
	return &unixOwnedProcess{cmd: cmd, identity: identity, stdout: stdout, stderr: stderr}, nil
}

func (defaultProcessController) OpenOwned(identity ProcessIdentity) (OwnedProcess, error) {
	return &unixOwnedProcess{identity: identity}, nil
}

func (defaultProcessController) identity(cmd *exec.Cmd) (ProcessIdentity, error) {
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

func (p *unixOwnedProcess) Identity() ProcessIdentity { return p.identity }

func (p *unixOwnedProcess) Stdout() io.ReadCloser { return p.stdout }

func (p *unixOwnedProcess) Stderr() io.ReadCloser { return p.stderr }

func (p *unixOwnedProcess) Wait() error {
	if p.cmd == nil {
		return ErrProcessUnverifiable
	}
	return p.cmd.Wait()
}

func (p *unixOwnedProcess) TreeRunning() (bool, error) {
	identity := p.identity
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

func (p *unixOwnedProcess) Terminate(gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	identity := p.identity
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

func (p *unixOwnedProcess) Release() error {
	p.released = true
	return nil
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
