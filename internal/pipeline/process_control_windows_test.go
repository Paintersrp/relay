//go:build windows

package pipeline

import (
	"context"
	"testing"
	"time"
)

func TestWindowsOwnedProcessRootExitChildRemainsInJob(t *testing.T) {
	controller := DefaultProcessController()
	owned, err := controller.StartOwned(context.Background(), CommandSpec{
		Binary: "powershell.exe",
		Args: []string{
			"-NoProfile",
			"-Command",
			`Start-Process powershell.exe -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 5'`,
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("start owned: %v", err)
	}
	defer owned.Release()

	if err := owned.Wait(); err != nil {
		t.Fatalf("wait root: %v", err)
	}
	running, err := owned.TreeRunning()
	if err != nil {
		t.Fatalf("tree running: %v", err)
	}
	if !running {
		t.Fatal("expected child process to remain visible through the job after root exit")
	}
	result, err := owned.Terminate(2 * time.Second)
	if err != nil {
		t.Fatalf("terminate job: %v", err)
	}
	if !result.VerifiedAbsent {
		t.Fatalf("expected verified absence after job termination, got %+v", result)
	}
}

func TestWindowsOwnedProcessReleaseIsIdempotent(t *testing.T) {
	controller := DefaultProcessController()
	owned, err := controller.StartOwned(context.Background(), CommandSpec{
		Binary: "cmd.exe",
		Args:   []string{"/C", "exit 0"},
	})
	if err != nil {
		t.Fatalf("start owned: %v", err)
	}
	if err := owned.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if running, err := owned.TreeRunning(); err != nil {
		t.Fatalf("tree running: %v", err)
	} else if running {
		t.Fatal("expected exited process tree to be absent")
	}
	if err := owned.Release(); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if err := owned.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
}
