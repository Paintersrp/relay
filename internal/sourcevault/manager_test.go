package sourcevault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestManagerPreservesExactClosureAcrossSourceChangesAndRetentionGenerations(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "payload.bin", []byte{0, 1, 2, 3, 255}, "first")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)

	before := snapshotSourceRepository(t, repo)
	result, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), first.commit, first.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready || result.Closure.Generation != 1 || result.CommitOID != first.commit || result.TreeOID != first.tree {
		t.Fatalf("first import = %#v", result)
	}
	if after := snapshotSourceRepository(t, repo); !reflect.DeepEqual(after, before) {
		t.Fatalf("source repository changed during import\nbefore: %#v\nafter:  %#v", before, after)
	}

	owners := []string{
		workflowstore.SourceVaultOwnerOperationPacket,
		workflowstore.SourceVaultOwnerArtifact,
		workflowstore.SourceVaultOwnerWorkflowResult,
		workflowstore.SourceVaultOwnerAuditRecord,
	}
	retentions := make([]workflowstore.SourceVaultRetention, 0, len(owners))
	for _, ownerClass := range owners {
		retention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: result.Closure.ClosureID, OwnerClass: ownerClass, OwnerIdentity: ownerClass + "-1"})
		if err != nil {
			t.Fatal(err)
		}
		retentions = append(retentions, retention)
	}

	second := commitFile(t, repo, "later.txt", []byte("later\n"), "move branch")
	if second.commit == first.commit {
		t.Fatal("branch did not move")
	}
	read, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: first.blob, ExpectedType: "blob", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read.Bytes, []byte{0, 1, 2, 3, 255}) {
		t.Fatalf("vault blob bytes = %v", read.Bytes)
	}
	if read.ObjectOID != first.blob || read.ObjectType != "blob" {
		t.Fatalf("read identity = %#v", read)
	}

	for _, retention := range retentions[:3] {
		if _, err := manager.ReleaseRetention(ctx, retention.RetentionID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.CleanupClosure(ctx, result.Closure.ClosureID); ErrorCode(err) != CodeCleanupBlocked {
		t.Fatalf("cleanup with one owner error = %v", err)
	}
	if _, err := manager.ReleaseRetention(ctx, retentions[3].RetentionID); err != nil {
		t.Fatal(err)
	}
	released, err := manager.CleanupClosure(ctx, result.Closure.ClosureID)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != workflowstore.SourceVaultClosureStateReleased || !released.ReleasedAt.Valid {
		t.Fatalf("released closure = %#v", released)
	}

	reimport, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(storeTarget(t, ctx, store, "relay"), first.commit, first.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if reimport.Closure.Generation != 2 || reimport.Closure.ClosureID == result.Closure.ClosureID || reimport.RefName == result.RefName {
		t.Fatalf("reimport = %#v", reimport)
	}
	retainedAgain, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: reimport.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: workflowstore.SourceVaultOwnerArtifact + "-1"})
	if err != nil {
		t.Fatal(err)
	}
	if retainedAgain.ClosureRowID != reimport.Closure.ID {
		t.Fatalf("generation-2 retention = %#v", retainedAgain)
	}
	history, err := store.ListSourceVaultClosuresByIdentity(ctx, result.Vault.ID, first.commit, first.tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].State != workflowstore.SourceVaultClosureStateReleased || history[1].State != workflowstore.SourceVaultClosureStateReady {
		t.Fatalf("closure history = %#v", history)
	}
}

func TestManagerConfiguredFreshnessAndExplicitCommitIndependence(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "first.txt", []byte("first\n"), "first")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	captured := storeTarget(t, ctx, store, "relay")
	configured := configuredRevision(captured, first.commit, first.tree)

	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{
			RepoTarget:                   "relay",
			ExpectedConfigurationVersion: captured.ConfigurationVersion,
			ConfiguredBranchRef:          "refs/heads/reconfigured",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ImportClosure(ctx, ImportRequest{Revision: configured}); ErrorCode(err) != CodeStaleConfiguredAuthority {
		t.Fatalf("stale configured import error = %v", err)
	}
	assertTableCount(t, store, "source_vaults", 0)
	assertTableCount(t, store, "source_vault_closures", 0)
	assertTableCount(t, store, "source_vault_retentions", 0)

	explicit := explicitRevision(captured, first.commit, first.tree)
	result, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicit})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitOID != first.commit || result.TreeOID != first.tree {
		t.Fatalf("explicit result = %#v", result)
	}
}

func TestManagerBranchMovementNeverSelectsNewTipAndMissingOriginalFailsClosed(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "first.txt", []byte("first\n"), "first")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	revision := configuredRevision(storeTarget(t, ctx, store, "relay"), first.commit, first.tree)

	second := commitFile(t, repo, "second.txt", []byte("second\n"), "second")
	result, err := manager.ImportClosure(ctx, ImportRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitOID != first.commit || result.CommitOID == second.commit {
		t.Fatalf("branch movement selected wrong commit: %#v", result)
	}

	otherStore := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, otherStore, "relay", repo, "refs/heads/main")
	otherManager := openSourceVaultManager(t, ctx, otherStore)
	missingRevision := configuredRevision(storeTarget(t, ctx, otherStore, "relay"), first.commit, first.tree)
	removeLooseGitObject(t, repo, first.commit)
	_, err = otherManager.ImportClosure(ctx, ImportRequest{Revision: missingRevision})
	if ErrorCode(err) != CodeSourceObjectUnavailable || FailureReason(err) != workflowstore.SourceVaultFailureSourceCommitMissing {
		t.Fatalf("missing original object error = %v code=%q reason=%q", err, ErrorCode(err), FailureReason(err))
	}
	closures, listErr := otherStore.ListSourceVaultClosuresForReconciliation(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(closures) != 1 || closures[0].CommitOID != first.commit || closures[0].State != workflowstore.SourceVaultClosureStateUnavailable {
		t.Fatalf("missing-object closure = %#v", closures)
	}
}

func TestManagerImportsUnreferencedCommitAndSurvivesBranchDeletionDetachmentAndSourceGC(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "retained.txt", []byte("retained\n"), "retained")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	revision := configuredRevision(storeTarget(t, ctx, store, "relay"), first.commit, first.tree)

	runTestGit(t, repo, "checkout", "--detach", first.commit)
	runTestGit(t, repo, "branch", "-D", "main")
	if refs := runTestGit(t, repo, "for-each-ref", "--format=%(refname)"); refs != "" {
		t.Fatalf("commit remained advertised by refs: %q", refs)
	}
	result, err := manager.ImportClosure(ctx, ImportRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: result.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "artifact-detached"}); err != nil {
		t.Fatal(err)
	}

	runTestGit(t, repo, "checkout", "--orphan", "replacement")
	if err := os.Remove(filepath.Join(repo, "retained.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "replacement.txt"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", "-A")
	runTestGit(t, repo, "commit", "-m", "replacement")
	replacement := runTestGit(t, repo, "rev-parse", "HEAD")
	runTestGit(t, repo, "checkout", "--detach", replacement)
	runTestGit(t, repo, "branch", "-D", "replacement")
	runTestGit(t, repo, "reflog", "expire", "--expire=now", "--all")
	runTestGit(t, repo, "gc", "--prune=now")
	if gitObjectExists(repo, first.commit) {
		t.Fatal("source garbage collection retained the original commit")
	}
	read, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: first.blob, ExpectedType: "blob", MaxBytes: 1024})
	if err != nil || string(read.Bytes) != "retained\n" {
		t.Fatalf("vault read after source GC = %#v, %v", read, err)
	}
}

func TestManagerCleanupPreservesUnrelatedAndSharedClosureReachability(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	first := commitFile(t, repo, "shared.txt", []byte("shared\n"), "first")
	second := commitFile(t, repo, "second.txt", []byte("second\n"), "second")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	target := storeTarget(t, ctx, store, "relay")
	firstResult, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(target, first.commit, first.tree)})
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(target, second.commit, second.tree)})
	if err != nil {
		t.Fatal(err)
	}
	firstRetention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: firstResult.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "artifact-first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: secondResult.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "artifact-second"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseRetention(ctx, firstRetention.RetentionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CleanupClosure(ctx, firstResult.Closure.ClosureID); err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: secondResult.Closure.ClosureID, ObjectOID: first.blob, ExpectedType: "blob", MaxBytes: 1024})
	if err != nil || string(read.Bytes) != "shared\n" {
		t.Fatalf("shared object after unrelated cleanup = %#v, %v", read, err)
	}
	vaultPath, err := manager.git.VaultPath(secondResult.Vault.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	if oid, exists, err := manager.git.ReadRef(ctx, vaultPath, secondResult.RefName); err != nil || !exists || oid != second.commit {
		t.Fatalf("unrelated generation ref = %q exists=%v err=%v", oid, exists, err)
	}
}

func TestManagerVaultOnlyLookupBoundaries(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commit := commitFile(t, repo, "binary.bin", []byte{0, 4, 8, 12, 0, 255}, "binary")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	result, err := manager.ImportClosure(ctx, ImportRequest{Revision: explicitRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: commit.commit, ExpectedType: "commit", MaxBytes: 4096})
	if ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("unretained lookup error = %v", err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: result.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerWorkflowResult, OwnerIdentity: "workflow-lookup"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		oid      string
		typeName string
		limit    int64
		wantCode string
	}{
		{name: "short oid", oid: "abc", typeName: "blob", limit: 64, wantCode: CodeInvalidRequest},
		{name: "uppercase oid", oid: strings.ToUpper(commit.blob), typeName: "blob", limit: 64, wantCode: CodeInvalidRequest},
		{name: "wrong expected type", oid: commit.blob, typeName: "commit", limit: 64, wantCode: CodeObjectUnavailable},
		{name: "zero limit", oid: commit.blob, typeName: "blob", limit: 0, wantCode: CodeInvalidRequest},
		{name: "excessive limit", oid: commit.blob, typeName: "blob", limit: MaxObjectReadBytes + 1, wantCode: CodeInvalidRequest},
		{name: "bounded overflow", oid: commit.blob, typeName: "blob", limit: 2, wantCode: CodeObjectLimitExceeded},
		{name: "missing object", oid: strings.Repeat("f", 40), typeName: "blob", limit: 64, wantCode: CodeObjectUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: tc.oid, ExpectedType: tc.typeName, MaxBytes: tc.limit})
			if ErrorCode(err) != tc.wantCode {
				t.Fatalf("lookup error = %v code=%q, want %q", err, ErrorCode(err), tc.wantCode)
			}
		})
	}

	for _, object := range []struct {
		oid      string
		typeName string
	}{
		{oid: commit.commit, typeName: "commit"},
		{oid: commit.tree, typeName: "tree"},
		{oid: commit.blob, typeName: "blob"},
	} {
		read, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: object.oid, ExpectedType: object.typeName, MaxBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		if read.ObjectOID != object.oid || read.ObjectType != object.typeName || len(read.Bytes) == 0 {
			t.Fatalf("read result = %#v", read)
		}
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadObject(ctx, ReadObjectRequest{ClosureID: result.Closure.ClosureID, ObjectOID: commit.blob, ExpectedType: "blob", MaxBytes: 4096})
	if err != nil || !reflect.DeepEqual(read.Bytes, []byte{0, 4, 8, 12, 0, 255}) {
		t.Fatalf("lookup after source removal = %#v, %v", read, err)
	}
}

func TestCommandGitVaultPathDirectoryAndDiagnosticSafety(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "vault-root")
	git, err := newCommandGit(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", " ../escape", "../escape", "repositories/../escape", "repositories//escape", "/absolute", `repositories\\escape`} {
		if _, err := git.VaultPath(invalid); err == nil {
			t.Fatalf("unsafe vault path %q was accepted", invalid)
		}
	}
	path, err := git.VaultPath("repositories/vault-safe.git")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.EnsureVault(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := git.EnsureVault(ctx, path); err != nil {
		t.Fatalf("valid existing bare vault was not reusable: %v", err)
	}
	if value, err := runGit(ctx, workflowstore.SourceVaultFailureVaultGitStartFailed, path, true, "rev-parse", "--is-bare-repository"); err != nil || strings.TrimSpace(value) != "true" {
		t.Fatalf("vault is not bare: %q %v", value, err)
	}

	nonBare := filepath.Join(root, "repositories", "not-bare.git")
	if err := os.MkdirAll(nonBare, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := git.ValidateVault(ctx, nonBare); FailureReason(typedFailure(ctx, err, failureReason(ctx, err, ""))) != workflowstore.SourceVaultFailureVaultInvalid {
		t.Fatalf("non-bare vault error = %v", err)
	}

	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err == nil {
		if _, err := git.VaultPath("link/vault.git"); err == nil {
			t.Fatal("symlinked vault path was accepted")
		}
	}

	buffer := newLimitedBuffer(4)
	if n, err := buffer.Write([]byte("123456789")); err != nil || n != 9 || buffer.String() != "1234" {
		t.Fatalf("bounded diagnostic buffer = %q n=%d err=%v", buffer.String(), n, err)
	}
}

func TestManagerFailureReasonsPersistAtOwningStage(t *testing.T) {
	indexed := typedFailure(context.Background(), &gitFailure{reason: workflowstore.SourceVaultFailurePackIndexFailed, code: CodeVaultUnavailable}, workflowstore.SourceVaultFailurePackIndexFailed)
	if FailureReason(indexed) != workflowstore.SourceVaultFailurePackIndexFailed || ErrorCode(indexed) != CodeVaultUnavailable {
		t.Fail()
	}
	generated := typedFailure(context.Background(), &gitFailure{reason: workflowstore.SourceVaultFailurePackGenerationFailed, code: CodeSourceObjectUnavailable}, workflowstore.SourceVaultFailurePackGenerationFailed)
	if FailureReason(generated) != workflowstore.SourceVaultFailurePackGenerationFailed || ErrorCode(generated) != CodeSourceObjectUnavailable {
		t.Fail()
	}
}

func TestOpenRepairsInterruptedImportWithoutSecondRestart(t *testing.T) {
	for _, matchingRef := range []bool{false, true} {
		name := "no ref"
		if matchingRef {
			name = "matching ref"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := newGitRepository(t)
			commit := commitFile(t, repo, "file.txt", []byte("one\n"), "one")
			store := openSourceVaultTestStore(t)
			registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
			root := filepath.Join(t.TempDir(), "vaults")
			vaultID := workflowstore.NewSourceVaultID()
			closureID := workflowstore.NewSourceVaultClosureID()
			relativePath := filepath.ToSlash(filepath.Join("repositories", vaultID+".git"))
			refName := "refs/relay/closures/" + closureID
			if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{VaultID: vaultID, RepoTarget: "relay", RelativePath: relativePath})
				if err != nil {
					return err
				}
				_, err = tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
					VaultRowID: vault.ID, ClosureID: closureID, CommitOID: commit.commit, TreeOID: commit.tree,
					RefName: refName, StartedAt: canonicalTime(testTime()),
				})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			vaultPath := filepath.Join(root, filepath.FromSlash(relativePath))
			if err := os.MkdirAll(filepath.Dir(vaultPath), 0o755); err != nil {
				t.Fatal(err)
			}
			runStandaloneGit(t, "init", "--bare", vaultPath)
			if matchingRef {
				runStandaloneGit(t, "--git-dir", vaultPath, "fetch", repo, commit.commit)
				runStandaloneGit(t, "--git-dir", vaultPath, "update-ref", refName, commit.commit)
			}

			manager, err := Open(ctx, root, store)
			if err != nil || manager == nil {
				t.Fatalf("first startup after safe repair = %v manager=%v", err, manager)
			}
			closure, err := store.GetSourceVaultClosureByClosureID(ctx, closureID)
			if err != nil {
				t.Fatal(err)
			}
			if closure.State != workflowstore.SourceVaultClosureStateUnavailable || !closure.FailureReason.Valid || closure.FailureReason.String != workflowstore.SourceVaultFailureInterruptedImport {
				t.Fatalf("repaired closure = %#v", closure)
			}
			cmd := exec.Command("git", "--git-dir", vaultPath, "show-ref", "--verify", "--quiet", refName)
			if err := cmd.Run(); err == nil {
				t.Fatal("safe repair left the generation ref")
			}
		})
	}
}

func TestManagerErrorsAreTypedAndSanitized(t *testing.T) {
	ctx := context.Background()
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", "/source", "refs/heads/main")
	git := newFakeGit()
	secretDiagnostic := "https://token@example.test/private credential=super-secret /private/source"
	git.failStage = "verify_source"
	git.failErr = &gitFailure{
		reason: workflowstore.SourceVaultFailureSourceGitStartFailed,
		code:   CodeSourceObjectUnavailable,
		err:    errors.New(secretDiagnostic),
	}
	manager, err := newManager(store, git)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), strings.Repeat("a", 40), strings.Repeat("b", 40))})
	if ErrorCode(err) != CodeSourceObjectUnavailable || FailureReason(err) != workflowstore.SourceVaultFailureSourceGitStartFailed {
		t.Fatalf("typed Git error = %v code=%q reason=%q", err, ErrorCode(err), FailureReason(err))
	}
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "/private") {
		t.Fatalf("manager error leaked diagnostic: %q", err.Error())
	}

	databaseStore := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, databaseStore, "relay", "/source", "refs/heads/main")
	target := storeTarget(t, ctx, databaseStore, "relay")
	databaseManager, err := newManager(databaseStore, newFakeGit())
	if err != nil {
		t.Fatal(err)
	}
	if err := databaseStore.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = databaseManager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(target, strings.Repeat("a", 40), strings.Repeat("b", 40))})
	if ErrorCode(err) != CodeDatabaseFailure {
		t.Fatalf("database error = %v code=%q", err, ErrorCode(err))
	}
	if strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "sql") {
		t.Fatalf("database error leaked implementation text: %q", err.Error())
	}
}

func runStandaloneGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = controlledGitEnvironment()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

type gitCommitFixture struct {
	commit string
	tree   string
	blob   string
}

type repositorySnapshot struct {
	head       string
	status     string
	refs       string
	config     string
	alternates bool
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.name", "Relay Tests")
	runTestGit(t, repo, "config", "user.email", "relay@example.test")
	runTestGit(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
	return repo
}

func commitFile(t *testing.T, repo, path string, data []byte, message string) gitCommitFixture {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", "--", path)
	runTestGit(t, repo, "commit", "-m", message)
	return gitCommitFixture{
		commit: runTestGit(t, repo, "rev-parse", "HEAD"),
		tree:   runTestGit(t, repo, "rev-parse", "HEAD^{tree}"),
		blob:   runTestGit(t, repo, "rev-parse", "HEAD:"+path),
	}
}

func removeLooseGitObject(t *testing.T, repo, oid string) {
	t.Helper()
	path := filepath.Join(repo, ".git", "objects", oid[:2], oid[2:])
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove loose Git object %s: %v", oid, err)
	}
}

func gitObjectExists(repo, oid string) bool {
	cmd := exec.Command("git", "-C", repo, "cat-file", "-e", oid+"^{object}")
	cmd.Env = controlledGitEnvironment()
	return cmd.Run() == nil
}

func snapshotSourceRepository(t *testing.T, repo string) repositorySnapshot {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, ".git", "objects", "info", "alternates"))
	return repositorySnapshot{
		head:       runTestGit(t, repo, "rev-parse", "HEAD"),
		status:     runTestGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"),
		refs:       runTestGit(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"),
		config:     sortedLines(runTestGit(t, repo, "config", "--local", "--list")),
		alternates: err == nil,
	}
}

func sortedLines(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func runTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", commandArgs...)
	cmd.Env = controlledGitEnvironment()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(commandArgs, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func openSourceVaultTestStoreAt(t *testing.T, dbPath, artifactRoot string) *workflowstore.Store {
	t.Helper()
	store, err := workflowstore.Open(dbPath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func openSourceVaultTestStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	root := t.TempDir()
	return openSourceVaultTestStoreAt(t, filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
}

func openSourceVaultManager(t *testing.T, ctx context.Context, store *workflowstore.Store) *Manager {
	t.Helper()
	manager, err := Open(ctx, filepath.Join(t.TempDir(), "vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func registerSourceVaultRepository(t *testing.T, ctx context.Context, store *workflowstore.Store, repoTarget, localPath, ref string) {
	t.Helper()
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget:          repoTarget,
			LocalPath:           localPath,
			ConfiguredBranchRef: sql.NullString{String: ref, Valid: ref != ""},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func storeTarget(t *testing.T, ctx context.Context, store *workflowstore.Store, repoTarget string) workflowstore.RepositoryTarget {
	t.Helper()
	target, err := store.GetRepositoryTarget(ctx, repoTarget)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func configuredRevision(target workflowstore.RepositoryTarget, commitOID, treeOID string) workflowrepos.ResolvedRevision {
	return workflowrepos.ResolvedRevision{
		RepositoryTarget:                     target,
		RevisionSource:                       workflowrepos.RevisionSourceConfiguredWorkingBranch,
		ConfiguredWorkingBranchRef:           target.ConfiguredBranchRef.String,
		RepositoryTargetConfigurationVersion: target.ConfigurationVersion,
		CommitOID:                            commitOID,
		TreeOID:                              treeOID,
	}
}

func explicitRevision(target workflowstore.RepositoryTarget, commitOID, treeOID string) workflowrepos.ResolvedRevision {
	return workflowrepos.ResolvedRevision{
		RepositoryTarget:                     target,
		RevisionSource:                       workflowrepos.RevisionSourceExplicitCommit,
		RepositoryTargetConfigurationVersion: target.ConfigurationVersion,
		CommitOID:                            commitOID,
		TreeOID:                              treeOID,
	}
}

func assertTableCount(t *testing.T, store *workflowstore.Store, table string, want int64) {
	t.Helper()
	var got int64
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("table %s count = %d, want %d", table, got, want)
	}
}

func seedSourceVaultClosureState(t *testing.T, ctx context.Context, store *workflowstore.Store, state string, activeOwner bool) workflowstore.SourceVaultClosure {
	t.Helper()
	var closure workflowstore.SourceVaultClosure
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{VaultID: workflowstore.NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/relay.git"})
		if err != nil {
			return err
		}
		closureID := workflowstore.NewSourceVaultClosureID()
		acquired, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40),
			RefName: "refs/relay/closures/" + closureID, StartedAt: canonicalTime(testTime()),
		})
		if err != nil {
			return err
		}
		closure = acquired.Closure
		switch state {
		case workflowstore.SourceVaultClosureStateImporting:
			return nil
		case workflowstore.SourceVaultClosureStateReady:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: canonicalTime(testTime())})
		case workflowstore.SourceVaultClosureStateUnavailable:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateUnavailable, FailureReason: sql.NullString{String: workflowstore.SourceVaultFailureInterruptedImport, Valid: true}, TransitionAt: canonicalTime(testTime())})
		case workflowstore.SourceVaultClosureStateReleasing, workflowstore.SourceVaultClosureStateReleased:
			closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting, NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: canonicalTime(testTime())})
			if err == nil {
				closure, err = tx.BeginSourceVaultClosureRelease(ctx, closure.ClosureID, canonicalTime(testTime()))
			}
			if err == nil && state == workflowstore.SourceVaultClosureStateReleased {
				closure, err = tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{ClosureID: closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateReleasing, NextState: workflowstore.SourceVaultClosureStateReleased, TransitionAt: canonicalTime(testTime())})
			}
		default:
			return fmt.Errorf("unknown state %q", state)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if activeOwner {
		if _, err := store.DB().Exec(`
INSERT INTO source_vault_retentions (retention_id, closure_row_id, owner_class, owner_identity, state)
VALUES (?, ?, 'artifact', 'late-owner', 'active')`, workflowstore.NewSourceVaultRetentionID(), closure.ID); err != nil {
			t.Fatal(err)
		}
	}
	return closure
}

func testTime() time.Time {
	return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
}

type fakeGit struct {
	mu        sync.Mutex
	refs      map[string]string
	objects   map[string][]byte
	trees     map[string]string
	failStage string
	failErr   error
	cancelAt  string
	cancel    context.CancelFunc
	calls     []string
}

func newFakeGit() *fakeGit {
	return &fakeGit{
		refs: map[string]string{},
		objects: map[string][]byte{
			strings.Repeat("a", 40): []byte("commit\n"),
			strings.Repeat("b", 40): []byte("tree\n"),
		},
		trees: map[string]string{
			strings.Repeat("a", 40): strings.Repeat("b", 40),
		},
	}
}

func (g *fakeGit) call(stage string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, stage)
	if g.cancelAt == stage && g.cancel != nil {
		g.cancel()
	}
	if g.failStage == stage {
		return g.failErr
	}
	return nil
}

func (g *fakeGit) ValidateRepositorySeparation(context.Context, string) (bool, error) {
	if err := g.call("repository_separation"); err != nil {
		return false, err
	}
	return true, nil
}

func (g *fakeGit) VaultPath(relativePath string) (string, error) {
	if err := g.call("vault_path"); err != nil {
		return "", err
	}
	return "/vault/" + relativePath, nil
}
func (g *fakeGit) EnsureVault(context.Context, string) error   { return g.call("ensure") }
func (g *fakeGit) ValidateVault(context.Context, string) error { return g.call("validate") }
func (g *fakeGit) VerifySource(ctx context.Context, _ string, _ string, _ string) error {
	if err := g.call("verify_source"); err != nil {
		return err
	}
	return ctx.Err()
}
func (g *fakeGit) ImportClosure(context.Context, string, string, string) error {
	return g.call("import")
}
func (g *fakeGit) VerifyVaultClosure(_ context.Context, _ string, commitOID, treeOID, refName string) error {
	if err := g.call("verify_vault"); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if value, ok := g.refs[refName]; !ok {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMissing, code: CodeVaultUnavailable}
	} else if value != commitOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
	}
	if expectedTree, ok := g.trees[commitOID]; !ok || expectedTree != treeOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureVaultTreeMismatch, code: CodeObjectMismatch}
	}
	return nil
}
func (g *fakeGit) ReadRef(ctx context.Context, _ string, refName string) (string, bool, error) {
	if err := g.call("read_ref"); err != nil {
		return "", false, err
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	oid, ok := g.refs[refName]
	return oid, ok, nil
}
func (g *fakeGit) CreateRef(_ context.Context, _ string, refName, commitOID string) error {
	if err := g.call("create_ref"); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if prior, exists := g.refs[refName]; exists && prior != commitOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
	}
	g.refs[refName] = commitOID
	return nil
}
func (g *fakeGit) DeleteRef(_ context.Context, _ string, refName, commitOID string) error {
	if err := g.call("delete_ref"); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if prior, exists := g.refs[refName]; exists && prior != commitOID {
		return &gitFailure{reason: workflowstore.SourceVaultFailureRefMismatch, code: CodeObjectMismatch}
	}
	delete(g.refs, refName)
	return nil
}
func (g *fakeGit) ReadObject(_ context.Context, _ string, oid, _ string, maxBytes int64) ([]byte, error) {
	if err := g.call("read_object"); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.objects[oid]
	if !ok {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	if int64(len(value)) > maxBytes {
		return nil, &Error{Code: CodeObjectLimitExceeded}
	}
	return append([]byte(nil), value...), nil
}

func (g *fakeGit) ReadTree(_ context.Context, _ string, treeOID string) ([]RetainedTreeEntry, error) {
	if err := g.call("read_tree"); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.objects[treeOID]
	if !ok {
		return nil, &Error{Code: CodeObjectUnavailable}
	}
	return parseRawTree(strings.NewReader(string(value)))
}

func (g *fakeGit) ReadBlobRange(_ context.Context, _ string, blobOID string, offset, limit int64) (ReadRetainedBlobRangeResult, error) {
	if err := g.call("read_blob_range"); err != nil {
		return ReadRetainedBlobRangeResult{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.objects[blobOID]
	if !ok {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeObjectUnavailable}
	}
	if offset < 0 || offset > int64(len(value)) || limit <= 0 {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	end := offset + limit
	if end < offset || end > int64(len(value)) {
		end = int64(len(value))
	}
	return ReadRetainedBlobRangeResult{BlobOID: blobOID, Offset: offset, TotalSize: int64(len(value)), Bytes: append([]byte(nil), value[offset:end]...)}, nil
}

func (g *fakeGit) GarbageCollect(context.Context, string) error { return g.call("gc") }
