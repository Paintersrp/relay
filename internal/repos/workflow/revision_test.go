package workflowrepos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestConfiguredAndExplicitRevisionIgnoreCheckoutState(t *testing.T) {
	ctx := context.Background()
	registry, store, repo := newBranchTestRegistry(t)
	inspectAndConfirm(t, registry, repo, "relay", "refs/heads/main")
	mainCommit := gitOutput(t, repo, "rev-parse", "refs/heads/main^{commit}")
	mainTree := gitOutput(t, repo, "rev-parse", "refs/heads/main^{tree}")

	for _, checkout := range []string{"main", "alternate", "detached"} {
		t.Run(checkout, func(t *testing.T) {
			switch checkout {
			case "main", "alternate":
				gitRun(t, repo, "checkout", checkout)
			case "detached":
				gitRun(t, repo, "checkout", "--detach", "refs/heads/alternate")
			}
			configured, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"})
			if err != nil {
				t.Fatal(err)
			}
			if configured.RevisionSource != RevisionSourceConfiguredWorkingBranch ||
				configured.ConfiguredWorkingBranchRef != "refs/heads/main" ||
				configured.RepositoryTargetConfigurationVersion != 1 ||
				configured.CommitOID != mainCommit ||
				configured.TreeOID != mainTree {
				t.Fatalf("configured result = %#v", configured)
			}
			explicit, err := registry.ResolveRevision(ctx, RevisionRequest{
				RepoTarget:        "relay",
				ExplicitCommitOID: mainCommit,
			})
			if err != nil {
				t.Fatal(err)
			}
			if explicit.RevisionSource != RevisionSourceExplicitCommit ||
				explicit.ConfiguredWorkingBranchRef != "" ||
				explicit.RepositoryTargetConfigurationVersion != 1 ||
				explicit.CommitOID != mainCommit ||
				explicit.TreeOID != mainTree {
				t.Fatalf("explicit result = %#v", explicit)
			}
		})
	}

	stored, err := store.GetRepositoryTarget(ctx, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConfigurationVersion != 1 ||
		stored.ConfiguredBranchRef.String != "refs/heads/main" {
		t.Fatalf("resolution mutated target = %#v", stored)
	}
}

func TestOmittedRevisionFailureMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("unconfigured under every checkout state", func(t *testing.T) {
		registry, store, repo := newBranchTestRegistry(t)
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.CreateRepositoryTarget(ctx, "relay", repo)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, checkout := range []string{"main", "alternate", "detached"} {
			switch checkout {
			case "main", "alternate":
				gitRun(t, repo, "checkout", checkout)
			case "detached":
				gitRun(t, repo, "checkout", "--detach", "refs/heads/alternate")
			}
			if _, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"}); !errors.Is(err, ErrRepositoryUnconfigured) {
				t.Fatalf("checkout %s error = %v", checkout, err)
			}
		}
	})

	t.Run("missing and deleted stored refs", func(t *testing.T) {
		for _, ref := range []string{"refs/heads/missing", "refs/heads/deleted"} {
			registry, store, repo := newBranchTestRegistry(t)
			if strings.HasSuffix(ref, "deleted") {
				gitRun(t, repo, "branch", "deleted")
			}
			if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
					RepoTarget:          "relay",
					LocalPath:           repo,
					ConfiguredBranchRef: sql.NullString{String: ref, Valid: true},
				})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(ref, "deleted") {
				gitRun(t, repo, "branch", "-D", "deleted")
			}
			if _, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"}); !errors.Is(err, ErrConfiguredBranchUnavailable) {
				t.Fatalf("ref %s error = %v", ref, err)
			}
		}
	})

	t.Run("unborn stored ref", func(t *testing.T) {
		store, _ := openRepositoryWorkflowStore(t)
		repo := t.TempDir()
		gitRun(t, repo, "init", "-b", "main")
		gitRun(t, repo, "config", "user.email", "relay@example.test")
		gitRun(t, repo, "config", "user.name", "Relay Test")
		registry, err := NewRegistry(store)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
				RepoTarget:          "relay",
				LocalPath:           repo,
				ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"}); !errors.Is(err, ErrConfiguredBranchUnavailable) {
			t.Fatalf("unborn ref error = %v", err)
		}
	})
}

func TestExplicitRevisionClosedInputAndObjectMatrix(t *testing.T) {
	ctx := context.Background()
	registry, store, repo := newBranchTestRegistry(t)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTarget(ctx, "relay", repo)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	commit := gitOutput(t, repo, "rev-parse", "HEAD")
	tree := gitOutput(t, repo, "rev-parse", "HEAD^{tree}")
	blob := gitOutput(t, repo, "rev-parse", "HEAD:README.md")

	invalid := []string{
		"",
		commit[:12],
		strings.ToUpper(commit),
		"refs/heads/main",
		"HEAD",
		"main",
		commit + "^{tree}",
		strings.Repeat("g", 40),
	}
	for _, value := range invalid {
		t.Run("invalid-"+strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			request := RevisionRequest{RepoTarget: "relay", ExplicitCommitOID: value}
			if value == "" {
				_, err := registry.ResolveRevision(ctx, request)
				if !errors.Is(err, ErrRepositoryUnconfigured) {
					t.Fatalf("empty explicit value error = %v", err)
				}
				return
			}
			if _, err := registry.ResolveRevision(ctx, request); !errors.Is(err, ErrInvalidExplicitCommit) {
				t.Fatalf("value %q error = %v", value, err)
			}
		})
	}
	for name, oid := range map[string]string{
		"missing": strings.Repeat("a", 40),
		"tree":    tree,
		"blob":    blob,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := registry.ResolveRevision(ctx, RevisionRequest{
				RepoTarget:        "relay",
				ExplicitCommitOID: oid,
			}); !errors.Is(err, ErrRepositoryObject) {
				t.Fatalf("%s object error = %v", name, err)
			}
		})
	}

	other := newGitRepository(t)
	foreignCommit := gitOutput(t, other, "rev-parse", "HEAD")
	if _, err := registry.ResolveRevision(ctx, RevisionRequest{
		RepoTarget:        "relay",
		ExplicitCommitOID: foreignCommit,
	}); !errors.Is(err, ErrRepositoryObject) {
		t.Fatalf("foreign commit error = %v", err)
	}
}

func TestProjectCleanlinessPolicyMatrixAndPreservation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		dirty func(*testing.T, string)
	}{
		{
			name: "staged",
			dirty: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitRun(t, repo, "add", "staged.txt")
			},
		},
		{
			name: "unstaged tracked",
			dirty: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "untracked",
			dirty: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "combined",
			dirty: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitRun(t, repo, "add", "staged.txt")
				if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("unstaged\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry, _, repo := newBranchTestRegistry(t)
			inspectAndConfirm(t, registry, repo, "relay", "refs/heads/main")
			tc.dirty(t, repo)
			before := gitOutput(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
			if _, err := registry.ResolveRevision(ctx, RevisionRequest{
				RepoTarget: "relay",
				Policy: RepositoryUsePolicy{
					RequireCleanWorktree: true,
				},
			}); !errors.Is(err, ErrDirtyProjectWorktree) {
				t.Fatalf("dirty state error = %v", err)
			}
			after := gitOutput(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
			if before != after {
				t.Fatalf("resolver altered dirty state\nbefore=%q\nafter=%q", before, after)
			}
		})
	}

	t.Run("clean succeeds", func(t *testing.T) {
		registry, _, repo := newBranchTestRegistry(t)
		inspectAndConfirm(t, registry, repo, "relay", "refs/heads/main")
		if _, err := registry.ResolveRevision(ctx, RevisionRequest{
			RepoTarget: "relay",
			Policy: RepositoryUsePolicy{
				RequireCleanWorktree: true,
			},
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("non Project omits status command and permits dirtiness", func(t *testing.T) {
		store, _ := openRepositoryWorkflowStore(t)
		repo := newGitRepository(t)
		if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		recorder := &recordingGitRunner{delegate: newExecGitRunner()}
		registry, err := NewRegistryWithRunner(store, recorder)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
				RepoTarget:          "relay",
				LocalPath:           repo,
				ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"}); err != nil {
			t.Fatal(err)
		}
		for _, command := range recorder.commands {
			if len(command) > 0 && command[0] == "status" {
				t.Fatalf("non-Project resolution issued cleanliness command: %v", command)
			}
		}
	})
}

func TestGovernanceAvailabilitySuccessFailureAndComposition(t *testing.T) {
	ctx := context.Background()

	t.Run("clean and dirty governance-only success", func(t *testing.T) {
		_, store, repo, commit := newGovernanceRegistry(t, validGovernanceManifest())
		recorder := &recordingGitRunner{delegate: newExecGitRunner()}
		registry, err := NewRegistryWithRunner(store, recorder)
		if err != nil {
			t.Fatal(err)
		}
		beforeCounts := durableWorkflowCounts(t, store)
		for _, dirty := range []bool{false, true} {
			if dirty {
				if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := registry.ResolveRevision(ctx, RevisionRequest{
				RepoTarget:        "relay-specs",
				ExplicitCommitOID: commit,
				Policy: RepositoryUsePolicy{
					RequireGovernanceAuthority: true,
				},
				Governance: GovernanceRequest{
					ManifestPath: "planner-source-manifest.json",
					Domain:       "selected_pass_execution_spec",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.GovernanceAvailability == nil ||
				result.GovernanceAvailability.ManifestBlobOID == "" ||
				len(result.GovernanceAvailability.Members) != 2 ||
				result.GovernanceAvailability.Members[0].Path != "contracts/cross-cutting.md" ||
				result.GovernanceAvailability.Members[1].Path != "contracts/execution-spec.md" {
				t.Fatalf("governance availability = %#v", result.GovernanceAvailability)
			}
		}
		afterCounts := durableWorkflowCounts(t, store)
		if !reflect.DeepEqual(beforeCounts, afterCounts) {
			t.Fatalf("resolution persisted transient authority\nbefore=%v\nafter=%v", beforeCounts, afterCounts)
		}
		assertReadOnlyGitCommands(t, recorder.commands)
	})

	t.Run("combined Project and governance dirty precedence", func(t *testing.T) {
		registry, _, repo, commit := newGovernanceRegistry(t, `{"domains":{}}`)
		if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := registry.ResolveRevision(ctx, RevisionRequest{
			RepoTarget:        "relay-specs",
			ExplicitCommitOID: commit,
			Policy: RepositoryUsePolicy{
				RequireCleanWorktree:       true,
				RequireGovernanceAuthority: true,
			},
			Governance: GovernanceRequest{
				ManifestPath: "planner-source-manifest.json",
				Domain:       "selected_pass_execution_spec",
			},
		})
		if !errors.Is(err, ErrDirtyProjectWorktree) {
			t.Fatalf("combined policy error = %v, want dirty precedence", err)
		}
	})

	cases := []struct {
		name         string
		manifest     string
		manifestPath string
		domain       string
		mutate       func(*testing.T, string, string)
	}{
		{name: "missing manifest", manifest: validGovernanceManifest(), manifestPath: "missing.json", domain: "selected_pass_execution_spec"},
		{name: "missing domain", manifest: `{"domains":{"other":["contracts/cross-cutting.md"]}}`, manifestPath: "planner-source-manifest.json", domain: "selected_pass_execution_spec"},
		{name: "duplicate member", manifest: `{"domains":{"selected_pass_execution_spec":["contracts/cross-cutting.md","contracts/cross-cutting.md"]}}`, manifestPath: "planner-source-manifest.json", domain: "selected_pass_execution_spec"},
		{name: "invalid member", manifest: `{"domains":{"selected_pass_execution_spec":["../escape.md"]}}`, manifestPath: "planner-source-manifest.json", domain: "selected_pass_execution_spec"},
		{name: "missing member", manifest: `{"domains":{"selected_pass_execution_spec":["contracts/missing.md"]}}`, manifestPath: "planner-source-manifest.json", domain: "selected_pass_execution_spec"},
		{
			name:         "manifest non blob",
			manifest:     validGovernanceManifest(),
			manifestPath: "manifest-directory",
			domain:       "selected_pass_execution_spec",
			mutate: func(t *testing.T, repo, _ string) {
				if err := os.MkdirAll(filepath.Join(repo, "manifest-directory"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo, "manifest-directory", "member"), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				gitRun(t, repo, "add", "manifest-directory")
				gitRun(t, repo, "commit", "-m", "add manifest directory")
			},
		},
		{
			name:         "member non blob",
			manifest:     `{"domains":{"selected_pass_execution_spec":["contracts"]}}`,
			manifestPath: "planner-source-manifest.json",
			domain:       "selected_pass_execution_spec",
		},
		{
			name:         "unavailable member blob",
			manifest:     validGovernanceManifest(),
			manifestPath: "planner-source-manifest.json",
			domain:       "selected_pass_execution_spec",
			mutate: func(t *testing.T, repo, commit string) {
				blob := gitOutput(t, repo, "rev-parse", commit+":contracts/execution-spec.md")
				deleteLooseObject(t, repo, blob)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry, _, repo, commit := newGovernanceRegistry(t, tc.manifest)
			if tc.mutate != nil {
				tc.mutate(t, repo, commit)
				commit = gitOutput(t, repo, "rev-parse", "HEAD")
			}
			_, err := registry.ResolveRevision(ctx, RevisionRequest{
				RepoTarget:        "relay-specs",
				ExplicitCommitOID: commit,
				Policy: RepositoryUsePolicy{
					RequireGovernanceAuthority: true,
				},
				Governance: GovernanceRequest{
					ManifestPath: tc.manifestPath,
					Domain:       tc.domain,
				},
			})
			if !errors.Is(err, ErrGovernanceUnavailable) {
				t.Fatalf("%s error = %v", tc.name, err)
			}
		})
	}

	t.Run("missing selected commit object", func(t *testing.T) {
		registry, _, _, _ := newGovernanceRegistry(t, validGovernanceManifest())
		_, err := registry.ResolveRevision(ctx, RevisionRequest{
			RepoTarget:        "relay-specs",
			ExplicitCommitOID: strings.Repeat("a", 40),
			Policy: RepositoryUsePolicy{
				RequireGovernanceAuthority: true,
			},
			Governance: GovernanceRequest{
				ManifestPath: "planner-source-manifest.json",
				Domain:       "selected_pass_execution_spec",
			},
		})
		if !errors.Is(err, ErrRepositoryObject) {
			t.Fatalf("missing commit error = %v", err)
		}
	})

	t.Run("missing selected commit and tree objects", func(t *testing.T) {
		registry, _, repo, commit := newGovernanceRegistry(t, validGovernanceManifest())
		tree := gitOutput(t, repo, "rev-parse", commit+"^{tree}")
		deleteLooseObject(t, repo, tree)
		_, err := registry.ResolveRevision(ctx, RevisionRequest{
			RepoTarget:        "relay-specs",
			ExplicitCommitOID: commit,
			Policy: RepositoryUsePolicy{
				RequireGovernanceAuthority: true,
			},
			Governance: GovernanceRequest{
				ManifestPath: "planner-source-manifest.json",
				Domain:       "selected_pass_execution_spec",
			},
		})
		if !errors.Is(err, ErrRepositoryObject) && !errors.Is(err, ErrGovernanceUnavailable) {
			t.Fatalf("missing tree error = %v", err)
		}
	})
}

func TestRevisionCommandSurfaceIsReadOnlyAcrossSuccessAndFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := openRepositoryWorkflowStore(t)
	repo := newGitRepository(t)
	recorder := &recordingGitRunner{delegate: newExecGitRunner()}
	registry, err := NewRegistryWithRunner(store, recorder)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget:          "relay",
			LocalPath:           repo,
			ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = registry.ResolveRevision(ctx, RevisionRequest{RepoTarget: "relay"})
	_, _ = registry.ResolveRevision(ctx, RevisionRequest{
		RepoTarget:        "relay",
		ExplicitCommitOID: strings.Repeat("a", 40),
	})
	_, _ = registry.ResolveRevision(ctx, RevisionRequest{
		RepoTarget: "relay",
		Policy: RepositoryUsePolicy{
			RequireCleanWorktree: true,
		},
	})
	assertReadOnlyGitCommands(t, recorder.commands)
}

func validGovernanceManifest() string {
	value := map[string]any{
		"manifest_version": "1.0",
		"domains": map[string]any{
			"selected_pass_execution_spec": []string{
				"contracts/cross-cutting.md",
				"contracts/execution-spec.md",
			},
		},
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func newGovernanceRegistry(
	t *testing.T,
	manifest string,
) (*Registry, *workflowstore.Store, string, string) {
	t.Helper()
	store, _ := openRepositoryWorkflowStore(t)
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "relay@example.test")
	gitRun(t, repo, "config", "user.name", "Relay Test")
	if err := os.MkdirAll(filepath.Join(repo, "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"cross-cutting.md", "execution-spec.md"} {
		if err := os.WriteFile(filepath.Join(repo, "contracts", path), []byte(path+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "planner-source-manifest.json"), []byte(manifest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "governance")
	commit := gitOutput(t, repo, "rev-parse", "HEAD")
	registry, err := NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(
			context.Background(),
			workflowstore.CreateRepositoryTargetParams{
				RepoTarget:          "relay-specs",
				LocalPath:           repo,
				ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
			},
		)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return registry, store, repo, commit
}

func deleteLooseObject(t *testing.T, repo, oid string) {
	t.Helper()
	path := filepath.Join(repo, ".git", "objects", oid[:2], oid[2:])
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove Git object %s: %v", oid, err)
	}
}

func durableWorkflowCounts(t *testing.T, store *workflowstore.Store) map[string]int {
	t.Helper()
	tables := []string{
		"repository_targets",
		"artifacts",
		"operation_packets",
		"operation_packet_retention_dependencies",
	}
	out := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		out[table] = count
	}
	return out
}
