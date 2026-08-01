package indexer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexerprotocol"
)

// stagedGeneration writes a minimal path-based generation: the three
// manifests and one shards directory with one shard file.
func stagedGeneration(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, sourceindex.ShardDirectoryName), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sourceindex.GenerationManifestFileName, sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName, filepath.Join(sourceindex.ShardDirectoryName, "000000.zoekt")} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// captureStagedOpened records every artifact descriptor the path enumerator
// opens so tests can prove complete cleanup.
func captureStagedOpened(t *testing.T) *[]*os.File {
	t.Helper()
	var opened []*os.File
	old := openStagedFile
	openStagedFile = func(path string) (*os.File, error) {
		f, err := os.Open(path)
		if err == nil {
			opened = append(opened, f)
		}
		return f, err
	}
	t.Cleanup(func() { openStagedFile = old })
	return &opened
}

// fakeStagedIdentity supplies deterministic identities where the platform
// cannot, so path enumeration can open artifacts on every host.
func fakeStagedIdentity(t *testing.T) {
	t.Helper()
	old := stagedFileIdentity
	var seq uint64
	stagedFileIdentity = func(os.FileInfo) (FileIdentity, error) {
		seq++
		return FileIdentity{Device: 1, Inode: seq}, nil
	}
	t.Cleanup(func() { stagedFileIdentity = old })
}

func assertStagedClosed(t *testing.T, opened []*os.File) {
	t.Helper()
	for _, f := range opened {
		if err := f.Close(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("artifact %s still open: %v", f.Name(), err)
		}
	}
}

func TestPathFilesListArtifactsFailureClosesOpenedFiles(t *testing.T) {
	t.Run("unexpected directory", func(t *testing.T) {
		root := stagedGeneration(t)
		if err := os.Mkdir(filepath.Join(root, "unexpected"), 0700); err != nil {
			t.Fatal(err)
		}
		opened := captureStagedOpened(t)
		fakeStagedIdentity(t)
		if _, err := (pathFiles{root: root}).ListArtifacts(); err == nil {
			t.Fatal("accepted unexpected directory")
		}
		assertStagedClosed(t, *opened)
	})
	t.Run("repeated identity", func(t *testing.T) {
		root := stagedGeneration(t)
		if err := os.WriteFile(filepath.Join(root, "extra"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		opened := captureStagedOpened(t)
		old := stagedFileIdentity
		stagedFileIdentity = func(os.FileInfo) (FileIdentity, error) {
			return FileIdentity{Device: 1, Inode: 7}, nil
		}
		t.Cleanup(func() { stagedFileIdentity = old })
		if _, err := (pathFiles{root: root}).ListArtifacts(); err == nil {
			t.Fatal("accepted repeated artifact identity")
		}
		assertStagedClosed(t, *opened)
	})
}

func TestVerifyClosesArtifactsOnEveryExit(t *testing.T) {
	root := stagedGeneration(t)
	opened := captureStagedOpened(t)
	fakeStagedIdentity(t)
	old := verifyBound
	verifyBound = func(files VerifiedGenerationFiles, r indexerprotocol.BuildRequest) (*VerifiedGeneration, error) {
		artifacts, err := files.ListArtifacts()
		if err != nil {
			return nil, err
		}
		var shardCount int64
		for _, o := range artifacts {
			if o.Kind == sourceindex.ArtifactZoektShard {
				shardCount++
			}
		}
		return &VerifiedGeneration{Opened: artifacts, ShardCount: shardCount}, nil
	}
	t.Cleanup(func() { verifyBound = old })
	t.Run("shard count mismatch closes every verified artifact", func(t *testing.T) {
		if err := Verify(root, indexerprotocol.BuildRequest{}, 0); err == nil {
			t.Fatal("accepted shard count mismatch")
		}
		assertStagedClosed(t, *opened)
	})
	t.Run("successful verification closes each artifact exactly once", func(t *testing.T) {
		if err := Verify(root, indexerprotocol.BuildRequest{}, 1); err != nil {
			t.Fatalf("verification failed: %v", err)
		}
		assertStagedClosed(t, *opened)
		for _, f := range *opened {
			if err := f.Close(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("artifact %s was not closed exactly once: %v", f.Name(), err)
			}
		}
	})
}
