//go:build linux

package pipeline

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func withProcessGroupProbe(t *testing.T, probe func(int) (bool, error)) {
	t.Helper()
	previousProbe := unixProcessGroupPresent
	previousInterval := unixProcessGroupPollInterval
	unixProcessGroupPresent = probe
	unixProcessGroupPollInterval = time.Millisecond
	t.Cleanup(func() {
		unixProcessGroupPresent = previousProbe
		unixProcessGroupPollInterval = previousInterval
	})
}

func TestWaitForProcessGroupAbsentDoesNotTrustRootWaitAlone(t *testing.T) {
	withProcessGroupProbe(t, func(int) (bool, error) {
		return true, nil
	})

	absent, err := waitForProcessGroupAbsent(1234, time.Millisecond)
	if absent {
		t.Fatal("expected present process group to keep cleanup unverified")
	}
	if err == nil || !strings.Contains(err.Error(), "still present") {
		t.Fatalf("expected still-present error, got %v", err)
	}
}

func TestWaitForProcessGroupAbsentVerifiesOnlyAfterProbeAbsent(t *testing.T) {
	calls := 0
	withProcessGroupProbe(t, func(int) (bool, error) {
		calls++
		return calls < 2, nil
	})

	absent, err := waitForProcessGroupAbsent(1234, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("expected successful absence proof, got %v", err)
	}
	if !absent {
		t.Fatal("expected absent process group to verify cleanup")
	}
	if calls < 2 {
		t.Fatalf("expected polling to wait for absent probe, got %d calls", calls)
	}
}

func TestWaitForProcessGroupAbsentPreservesProbeError(t *testing.T) {
	probeErr := errors.New("probe failed")
	withProcessGroupProbe(t, func(int) (bool, error) {
		return false, probeErr
	})

	absent, err := waitForProcessGroupAbsent(1234, 50*time.Millisecond)
	if absent {
		t.Fatal("probe error must not verify cleanup")
	}
	if !errors.Is(err, probeErr) {
		t.Fatalf("expected probe error, got %v", err)
	}
}
