//go:build windows

package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

func TestWindowsStartOwnedPostCreateIdentityFailureCleansUpNativeJob(t *testing.T) {
	controller := defaultProcessController{
		processCreationTime: func(pid int) (time.Time, error) {
			return time.Time{}, errors.New("creation time unavailable")
		},
	}
	owned, err := controller.StartOwned(context.Background(), CommandSpec{
		Binary: "powershell.exe",
		Args:   []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 30"},
	})
	if owned != nil {
		defer owned.Release()
	}
	if err == nil {
		t.Fatal("expected post-create identity failure")
	}
	startErr, ok := err.(*OwnedStartError)
	if !ok {
		t.Fatalf("expected OwnedStartError, got %T: %v", err, err)
	}
	if !startErr.NativeStarted {
		t.Fatalf("expected native start evidence: %+v", startErr)
	}
	if !startErr.Cleanup.VerifiedAbsent {
		t.Fatalf("expected cleanup to verify absence, got cleanup=%+v cleanupErr=%v", startErr.Cleanup, startErr.CleanupError)
	}
	if startErr.Identity.StartedAt != "" {
		t.Fatalf("identity failure should not fabricate StartedAt: %+v", startErr.Identity)
	}
	if owned == nil {
		t.Fatal("expected owned process evidence for cleanup/release")
	}
	if running, treeErr := owned.TreeRunning(); treeErr != nil {
		t.Fatalf("tree running after cleanup: %v", treeErr)
	} else if running {
		t.Fatal("expected native cleanup to remove process tree")
	}
}

func TestWindowsNamedJobReopenWithoutStartedAtUsesJobIdentity(t *testing.T) {
	controller := DefaultProcessController()
	owned, err := controller.StartOwned(context.Background(), CommandSpec{
		Binary: "powershell.exe",
		Args:   []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 30"},
	})
	if err != nil {
		t.Fatalf("start owned: %v", err)
	}
	defer owned.Release()

	identity := owned.Identity()
	if identity.Nonce == "" {
		t.Fatalf("expected named job nonce: %+v", identity)
	}
	identity.StartedAt = ""
	reopened, err := controller.OpenOwned(identity)
	if err != nil {
		t.Fatalf("reopen named job without StartedAt: %v", err)
	}
	defer reopened.Release()
	running, err := reopened.TreeRunning()
	if err != nil {
		t.Fatalf("tree running: %v", err)
	}
	if !running {
		t.Fatal("expected reopened named job to observe live process tree")
	}
	result, err := reopened.Terminate(2 * time.Second)
	if err != nil {
		t.Fatalf("terminate reopened job: %v", err)
	}
	if !result.VerifiedAbsent {
		t.Fatalf("expected verified absence after reopened termination, got %+v", result)
	}
}
