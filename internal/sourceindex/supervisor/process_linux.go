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

// runChild owns direct-child reaping: cmd.Wait is called exactly once, only
// after waitid has observed the leader without reaping it.  Keeping the leader
// unreaped preserves its process-group identity while residual pipes are fixed.
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

	overflow := make(chan struct{}, 1)
	signalOverflow := func() {
		select {
		case overflow <- struct{}{}:
		default:
		}
	}
	out := &boundedBuffer{limit: MaxIndexerResponseBytes, cancel: signalOverflow}
	errout := &boundedBuffer{limit: MaxIndexerStderrBytes, cancel: signalOverflow}
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
	go func() { exited <- unix.Waitid(unix.P_PID, cmd.Process.Pid, nil, unix.WEXITED|unix.WNOWAIT, nil) }()

	terminated := false
	terminate := func() {
		if !terminated {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			terminated = true
		}
	}
	var exitObserveErr error
	select {
	case exitObserveErr = <-exited:
	case <-ctx.Done():
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
		select {
		case exitObserveErr = <-exited:
		case <-time.After(processShutdownGrace):
		}
	case <-overflow:
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
		select {
		case exitObserveErr = <-exited:
		case <-time.After(processShutdownGrace):
		}
	}

	if ctx.Err() != nil {
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
		select {
		case <-drained:
		case <-time.After(processShutdownGrace):
		}
		_ = cmd.Wait()
		return indexerprotocol.BuildResponse{}, "cancelled", "source-index build cancelled", ctx.Err()
	}
	// If exit observation won the select race with an overflowing reader, honor
	// overflow immediately rather than treating retained descriptors as normal.
	select {
	case <-overflow:
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
	case <-drained:
		// Both readers already completed; classification below remains stable.
	default:
	}
	if out.exceeded || errout.exceeded {
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
		select {
		case <-drained:
		case <-time.After(processShutdownGrace):
		}
		_ = cmd.Wait()
		if out.exceeded {
			return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer response exceeded supervisor limit", errors.New("indexer response exceeded limit")
		}
		return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer diagnostics exceeded supervisor limit", errors.New("indexer diagnostics exceeded limit")
	}
	if exitObserveErr != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		_ = cmd.Wait()
		return indexerprotocol.BuildResponse{}, "indexer_process_failed", "cannot observe indexer process", exitObserveErr
	}

	// A normal leader exit does not close pipes held by descendants.  First give
	// valid trailing output a chance to drain, then kill the still-identifiable
	// original group before reaping its leader.
	select {
	case <-drained:
	case <-time.After(processShutdownGrace):
		terminate()
		_ = stdout.Close()
		_ = stderr.Close()
		select {
		case <-drained:
		case <-time.After(processShutdownGrace):
		}
	}
	waitErr := cmd.Wait()
	if out.exceeded || errout.exceeded {
		return indexerprotocol.BuildResponse{}, "indexer_output_exceeded", "indexer response exceeded supervisor limit", errors.New("indexer response exceeded limit")
	}
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
