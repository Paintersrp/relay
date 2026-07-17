package workflow

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestRepositoryApplicationTransfersCompleteConfiguredAndExplicitResults(t *testing.T) {
	ctx := context.Background()
	store, registry, repo := newApplicationRepositoryFixture(t)
	service := &Service{store: store, registry: registry}

	listed, err := service.ListRepositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 ||
		!listed[0].ConfiguredBranchRef.Valid ||
		listed[0].ConfiguredBranchRef.String != "refs/heads/main" ||
		listed[0].ConfigurationVersion != 1 {
		t.Fatalf("list result = %#v", listed)
	}
	got, err := service.GetRepository(ctx, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfiguredBranchRef.String != "refs/heads/main" || got.ConfigurationVersion != 1 {
		t.Fatalf("get result = %#v", got)
	}

	configured, err := service.ResolveRepositoryRevision(ctx, RepositoryRevisionInput{
		RepoTarget: "relay",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCommit := appGitOutput(t, repo, "rev-parse", "refs/heads/main^{commit}")
	wantTree := appGitOutput(t, repo, "rev-parse", "refs/heads/main^{tree}")
	if configured.RevisionSource != workflowrepos.RevisionSourceConfiguredWorkingBranch ||
		configured.ConfiguredWorkingBranchRef != "refs/heads/main" ||
		configured.RepositoryTargetConfigurationVersion != 1 ||
		configured.CommitOID != wantCommit ||
		configured.TreeOID != wantTree {
		t.Fatalf("configured application result = %#v", configured)
	}

	explicit, err := service.ResolveRepositoryRevision(ctx, RepositoryRevisionInput{
		RepoTarget:        "relay",
		ExplicitCommitOID: wantCommit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.RevisionSource != workflowrepos.RevisionSourceExplicitCommit ||
		explicit.ConfiguredWorkingBranchRef != "" ||
		explicit.RepositoryTargetConfigurationVersion != 1 ||
		explicit.CommitOID != wantCommit ||
		explicit.TreeOID != wantTree {
		t.Fatalf("explicit application result = %#v", explicit)
	}
}

func TestRepositoryApplicationDelegatesFailurePoliciesWithoutFallback(t *testing.T) {
	ctx := context.Background()
	store, registry, repo := newApplicationRepositoryFixture(t)
	service := &Service{store: store, registry: registry}

	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.ResolveRepositoryRevision(ctx, RepositoryRevisionInput{
		RepoTarget: "relay",
		Policy: RepositoryUsePolicy{
			RequireCleanWorktree: true,
		},
	})
	if !errors.Is(err, workflowrepos.ErrDirtyProjectWorktree) {
		t.Fatalf("dirty policy error = %v", err)
	}

	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, "unconfigured", appNewGitRepository(t))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveRepositoryRevision(ctx, RepositoryRevisionInput{
		RepoTarget: "unconfigured",
	}); !errors.Is(err, workflowrepos.ErrRepositoryUnconfigured) {
		t.Fatalf("unconfigured error = %v", err)
	}

	if _, err := service.ResolveRepositoryRevision(ctx, RepositoryRevisionInput{
		RepoTarget: "relay",
		Policy: RepositoryUsePolicy{
			RequireGovernanceAuthority: true,
		},
		Governance: GovernanceRequest{
			ManifestPath: "planner-source-manifest.json",
			Domain:       "selected_pass_execution_spec",
		},
	}); !errors.Is(err, workflowrepos.ErrGovernanceUnavailable) {
		t.Fatalf("non-governance target error = %v", err)
	}
}

func newApplicationRepositoryFixture(
	t *testing.T,
) (*workflowstore.Store, *workflowrepos.Registry, string) {
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
	repo := appNewGitRepository(t)
	if err := store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(
			context.Background(),
			workflowstore.CreateRepositoryTargetParams{
				RepoTarget:          "relay",
				LocalPath:           repo,
				ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
			},
		)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	return store, registry, repo
}

func appNewGitRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	appGitRun(t, repo, "init", "-b", "main")
	appGitRun(t, repo, "config", "user.email", "relay@example.test")
	appGitRun(t, repo, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("relay\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appGitRun(t, repo, "add", "README.md")
	appGitRun(t, repo, "commit", "-m", "initial")
	return repo
}

func appGitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func appGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
