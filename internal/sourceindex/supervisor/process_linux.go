//go:build linux

package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"relay/internal/sourceindex/indexerprotocol"
)

// observeChildExit observes, but deliberately does not reap, the direct child.
// Tests replace this narrow seam to exercise an observation failure.
var observeChildExit = func(pid int) error {
	return unix.Waitid(unix.P_PID, pid, nil, unix.WEXITED|unix.WNOWAIT, nil)
}

type outputStream uint8

const (
	stdoutStream outputStream = iota
	stderrStream
)

// runChild owns direct-child reaping. Keeping the leader unreaped until the
// group is signalled matters: once reaped, its numeric process-group ID could
// be reused, making a later negative-PID signal target an unrelated group.
func (s *Supervisor) runChild(ctx context.Context, request []byte, generationID string) (indexerprotocol.BuildResponse, string, string, error) {
	cmd := exec.Command(s.config.IndexerPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env, cmd.Stdin = childEnvironment(), bytes.NewReader(request)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot prepare indexer output", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot prepare indexer diagnostics", err
	}
	if err := cmd.Start(); err != nil {
		return indexerprotocol.BuildResponse{}, "indexer_start_failed", "cannot start indexer", err
	}

	overflow := make(chan outputStream, 2)
	signalOverflow := func(stream outputStream) func() {
		return func() {
			select {
			case overflow <- stream:
			default:
			}
		}
	}
	out := &boundedBuffer{limit: MaxIndexerResponseBytes, cancel: signalOverflow(stdoutStream)}
	errout := &boundedBuffer{limit: MaxIndexerStderrBytes, cancel: signalOverflow(stderrStream)}
	drained := make(chan struct{})
	go func() {
		var copies sync.WaitGroup
		copies.Add(2)
		go func() { defer copies.Done(); _, _ = io.Copy(out, stdout) }()
		go func() { defer copies.Done(); _, _ = io.Copy(errout, stderr) }()
		copies.Wait()
		close(drained)
	}()

	exited := make(chan error, 1)
	go func() { exited <- observeChildExit(cmd.Process.Pid) }()

	var closeReaders sync.Once
	closePipes := func() {
		closeReaders.Do(func() {
			_ = stdout.Close()
			_ = stderr.Close()
		})
	}
	terminated := false
	terminate := func() {
		if terminated {
			return
		}
		// This must happen before reaping; see the process-group comment above.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		terminated = true
	}
	var startReap sync.Once
	reaped := make(chan error, 1)
	beginReap := func() {
		startReap.Do(func() {
			// This is the sole owner of cmd.Wait. The buffer lets it finish after
			// a bounded caller has returned.
			go func() { reaped <- cmd.Wait() }()
		})
	}

	// shutdown is the only abnormal-termination path. Its deadline covers both
	// reaping and pipe draining, so no cleanup phase can extend the grace period.
	shutdown := func(deadline time.Time) (error, bool) {
		terminate()
		closePipes()
		beginReap()
		var waitErr error
		gotReap, gotDrain := false, false
		reapCh, drainCh := reaped, drained
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		for !gotReap || !gotDrain {
			select {
			case waitErr = <-reapCh:
				gotReap = true
				reapCh = nil
			case <-drainCh:
				gotDrain = true
				drainCh = nil
			case <-timer.C:
				return waitErr, gotReap
			}
		}
		return waitErr, true
	}
	overflowFailure := func(stream outputStream) (indexerprotocol.BuildResponse, string, string, error) {
		if stream == stdoutStream {
			return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer response exceeded supervisor limit", errors.New("indexer response exceeded limit")
		}
		return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer diagnostics exceeded supervisor limit", errors.New("indexer diagnostics exceeded limit")
	}

	select {
	case observeErr := <-exited:
		if observeErr != nil {
			shutdown(time.Now().Add(processShutdownGrace))
			return indexerprotocol.BuildResponse{}, "indexer_process_failed", "cannot observe indexer process", observeErr
		}
	case <-ctx.Done():
		shutdown(time.Now().Add(processShutdownGrace))
		return indexerprotocol.BuildResponse{}, "cancelled", "source-index build cancelled", ctx.Err()
	case stream := <-overflow:
		shutdown(time.Now().Add(processShutdownGrace))
		return overflowFailure(stream)
	}

	// A normal leader exit may leave descendants holding its output descriptors.
	// Do not reap until either normal draining completes or this deadline expires.
	deadline := time.Now().Add(processShutdownGrace)
	timer := time.NewTimer(processShutdownGrace)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		shutdown(time.Now().Add(processShutdownGrace))
		return indexerprotocol.BuildResponse{}, "cancelled", "source-index build cancelled", ctx.Err()
	case stream := <-overflow:
		shutdown(time.Now().Add(processShutdownGrace))
		return overflowFailure(stream)
	case <-drained:
		// A writer can signal overflow immediately before both copies finish.
		select {
		case stream := <-overflow:
			shutdown(time.Now().Add(processShutdownGrace))
			return overflowFailure(stream)
		default:
		}
		beginReap()
		waitErr := <-reaped
		return s.finishChild(out, waitErr, generationID)
	case <-timer.C:
		// This is a single sequence: shutdown receives the original deadline,
		// rather than granting descendants another full grace interval.
		waitErr, reapedInTime := shutdown(deadline)
		if !reapedInTime {
			return indexerprotocol.BuildResponse{}, "indexer_process_failed", "cannot reap indexer process", errors.New("indexer process did not reap during shutdown")
		}
		return s.finishChild(out, waitErr, generationID)
	}
}

func (s *Supervisor) finishChild(out *boundedBuffer, waitErr error, generationID string) (indexerprotocol.BuildResponse, string, string, error) {
	response, parseErr := indexerprotocol.ParseBuildResponse(out.bytes())
	if parseErr != nil {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response is invalid", fmt.Errorf("%w: invalid response", ErrChildProtocol)
	}
	if response.GenerationID != "" && response.GenerationID != generationID {
		return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer response generation does not match", fmt.Errorf("%w: generation mismatch", ErrChildProtocol)
	}
	if waitErr == nil && response.Status == indexerprotocol.BuildStatusSuccess && response.Result != nil && response.Failure == nil {
		return response, "", "", nil
	}
	if waitErr != nil && response.Status == indexerprotocol.BuildStatusFailed && response.Failure != nil && response.Result == nil {
		return response, "", "", nil
	}
	return indexerprotocol.BuildResponse{}, "indexer_protocol_failed", "indexer process and response disagree", fmt.Errorf("%w: exit mismatch", ErrChildProtocol)
}
