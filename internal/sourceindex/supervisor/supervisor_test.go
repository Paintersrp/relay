package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	workflow "relay/internal/store/workflow"
)

func TestNewValidatesConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	indexer, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	authority := &testAuthority{}
	if _, err := New(store, authority, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); err != nil {
		t.Fatalf("New valid configuration: %v", err)
	}
	for _, config := range []Config{
		{IndexRoot: "relative", IndexerPath: indexer},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: "relative"},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: filepath.Join(root, "missing")},
		{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: root},
		{IndexRoot: filepath.Join(root, "source"), IndexerPath: indexer, ProtectedStorage: sourceindex.ProtectedStorage{SourceVaultRoot: root}},
	} {
		if _, err := New(store, authority, config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Errorf("New(%+v) error = %v, want invalid configuration", config, err)
		}
	}
	if _, err := New(nil, authority, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("nil store error = %v", err)
	}
	if _, err := New(store, nil, Config{IndexRoot: filepath.Join(root, "indexes"), IndexerPath: indexer}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Errorf("nil authority error = %v", err)
	}
}

func TestCleanupRemovesOnlyExactOwnedStaging(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("anchored staging cleanup is supported on Linux")
	}
	root := t.TempDir()
	generationID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	nonce := "0123456789abcdef0123456789abcdef"
	owned := filepath.Join(root, sourceindex.StagingDirectoryName, generationID+"-"+nonce)
	other := filepath.Join(root, sourceindex.StagingDirectoryName, generationID+"-"+"ffffffffffffffffffffffffffffffff")
	if err := os.MkdirAll(owned, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{config: Config{IndexRoot: root}}
	if err := s.cleanup(generationID, nonce); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Lstat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned staging remains: %v", err)
	}
	if info, err := os.Lstat(other); err != nil || !info.IsDir() {
		t.Fatalf("other staging was changed: %v", err)
	}
}

func TestCleanupRejectsSymlinkStagingParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("anchored staging cleanup is supported on Linux")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, sourceindex.StagingDirectoryName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	s := &Supervisor{config: Config{IndexRoot: root}}
	if err := s.cleanup("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("cleanup accepted a symlink staging parent")
	}
}

func TestSafeFailureMessageIsSupervisorOwned(t *testing.T) {
	for _, code := range []string{
		"invalid_request", "unsafe_path", "source_unavailable", "source_mismatch",
		"tree_invalid", "object_invalid", "content_read_failed", "index_build_failed",
		"artifact_write_failed", "verification_failed", "cancelled", "internal",
	} {
		message := safeFailureMessage(code)
		if message == "" || message == code {
			t.Fatalf("safeFailureMessage(%q) = %q", code, message)
		}
	}
	malicious := "/private/repository TOKEN=secret\nstack trace\ngit fetch origin"
	if got := safeFailureMessage(malicious); got == malicious || got != "source-index worker reported build failure" {
		t.Fatalf("untrusted child text reached persistence message: %q", got)
	}
}

type testStore struct{}
type testAuthority struct{}

func (*testStore) GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) MarkSourceIndexGenerationReady(context.Context, workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testStore) MarkSourceIndexGenerationFailed(context.Context, workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected call")
}
func (*testAuthority) AcquireSourceIndexLease(context.Context, sourceindex.GenerationIdentity) (SourceLease, error) {
	return nil, errors.New("unexpected call")
}

type finalizationStore struct {
	marks  int
	params workflow.MarkSourceIndexGenerationFailedParams
	ctx    context.Context
}

func (s *finalizationStore) GetSourceIndexGeneration(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) BeginSourceIndexGenerationBuild(context.Context, string) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) MarkSourceIndexGenerationReady(context.Context, workflow.MarkSourceIndexGenerationReadyParams) (workflow.SourceIndexGeneration, error) {
	return workflow.SourceIndexGeneration{}, errors.New("unexpected")
}
func (s *finalizationStore) MarkSourceIndexGenerationFailed(ctx context.Context, params workflow.MarkSourceIndexGenerationFailedParams) (workflow.SourceIndexGeneration, error) {
	s.marks++
	s.params, s.ctx = params, ctx
	return workflow.SourceIndexGeneration{}, nil
}

type finalizationLease struct{ closes int }

func (*finalizationLease) RepositoryPath() string { return "/retained/repository" }
func (l *finalizationLease) Close() error         { l.closes++; return nil }

func TestFailFinalizesAfterCallerCancellationAndCleansExactAttempt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-rooted cleanup is Linux-only")
	}
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	canonical, err := sourceindex.StagingDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	private, err := sourceindex.PrivateBuildDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{canonical, private} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store := &finalizationStore{}
	lease := &finalizationLease{}
	s := &Supervisor{store: store, config: Config{IndexRoot: root}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.fail(ctx, id, "cancelled", "ignored", lease, nonce, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("fail error = %v", err)
	}
	if store.marks != 1 || store.params.FailureCode != "cancelled" || store.ctx.Err() != nil {
		t.Fatalf("finalization = %#v marks=%d context=%v", store.params, store.marks, store.ctx.Err())
	}
	if lease.closes != 1 {
		t.Fatalf("lease closes = %d, want 1", lease.closes)
	}
	for _, path := range []string{canonical, private} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned attempt remains at %s: %v", path, err)
		}
	}
}

func TestFailPreservesUnsafeAttemptWhenCleanupFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor-rooted cleanup is Linux-only")
	}
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	canonical, err := sourceindex.StagingDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(canonical, 0700); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(canonical, "unknown")
	if err := os.WriteFile(unsafe, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &finalizationStore{}
	lease := &finalizationLease{}
	s := &Supervisor{store: store, config: Config{IndexRoot: root}}
	if _, err := s.fail(context.Background(), id, "internal", "ignored", lease, nonce, errors.New("child failed")); !errors.Is(err, ErrFailureFinalization) {
		t.Fatalf("fail error = %v", err)
	}
	if store.marks != 0 || lease.closes != 1 {
		t.Fatalf("marks=%d closes=%d", store.marks, lease.closes)
	}
	if _, err := os.Lstat(unsafe); err != nil {
		t.Fatalf("unsafe content was not preserved: %v", err)
	}
}
