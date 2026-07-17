package workflowrepos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestBranchAwareInspectConfirmLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("new omitted proposal stays unconfigured at version one", func(t *testing.T) {
		registry, store, repo := newBranchTestRegistry(t)
		inspection, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:          repo,
			RepoTargetOverride: "relay",
		})
		if err != nil {
			t.Fatal(err)
		}
		if inspection.ConfigurationDisposition != ConfigurationDispositionPreserve ||
			inspection.ProposedConfiguredBranchRef.Valid ||
			inspection.ProposedConfigurationVersion != 1 {
			t.Fatalf("inspection configuration = %#v", inspection)
		}
		result, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                repo,
			RepoTargetOverride:       "relay",
			ExpectedConfirmationHash: inspection.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != RegistrationOutcomeCreated ||
			result.Repository.ConfiguredBranchRef.Valid ||
			result.Repository.ConfigurationVersion != 1 {
			t.Fatalf("registration result = %#v", result)
		}
		stored, err := store.GetRepositoryTarget(ctx, "relay")
		if err != nil {
			t.Fatal(err)
		}
		if stored.ConfiguredBranchRef.Valid || stored.ConfigurationVersion != 1 {
			t.Fatalf("stored target = %#v", stored)
		}
	})

	t.Run("new configured target is version one", func(t *testing.T) {
		registry, _, repo := newBranchTestRegistry(t)
		inspection, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
		})
		if err != nil {
			t.Fatal(err)
		}
		if inspection.ConfigurationDisposition != ConfigurationDispositionConfigure ||
			inspection.ProposedConfigurationVersion != 1 ||
			inspection.ProposedBranchCommitOID == "" ||
			inspection.ProposedBranchTreeOID == "" {
			t.Fatalf("inspection = %#v", inspection)
		}
		result, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
			ExpectedConfirmationHash:    inspection.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Repository.ConfiguredBranchRef.Valid ||
			result.Repository.ConfiguredBranchRef.String != "refs/heads/main" ||
			result.Repository.ConfigurationVersion != 1 {
			t.Fatalf("configured target = %#v", result.Repository)
		}
	})

	t.Run("configure change equal and omitted preserve exact versions", func(t *testing.T) {
		registry, store, repo := newBranchTestRegistry(t)
		createInspection, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:          repo,
			RepoTargetOverride: "relay",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                repo,
			RepoTargetOverride:       "relay",
			ExpectedConfirmationHash: createInspection.ConfirmationHash,
		}); err != nil {
			t.Fatal(err)
		}

		configure, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
		})
		if err != nil {
			t.Fatal(err)
		}
		if configure.ConfigurationDisposition != ConfigurationDispositionConfigure ||
			configure.ExpectedConfigurationVersion != 1 ||
			configure.ProposedConfigurationVersion != 2 {
			t.Fatalf("configure inspection = %#v", configure)
		}
		configured, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
			ExpectedConfirmationHash:    configure.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if configured.Repository.ConfigurationVersion != 2 {
			t.Fatalf("configured version = %d, want 2", configured.Repository.ConfigurationVersion)
		}

		equal, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
		})
		if err != nil {
			t.Fatal(err)
		}
		if equal.ConfigurationDisposition != ConfigurationDispositionPreserve ||
			equal.ProposedConfigurationVersion != 2 {
			t.Fatalf("equal inspection = %#v", equal)
		}
		equalResult, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
			ExpectedConfirmationHash:    equal.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if equalResult.Repository.ConfigurationVersion != 2 {
			t.Fatal("equal ref confirmation incremented configuration version")
		}

		omitted, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:          repo,
			RepoTargetOverride: "relay",
		})
		if err != nil {
			t.Fatal(err)
		}
		if omitted.ConfigurationDisposition != ConfigurationDispositionPreserve ||
			!omitted.ProposedConfiguredBranchRef.Valid ||
			omitted.ProposedConfiguredBranchRef.String != "refs/heads/main" ||
			omitted.ProposedConfigurationVersion != 2 {
			t.Fatalf("omitted inspection = %#v", omitted)
		}
		omittedResult, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                repo,
			RepoTargetOverride:       "relay",
			ExpectedConfirmationHash: omitted.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if omittedResult.Repository.ConfigurationVersion != 2 {
			t.Fatal("omitted proposal incremented configuration version")
		}

		change, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/alternate",
		})
		if err != nil {
			t.Fatal(err)
		}
		if change.ConfigurationDisposition != ConfigurationDispositionChange ||
			change.ProposedConfigurationVersion != 3 {
			t.Fatalf("change inspection = %#v", change)
		}
		changed, err := registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/alternate",
			ExpectedConfirmationHash:    change.ConfirmationHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		if changed.Repository.ConfigurationVersion != 3 ||
			changed.Repository.ConfiguredBranchRef.String != "refs/heads/alternate" {
			t.Fatalf("changed target = %#v", changed.Repository)
		}
		stored, err := store.GetRepositoryTargetByLocalPath(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if stored.ConfigurationVersion != 3 {
			t.Fatalf("stored version = %d, want 3", stored.ConfigurationVersion)
		}
	})
}

func TestBranchInspectionIsCheckoutIndependent(t *testing.T) {
	ctx := context.Background()
	registry, _, repo := newBranchTestRegistry(t)
	wantCommit := gitOutput(t, repo, "rev-parse", "refs/heads/main^{commit}")
	wantTree := gitOutput(t, repo, "rev-parse", "refs/heads/main^{tree}")

	var observations [][2]string
	for _, checkout := range []string{"main", "alternate", "detached"} {
		t.Run(checkout, func(t *testing.T) {
			switch checkout {
			case "main", "alternate":
				gitRun(t, repo, "checkout", checkout)
			case "detached":
				gitRun(t, repo, "checkout", "--detach", "refs/heads/alternate")
			}
			inspection, err := registry.Inspect(ctx, InspectionInput{
				LocalPath:                   repo,
				RepoTargetOverride:          "relay",
				ProposedConfiguredBranchRef: "refs/heads/main",
			})
			if err != nil {
				t.Fatal(err)
			}
			if inspection.ProposedBranchCommitOID != wantCommit ||
				inspection.ProposedBranchTreeOID != wantTree {
				t.Fatalf("checkout %s resolved %s/%s, want %s/%s",
					checkout,
					inspection.ProposedBranchCommitOID,
					inspection.ProposedBranchTreeOID,
					wantCommit,
					wantTree,
				)
			}
			observations = append(observations, [2]string{
				inspection.ProposedBranchCommitOID,
				inspection.ProposedBranchTreeOID,
			})
		})
	}
	for _, got := range observations {
		if got != observations[0] {
			t.Fatalf("checkout changed branch observation: %v", observations)
		}
	}
}

func TestBranchInspectionRejectsClosedInvalidRefClasses(t *testing.T) {
	ctx := context.Background()
	registry, _, repo := newBranchTestRegistry(t)
	gitRun(t, repo, "tag", "v1")
	gitRun(t, repo, "update-ref", "refs/remotes/origin/main", gitOutput(t, repo, "rev-parse", "HEAD"))

	invalid := []string{
		"main",
		"HEAD",
		"refs/tags/v1",
		"refs/remotes/origin/main",
		"refs/meta/config",
		"refs/heads/main^{commit}",
		" refs/heads/main",
		"refs/heads/main ",
		"refs/heads/bad..name",
		"refs/heads/bad.lock",
		string([]byte("refs/heads/bad\xff")),
	}
	for _, ref := range invalid {
		t.Run(strings.ReplaceAll(ref, "/", "_"), func(t *testing.T) {
			_, err := registry.Inspect(ctx, InspectionInput{
				LocalPath:                   repo,
				RepoTargetOverride:          "relay",
				ProposedConfiguredBranchRef: ref,
			})
			if !errors.Is(err, ErrInvalidConfiguredBranch) {
				t.Fatalf("ref %q error = %v, want ErrInvalidConfiguredBranch", ref, err)
			}
		})
	}

	for _, ref := range []string{"refs/heads/missing", "refs/heads/deleted"} {
		t.Run(ref, func(t *testing.T) {
			if strings.HasSuffix(ref, "deleted") {
				gitRun(t, repo, "branch", "deleted")
				gitRun(t, repo, "branch", "-D", "deleted")
			}
			_, err := registry.Inspect(ctx, InspectionInput{
				LocalPath:                   repo,
				RepoTargetOverride:          "relay",
				ProposedConfiguredBranchRef: ref,
			})
			if !errors.Is(err, ErrConfiguredBranchUnavailable) {
				t.Fatalf("ref %q error = %v", ref, err)
			}
		})
	}

	unborn := t.TempDir()
	gitRun(t, unborn, "init")
	gitRun(t, unborn, "config", "user.email", "relay@example.test")
	gitRun(t, unborn, "config", "user.name", "Relay Test")
	_, err := registry.Inspect(ctx, InspectionInput{
		LocalPath:                   unborn,
		RepoTargetOverride:          "unborn",
		ProposedConfiguredBranchRef: "refs/heads/main",
	})
	if !errors.Is(err, ErrConfiguredBranchUnavailable) {
		t.Fatalf("unborn branch error = %v", err)
	}
}

func TestBranchConfirmationRejectsBranchAndConfigurationStaleness(t *testing.T) {
	ctx := context.Background()

	t.Run("branch movement", func(t *testing.T) {
		registry, _, repo := newBranchTestRegistry(t)
		inspection, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
		})
		if err != nil {
			t.Fatal(err)
		}
		gitRun(t, repo, "checkout", "main")
		if err := os.WriteFile(filepath.Join(repo, "moved.txt"), []byte("moved\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun(t, repo, "add", "moved.txt")
		gitRun(t, repo, "commit", "-m", "move main")
		_, err = registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/main",
			ExpectedConfirmationHash:    inspection.ConfirmationHash,
		})
		var confirmation *ConfirmationError
		if !errors.As(err, &confirmation) || confirmation.Reason != "stale" {
			t.Fatalf("branch movement error = %v", err)
		}
	})

	t.Run("configuration version movement", func(t *testing.T) {
		registry, store, repo := newBranchTestRegistry(t)
		created := inspectAndConfirm(t, registry, repo, "relay", "refs/heads/main")
		if created.Repository.ConfigurationVersion != 1 {
			t.Fatal("unexpected initial version")
		}
		inspection, err := registry.Inspect(ctx, InspectionInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/alternate",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{
				RepoTarget:                   "relay",
				ExpectedConfigurationVersion: 1,
				ConfiguredBranchRef:          "refs/heads/other",
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		_, err = registry.Confirm(ctx, ConfirmationInput{
			LocalPath:                   repo,
			RepoTargetOverride:          "relay",
			ProposedConfiguredBranchRef: "refs/heads/alternate",
			ExpectedConfirmationHash:    inspection.ConfirmationHash,
		})
		var confirmation *ConfirmationError
		if !errors.As(err, &confirmation) || confirmation.Reason != "stale" {
			t.Fatalf("configuration movement error = %v", err)
		}
		target, getErr := store.GetRepositoryTarget(ctx, "relay")
		if getErr != nil {
			t.Fatal(getErr)
		}
		if target.ConfiguredBranchRef.String != "refs/heads/other" ||
			target.ConfigurationVersion != 2 {
			t.Fatalf("stale confirmation overwrote state: %#v", target)
		}
	})
}

func TestBranchInspectionPreservesRegistrationConflicts(t *testing.T) {
	ctx := context.Background()
	registry, _, first := newBranchTestRegistry(t)
	second := newGitRepository(t)
	inspectAndConfirm(t, registry, first, "relay", "refs/heads/main")

	targetConflict, err := registry.Inspect(ctx, InspectionInput{
		LocalPath:                   second,
		RepoTargetOverride:          "relay",
		ProposedConfiguredBranchRef: "refs/heads/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if targetConflict.State != InspectionStateConflict ||
		targetConflict.ConflictKind != ConflictKindTarget {
		t.Fatalf("target conflict = %#v", targetConflict)
	}

	pathConflict, err := registry.Inspect(ctx, InspectionInput{
		LocalPath:                   first,
		RepoTargetOverride:          "other",
		ProposedConfiguredBranchRef: "refs/heads/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pathConflict.State != InspectionStateConflict ||
		pathConflict.ConflictKind != ConflictKindPath {
		t.Fatalf("path conflict = %#v", pathConflict)
	}
}

func TestBranchInspectConfirmIssuesNoFetchOrMutationCommands(t *testing.T) {
	ctx := context.Background()
	store, _ := openRepositoryWorkflowStore(t)
	repo := newGitRepository(t)
	recorder := &recordingGitRunner{delegate: newExecGitRunner()}
	registry, err := NewRegistryWithRunner(store, recorder)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := registry.Inspect(ctx, InspectionInput{
		LocalPath:                   repo,
		RepoTargetOverride:          "relay",
		ProposedConfiguredBranchRef: "refs/heads/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Confirm(ctx, ConfirmationInput{
		LocalPath:                   repo,
		RepoTargetOverride:          "relay",
		ProposedConfiguredBranchRef: "refs/heads/main",
		ExpectedConfirmationHash:    inspection.ConfirmationHash,
	}); err != nil {
		t.Fatal(err)
	}
	assertReadOnlyGitCommands(t, recorder.commands)
	before := repositoryStateSnapshot(t, repo)
	_, _ = registry.Inspect(ctx, InspectionInput{
		LocalPath:                   repo,
		RepoTargetOverride:          "relay",
		ProposedConfiguredBranchRef: "refs/heads/missing",
	})
	after := repositoryStateSnapshot(t, repo)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed inspection mutated repository\nbefore=%v\nafter=%v", before, after)
	}
	assertReadOnlyGitCommands(t, recorder.commands)
}

type recordingGitRunner struct {
	delegate GitRunner
	commands [][]string
}

func (r *recordingGitRunner) Run(ctx context.Context, directory string, args ...string) (GitCommandResult, error) {
	r.commands = append(r.commands, append([]string{}, args...))
	return r.delegate.Run(ctx, directory, args...)
}

func assertReadOnlyGitCommands(t *testing.T, commands [][]string) {
	t.Helper()
	for _, command := range commands {
		if len(command) == 0 {
			t.Fatal("empty Git command")
		}
		switch command[0] {
		case "rev-parse", "remote", "check-ref-format", "cat-file", "status", "ls-tree":
		default:
			t.Fatalf("prohibited Git command issued: git %s", strings.Join(command, " "))
		}
		forbidden := []string{"fetch", "pull", "clone", "checkout", "switch", "reset", "merge", "rebase", "stash", "clean", "add", "commit", "update-ref", "branch"}
		for _, value := range command {
			for _, blocked := range forbidden {
				if value == blocked {
					t.Fatalf("prohibited Git argument issued: git %s", strings.Join(command, " "))
				}
			}
		}
	}
}

func repositoryStateSnapshot(t *testing.T, repo string) []string {
	t.Helper()
	return []string{
		gitOutput(t, repo, "symbolic-ref", "-q", "HEAD"),
		gitOutput(t, repo, "rev-parse", "HEAD"),
		gitOutput(t, repo, "status", "--porcelain=v1", "--untracked-files=all"),
		gitOutput(t, repo, "for-each-ref", "--format=%(refname):%(objectname)", "refs/heads"),
	}
}

func newBranchTestRegistry(t *testing.T) (*Registry, *workflowstore.Store, string) {
	t.Helper()
	store, _ := openRepositoryWorkflowStore(t)
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	return registry, store, newGitRepository(t)
}

func openRepositoryWorkflowStore(t *testing.T) (*workflowstore.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(
		filepath.Join(root, "workflow.sqlite"),
		filepath.Join(root, "artifacts"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "relay@example.test")
	gitRun(t, repo, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "initial")
	gitRun(t, repo, "branch", "alternate")
	gitRun(t, repo, "branch", "other")
	return repo
}

func inspectAndConfirm(
	t *testing.T,
	registry *Registry,
	repo string,
	target string,
	ref string,
) RegistrationResult {
	t.Helper()
	ctx := context.Background()
	inspection, err := registry.Inspect(ctx, InspectionInput{
		LocalPath:                   repo,
		RepoTargetOverride:          target,
		ProposedConfiguredBranchRef: ref,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Confirm(ctx, ConfirmationInput{
		LocalPath:                   repo,
		RepoTargetOverride:          target,
		ProposedConfiguredBranchRef: ref,
		ExpectedConfirmationHash:    inspection.ConfirmationHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func nullableRef(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
