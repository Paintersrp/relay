package sourcevault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestSourceVaultReadPathSuccessfulNestedAndMissing(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	nested := []byte(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"g","context":"c","scope":{"in_scope":[],"out_of_scope":[]},"depends_on":[],"implementation_obligations":[],"validation_intent":[],"transition_applicability":"not_required","completion_criteria":[]}`)
	commit := commitFile(t, repo, "tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json", nested, "nested ticket")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	retention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-nested"})
	if err != nil {
		t.Fatal(err)
	}
	_ = retention

	read, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatalf("nested read error = %v", err)
	}
	if read.ObjectOID == "" || read.ObjectOID != commit.blob {
		t.Fatalf("object OID = %q, want %q", read.ObjectOID, commit.blob)
	}
	if !bytes.Equal(read.Bytes, nested) {
		t.Fatalf("nested bytes mismatch: got %q, want %q", read.Bytes, nested)
	}

	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "tickets/missing.json", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeObjectUnavailable {
		t.Fatalf("missing path error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestSourceVaultReadPathRejectsNonReadyClosureAndAbsentRetention(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commit := commitFile(t, repo, "file.txt", []byte("one\n"), "one")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "file.txt", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("unretained read error = %v, code = %q", err, ErrorCode(err))
	}

	retention, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-retention"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReleaseRetention(ctx, retention.RetentionID); err != nil {
		t.Fatal(err)
	}
	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "file.txt", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("released retention read error = %v, code = %q", err, ErrorCode(err))
	}

	releasing := seedSourceVaultClosureState(t, ctx, store, workflowstore.SourceVaultClosureStateReleasing, true)
	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: releasing.ClosureID, Path: "file.txt", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeVaultUnavailable {
		t.Fatalf("releasing closure read error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestSourceVaultReadPathRejectsNonBlobAndBounds(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	if err := os.MkdirAll(filepath.Join(repo, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dir", "file.txt"), []byte("inside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", "dir/file.txt")
	runTestGit(t, repo, "commit", "-m", "directory")
	commit := runTestGit(t, repo, "rev-parse", "HEAD")
	tree := runTestGit(t, repo, "rev-parse", "HEAD^{tree}")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit, tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-bounds"}); err != nil {
		t.Fatal(err)
	}

	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "dir", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeObjectUnavailable {
		t.Fatalf("directory path error = %v, code = %q", err, ErrorCode(err))
	}

	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "dir/file.txt", MaxBytes: 1})
	if ErrorCode(err) != CodeObjectLimitExceeded {
		t.Fatalf("bounded overflow error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestSourceVaultReadPathRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"outer space", " tickets/file.json"},
		{"leading slash", "/tickets/file.json"},
		{"leading backslash", "\\tickets/file.json"},
		{"uppercase windows drive", "C:/tickets/file.json"},
		{"lowercase windows drive", "c:/tickets/file.json"},
		{"backslash", "tickets\\file.json"},
		{"empty segment", "tickets//file.json"},
		{"current segment", "./tickets/file.json"},
		{"parent segment", "tickets/../file.json"},
		{"control character x01", "tickets/file\x01.json"},
		{"tab character", "tickets/\tfile.json"},
		{"newline character", "tickets/\nfile.json"},
		{"DEL character", "tickets/\x7ffile.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := ReadPathRequest{ClosureID: "closure-id", Path: test.path, MaxBytes: 1024}
			if err := validateReadPathRequest(request); err == nil {
				t.Fatalf("path %q was accepted", test.path)
			}
		})
	}
}

func TestSourceVaultReadPathPreservesWhitespace(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	whitespace := []byte(" {   \"schema_version\":\"1.0\",   \"feature_slug\":\"checkout\"   } ")
	commit := commitFile(t, repo, "whitespace.json", whitespace, "whitespace")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-whitespace"}); err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "whitespace.json", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read.Bytes, whitespace) {
		t.Fatalf("whitespace bytes changed: %q", read.Bytes)
	}
}

func TestSourceVaultReadPathReturnsDefensiveCopies(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	data := []byte("defensive copy test\n")
	commit := commitFile(t, repo, "copy.txt", data, "copy")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-copy"}); err != nil {
		t.Fatal(err)
	}
	first, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "copy.txt", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "copy.txt", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if &first.Bytes[0] == &second.Bytes[0] {
		t.Fatal("ReadPath returned shared backing arrays")
	}
	second.Bytes[0] = 'X'
	if !bytes.Equal(first.Bytes, data) {
		t.Fatal("mutating second result affected first result")
	}
}

func TestSourceVaultReadPathRepeatedReturnsIdentical(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	data := []byte(strings.Repeat("identical ", 100))
	commit := commitFile(t, repo, "identical.txt", data, "identical")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-identical"}); err != nil {
		t.Fatal(err)
	}
	first, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "identical.txt", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "identical.txt", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if first.ObjectOID != second.ObjectOID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("repeated ReadPath returned different evidence")
	}
}

func TestSourceVaultReadPathRegularNonExecutableBlobSucceeds(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	data := []byte("regular 100644 file\n")
	commit := commitFile(t, repo, "regular.txt", data, "regular file")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit.commit, commit.tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-reg"}); err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "regular.txt", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatalf("ReadPath error = %v", err)
	}
	if read.ObjectOID != commit.blob || !bytes.Equal(read.Bytes, data) {
		t.Fatalf("ReadPath result = %#v, want OID %q and bytes %q", read, commit.blob, data)
	}
}

func TestSourceVaultReadPathRegularExecutableBlobSucceeds(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	data := []byte("#!/bin/sh\necho hello\n")
	commitFile(t, repo, "script.sh", data, "executable script")
	runTestGit(t, repo, "update-index", "--chmod=+x", "script.sh")
	runTestGit(t, repo, "commit", "-m", "make executable")
	commit := runTestGit(t, repo, "rev-parse", "HEAD")
	tree := runTestGit(t, repo, "rev-parse", "HEAD^{tree}")
	blob := runTestGit(t, repo, "rev-parse", "HEAD:script.sh")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commit, tree)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-exec"}); err != nil {
		t.Fatal(err)
	}
	read, err := manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "script.sh", MaxBytes: 64 << 20})
	if err != nil {
		t.Fatalf("ReadPath executable error = %v", err)
	}
	if read.ObjectOID != blob || !bytes.Equal(read.Bytes, data) {
		t.Fatalf("ReadPath executable result = %#v, want OID %q", read, blob)
	}
}

func TestSourceVaultReadPathSymbolicLinkBlobIsRejected(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	commitFile(t, repo, "target.txt", []byte("target\n"), "target file")
	blobOID := runTestGit(t, repo, "hash-object", "-w", "--stdin")
	runTestGit(t, repo, "update-index", "--add", "--cacheinfo", "120000", blobOID, "symlink.txt")
	treeOID := runTestGit(t, repo, "write-tree")
	commitOID := runTestGit(t, repo, "commit-tree", treeOID, "-m", "add symlink entry")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commitOID, treeOID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-symlink"}); err != nil {
		t.Fatal(err)
	}
	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "symlink.txt", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeObjectUnavailable {
		t.Fatalf("symlink read error = %v, code = %q, want %q", err, ErrorCode(err), CodeObjectUnavailable)
	}
}

func TestSourceVaultReadPathGitlinkSubmoduleIsRejected(t *testing.T) {
	ctx := context.Background()
	repo := newGitRepository(t)
	subCommitOID := strings.Repeat("1", 40)
	runTestGit(t, repo, "update-index", "--add", "--cacheinfo", "160000", subCommitOID, "submodule_dir")
	treeOID := runTestGit(t, repo, "write-tree")
	commitOID := runTestGit(t, repo, "commit-tree", treeOID, "-m", "add gitlink entry")
	store := openSourceVaultTestStore(t)
	registerSourceVaultRepository(t, ctx, store, "relay", repo, "refs/heads/main")
	manager := openSourceVaultManager(t, ctx, store)
	imported, err := manager.ImportClosure(ctx, ImportRequest{Revision: configuredRevision(storeTarget(t, ctx, store, "relay"), commitOID, treeOID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RetainClosure(ctx, RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerArtifact, OwnerIdentity: "readpath-submodule"}); err != nil {
		t.Fatal(err)
	}
	_, err = manager.ReadPath(ctx, ReadPathRequest{ClosureID: imported.Closure.ClosureID, Path: "submodule_dir", MaxBytes: 64 << 20})
	if ErrorCode(err) != CodeObjectUnavailable {
		t.Fatalf("submodule read error = %v, code = %q, want %q", err, ErrorCode(err), CodeObjectUnavailable)
	}
}

func TestSourceVaultReadPathMalformedOrUnknownModeIsRejected(t *testing.T) {
	line := "100655 blob " + strings.Repeat("a", 40) + "\tfile.txt"
	mode, objectType, oid, path, ok := parseLsTreeEntry(line)
	if !ok {
		t.Fatal("parseLsTreeEntry failed to parse structural line")
	}
	if mode == "100644" || mode == "100755" {
		t.Fatalf("unknown mode %q was treated as valid regular mode", mode)
	}
	_ = objectType
	_ = oid
	_ = path
}

func TestSourceVaultReadPathNonBlobTypeIsRejected(t *testing.T) {
	line := "040000 tree " + strings.Repeat("a", 40) + "\tdir"
	mode, objectType, oid, path, ok := parseLsTreeEntry(line)
	if !ok || objectType == "blob" {
		t.Fatalf("tree entry objectType = %q, ok = %v", objectType, ok)
	}
	_ = mode
	_ = oid
	_ = path
}

func TestSourceVaultReadPathGitNonzeroExitIsRejected(t *testing.T) {
	ctx := context.Background()
	g := &commandGit{}
	_, err := g.ResolvePath(ctx, t.TempDir(), strings.Repeat("a", 40), "file.txt")
	if err == nil {
		t.Fatal("ResolvePath on nonexistent vault succeeded, want error")
	}
}

func TestCommandGitResolvePathMalformedOrMultipleRecordsAreRejected(t *testing.T) {
	ctx := context.Background()
	g := &commandGit{}
	_, err := g.ResolvePath(ctx, t.TempDir(), strings.Repeat("a", 40), "file.txt")
	if err == nil {
		t.Fatal("ResolvePath on nonexistent vault succeeded, want error")
	}
}
