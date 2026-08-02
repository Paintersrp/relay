package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/fsatomic"
	"relay/internal/sourceindex/indexerprotocol"
)

func restoreBuildSeams(t *testing.T) {
	t.Helper()
	a, b, c, d := repositoryCheck, makePrivateDirectory, traverseTree, classifyContent
	e, f, g, h := newCoverageManifest, buildShards, listArtifactFiles, newArtifactManifest
	i, j, k, l := newGenerationManifest, writeArtifact, verifyStaged, syncPreparedBuild
	m, n, o := renamePrivateBuild, syncStagingParent, cleanupAttempt
	t.Cleanup(func() {
		repositoryCheck, makePrivateDirectory, traverseTree, classifyContent = a, b, c, d
		newCoverageManifest, buildShards, listArtifactFiles, newArtifactManifest = e, f, g, h
		newGenerationManifest, writeArtifact, verifyStaged, syncPreparedBuild = i, j, k, l
		renamePrivateBuild, syncStagingParent, cleanupAttempt = m, n, o
	})
}

func TestBuildFailureSeamsCleanExactAttempt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact descriptor-rooted cleanup is Linux-only")
	}
	for _, name := range []string{"repository", "private directory", "traversal", "content", "coverage", "shards", "coverage write", "artifact write", "generation write", "verification", "sync", "rename"} {
		t.Run(name, func(t *testing.T) {
			fixture := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
			r := fixture.request
			otherNonce, otherID := strings.Repeat("b", 32), strings.Repeat("c", 64)
			other := filepath.Join(r.IndexRoot, "staging", r.GenerationID+"-"+otherNonce)
			otherGeneration := filepath.Join(r.IndexRoot, "staging", otherID+"-"+otherNonce)
			if err := os.MkdirAll(other, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(otherGeneration, 0700); err != nil {
				t.Fatal(err)
			}
			restoreBuildSeams(t)
			sentinel := errors.New(name)
			switch name {
			case "repository":
				repositoryCheck = func(context.Context, indexerprotocol.BuildRequest) error { return sentinel }
			case "private directory":
				makePrivateDirectory = func(string, os.FileMode) error { return sentinel }
			case "traversal":
				traverseTree = func(context.Context, indexerprotocol.BuildRequest) ([]treeEntry, error) { return nil, sentinel }
			case "content":
				classifyContent = func(context.Context, indexerprotocol.BuildRequest, []treeEntry) ([]sourceindex.CoverageEntry, []document, error) {
					return nil, nil, sentinel
				}
			case "coverage":
				newCoverageManifest = func(string, string, string, []sourceindex.CoverageEntry) (sourceindex.CoverageManifest, error) {
					return sourceindex.CoverageManifest{}, sentinel
				}
			case "shards":
				buildShards = func(string, indexerprotocol.BuildRequest, []document, int64) (int64, error) { return 0, sentinel }
			case "coverage write", "artifact write", "generation write":
				writeArtifact = func(path string, data []byte) error {
					if strings.Contains(path, strings.Split(name, " ")[0]) {
						return sentinel
					}
					return write(path, data)
				}
			case "verification":
				verifyStaged = func(string, indexerprotocol.BuildRequest, int64) error { return sentinel }
			case "sync":
				syncPreparedBuild = func(string, string) error { return sentinel }
			case "rename":
				renamePrivateBuild = func(string, string) error { return sentinel }
			}
			_, err := Build(context.Background(), r)
			if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), name) {
				t.Fatalf("failure does not identify %q: %v", name, err)
			}
			for _, path := range []string{filepath.Join(r.IndexRoot, "staging", r.GenerationID+"-"+r.StagingNonce), filepath.Join(r.IndexRoot, "staging", ".relay-build-"+r.GenerationID+"-"+r.StagingNonce)} {
				if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("exact attempt remains %s: %v", path, statErr)
				}
			}
			for _, path := range []string{other, otherGeneration} {
				if _, statErr := os.Stat(path); statErr != nil {
					t.Fatalf("unrelated attempt changed: %v", statErr)
				}
			}
		})
	}
}

func TestBuildPreexistingPrivateAndPostExposureFailures(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact descriptor-rooted cleanup is Linux-only")
	}
	t.Run("pre-existing private is preserved", func(t *testing.T) {
		f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
		private, _ := sourceindex.PrivateBuildDirectory(f.request.IndexRoot, f.request.GenerationID, f.request.StagingNonce)
		if err := os.MkdirAll(private, 0700); err != nil {
			t.Fatal(err)
		}
		unknown := filepath.Join(private, "unknown")
		if err := os.WriteFile(unknown, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(context.Background(), f.request); err == nil {
			t.Fatal("reused pre-existing private attempt")
		}
		if _, err := os.Stat(unknown); err != nil {
			t.Fatalf("pre-existing unknown content was removed: %v", err)
		}
	})
	t.Run("post-rename parent sync", func(t *testing.T) {
		f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
		restoreBuildSeams(t)
		sentinel := errors.New("parent sync")
		syncStagingParent = func(string) error { return sentinel }
		if _, err := Build(context.Background(), f.request); !errors.Is(err, sentinel) {
			t.Fatalf("post-exposure failure = %v", err)
		}
		canonical := filepath.Join(f.request.IndexRoot, "staging", f.request.GenerationID+"-"+f.request.StagingNonce)
		private, _ := sourceindex.PrivateBuildDirectory(f.request.IndexRoot, f.request.GenerationID, f.request.StagingNonce)
		if _, err := os.Stat(canonical); err != nil {
			t.Fatalf("canonical attempt was removed: %v", err)
		}
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("private path remains: %v", err)
		}
		if err := fsatomic.RemoveOwnedGenerationAttempt(f.request.IndexRoot, f.request.GenerationID, f.request.StagingNonce); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBuildCancellationAndCleanupFailureRemainObservable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exact descriptor-rooted cleanup is Linux-only")
	}
	t.Run("cancellation before exposure", func(t *testing.T) {
		f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
		restoreBuildSeams(t)
		ctx, cancel := context.WithCancel(context.Background())
		verifyStaged = func(string, indexerprotocol.BuildRequest, int64) error { cancel(); return nil }
		if _, err := Build(ctx, f.request); err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("cancellation failure = %v", err)
		}
		private, _ := sourceindex.PrivateBuildDirectory(f.request.IndexRoot, f.request.GenerationID, f.request.StagingNonce)
		if _, err := os.Lstat(private); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancelled private attempt remains: %v", err)
		}
	})
	t.Run("cleanup failure joins original failure", func(t *testing.T) {
		f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
		restoreBuildSeams(t)
		buildFailure, cleanupFailure := errors.New("traversal failure"), errors.New("cleanup failure")
		traverseTree = func(context.Context, indexerprotocol.BuildRequest) ([]treeEntry, error) { return nil, buildFailure }
		cleanupAttempt = func(string, string, string) error { return cleanupFailure }
		if _, err := Build(context.Background(), f.request); !errors.Is(err, buildFailure) || !errors.Is(err, cleanupFailure) {
			t.Fatalf("joined failures = %v", err)
		}
		private, _ := sourceindex.PrivateBuildDirectory(f.request.IndexRoot, f.request.GenerationID, f.request.StagingNonce)
		if _, err := os.Stat(private); err != nil {
			t.Fatalf("unsafe content was unexpectedly removed: %v", err)
		}
	})
}
