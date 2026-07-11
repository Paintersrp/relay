package workflowrepos

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	workflowstore "relay/internal/store/workflow"
)

type scriptedInspectionRunner struct {
	run func(string, ...string) (GitCommandResult, error)
}

func (runner scriptedInspectionRunner) Run(
	_ context.Context,
	directory string,
	args ...string,
) (GitCommandResult, error) {
	return runner.run(directory, args...)
}

func TestDeriveRepoTarget(t *testing.T) {
	testCases := map[string]string{
		"https://github.com/Paintersrp/relay.git":         "relay",
		"https://github.com/Paintersrp/relay.git?x=1#ref": "relay",
		"ssh://git@github.com/Paintersrp/Relay.git":       "Relay",
		"git://github.com/Paintersrp/relay.git":           "relay",
		"git@github.com:Paintersrp/relay.git":             "relay",
		"file:///C:/Code/relay.git":                       "relay",
	}
	for remoteURL, expected := range testCases {
		actual, err := deriveRepoTarget(remoteURL)
		if err != nil {
			t.Fatalf("derive %q: %v", remoteURL, err)
		}
		if actual != expected {
			t.Fatalf("derive %q = %q, want %q", remoteURL, actual, expected)
		}
	}
	for _, remoteURL := range []string{
		"",
		"owner/repo.git",
		"https://github.com/",
		"https://github.com/owner/%2F.git",
		"git@example.com:",
	} {
		if _, err := deriveRepoTarget(remoteURL); err == nil {
			t.Fatalf("expected %q to be rejected", remoteURL)
		}
	}
}

func TestInspectAndConfirmRepository(t *testing.T) {
	requireGit(t)

	ctx := context.Background()
	root := t.TempDir()
	repositoryPath := filepath.Join(root, "repository")
	if err := os.MkdirAll(filepath.Join(repositoryPath, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repositoryPath, "init")
	runGitTestCommand(t, repositoryPath, "remote", "add", "origin", "git@github.com:Paintersrp/relay.git")

	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	inspection, err := registry.Inspect(ctx, InspectionInput{
		LocalPath: filepath.Join(repositoryPath, "nested"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != InspectionStateReady ||
		inspection.RepoTarget != "relay" ||
		inspection.RegistrationDisposition != RegistrationDispositionCreate ||
		inspection.ConfirmationHash == "" {
		t.Fatalf("unexpected inspection: %+v", inspection)
	}
	if !sameRepositoryPath(inspection.ResolvedLocalPath, repositoryPath) {
		t.Fatalf("resolved path = %q, want %q", inspection.ResolvedLocalPath, repositoryPath)
	}

	registered, err := registry.Confirm(ctx, ConfirmationInput{
		LocalPath:                filepath.Join(repositoryPath, "nested"),
		ExpectedConfirmationHash: inspection.ConfirmationHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if registered.Outcome != RegistrationOutcomeCreated ||
		registered.Repository.RepoTarget != "relay" {
		t.Fatalf("unexpected registration: %+v", registered)
	}

	reinspection, err := registry.Inspect(ctx, InspectionInput{LocalPath: repositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	if reinspection.RegistrationDisposition != RegistrationDispositionReuse {
		t.Fatalf("unexpected reuse inspection: %+v", reinspection)
	}
	reused, err := registry.Confirm(ctx, ConfirmationInput{
		LocalPath:                repositoryPath,
		ExpectedConfirmationHash: reinspection.ConfirmationHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reused.Outcome != RegistrationOutcomeReused {
		t.Fatalf("unexpected reuse result: %+v", reused)
	}
}

func TestInspectSupportsLinkedWorktree(t *testing.T) {
	requireGit(t)

	root := t.TempDir()
	mainPath := filepath.Join(root, "main")
	linkedPath := filepath.Join(root, "linked")
	if err := os.MkdirAll(mainPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, mainPath, "init")
	runGitTestCommand(t, mainPath, "config", "user.email", "relay@example.invalid")
	runGitTestCommand(t, mainPath, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(mainPath, "README.md"), []byte("relay\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, mainPath, "add", "README.md")
	runGitTestCommand(t, mainPath, "commit", "-m", "initial")
	runGitTestCommand(t, mainPath, "remote", "add", "origin", "git@example.com:owner/relay.git")
	runGitTestCommand(t, mainPath, "worktree", "add", "-b", "linked-test", linkedPath)

	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := registry.Inspect(context.Background(), InspectionInput{LocalPath: linkedPath})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != InspectionStateReady ||
		!sameRepositoryPath(inspection.ResolvedLocalPath, linkedPath) {
		t.Fatalf("unexpected linked-worktree inspection: %+v", inspection)
	}
	if info, err := os.Stat(filepath.Join(linkedPath, ".git")); err != nil || info.IsDir() {
		t.Fatalf("linked worktree .git marker was not a file: info=%v err=%v", info, err)
	}
}

func TestInspectRejectsOutsideWorktreeAndBareRepository(t *testing.T) {
	requireGit(t)

	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Inspect(context.Background(), InspectionInput{LocalPath: outside}); !errors.Is(err, ErrInvalidRepositoryPath) {
		t.Fatalf("outside-worktree error = %v", err)
	}

	bare := filepath.Join(root, "bare.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, bare, "init", "--bare")
	if _, err := registry.Inspect(context.Background(), InspectionInput{LocalPath: bare}); !errors.Is(err, ErrInvalidRepositoryPath) {
		t.Fatalf("bare-repository error = %v", err)
	}
}

func TestInspectRemoteSelectionAndOverrides(t *testing.T) {
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))

	tests := []struct {
		name       string
		remotes    string
		urls       map[string]string
		input      InspectionInput
		wantState  string
		wantName   string
		wantKey    string
		wantSource string
		wantReason string
		wantNotice string
	}{
		{
			name:       "origin preferred",
			remotes:    "upstream\norigin",
			urls:       map[string]string{"origin": "git@example.com:owner/relay.git", "upstream": "git@example.com:owner/upstream.git"},
			input:      InspectionInput{LocalPath: root},
			wantState:  InspectionStateReady,
			wantName:   "origin",
			wantKey:    "relay",
			wantSource: RepoTargetSourceRemoteBasename,
		},
		{
			name:       "sole remote selected",
			remotes:    "upstream",
			urls:       map[string]string{"upstream": "git@example.com:owner/relay.git"},
			input:      InspectionInput{LocalPath: root},
			wantState:  InspectionStateReady,
			wantName:   "upstream",
			wantKey:    "relay",
			wantSource: RepoTargetSourceRemoteBasename,
		},
		{
			name:      "multiple remotes require selection",
			remotes:   "upstream\nfork",
			urls:      map[string]string{"upstream": "git@example.com:owner/upstream.git", "fork": "git@example.com:owner/fork.git"},
			input:     InspectionInput{LocalPath: root},
			wantState: InspectionStateNeedsRemoteSelection,
		},
		{
			name:       "no usable remote requires override with concrete reason",
			remotes:    "",
			urls:       map[string]string{},
			input:      InspectionInput{LocalPath: root},
			wantState:  InspectionStateNeedsTargetOverride,
			wantReason: TargetOverrideReasonNoUsableRemote,
			wantNotice: "No usable configured Git remote was found.",
		},
		{
			name:       "no remote with override",
			remotes:    "",
			urls:       map[string]string{},
			input:      InspectionInput{LocalPath: root, RepoTargetOverride: "local-relay"},
			wantState:  InspectionStateReady,
			wantKey:    "local-relay",
			wantSource: RepoTargetSourceOperatorOverride,
		},
		{
			name:       "unsupported selected URL requires override",
			remotes:    "origin",
			urls:       map[string]string{"origin": "owner/relay.git"},
			input:      InspectionInput{LocalPath: root},
			wantState:  InspectionStateNeedsTargetOverride,
			wantName:   "origin",
			wantReason: TargetOverrideReasonUnsupportedRemote,
			wantNotice: `Remote "origin" uses URL "owner/relay.git", which Relay cannot normalize`,
		},
		{
			name:       "unsupported selected URL accepts valid override",
			remotes:    "origin",
			urls:       map[string]string{"origin": "owner/relay.git"},
			input:      InspectionInput{LocalPath: root, RepoTargetOverride: "relay-local"},
			wantState:  InspectionStateReady,
			wantName:   "origin",
			wantKey:    "relay-local",
			wantSource: RepoTargetSourceOperatorOverride,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := NewRegistryWithRunner(store, remoteRunner(t, root, tt.remotes, tt.urls))
			if err != nil {
				t.Fatal(err)
			}
			got, err := registry.Inspect(context.Background(), tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tt.wantState {
				t.Fatalf("state = %q, want %q: %+v", got.State, tt.wantState, got)
			}
			if tt.wantName != "" && (got.SelectedRemote == nil || got.SelectedRemote.Name != tt.wantName) {
				t.Fatalf("selected remote = %+v, want %q", got.SelectedRemote, tt.wantName)
			}
			if got.RepoTarget != tt.wantKey {
				t.Fatalf("repo target = %q, want %q", got.RepoTarget, tt.wantKey)
			}
			if got.RepoTargetSource != tt.wantSource {
				t.Fatalf("target source = %q, want %q", got.RepoTargetSource, tt.wantSource)
			}
			if got.TargetOverrideReason != tt.wantReason {
				t.Fatalf("target override reason = %q, want %q", got.TargetOverrideReason, tt.wantReason)
			}
			if tt.wantNotice != "" {
				joined := strings.Join(got.Notices, "\n")
				if !strings.Contains(joined, tt.wantNotice) {
					t.Fatalf("notices = %q, want substring %q", joined, tt.wantNotice)
				}
			}
		})
	}

	registry, err := NewRegistryWithRunner(store, remoteRunner(t, root, "", map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Inspect(context.Background(), InspectionInput{
		LocalPath:          root,
		RepoTargetOverride: "invalid target",
	}); err == nil {
		t.Fatal("expected invalid override to fail")
	}
	if _, err := registry.Inspect(context.Background(), InspectionInput{
		LocalPath:  root,
		RemoteName: "missing",
	}); err == nil {
		t.Fatal("expected unknown remote selection to fail")
	}
}

func TestInspectReportsTargetAndPathConflicts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}

	firstPath := t.TempDir()
	if _, err := registry.Register(ctx, "relay", firstPath); err != nil {
		t.Fatal(err)
	}

	targetPath := t.TempDir()
	targetConflictRegistry, err := NewRegistryWithRunner(
		store,
		scriptedInspectionRunner{run: readyRunner(t, targetPath, "git@example.com:owner/relay.git")},
	)
	if err != nil {
		t.Fatal(err)
	}
	targetConflict, err := targetConflictRegistry.Inspect(ctx, InspectionInput{LocalPath: targetPath})
	if err != nil {
		t.Fatal(err)
	}
	if targetConflict.State != InspectionStateConflict ||
		targetConflict.ConflictKind != ConflictKindTarget {
		t.Fatalf("unexpected target conflict: %+v", targetConflict)
	}

	pathConflictRegistry, err := NewRegistryWithRunner(
		store,
		scriptedInspectionRunner{run: readyRunner(t, firstPath, "git@example.com:owner/other.git")},
	)
	if err != nil {
		t.Fatal(err)
	}
	pathConflict, err := pathConflictRegistry.Inspect(ctx, InspectionInput{LocalPath: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	if pathConflict.State != InspectionStateConflict ||
		pathConflict.ConflictKind != ConflictKindPath {
		t.Fatalf("unexpected path conflict: %+v", pathConflict)
	}
}

func TestInspectTreatsRegisteredKeyCaseInsensitively(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	existingPath := t.TempDir()
	if _, err := registry.Register(ctx, "relay", existingPath); err != nil {
		t.Fatal(err)
	}
	candidatePath := t.TempDir()
	candidate, err := NewRegistryWithRunner(
		store,
		scriptedInspectionRunner{run: readyRunner(t, candidatePath, "git@example.com:owner/Relay.git")},
	)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := candidate.Inspect(ctx, InspectionInput{LocalPath: candidatePath})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != InspectionStateConflict ||
		inspection.ConflictKind != ConflictKindTarget ||
		inspection.ExistingRepository == nil ||
		inspection.ExistingRepository.RepoTarget != "relay" {
		t.Fatalf("unexpected case-insensitive collision: %+v", inspection)
	}
}

func TestInspectUsesCaseInsensitiveCanonicalPathOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path identity behavior is platform-specific")
	}
	ctx := context.Background()
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	if _, err := registry.Register(ctx, "relay", path); err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(path)
	got, err := store.GetRepositoryTargetByLocalPath(ctx, upper)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoTarget != "relay" {
		t.Fatalf("unexpected Windows path lookup: %+v", got)
	}
}

func TestConfirmReclassifiesConcurrentInsertionsAfterTransactionEnds(t *testing.T) {
	tests := []struct {
		name         string
		insertTarget string
		insertPath   func(t *testing.T, current string) string
		wantOutcome  string
		wantConflict string
	}{
		{
			name:         "equivalent insertion becomes reuse",
			insertTarget: "relay",
			insertPath:   func(_ *testing.T, current string) string { return current },
			wantOutcome:  RegistrationOutcomeReused,
		},
		{
			name:         "same target at another path becomes target conflict",
			insertTarget: "relay",
			insertPath:   func(t *testing.T, _ string) string { return filepath.Clean(t.TempDir()) },
			wantConflict: ConflictKindTarget,
		},
		{
			name:         "same path under another target becomes path conflict",
			insertTarget: "other",
			insertPath:   func(_ *testing.T, current string) string { return current },
			wantConflict: ConflictKindPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dbPath := filepath.Join(root, "relay.sqlite")
			firstStore := openInspectionTestStore(t, dbPath)
			secondStore, err := workflowstore.Open(dbPath, filepath.Join(root, "artifacts-second"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = secondStore.Close() })

			repositoryPath := t.TempDir()
			registry, err := NewRegistryWithRunner(
				firstStore,
				scriptedInspectionRunner{run: readyRunner(t, repositoryPath, "git@example.com:owner/relay.git")},
			)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := registry.Inspect(
				context.Background(),
				InspectionInput{LocalPath: repositoryPath},
			)
			if err != nil {
				t.Fatal(err)
			}

			writerReady := make(chan struct{})
			releaseWriter := make(chan struct{})
			writerDone := make(chan error, 1)
			const commitDelay = 250 * time.Millisecond
			registry.beforeCreate = func() {
				registry.beforeCreate = nil
				go func() {
					writerDone <- secondStore.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
						if _, err := tx.CreateRepositoryTarget(
							context.Background(),
							tt.insertTarget,
							tt.insertPath(t, repositoryPath),
						); err != nil {
							return err
						}
						close(writerReady)
						<-releaseWriter
						return nil
					})
				}()
				<-writerReady
				time.AfterFunc(commitDelay, func() { close(releaseWriter) })
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			started := time.Now()
			result, err := registry.Confirm(ctx, ConfirmationInput{
				LocalPath:                repositoryPath,
				ExpectedConfirmationHash: inspection.ConfirmationHash,
			})
			if elapsed := time.Since(started); elapsed < commitDelay {
				t.Fatalf("confirmation completed in %s before delayed writer committed", elapsed)
			}
			if writerErr := <-writerDone; writerErr != nil {
				t.Fatalf("concurrent writer: %v", writerErr)
			}

			if tt.wantOutcome != "" {
				if err != nil {
					t.Fatalf("confirmation returned raw concurrent error: %v", err)
				}
				if result.Outcome != tt.wantOutcome {
					t.Fatalf("outcome = %q, want %q", result.Outcome, tt.wantOutcome)
				}
				return
			}
			var confirmationError *ConfirmationError
			if !errors.As(err, &confirmationError) ||
				confirmationError.Reason != "conflict" ||
				confirmationError.Inspection.ConflictKind != tt.wantConflict {
				t.Fatalf("error = %#v, want %s conflict", err, tt.wantConflict)
			}
		})
	}
}

func TestInspectPropagatesGitAvailabilityFailures(t *testing.T) {
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))

	for _, expected := range []error{ErrGitUnavailable, ErrGitTimeout, ErrGitOutputLimit} {
		registry, err := NewRegistryWithRunner(store, scriptedInspectionRunner{
			run: func(string, ...string) (GitCommandResult, error) {
				return GitCommandResult{}, expected
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Inspect(context.Background(), InspectionInput{LocalPath: root}); !errors.Is(err, expected) {
			t.Fatalf("error = %v, want %v", err, expected)
		}
	}
}

func TestExecInspectionRunnerClassifiesMissingExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-git")
	runner := execInspectionRunner{
		commandFactory: func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, missing, args...)
		},
		timeout:     time.Second,
		outputLimit: 64,
	}
	if _, err := runner.Run(context.Background(), t.TempDir(), "status"); !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("error = %v, want ErrGitUnavailable", err)
	}
}

func TestExecInspectionRunnerEnforcesTimeoutAndTerminatesProcess(t *testing.T) {
	// The helper sleeps far longer than the configured timeout. Margins are
	// generous to absorb subprocess spawn overhead (notably on Windows,
	// where launching a fresh test binary can itself take over 100ms)
	// without making the test flaky, while still proving the process is
	// killed well before it would complete on its own and never reaches
	// the point of writing its completion marker.
	const helperSleep = 3 * time.Second
	const contextTimeout = 300 * time.Millisecond
	const maxObservedElapsed = 2 * time.Second

	marker := filepath.Join(t.TempDir(), "helper-completed")
	runner := helperProcessInspectionRunner("sleep", marker, contextTimeout, 64)
	started := time.Now()
	if _, err := runner.Run(context.Background(), t.TempDir(), "status"); !errors.Is(err, ErrGitTimeout) {
		t.Fatalf("error = %v, want ErrGitTimeout", err)
	}
	if elapsed := time.Since(started); elapsed >= maxObservedElapsed {
		t.Fatalf("timed-out process returned after %s, want well under %s", elapsed, helperSleep)
	}
	time.Sleep(helperSleep + 500*time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("timed-out helper was not terminated; marker error = %v", err)
	}
}

func TestExecInspectionRunnerBoundsStdoutAndStderr(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			runner := helperProcessInspectionRunner(mode, "", time.Second, 64)
			result, err := runner.Run(context.Background(), t.TempDir(), "status")
			if !errors.Is(err, ErrGitOutputLimit) {
				t.Fatalf("error = %v, want ErrGitOutputLimit", err)
			}
			if len(result.Stdout) > 64 || len(result.Stderr) > 64 {
				t.Fatalf("captured output exceeded limit: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
			}
		})
	}
}

func TestInspectWithRealGitDoesNotMutateRepositoryOrConfiguration(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	runGitTestCommand(t, root, "init")
	runGitTestCommand(t, root, "config", "user.name", "Relay Test")
	runGitTestCommand(t, root, "config", "user.email", "relay@example.invalid")
	runGitTestCommand(t, root, "remote", "add", "origin", "git@example.invalid:owner/relay.git")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, root, "add", "tracked.txt")
	runGitTestCommand(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	beforeStatus := runGitTestOutput(t, root, "status", "--porcelain=v1", "--untracked-files=all")
	beforeConfig := runGitTestOutput(t, root, "config", "--local", "--list", "--show-origin")
	beforeHead := runGitTestOutput(t, root, "rev-parse", "HEAD")
	beforeTracked, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	beforeUntracked, err := os.ReadFile(filepath.Join(root, "untracked.txt"))
	if err != nil {
		t.Fatal(err)
	}

	store := openInspectionTestStore(t, filepath.Join(t.TempDir(), "relay.sqlite"))
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := registry.Inspect(
		context.Background(),
		InspectionInput{LocalPath: filepath.Join(root, ".git", "..")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.State != InspectionStateReady || inspection.RepoTarget != "relay" {
		t.Fatalf("inspection = %+v", inspection)
	}

	if after := runGitTestOutput(t, root, "status", "--porcelain=v1", "--untracked-files=all"); after != beforeStatus {
		t.Fatalf("worktree status changed:\nbefore=%q\nafter=%q", beforeStatus, after)
	}
	if after := runGitTestOutput(t, root, "config", "--local", "--list", "--show-origin"); after != beforeConfig {
		t.Fatalf("local Git configuration changed:\nbefore=%q\nafter=%q", beforeConfig, after)
	}
	if after := runGitTestOutput(t, root, "rev-parse", "HEAD"); after != beforeHead {
		t.Fatalf("HEAD changed: before=%q after=%q", beforeHead, after)
	}
	afterTracked, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	afterUntracked, err := os.ReadFile(filepath.Join(root, "untracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterTracked) != string(beforeTracked) || string(afterUntracked) != string(beforeUntracked) {
		t.Fatal("inspection changed tracked or untracked file content")
	}
}

func helperProcessInspectionRunner(
	mode string,
	marker string,
	timeout time.Duration,
	outputLimit int,
) execInspectionRunner {
	return execInspectionRunner{
		commandFactory: func(ctx context.Context, _ ...string) *exec.Cmd {
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestInspectionGitHelperProcess")
			command.Env = append(
				os.Environ(),
				"RELAY_GIT_HELPER=1",
				"RELAY_GIT_HELPER_MODE="+mode,
				"RELAY_GIT_HELPER_MARKER="+marker,
			)
			return command
		},
		timeout:     timeout,
		outputLimit: outputLimit,
	}
}

func TestInspectionGitHelperProcess(t *testing.T) {
	if os.Getenv("RELAY_GIT_HELPER") != "1" {
		return
	}
	switch os.Getenv("RELAY_GIT_HELPER_MODE") {
	case "sleep":
		time.Sleep(3 * time.Second)
		if err := os.WriteFile(os.Getenv("RELAY_GIT_HELPER_MARKER"), []byte("completed"), 0o644); err != nil {
			os.Exit(2)
		}
	case "stdout":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 4096))
	case "stderr":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 4096))
	default:
		os.Exit(3)
	}
	os.Exit(0)
}

func TestConfirmRejectsMissingAndStaleInspection(t *testing.T) {
	root := t.TempDir()
	store := openInspectionTestStore(t, filepath.Join(root, "relay.sqlite"))
	registry, err := NewRegistryWithRunner(store, scriptedInspectionRunner{
		run: readyRunner(t, root, "git@example.com:owner/relay.git"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Confirm(context.Background(), ConfirmationInput{LocalPath: root}); err == nil {
		t.Fatal("expected missing confirmation hash to fail")
	}
	_, err = registry.Confirm(context.Background(), ConfirmationInput{
		LocalPath:                root,
		ExpectedConfirmationHash: strings.Repeat("0", 64),
	})
	var confirmationError *ConfirmationError
	if !errors.As(err, &confirmationError) || confirmationError.Reason != "stale" {
		t.Fatalf("error = %v, want stale ConfirmationError", err)
	}
}

func remoteRunner(
	t *testing.T,
	root string,
	remotes string,
	urls map[string]string,
) scriptedInspectionRunner {
	t.Helper()
	return scriptedInspectionRunner{run: func(_ string, args ...string) (GitCommandResult, error) {
		command := strings.Join(args, " ")
		switch command {
		case "rev-parse --show-toplevel":
			return GitCommandResult{Stdout: root}, nil
		case "rev-parse --is-bare-repository":
			return GitCommandResult{Stdout: "false"}, nil
		case "remote":
			return GitCommandResult{Stdout: remotes}, nil
		default:
			const prefix = "remote get-url "
			if strings.HasPrefix(command, prefix) {
				name := strings.TrimPrefix(command, prefix)
				if value, ok := urls[name]; ok {
					return GitCommandResult{Stdout: value}, nil
				}
			}
			return GitCommandResult{}, errors.New("unexpected Git command: " + command)
		}
	}}
}

func readyRunner(t *testing.T, root string, remoteURL string) func(string, ...string) (GitCommandResult, error) {
	t.Helper()
	return remoteRunner(t, root, "origin", map[string]string{"origin": remoteURL}).run
}

func openInspectionTestStore(t *testing.T, dbPath string) *workflowstore.Store {
	t.Helper()
	store, err := workflowstore.Open(dbPath, filepath.Join(filepath.Dir(dbPath), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable is unavailable")
	}
}

func runGitTestCommand(t *testing.T, directory string, args ...string) {
	t.Helper()
	_ = runGitTestOutput(t, directory, args...)
}

func runGitTestOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
