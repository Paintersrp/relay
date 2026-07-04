//go:build windows

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
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

func TestWindowsOwnedProcessStdinDeliveryIsExact(t *testing.T) {
	payload := strings.Repeat("relay-stdin-payload\n", 8192)
	sum := sha256.Sum256([]byte(payload))
	expected := hex.EncodeToString(sum[:])

	controller := DefaultProcessController()
	owned, err := controller.StartOwned(context.Background(), CommandSpec{
		Binary: "powershell.exe",
		Args: []string{
			"-NoProfile",
			"-Command",
			`$inputText = [Console]::In.ReadToEnd(); $sha = [System.Security.Cryptography.SHA256]::Create(); $bytes = [Text.Encoding]::UTF8.GetBytes($inputText); [BitConverter]::ToString($sha.ComputeHash($bytes)).Replace("-", "").ToLowerInvariant()`,
		},
		Stdin: payload,
	})
	if err != nil {
		t.Fatalf("start owned: %v", err)
	}
	defer owned.Release()

	out, readErr := io.ReadAll(owned.Stdout())
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	if err := owned.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != expected {
		t.Fatalf("stdin hash mismatch: got %s want %s", got, expected)
	}
}
