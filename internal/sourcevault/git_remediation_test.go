package sourcevault

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestCommandGitDisablesPromisorLazyFetch(t *testing.T) {
	ctx := context.Background()
	source := newGitRepository(t)
	commit := commitFile(t, source, "retained.txt", []byte("retained\n"), "retained")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runStandaloneGit(t, "init", "--bare", remote)
	runTestGit(t, source, "remote", "add", "origin", remote)
	runTestGit(t, source, "push", "origin", "HEAD:refs/heads/main")
	runTestGit(t, source, "config", "core.repositoryformatversion", "1")
	runTestGit(t, source, "config", "extensions.partialClone", "origin")
	runTestGit(t, source, "config", "remote.origin.promisor", "true")
	runTestGit(t, source, "config", "remote.origin.partialclonefilter", "blob:none")
	marker := filepath.Join(t.TempDir(), "upload-pack-invoked")
	probe := writeUploadPackProbe(t, marker)
	runTestGit(t, source, "config", "remote.origin.uploadpack", commandPath(probe))
	removeLooseGitObject(t, source, commit.commit)
	if gitObjectExists(source, commit.commit) {
		t.Fatal("promised commit remained locally available")
	}

	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", source, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	_, err := manager.ImportClosure(ctx, ImportRequest{
		Revision: explicitRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree),
	})
	if ErrorCode(err) != CodeSourceObjectUnavailable || FailureReason(err) != workflowstore.SourceVaultFailureSourceCommitMissing {
		t.Fatalf("promisor import error = %v code=%q reason=%q", err, ErrorCode(err), FailureReason(err))
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("lazy fetch invoked upload-pack probe: %v", statErr)
	}
	if gitObjectExists(source, commit.commit) {
		t.Fatal("promisor import materialized the missing commit")
	}
	runStandaloneGit(t, "--git-dir", remote, "cat-file", "-e", commit.commit+"^{commit}")
}

func TestCommandGitRejectsAllSourceVaultOverlapDirections(t *testing.T) {
	ctx := context.Background()

	t.Run("source equals vault root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "vault-root")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, root, "init")
		runTestGit(t, root, "config", "user.name", "Relay Tests")
		runTestGit(t, root, "config", "user.email", "relay@example.test")
		commitFile(t, root, "initial.txt", []byte("initial\n"), "initial")
		before := snapshotSourceRepository(t, root)
		requireUnsafeVaultLayout(t, ctx, root, root)
		if after := snapshotSourceRepository(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("equal source/root changed\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("vault root below source", func(t *testing.T) {
		source := newGitRepository(t)
		commitFile(t, source, "initial.txt", []byte("initial\n"), "initial")
		root := filepath.Join(source, "vault-storage")
		before := snapshotSourceRepository(t, source)
		requireUnsafeVaultLayout(t, ctx, root, source)
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("unsafe descendant vault root was created: %v", err)
		}
		if after := snapshotSourceRepository(t, source); !reflect.DeepEqual(after, before) {
			t.Fatalf("ancestor source changed\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("source below managed vault subtree", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "vault-root")
		source := filepath.Join(root, "repositories", "registered-source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, source, "init")
		runTestGit(t, source, "config", "user.name", "Relay Tests")
		runTestGit(t, source, "config", "user.email", "relay@example.test")
		commitFile(t, source, "initial.txt", []byte("initial\n"), "initial")
		before := snapshotSourceRepository(t, source)
		requireUnsafeVaultLayout(t, ctx, root, source)
		if after := snapshotSourceRepository(t, source); !reflect.DeepEqual(after, before) {
			t.Fatalf("managed-subtree source changed\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("late registered source below vault root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "vault-root")
		git, err := newCommandGit(ctx, root, nil)
		if err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(root, "repositories", "late-source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, source, "init")
		runTestGit(t, source, "config", "user.name", "Relay Tests")
		runTestGit(t, source, "config", "user.email", "relay@example.test")
		commitFile(t, source, "initial.txt", []byte("initial\n"), "initial")
		before := snapshotSourceRepository(t, source)
		if _, err := git.ValidateRepositorySeparation(ctx, source); ErrorCode(err) != CodeUnsafeVaultRoot {
			t.Fatalf("late overlapping repository error = %v code=%q", err, ErrorCode(err))
		}
		if after := snapshotSourceRepository(t, source); !reflect.DeepEqual(after, before) {
			t.Fatalf("late source changed\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("symlinked ancestor resolves into source", func(t *testing.T) {
		realParent := t.TempDir()
		source := filepath.Join(realParent, "source")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, source, "init")
		linkParent := filepath.Join(t.TempDir(), "linked-parent")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		root := filepath.Join(linkParent, "source", "vaults")
		requireUnsafeVaultLayout(t, ctx, root, source)
		if _, err := os.Lstat(filepath.Join(source, "vaults")); !os.IsNotExist(err) {
			t.Fatalf("symlinked unsafe vault root was created: %v", err)
		}
	})

	t.Run("disjoint layout remains valid", func(t *testing.T) {
		source := newGitRepository(t)
		commitFile(t, source, "initial.txt", []byte("initial\n"), "initial")
		root := filepath.Join(t.TempDir(), "vault-root")
		before := snapshotSourceRepository(t, source)
		git, err := newCommandGit(ctx, root, []workflowstore.RepositoryTarget{{RepoTarget: "relay", LocalPath: source}})
		if err != nil {
			t.Fatal(err)
		}
		if exists, err := git.ValidateRepositorySeparation(ctx, source); err != nil || !exists {
			t.Fatalf("disjoint repository validation = %v, %v", exists, err)
		}
		if after := snapshotSourceRepository(t, source); !reflect.DeepEqual(after, before) {
			t.Fatalf("disjoint source changed\nbefore: %#v\nafter:  %#v", before, after)
		}
	})
}

func requireUnsafeVaultLayout(t *testing.T, ctx context.Context, root, source string) {
	t.Helper()
	_, err := newCommandGit(ctx, root, []workflowstore.RepositoryTarget{{RepoTarget: "relay", LocalPath: source}})
	if ErrorCode(err) != CodeUnsafeVaultRoot {
		t.Fatalf("unsafe vault layout root=%q source=%q error=%v code=%q", root, source, err, ErrorCode(err))
	}
}

func writeUploadPackProbe(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upload-pack-probe")
	var content string
	if runtime.GOOS == "windows" {
		path += ".cmd"
		content = "@echo invoked>\"" + strings.ReplaceAll(marker, "\"", "\"\"") + "\"\r\n@git-upload-pack %*\r\n"
	} else {
		content = "#!/bin/sh\nprintf invoked > " + shellSingleQuote(marker) + "\nexec git-upload-pack \"$@\"\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func commandPath(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return shellSingleQuote(value)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
