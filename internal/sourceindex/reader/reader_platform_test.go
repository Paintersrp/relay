//go:build linux || darwin || freebsd || netbsd

package reader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/zoektbuild"
	"relay/internal/sourceindex/zoektread"
	workflow "relay/internal/store/workflow"
)

func realMetadata(t *testing.T, identity sourceindex.GenerationIdentity, id string) zoektbuild.Metadata {
	t.Helper()
	repo, err := sourceindex.GenerationRepositoryName(id)
	if err != nil {
		t.Fatal(err)
	}
	return zoektbuild.Metadata{RepositoryName: repo, Branch: sourceindex.GenerationBranchName, Version: identity.CommitOID, IndexOptions: identity.BuildOptionsSHA256, Values: map[string]string{"relay_generation_id": id, "relay_vault_id": identity.VaultID, "relay_commit_oid": identity.CommitOID, "relay_tree_oid": identity.TreeOID, "relay_engine_revision": identity.EngineRevision, "relay_build_contract_version": identity.BuildContractVersion, "relay_build_options_sha256": identity.BuildOptionsSHA256}}
}

// buildRealGeneration writes one complete valid generation to the real
// filesystem using the real Zoekt builder, and returns the exact ready row.
func buildRealGeneration(t *testing.T, root string, identity sourceindex.GenerationIdentity, shards [][]zoektbuild.Document, fallback []string) (workflow.SourceIndexGeneration, string, string) {
	t.Helper()
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
	if err := os.MkdirAll(filepath.Join(genDir, sourceindex.ShardDirectoryName), 0700); err != nil {
		t.Fatal(err)
	}
	meta := realMetadata(t, identity, id)
	var entries []sourceindex.CoverageEntry
	for i, docs := range shards {
		path := filepath.Join(genDir, sourceindex.ShardDirectoryName, fmt.Sprintf("%06d.zoekt", i))
		if err := zoektbuild.Write(path, id, i, meta, docs); err != nil {
			t.Fatal(err)
		}
		for _, doc := range docs {
			p, err := sourceindex.NewPathIdentity([]byte(doc.Name))
			if err != nil {
				t.Fatal(err)
			}
			entries = append(entries, sourceindex.CoverageEntry{Path: p, Mode: "100644", ObjectType: "blob", ObjectOID: testOID, SizeBytes: int64(len(doc.Content)), Status: sourceindex.CoverageIndexedText})
		}
	}
	for _, name := range fallback {
		p, err := sourceindex.NewPathIdentity([]byte(name))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, sourceindex.CoverageEntry{Path: p, Mode: "100644", ObjectType: "blob", ObjectOID: testOID, SizeBytes: 1, Status: sourceindex.CoverageFallbackPath})
	}
	cm, err := sourceindex.NewCoverageManifest(id, identity.CommitOID, identity.TreeOID, entries)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := sourceindex.MarshalCoverageManifest(cm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, sourceindex.CoverageManifestFileName), cb, 0600); err != nil {
		t.Fatal(err)
	}
	var artifacts []sourceindex.ArtifactFile
	for i := range shards {
		rel := fmt.Sprintf("shards/%06d.zoekt", i)
		b, err := os.ReadFile(filepath.Join(genDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, sourceindex.ArtifactFile{Kind: sourceindex.ArtifactZoektShard, RelativePath: rel, SHA256: sha256sum(b), SizeBytes: int64(len(b))})
	}
	artifacts = append(artifacts, sourceindex.ArtifactFile{Kind: sourceindex.ArtifactCoverage, RelativePath: sourceindex.CoverageManifestFileName, SHA256: sha256sum(cb), SizeBytes: int64(len(cb))})
	am, err := sourceindex.NewArtifactManifest(id, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	ab, err := sourceindex.MarshalArtifactManifest(am)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), ab, 0600); err != nil {
		t.Fatal(err)
	}
	gm, err := sourceindex.NewGenerationManifest(identity, sha256sum(cb), sha256sum(ab))
	if err != nil {
		t.Fatal(err)
	}
	gb, err := sourceindex.MarshalGenerationManifest(gm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb, 0600); err != nil {
		t.Fatal(err)
	}
	row := workflow.SourceIndexGeneration{
		GenerationID:             id,
		Identity:                 identity,
		State:                    workflow.SourceIndexGenerationReady,
		AttemptCount:             1,
		GenerationManifestSHA256: sha256sum(gb),
		CoverageManifestSHA256:   sha256sum(cb),
		ArtifactManifestSHA256:   sha256sum(ab),
		BuildingStartedAt:        "2026-01-01T00:00:00.000Z",
		ReadyAt:                  "2026-01-01T00:00:01.000Z",
	}
	return row, id, genDir
}

// writeShard writes one shard with full control over the repository metadata
// and index time, mirroring the pinned builder's deterministic layout.
func writeShard(t *testing.T, path, generation string, seq int, meta zoektbuild.Metadata, docs []zoektbuild.Document, mutate func(*zoekt.Repository), indexTime time.Time) {
	t.Helper()
	repo := &zoekt.Repository{Name: meta.RepositoryName, Branches: []zoekt.RepositoryBranch{{Name: meta.Branch, Version: meta.Version}}, IndexOptions: meta.IndexOptions, Metadata: meta.Values}
	if mutate != nil {
		mutate(repo)
	}
	builder, err := index.NewShardBuilder(repo)
	if err != nil {
		t.Fatal(err)
	}
	builder.IndexTime = indexTime
	s := sha256.Sum256([]byte(generation + ":" + strconv.Itoa(seq)))
	builder.ID = hex.EncodeToString(s[:])[:20]
	for _, doc := range docs {
		if err := builder.Add(index.Document{Name: doc.Name, Content: doc.Content, Branches: []string{meta.Branch}}); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Write(f); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// republishShard recomputes the artifact manifest digest chain after a shard
// file was rewritten, so tampering stays internally consistent.
func republishShard(t *testing.T, root, id string, row workflow.SourceIndexGeneration, seq int) workflow.SourceIndexGeneration {
	t.Helper()
	genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
	rel := fmt.Sprintf("shards/%06d.zoekt", seq)
	b, err := os.ReadFile(filepath.Join(genDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	ab, err := os.ReadFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	am, err := sourceindex.ParseArtifactManifest(ab)
	if err != nil {
		t.Fatal(err)
	}
	for i := range am.Files {
		if am.Files[i].RelativePath == rel {
			am.Files[i].SHA256 = sha256sum(b)
			am.Files[i].SizeBytes = int64(len(b))
		}
	}
	ab, err = sourceindex.MarshalArtifactManifest(am)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), ab, 0600); err != nil {
		t.Fatal(err)
	}
	gb, err := os.ReadFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	gm, err := sourceindex.ParseGenerationManifest(gb)
	if err != nil {
		t.Fatal(err)
	}
	gm.ArtifactManifestSHA256 = sha256sum(ab)
	gb, err = sourceindex.MarshalGenerationManifest(gm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb, 0600); err != nil {
		t.Fatal(err)
	}
	row.ArtifactManifestSHA256 = sha256sum(ab)
	row.GenerationManifestSHA256 = sha256sum(gb)
	return row
}

func openReady(t *testing.T, root string, row workflow.SourceIndexGeneration, identity sourceindex.GenerationIdentity) (*Reader, error) {
	t.Helper()
	return Open(context.Background(), &fakeStore{row: row}, Config{IndexRoot: root}, identity)
}

func TestOpenExactReadyGeneration(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	root := filepath.Join(t.TempDir(), "index")
	docs := [][]zoektbuild.Document{{{Name: "a.txt", Content: []byte("alpha beta gamma")}, {Name: "nested/b.txt", Content: []byte("Needle needle")}}}
	row, id, _ := buildRealGeneration(t, root, identity, docs, []string{"large.bin"})
	r, err := openReady(t, root, row, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if d := r.Descriptor(); d.GenerationID != id || d.Identity != identity {
		t.Fatalf("descriptor = %#v", d)
	}
	fallback := r.FallbackCandidates()
	if len(fallback) != 1 || string(fallback[0].Path) != "large.bin" {
		t.Fatalf("fallback = %#v", fallback)
	}
	got, err := r.IndexedTextCandidates(context.Background(), "Needle")
	if err != nil || len(got) != 1 || string(got[0].Path) != "nested/b.txt" {
		t.Fatalf("candidates = %v, %v", got, err)
	}
	got, err = r.IndexedTextCandidates(context.Background(), "needle")
	if err != nil || len(got) != 0 {
		t.Fatalf("case-insensitive candidates = %v, %v", got, err)
	}
	got, err = r.IndexedTextCandidates(context.Background(), "alpha")
	if err != nil || len(got) != 1 || string(got[0].Path) != "a.txt" {
		t.Fatalf("alpha candidates = %v, %v", got, err)
	}
	got, err = r.IndexedTextCandidates(context.Background(), "absent")
	if err != nil || len(got) != 0 {
		t.Fatalf("absent candidates = %v, %v", got, err)
	}
}

func TestOpenBuiltGenerationIntegration(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	root := filepath.Join(t.TempDir(), "index")
	docs := [][]zoektbuild.Document{{{Name: "alpha.txt", Content: []byte("alpha beta gamma delta")}, {Name: "epsilon.txt", Content: []byte("epsilon zeta eta")}}}
	row, id, genDir := buildRealGeneration(t, root, identity, docs, []string{"fallback-raw"})
	r, err := openReady(t, root, row, identity)
	if err != nil {
		t.Fatal(err)
	}
	known := func() {
		t.Helper()
		got, err := r.IndexedTextCandidates(context.Background(), "alpha")
		if err != nil || len(got) != 1 || string(got[0].Path) != "alpha.txt" {
			t.Fatalf("candidates = %v, %v", got, err)
		}
		got, err = r.IndexedTextCandidates(context.Background(), "eta")
		if err != nil || len(got) != 1 || string(got[0].Path) != "epsilon.txt" {
			t.Fatalf("eta candidates = %v, %v", got, err)
		}
		fallback := r.FallbackCandidates()
		if len(fallback) != 1 || string(fallback[0].Path) != "fallback-raw" {
			t.Fatalf("fallback = %#v", fallback)
		}
	}
	known()
	// A replacement of the generation pathname after opening must not redirect
	// later reader operations.
	replacement := filepath.Join(filepath.Dir(genDir), id+"-replaced")
	if err := os.Rename(genDir, replacement); err != nil {
		t.Fatal(err)
	}
	known()
	if _, err := os.Stat(filepath.Join(replacement, sourceindex.GenerationManifestFileName)); err != nil {
		t.Fatal(err)
	}
	// A replacement of the shard file after verification must not redirect
	// later reader operations either.
	shardPath := filepath.Join(replacement, sourceindex.ShardDirectoryName, "000000.zoekt")
	if err := os.WriteFile(shardPath, []byte("replacement shard"), 0600); err != nil {
		t.Fatal(err)
	}
	known()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	_ = id
}

func TestOpenRealFilesystemDescriptorSafety(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	docs := [][]zoektbuild.Document{{{Name: "a.txt", Content: []byte("alpha beta gamma")}}}

	t.Run("generations symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, id, _ := buildRealGeneration(t, root, identity, docs, nil)
		if err := os.RemoveAll(filepath.Join(root, sourceindex.GenerationDirectoryName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(root, sourceindex.GenerationDirectoryName)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
		_ = id
	})
	t.Run("generation directory symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, id, genDir := buildRealGeneration(t, root, identity, docs, nil)
		if err := os.RemoveAll(genDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), genDir); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
		_ = id
	})
	t.Run("traversal outside the root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, _, genDir := buildRealGeneration(t, root, identity, docs, nil)
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(genDir, sourceindex.CoverageManifestFileName)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(genDir, sourceindex.CoverageManifestFileName)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("shard symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, _, genDir := buildRealGeneration(t, root, identity, docs, nil)
		outside := filepath.Join(t.TempDir(), "outside.zoekt")
		if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestOpenRejectsShardContentTampering(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	validMeta := func() zoektbuild.Metadata {
		id, _ := sourceindex.GenerationID(identity)
		return realMetadata(t, identity, id)
	}
	docs := []zoektbuild.Document{{Name: "a.txt", Content: []byte("alpha beta gamma")}}
	valid := func(t *testing.T) (row workflow.SourceIndexGeneration, root, id, genDir string) {
		t.Helper()
		root = filepath.Join(t.TempDir(), "index")
		row, id, genDir = buildRealGeneration(t, root, identity, [][]zoektbuild.Document{docs}, nil)
		return row, root, id, genDir
	}
	cases := []struct {
		name   string
		tamper func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration
	}{
		{"corrupt shard", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			if err := os.WriteFile(filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), []byte("garbage"), 0600); err != nil {
				t.Fatal(err)
			}
			return republishShard(t, root, id, row, 0)
		}},
		{"truncated shard", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b[:len(b)/2], 0600); err != nil {
				t.Fatal(err)
			}
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong repository", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.RepositoryName = "other-repository"
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong branch", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Branch = "other-branch"
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong commit", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Version = testOID2
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong build options digest", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.IndexOptions = strings.Repeat("0", 64)
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong generation metadata", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Values["relay_generation_id"] = strings.Repeat("0", 64)
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong vault metadata", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Values["relay_vault_id"] = "other-vault"
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong tree metadata", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Values["relay_tree_oid"] = testTree2
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong engine revision", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Values["relay_engine_revision"] = strings.Repeat("0", 40)
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong build contract version", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			m := validMeta()
			m.Values["relay_build_contract_version"] = "other"
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, m, docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong sequence", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 1, validMeta(), docs, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"wrong timestamp", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, validMeta(), docs, nil, time.Unix(1, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"tombstone", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, validMeta(), docs, func(r *zoekt.Repository) { r.Tombstone = true }, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"additional branch", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, validMeta(), docs, func(r *zoekt.Repository) {
				r.Branches = append(r.Branches, zoekt.RepositoryBranch{Name: "other-branch", Version: testOID2})
			}, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
		{"duplicate indexed document", func(t *testing.T, row workflow.SourceIndexGeneration, root, id, genDir string) workflow.SourceIndexGeneration {
			writeShard(t, filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), id, 0, validMeta(), []zoektbuild.Document{docs[0], docs[0]}, nil, time.Unix(0, 0).UTC())
			return republishShard(t, root, id, row, 0)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, root, id, genDir := valid(t)
			row = tc.tamper(t, row, root, id, genDir)
			if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
				t.Fatalf("error = %v, want generation integrity", err)
			}
		})
	}
	t.Run("missing indexed document", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, id, _ := buildRealGeneration(t, root, identity, [][]zoektbuild.Document{docs}, nil)
		// The coverage manifest claims one additional indexed document that
		// no shard contains, with a self-consistent digest chain.
		genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
		path := filepath.Join(genDir, sourceindex.CoverageManifestFileName)
		cb, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cm, err := sourceindex.ParseCoverageManifest(cb)
		if err != nil {
			t.Fatal(err)
		}
		p, err := sourceindex.NewPathIdentity([]byte("phantom.txt"))
		if err != nil {
			t.Fatal(err)
		}
		cm.Entries = append(cm.Entries, sourceindex.CoverageEntry{Path: p, Mode: "100644", ObjectType: "blob", ObjectOID: testOID, SizeBytes: 1, Status: sourceindex.CoverageIndexedText})
		cb, err = sourceindex.MarshalCoverageManifest(cm)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, cb, 0600); err != nil {
			t.Fatal(err)
		}
		ab, err := os.ReadFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName))
		if err != nil {
			t.Fatal(err)
		}
		am, err := sourceindex.ParseArtifactManifest(ab)
		if err != nil {
			t.Fatal(err)
		}
		for i := range am.Files {
			if am.Files[i].RelativePath == sourceindex.CoverageManifestFileName {
				am.Files[i].SHA256 = sha256sum(cb)
				am.Files[i].SizeBytes = int64(len(cb))
			}
		}
		ab, err = sourceindex.MarshalArtifactManifest(am)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), ab, 0600); err != nil {
			t.Fatal(err)
		}
		gb, err := os.ReadFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName))
		if err != nil {
			t.Fatal(err)
		}
		gm, err := sourceindex.ParseGenerationManifest(gb)
		if err != nil {
			t.Fatal(err)
		}
		gm.CoverageManifestSHA256 = sha256sum(cb)
		gm.ArtifactManifestSHA256 = sha256sum(ab)
		gb, err = sourceindex.MarshalGenerationManifest(gm)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb, 0600); err != nil {
			t.Fatal(err)
		}
		row.GenerationManifestSHA256 = sha256sum(gb)
		row.CoverageManifestSHA256 = sha256sum(cb)
		row.ArtifactManifestSHA256 = sha256sum(ab)
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("additional repository", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "index")
		row, id, genDir := buildRealGeneration(t, root, identity, [][]zoektbuild.Document{docs}, nil)
		dir := t.TempDir()
		first := filepath.Join(dir, "first.zoekt")
		writeShard(t, first, id, 0, validMeta(), docs, nil, time.Unix(0, 0).UTC())
		otherMeta := validMeta()
		otherMeta.RepositoryName = "other-repository"
		second := filepath.Join(dir, "second.zoekt")
		writeShard(t, second, id, 1, otherMeta, docs, nil, time.Unix(0, 0).UTC())
		firstFile, err := os.Open(first)
		if err != nil {
			t.Fatal(err)
		}
		firstIndex, err := index.NewIndexFile(firstFile)
		if err != nil {
			t.Fatal(err)
		}
		secondFile, err := os.Open(second)
		if err != nil {
			t.Fatal(err)
		}
		secondIndex, err := index.NewIndexFile(secondFile)
		if err != nil {
			t.Fatal(err)
		}
		mergedDir := filepath.Join(dir, "merged")
		if err := os.Mkdir(mergedDir, 0700); err != nil {
			t.Fatal(err)
		}
		_, merged, err := index.Merge(mergedDir, firstIndex, secondIndex)
		if err != nil {
			t.Fatal(err)
		}
		mergedBytes, err := os.ReadFile(merged)
		if err != nil {
			t.Fatal(err)
		}
		shardPath := filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt")
		if err := os.WriteFile(shardPath, mergedBytes, 0600); err != nil {
			t.Fatal(err)
		}
		row = republishShard(t, root, id, row, 0)
		if _, err := openReady(t, root, row, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestOpenPartialShardFailureClosesEverything(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	fs := newFakeFS(t)
	recording := &recordingFS{dirFS: fs}
	oldFS := boundFS
	boundFS = recording
	defer func() { boundFS = oldFS }()
	root := filepath.Join(t.TempDir(), "index")
	id, _ := sourceindex.GenerationID(identity)
	meta := realMetadata(t, identity, id)
	realDocs := [][]zoektbuild.Document{
		{{Name: "a.txt", Content: []byte("alpha beta gamma")}},
		{{Name: "b.txt", Content: []byte("delta epsilon zeta")}},
	}
	// The fake descriptor space serves real Zoekt shard bytes so the complete
	// verifier accepts them.
	genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
	fs.mkdir(root)
	fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName))
	fs.mkdir(genDir)
	fs.mkdir(filepath.Join(genDir, sourceindex.ShardDirectoryName))
	shardBytes := make(map[int][]byte)
	for i, docs := range realDocs {
		path := filepath.Join(genDir, sourceindex.ShardDirectoryName, fmt.Sprintf("%06d.zoekt", i))
		if err := zoektbuild.Write(path, id, i, meta, docs); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		shardBytes[i] = b
		fs.writeFile(path, b)
	}
	spec := generationSpec{identity: identity, indexed: []string{"a.txt", "b.txt"}, shards: shardBytes}
	row, _, _ := buildGeneration(t, fs, root, spec)
	oldOpenShard := openShard
	var secondFile *os.File
	openedFirst := false
	firstClosed := false
	openShard = func(f *os.File, generation string, sequence int, want zoektread.Metadata) (shard, error) {
		if sequence == 0 {
			z, err := zoektread.Open(f, generation, sequence, want)
			if err != nil {
				return nil, err
			}
			openedFirst = true
			return &trackedShard{Reader: z, closed: &firstClosed}, nil
		}
		secondFile = f
		return nil, errors.New("shard open failed")
	}
	defer func() { openShard = oldOpenShard }()
	if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
		t.Fatalf("error = %v", err)
	}
	if !openedFirst || !firstClosed {
		t.Fatal("previously opened shard was not closed")
	}
	if secondFile != nil {
		if _, err := secondFile.Stat(); err == nil {
			t.Fatal("unopened shard descriptor was not closed")
		}
	}
	if len(recording.created) < 3 {
		t.Fatalf("recorded handles = %v", recording.created)
	}
	if !fs.closed[int(recording.created[1])] {
		t.Fatal("generation directory handle was not closed")
	}
	if !fs.closed[int(recording.created[len(recording.created)-1])] {
		t.Fatal("shards directory handle was not closed")
	}
}

// recordingFS records every directory handle it creates.
type recordingFS struct {
	dirFS
	created []dirHandle
}

func (r *recordingFS) OpenChild(parent dirHandle, name string) (dirHandle, indexer.FileIdentity, error) {
	handle, id, err := r.dirFS.OpenChild(parent, name)
	if err == nil {
		r.created = append(r.created, handle)
	}
	return handle, id, err
}

type trackedShard struct {
	*zoektread.Reader
	closed *bool
}

func (t *trackedShard) Close() error {
	*t.closed = true
	return t.Reader.Close()
}

func TestCloseClosesGenerationDirectoryAfterFullOpen(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	fs := newFakeFS(t)
	oldFS := boundFS
	boundFS = fs
	defer func() { boundFS = oldFS }()
	root := filepath.Join(t.TempDir(), "index")
	id, _ := sourceindex.GenerationID(identity)
	meta := realMetadata(t, identity, id)
	genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
	fs.mkdir(root)
	fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName))
	fs.mkdir(genDir)
	fs.mkdir(filepath.Join(genDir, sourceindex.ShardDirectoryName))
	path := filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt")
	if err := zoektbuild.Write(path, id, 0, meta, []zoektbuild.Document{{Name: "a.txt", Content: []byte("alpha beta gamma")}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fs.writeFile(path, b)
	spec := generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: b}}
	row, _, _ := buildGeneration(t, fs, root, spec)
	oldOpenShard := openShard
	openShard = func(f *os.File, generation string, sequence int, want zoektread.Metadata) (shard, error) {
		z, err := zoektread.Open(f, generation, sequence, want)
		if err != nil {
			return nil, err
		}
		return z, nil
	}
	defer func() { openShard = oldOpenShard }()
	r, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity)
	if err != nil {
		t.Fatal(err)
	}
	handle := r.dir.handle
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !fs.closed[int(handle)] {
		t.Fatal("generation directory handle was not closed with the reader")
	}
}

func TestQuerySemanticsOnRealShards(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	root := filepath.Join(t.TempDir(), "index")
	docs := [][]zoektbuild.Document{
		{
			{Name: "content.txt", Content: []byte("Needle needle")},
			{Name: "name-only.txt", Content: []byte("plain content")},
			{Name: "猫犬鳥.txt", Content: []byte("猫犬鳥 repeated 猫犬鳥")},
		},
	}
	row, _, _ := buildRealGeneration(t, root, identity, docs, nil)
	r, err := openReady(t, root, row, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cases := []struct {
		literal string
		want    []string
	}{
		{"Needle", []string{"content.txt"}},
		{"needle", nil},
		{"猫犬鳥", []string{"猫犬鳥.txt"}},
		{"猫犬Z", nil},
		{"repeated", []string{"猫犬鳥.txt"}},
	}
	for _, tc := range cases {
		got, err := r.IndexedTextCandidates(context.Background(), tc.literal)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%q candidates = %v, want %v", tc.literal, got, tc.want)
		}
		for i, w := range tc.want {
			if string(got[i].Path) != w {
				t.Fatalf("%q candidate[%d] = %q, want %q", tc.literal, i, got[i].Path, w)
			}
		}
	}
}
