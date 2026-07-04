//go:build windows

package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	procThreadAttributeJobList = 0x0002000D
	jobObjectQuery             = 0x0004
	jobObjectTerminate         = 0x0008
	jobObjectAssignProcess     = 0x0001
	jobObjectSetAttributes     = 0x0002
	jobObjectAllNeeded         = jobObjectQuery | jobObjectTerminate | jobObjectAssignProcess | jobObjectSetAttributes
	stillActive                = 259
)

type defaultProcessController struct{}

type windowsOwnedProcess struct {
	mu            sync.Mutex
	identity      ProcessIdentity
	jobName       string
	job           windows.Handle
	process       windows.Handle
	stdout        *os.File
	stderr        *os.File
	stdinDone     <-chan error
	stdinRequired bool
	releaseOnDone bool
	released      bool
}

type windowsExitError struct {
	code int
}

func (e windowsExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e windowsExitError) ExitCode() int { return e.code }

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

func (c defaultProcessController) StartOwned(ctx context.Context, spec CommandSpec) (OwnedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobName, err := newWindowsJobName()
	if err != nil {
		return nil, err
	}
	job, err := createWindowsJob(jobName)
	if err != nil {
		return nil, err
	}
	owned, err := c.createOwnedProcess(ctx, spec, jobName, job)
	if err != nil {
		if owned == nil {
			_ = windows.CloseHandle(job)
		}
		return nil, err
	}
	return owned, nil
}

func (defaultProcessController) OpenOwned(identity ProcessIdentity) (OwnedProcess, error) {
	if identity.Nonce == "" {
		if err := verifyWindowsProcessIdentity(identity); errors.Is(err, ErrProcessNotRunning) {
			return &windowsOwnedProcess{identity: identity}, nil
		}
		return nil, fmt.Errorf("%w: missing job name", ErrProcessUnverifiable)
	}
	job, err := openWindowsJob(identity.Nonce)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			if err := verifyWindowsProcessIdentity(identity); errors.Is(err, ErrProcessNotRunning) {
				return &windowsOwnedProcess{identity: identity, jobName: identity.Nonce}, nil
			}
			return nil, fmt.Errorf("%w: job %s missing while root process matches", ErrProcessUnverifiable, identity.Nonce)
		}
		return nil, fmt.Errorf("open job object: %w", err)
	}
	return &windowsOwnedProcess{identity: identity, jobName: identity.Nonce, job: job, releaseOnDone: true}, nil
}

func (c defaultProcessController) createOwnedProcess(ctx context.Context, spec CommandSpec, jobName string, job windows.Handle) (*windowsOwnedProcess, error) {
	binary, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, err
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinWrite.Close()
		stdinRead.Close()
		return nil, err
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinWrite.Close()
		stdinRead.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		return nil, err
	}
	childFiles := []*os.File{stdinRead, stdoutWrite, stderrWrite}
	parentFiles := []*os.File{stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite}
	cleanupChild := func() {
		for _, f := range childFiles {
			_ = f.Close()
		}
	}
	cleanupAll := func() {
		cleanupChild()
		for _, f := range parentFiles {
			_ = f.Close()
		}
	}
	for _, f := range childFiles {
		if err := windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			cleanupAll()
			return nil, err
		}
	}
	handles := []windows.Handle{
		windows.Handle(stdinRead.Fd()),
		windows.Handle(stdoutWrite.Fd()),
		windows.Handle(stderrWrite.Fd()),
	}
	attrList, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		cleanupAll()
		return nil, err
	}
	defer attrList.Delete()
	if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		cleanupAll()
		return nil, err
	}
	jobHandles := []windows.Handle{job}
	if err := attrList.Update(procThreadAttributeJobList, unsafe.Pointer(&jobHandles[0]), uintptr(len(jobHandles))*unsafe.Sizeof(jobHandles[0])); err != nil {
		cleanupAll()
		return nil, fmt.Errorf("configure job-list process attribute: %w", err)
	}

	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  windows.Handle(stdinRead.Fd()),
			StdOutput: windows.Handle(stdoutWrite.Fd()),
			StdErr:    windows.Handle(stderrWrite.Fd()),
		},
		ProcThreadAttributeList: attrList.List(),
	}
	pi := &windows.ProcessInformation{}
	cmdLine := windows.ComposeCommandLine(append([]string{binary}, spec.Args...))
	cmdLinePtr, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		cleanupAll()
		return nil, err
	}
	appPtr, err := windows.UTF16PtrFromString(binary)
	if err != nil {
		cleanupAll()
		return nil, err
	}
	var dirPtr *uint16
	if spec.WorkDir != "" {
		dirPtr, err = windows.UTF16PtrFromString(spec.WorkDir)
		if err != nil {
			cleanupAll()
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		cleanupAll()
		return nil, err
	}
	err = windows.CreateProcess(
		appPtr,
		cmdLinePtr,
		nil,
		nil,
		true,
		windows.CREATE_DEFAULT_ERROR_MODE|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT,
		nil,
		dirPtr,
		&si.StartupInfo,
		pi,
	)
	if err != nil {
		cleanupAll()
		return nil, err
	}
	_ = windows.CloseHandle(pi.Thread)
	cleanupChild()
	stdinDone := make(chan error, 1)
	go func() {
		var err error
		if spec.Stdin != "" {
			n, writeErr := io.WriteString(stdinWrite, spec.Stdin)
			if writeErr != nil {
				err = writeErr
			} else if n != len(spec.Stdin) {
				err = io.ErrShortWrite
			}
		}
		closeErr := stdinWrite.Close()
		if err == nil {
			err = closeErr
		}
		stdinDone <- err
		close(stdinDone)
	}()
	owned := &windowsOwnedProcess{
		identity: ProcessIdentity{
			PID:      int(pi.ProcessId),
			GroupID:  int(pi.ProcessId),
			Platform: runtime.GOOS,
			Nonce:    jobName,
		},
		jobName:       jobName,
		job:           job,
		process:       pi.Process,
		stdout:        stdoutRead,
		stderr:        stderrRead,
		stdinDone:     stdinDone,
		stdinRequired: spec.Stdin != "",
		releaseOnDone: true,
	}
	createdAt, err := windowsProcessCreationTime(int(pi.ProcessId))
	if err != nil {
		cleanup, cleanupErr := owned.Terminate(2 * time.Second)
		return owned, &OwnedStartError{
			Cause:         fmt.Errorf("capture process identity: %w", err),
			NativeStarted: true,
			Identity:      owned.identity,
			Cleanup:       cleanup,
			CleanupError:  cleanupErr,
		}
	}
	owned.identity.StartedAt = createdAt.UTC().Format(time.RFC3339Nano)
	return owned, nil
}

func (p *windowsOwnedProcess) Identity() ProcessIdentity { return p.identity }

func (p *windowsOwnedProcess) Stdout() io.ReadCloser { return p.stdout }

func (p *windowsOwnedProcess) Stderr() io.ReadCloser { return p.stderr }

func (p *windowsOwnedProcess) Wait() error {
	if p.process == 0 {
		return ErrProcessUnverifiable
	}
	event, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	if err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("wait returned unexpected event %d", event)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.process, &code); err != nil {
		return err
	}
	if code == 0 {
		return p.stdinError()
	}
	if code == stillActive {
		return fmt.Errorf("%w: process still active after wait", ErrProcessUnverifiable)
	}
	if err := p.stdinError(); err != nil {
		return fmt.Errorf("%w; stdin delivery: %v", windowsExitError{code: int(code)}, err)
	}
	return windowsExitError{code: int(code)}
}

func (p *windowsOwnedProcess) TreeRunning() (bool, error) {
	p.mu.Lock()
	job := p.job
	p.mu.Unlock()
	if job != 0 {
		active, err := windowsJobActiveProcessCount(job)
		if err != nil {
			return false, err
		}
		return active > 0, nil
	}
	if err := verifyWindowsProcessIdentity(p.identity); err != nil {
		if errors.Is(err, ErrProcessNotRunning) {
			return false, nil
		}
		return false, err
	}
	return true, fmt.Errorf("%w: job handle unavailable for matching root process", ErrProcessUnverifiable)
}

func (p *windowsOwnedProcess) Terminate(gracefulTimeout time.Duration) (ProcessTerminationResult, error) {
	p.mu.Lock()
	job := p.job
	p.mu.Unlock()
	if job == 0 {
		if err := verifyWindowsProcessIdentity(p.identity); errors.Is(err, ErrProcessNotRunning) {
			return ProcessTerminationResult{VerifiedAbsent: true, AlreadyAbsent: true}, nil
		}
		return ProcessTerminationResult{}, fmt.Errorf("%w: job handle unavailable for matching root process", ErrProcessUnverifiable)
	}
	if err := windows.TerminateJobObject(job, 1); err != nil {
		return ProcessTerminationResult{}, fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		active, err := windowsJobActiveProcessCount(job)
		if err != nil {
			return ProcessTerminationResult{}, err
		}
		if active == 0 {
			return ProcessTerminationResult{VerifiedAbsent: true, Forced: true}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ProcessTerminationResult{Forced: true}, fmt.Errorf("%w: job %s still has active processes after termination", ErrProcessUnverifiable, p.jobName)
}

func (p *windowsOwnedProcess) Release() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return nil
	}
	p.released = true
	var errs []string
	if p.stdout != nil {
		if err := p.stdout.Close(); err != nil {
			errs = append(errs, "close stdout: "+err.Error())
		}
		p.stdout = nil
	}
	if p.stderr != nil {
		if err := p.stderr.Close(); err != nil {
			errs = append(errs, "close stderr: "+err.Error())
		}
		p.stderr = nil
	}
	if p.process != 0 {
		if err := windows.CloseHandle(p.process); err != nil {
			errs = append(errs, "close process: "+err.Error())
		}
		p.process = 0
	}
	if p.job != 0 {
		if err := windows.CloseHandle(p.job); err != nil {
			errs = append(errs, "close job: "+err.Error())
		}
		p.job = 0
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (p *windowsOwnedProcess) stdinError() error {
	done := p.stdinDone
	if done == nil {
		return nil
	}
	err := <-done
	if err != nil && p.stdinRequired {
		return err
	}
	return nil
}

func createWindowsJob(name string) (windows.Handle, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	job, err := windows.CreateJobObject(nil, namePtr)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
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
		return 0, fmt.Errorf("configure job object: %w", err)
	}
	return job, nil
}

func openWindowsJob(name string) (windows.Handle, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	r1, _, e1 := windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW").Call(
		uintptr(jobObjectAllNeeded),
		0,
		uintptr(unsafe.Pointer(namePtr)),
	)
	if r1 == 0 {
		if e1 != windows.ERROR_SUCCESS {
			return 0, e1
		}
		return 0, windows.ERROR_FILE_NOT_FOUND
	}
	return windows.Handle(r1), nil
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
