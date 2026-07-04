//go:build windows

package pipeline

import (
	"crypto/rand"
	"encoding/hex"
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

type windowsJobRecord struct {
	name   string
	handle windows.Handle
}

type jobObjectBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

var (
	windowsJobsByCommand sync.Map
	windowsJobsByName    sync.Map
)

func (defaultProcessController) PrepareCommand(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	name, err := newWindowsJobName()
	if err != nil {
		return err
	}
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	job, err := windows.CreateJobObject(nil, namePtr)
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
	rec := windowsJobRecord{name: name, handle: job}
	windowsJobsByCommand.Store(cmd, rec)
	windowsJobsByName.Store(name, rec)
	return nil
}

func (defaultProcessController) Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error) {
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ProcessIdentity{}, ErrProcessUnverifiable
	}
	rawJob, ok := windowsJobsByCommand.Load(cmd)
	if !ok {
		return ProcessIdentity{}, fmt.Errorf("%w: missing job object", ErrProcessUnverifiable)
	}
	rec := rawJob.(windowsJobRecord)
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("open process for job assignment: %w", err)
	}
	if err := windows.AssignProcessToJobObject(rec.handle, handle); err != nil {
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
		Nonce:     rec.name,
	}, nil
}

func (defaultProcessController) IsRunning(identity ProcessIdentity) (bool, error) {
	if rec, ok := windowsJobRecordForIdentity(identity); ok {
		active, err := windowsJobActiveProcessCount(rec.handle)
		if err != nil {
			return false, err
		}
		if active == 0 {
			releaseWindowsJob(rec.name, rec.handle)
			return false, nil
		}
		return true, nil
	}
	if err := verifyWindowsProcessIdentity(identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return false, nil
		}
		return false, err
	}
	return true, fmt.Errorf("%w: job handle unavailable for matching root process", ErrProcessUnverifiable)
}

func (defaultProcessController) TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	rec, ok := windowsJobRecordForIdentity(identity)
	if !ok {
		if err := verifyWindowsProcessIdentity(identity); errors.Is(err, ErrProcessNotRunning) {
			return ProcessTerminationResult{VerifiedAbsent: true, AlreadyAbsent: true}, nil
		}
		return ProcessTerminationResult{}, fmt.Errorf("%w: job handle unavailable for matching root process", ErrProcessUnverifiable)
	}
	defer releaseWindowsJob(rec.name, rec.handle)
	if err := windows.TerminateJobObject(rec.handle, 1); err != nil {
		return ProcessTerminationResult{}, fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		active, err := windowsJobActiveProcessCount(rec.handle)
		if err != nil {
			return ProcessTerminationResult{}, err
		}
		if active == 0 {
			return ProcessTerminationResult{VerifiedAbsent: true, Forced: true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ProcessTerminationResult{Forced: true}, fmt.Errorf("%w: job %s still has active processes after termination", ErrProcessUnverifiable, rec.name)
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

func windowsJobRecordForIdentity(identity ProcessIdentity) (windowsJobRecord, bool) {
	if identity.Nonce == "" {
		return windowsJobRecord{}, false
	}
	value, ok := windowsJobsByName.Load(identity.Nonce)
	if !ok {
		return windowsJobRecord{}, false
	}
	return value.(windowsJobRecord), true
}

func releaseWindowsJob(name string, handle windows.Handle) {
	windowsJobsByName.Delete(name)
	windowsJobsByCommand.Range(func(key, value any) bool {
		if value.(windowsJobRecord).name == name {
			windowsJobsByCommand.Delete(key)
			return false
		}
		return true
	})
	_ = windows.CloseHandle(handle)
}

func windowsJobActiveProcessCount(job windows.Handle) (uint32, error) {
	var info jobObjectBasicAccountingInformation
	if err := windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		return 0, fmt.Errorf("query job accounting: %w", err)
	}
	return info.ActiveProcesses, nil
}

func newWindowsJobName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate job name: %w", err)
	}
	return "RelayExecutor-" + hex.EncodeToString(b[:]), nil
}
