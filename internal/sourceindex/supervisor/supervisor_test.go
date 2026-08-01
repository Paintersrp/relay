package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
