//go:build linux

package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const failedResponse = `{"version":"relay.source-indexer-protocol.v1","status":"failed","failure":{"code":"internal","message":"failed"}}`

func TestRunChildHealthyLongRunningProcessHasNoInternalTimeout(t *testing.T) {
	s := testProcessSupervisor(t, "sleep 6\nprintf '%s' '"+failedResponse+"'\nexit 1")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	started := time.Now()
	response, code, _, err := s.runChild(ctx, nil, strings.Repeat("a", 64))
	if err != nil || code != "" || response.Failure == nil {
		t.Fatalf("runChild = (%+v, %q, %v), want failed child response", response, code, err)
	}
	if elapsed := time.Since(started); elapsed < processShutdownGrace {
		t.Fatalf("healthy child ended after %v, before its six-second run", elapsed)
	}
}

func TestRunChildOutputLimitsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name, script string
	}{
		{"stdout", "printf '%1048577s' ''\nwhile :; do :; done"},
		{"stderr", "printf '%4097s' '' >&2\nwhile :; do :; done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testProcessSupervisor(t, tc.script)
			ctx, cancel := context.WithTimeout(context.Background(), processShutdownGrace+2*time.Second)
			defer cancel()
			started := time.Now()
			_, code, _, err := s.runChild(ctx, nil, strings.Repeat("a", 64))
			if code != "indexer_output_exceeded" || err == nil {
				t.Fatalf("runChild code, err = %q, %v", code, err)
			}
			if elapsed := time.Since(started); elapsed > processShutdownGrace+time.Second {
				t.Fatalf("overflow shutdown took %v", elapsed)
			}
		})
	}
}

func TestRunChildCancellationAndObservationFailureAreBounded(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		original := observeChildExit
		observing := make(chan struct{})
		release := make(chan struct{})
		observeChildExit = func(int) error {
			close(observing)
			<-release
			return nil
		}
		t.Cleanup(func() { observeChildExit = original })
		s := testProcessSupervisor(t, "(while :; do :; done) &\nwhile :; do :; done")
		ctx, cancel := context.WithCancel(context.Background())
		go func() { <-observing; cancel() }()
		started := time.Now()
		_, code, _, err := s.runChild(ctx, nil, strings.Repeat("a", 64))
		close(release)
		if code != "cancelled" || !errors.Is(err, context.Canceled) {
			t.Fatalf("runChild code, err = %q, %v", code, err)
		}
		if elapsed := time.Since(started); elapsed > processShutdownGrace+time.Second {
			t.Fatalf("cancellation shutdown took %v", elapsed)
		}
	})
	t.Run("observation failure", func(t *testing.T) {
		original := observeChildExit
		observeChildExit = func(int) error { return errors.New("observe") }
		t.Cleanup(func() { observeChildExit = original })
		s := testProcessSupervisor(t, "while :; do :; done")
		started := time.Now()
		_, code, _, err := s.runChild(context.Background(), nil, strings.Repeat("a", 64))
		if code != "indexer_process_failed" || err == nil {
			t.Fatalf("runChild code, err = %q, %v", code, err)
		}
		if elapsed := time.Since(started); elapsed > processShutdownGrace+time.Second {
			t.Fatalf("observation-failure shutdown took %v", elapsed)
		}
	})
}

func testProcessSupervisor(t *testing.T, body string) *Supervisor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "indexer")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return &Supervisor{config: Config{IndexerPath: path}}
}
