//go:build windows

package pipeline

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type defaultProcessController struct{}

var windowsJobs sync.Map

func (defaultProcessController) PrepareCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("configure job object: %w", err)
	}
	windowsJobs.Store(cmd, job)
	return nil
}

func (defaultProcessController) Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error) {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ProcessIdentity{}, ErrProcessUnverifiable
	}
	rawJob, ok := windowsJobs.Load(cmd)
	if !ok {
		return ProcessIdentity{}, fmt.Errorf("%w: missing job object", ErrProcessUnverifiable)
	}
	job := rawJob.(windows.Handle)
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("open process for job assignment: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		windows.CloseHandle(handle)
		return ProcessIdentity{}, fmt.Errorf("assign process to job object: %w", err)
	}
	windows.CloseHandle(handle)
	createdAt, err := windowsProcessCreationTime(cmd.Process.Pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	return ProcessIdentity{
		PID:       cmd.Process.Pid,
		GroupID:   cmd.Process.Pid,
		StartedAt: createdAt.UTC().Format(time.RFC3339Nano),
		Platform:  runtime.GOOS,
		Nonce:     fmt.Sprintf("%p", cmd),
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

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return ProcessTerminationResult{VerifiedAbsent: true, AlreadyAbsent: true}, nil
		}
		return ProcessTerminationResult{}, err
	}
	job, closeJob, err := windowsJobForIdentity(identity)
	if err != nil {
		return ProcessTerminationResult{}, err
	}
	defer closeJob()
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return ProcessTerminationResult{}, fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		running, err := defaultProcessController{}.IsRunning(identity)
		if err != nil {
			return ProcessTerminationResult{}, err
		}
		if !running {
			return ProcessTerminationResult{VerifiedAbsent: true, Forced: true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return ProcessTerminationResult{VerifiedAbsent: true, Forced: true}, nil
		}
		return ProcessTerminationResult{}, err
	}
	return ProcessTerminationResult{Forced: true}, fmt.Errorf("%w: process still present after job termination", ErrProcessUnverifiable)
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

func windowsJobForIdentity(identity ProcessIdentity) (windows.Handle, func(), error) {
	if identity.Nonce == "" {
		return 0, func() {}, fmt.Errorf("%w: missing job identity", ErrProcessUnverifiable)
	}
	var job windows.Handle
	var found bool
	windowsJobs.Range(func(key, value any) bool {
		if fmt.Sprintf("%p", key) == identity.Nonce {
			job = value.(windows.Handle)
			found = true
			return false
		}
		return true
	})
	if !found {
		return 0, func() {}, fmt.Errorf("%w: job handle unavailable", ErrProcessUnverifiable)
	}
	return job, func() {
		windows.CloseHandle(job)
		windowsJobs.Range(func(key, value any) bool {
			if value.(windows.Handle) == job {
				windowsJobs.Delete(key)
				return false
			}
			return true
		})
	}, nil
}
