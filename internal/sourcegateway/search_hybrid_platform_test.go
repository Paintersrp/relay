//go:build linux || darwin || freebsd || netbsd

package sourcegateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/reader"
	"relay/internal/sourceindex/zoektbuild"
	"relay/internal/sourcevault"
	workflow "relay/internal/store/workflow"
)

type hybridPlatformGenerationStore struct {
	row workflow.SourceIndexGeneration
}

func (s hybridPlatformGenerationStore) GetSourceIndexGenerationByIdentity(ctx context.Context, _ sourceindex.GenerationIdentity) (workflow.SourceIndexGeneration, error) {
	if err := ctx.Err(); err != nil {
		return workflow.SourceIndexGeneration{}, err
	}
	return s.row, nil
}

func hybridPlatformDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func hybridPlatformIdentity(t *testing.T) sourceindex.GenerationIdentity {
	t.Helper()
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", strings.Repeat("1", 40), strings.Repeat("2", 40), options)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

type hybridPlatformFixture struct {
	row      workflow.SourceIndexGeneration
	identity sourceindex.GenerationIdentity
	root     string
	vault    *fidelityVaultFake
}

func buildHybridPlatformFixture(t *testing.T) hybridPlatformFixture {
	t.Helper()
	identity := hybridPlatformIdentity(t)
	generationID, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "index")
	generationDir := filepath.Join(root, sourceindex.GenerationDirectoryName, generationID)
	if err := os.MkdirAll(filepath.Join(generationDir, sourceindex.ShardDirectoryName), 0700); err != nil {
		t.Fatal(err)
	}
	repository, err := sourceindex.GenerationRepositoryName(generationID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := zoektbuild.Metadata{
		RepositoryName: repository,
		Branch:         sourceindex.GenerationBranchName,
		Version:        identity.CommitOID,
		IndexOptions:   identity.BuildOptionsSHA256,
		Values: map[string]string{
			"relay_generation_id":          generationID,
			"relay_vault_id":               identity.VaultID,
			"relay_commit_oid":             identity.CommitOID,
			"relay_tree_oid":               identity.TreeOID,
			"relay_engine_revision":        identity.EngineRevision,
			"relay_build_contract_version": identity.BuildContractVersion,
			"relay_build_options_sha256":   identity.BuildOptionsSHA256,
		},
	}
	literal := []byte("needle")
	docs := []zoektbuild.Document{
		{Name: "01-exact.txt", Content: []byte("prefix needle suffix")},
		{Name: "02-false-positive.txt", Content: append([]byte(nil), literal...)},
		{Name: "03-case-mismatch.txt", Content: []byte("NEEDLE")},
	}
	shardPath := filepath.Join(generationDir, sourceindex.ShardDirectoryName, "000000.zoekt")
	if err := zoektbuild.Write(shardPath, generationID, 0, metadata, docs); err != nil {
		t.Fatal(err)
	}

	blobOIDs := map[string]string{
		"01-exact.txt":          strings.Repeat("3", 40),
		"02-false-positive.txt": strings.Repeat("4", 40),
		"03-case-mismatch.txt":  strings.Repeat("5", 40),
		"04-fallback-size.txt":  strings.Repeat("6", 40),
		"05-malformed.txt":      strings.Repeat("7", 40),
		"06-truncated.txt":      strings.Repeat("8", 40),
		"07-nul.txt":            strings.Repeat("9", 40),
	}
	rawPath := []byte{'z', 0xff}
	rawOID := strings.Repeat("a", 40)
	blobs := map[string][]byte{
		blobOIDs["01-exact.txt"]:          []byte("prefix needle suffix"),
		blobOIDs["02-false-positive.txt"]: []byte("NEEDLE"),
		blobOIDs["03-case-mismatch.txt"]:  []byte("NEEDLE"),
		blobOIDs["04-fallback-size.txt"]:  append([]byte(nil), literal...),
		blobOIDs["05-malformed.txt"]:      []byte{0xff, 'n', 'e', 'e', 'd', 'l', 'e'},
		blobOIDs["06-truncated.txt"]:      []byte{0xe2, 0x82},
		blobOIDs["07-nul.txt"]:            []byte("prefix\x00needle"),
		rawOID:                            append([]byte(nil), literal...),
	}
	entries := make([]sourceindex.CoverageEntry, 0, len(blobOIDs)+1)
	retainedEntries := make([]sourcevault.RetainedTreeEntry, 0, len(blobOIDs)+1)
	add := func(path []byte, mode, oid string, status sourceindex.CoverageStatus, content []byte) {
		pathIdentity, identityErr := sourceindex.NewPathIdentity(path)
		if identityErr != nil {
			t.Fatal(identityErr)
		}
		entries = append(entries, sourceindex.CoverageEntry{Path: pathIdentity, Mode: mode, ObjectType: "blob", ObjectOID: oid, SizeBytes: int64(len(content)), Status: status})
		retainedEntries = append(retainedEntries, sourcevault.RetainedTreeEntry{Name: append([]byte(nil), path...), Mode: mode, ObjectType: "blob", ObjectOID: oid})
	}
	add([]byte("01-exact.txt"), "100755", blobOIDs["01-exact.txt"], sourceindex.CoverageIndexedText, blobs[blobOIDs["01-exact.txt"]])
	add([]byte("02-false-positive.txt"), "100644", blobOIDs["02-false-positive.txt"], sourceindex.CoverageIndexedText, blobs[blobOIDs["02-false-positive.txt"]])
	add([]byte("03-case-mismatch.txt"), "100644", blobOIDs["03-case-mismatch.txt"], sourceindex.CoverageIndexedText, blobs[blobOIDs["03-case-mismatch.txt"]])
	add([]byte("04-fallback-size.txt"), "100644", blobOIDs["04-fallback-size.txt"], sourceindex.CoverageFallbackSize, blobs[blobOIDs["04-fallback-size.txt"]])
	add([]byte("05-malformed.txt"), "100644", blobOIDs["05-malformed.txt"], sourceindex.CoverageFallbackPath, blobs[blobOIDs["05-malformed.txt"]])
	add([]byte("06-truncated.txt"), "100644", blobOIDs["06-truncated.txt"], sourceindex.CoverageFallbackPath, blobs[blobOIDs["06-truncated.txt"]])
	add([]byte("07-nul.txt"), "100644", blobOIDs["07-nul.txt"], sourceindex.CoverageFallbackPath, blobs[blobOIDs["07-nul.txt"]])
	add(rawPath, "100644", rawOID, sourceindex.CoverageFallbackPath, blobs[rawOID])

	coverage, err := sourceindex.NewCoverageManifest(generationID, identity.CommitOID, identity.TreeOID, entries)
	if err != nil {
		t.Fatal(err)
	}
	coverageBytes, err := sourceindex.MarshalCoverageManifest(coverage)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationDir, sourceindex.CoverageManifestFileName), coverageBytes, 0600); err != nil {
		t.Fatal(err)
	}
	shardBytes, err := os.ReadFile(shardPath)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := sourceindex.NewArtifactManifest(generationID, []sourceindex.ArtifactFile{
		{Kind: sourceindex.ArtifactZoektShard, RelativePath: sourceindex.ShardDirectoryName + "/000000.zoekt", SHA256: hybridPlatformDigest(shardBytes), SizeBytes: int64(len(shardBytes))},
		{Kind: sourceindex.ArtifactCoverage, RelativePath: sourceindex.CoverageManifestFileName, SHA256: hybridPlatformDigest(coverageBytes), SizeBytes: int64(len(coverageBytes))},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := sourceindex.MarshalArtifactManifest(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationDir, sourceindex.ArtifactManifestFileName), artifactBytes, 0600); err != nil {
		t.Fatal(err)
	}
	generation, err := sourceindex.NewGenerationManifest(identity, hybridPlatformDigest(coverageBytes), hybridPlatformDigest(artifactBytes))
	if err != nil {
		t.Fatal(err)
	}
	generationBytes, err := sourceindex.MarshalGenerationManifest(generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationDir, sourceindex.GenerationManifestFileName), generationBytes, 0600); err != nil {
		t.Fatal(err)
	}
	return hybridPlatformFixture{
		row: workflow.SourceIndexGeneration{
			GenerationID:             generationID,
			Identity:                 identity,
			State:                    workflow.SourceIndexGenerationReady,
			AttemptCount:             1,
			GenerationManifestSHA256: hybridPlatformDigest(generationBytes),
			CoverageManifestSHA256:   hybridPlatformDigest(coverageBytes),
			ArtifactManifestSHA256:   hybridPlatformDigest(artifactBytes),
			BuildingStartedAt:        "2026-01-01T00:00:00.000Z",
			ReadyAt:                  "2026-01-01T00:00:01.000Z",
		},
		identity: identity,
		root:     root,
		vault: &fidelityVaultFake{
			trees: map[string][]sourcevault.RetainedTreeEntry{identity.TreeOID: retainedEntries},
			blobs: blobs,
			nodes: map[string]sourcevault.RetainedCommitNode{},
		},
	}
}

func TestHybridRealReaderMatchesRetainedAuthority(t *testing.T) {
	fixture := buildHybridPlatformFixture(t)
	indexReader, err := reader.Open(context.Background(), hybridPlatformGenerationStore{row: fixture.row}, reader.Config{IndexRoot: fixture.root}, fixture.identity)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := indexReader.Close(); closeErr != nil {
			t.Errorf("reader close: %v", closeErr)
		}
	}()

	generationID, err := sourceindex.GenerationID(fixture.identity)
	if err != nil {
		t.Fatal(err)
	}
	expectedDescriptor := reader.Descriptor{GenerationID: generationID, Identity: fixture.identity, GenerationManifestSHA256: fixture.row.GenerationManifestSHA256, CoverageManifestSHA256: fixture.row.CoverageManifestSHA256, ArtifactManifestSHA256: fixture.row.ArtifactManifestSHA256}
	if indexReader.Descriptor() != expectedDescriptor {
		t.Fatalf("descriptor=%#v want=%#v", indexReader.Descriptor(), expectedDescriptor)
	}
	indexed, err := indexReader.IndexedTextCandidates(context.Background(), "needle")
	if err != nil {
		t.Fatal(err)
	}
	var indexedPaths []string
	for _, candidate := range indexed {
		indexedPaths = append(indexedPaths, string(candidate.Path))
	}
	if !reflect.DeepEqual(indexedPaths, []string{"01-exact.txt", "02-false-positive.txt"}) {
		t.Fatalf("indexed paths=%#v", indexedPaths)
	}

	authority := fidelityAuthority(fixture.identity.CommitOID, fixture.identity.TreeOID, "", 1)
	service := newFidelityService(t, fixture.vault, authority)
	prepared, err := service.prepareSearch(context.Background(), textSearchRequest("needle"))
	if err != nil {
		t.Fatal(err)
	}
	hybrid, err := newHybridTextSearchCandidateSource(context.Background(), indexReader, indexReader.Descriptor(), "needle", prepared.prefixes)
	if err != nil {
		t.Fatal(err)
	}
	hybridResult, err := service.executePreparedSearch(context.Background(), prepared, hybrid)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := newRetainedTreeSearchCandidateSource(context.Background(), service, authority, prepared.prefixes)
	if err != nil {
		t.Fatal(err)
	}
	scannerResult, err := service.executePreparedSearch(context.Background(), prepared, scanner)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hybridResult.Matches, scannerResult.Matches) || hybridResult.Completion != scannerResult.Completion || hybridResult.Completion != SearchCompletionComplete {
		t.Fatalf("hybrid=%#v scanner=%#v", hybridResult, scannerResult)
	}
	if len(hybridResult.Matches) != 3 {
		t.Fatalf("authoritative matches=%#v", hybridResult.Matches)
	}
	for _, match := range hybridResult.Matches {
		if match.Path.PathID == pathID([]byte("02-false-positive.txt")) || match.Path.PathID == pathID([]byte("03-case-mismatch.txt")) || match.Path.PathID == pathID([]byte("05-malformed.txt")) || match.Path.PathID == pathID([]byte("06-truncated.txt")) || match.Path.PathID == pathID([]byte("07-nul.txt")) {
			t.Fatalf("ineligible or false-positive path matched=%#v", match)
		}
	}
	if hybridResult.Matches[0].Path.PathID != pathID([]byte("01-exact.txt")) || hybridResult.Matches[0].FileMode != "100755" || hybridResult.Matches[0].BlobOID != strings.Repeat("3", 40) || hybridResult.Matches[0].ByteOffset != 7 || hybridResult.Matches[0].MatchLength != 6 || hybridResult.Matches[0].OccurrenceOrdinal != 0 {
		t.Fatalf("exact match=%#v", hybridResult.Matches[0])
	}
	rawIndex := -1
	for index, match := range hybridResult.Matches {
		if match.Path.PathID == pathID([]byte{'z', 0xff}) {
			rawIndex = index
			if match.Path.ByteLength != 2 || match.Path.DisplayValid || match.BlobOID != strings.Repeat("a", 40) {
				t.Fatalf("raw path match=%#v", match)
			}
		}
	}
	if rawIndex < 0 || rawIndex != len(hybridResult.Matches)-1 {
		t.Fatalf("raw path order index=%d matches=%#v", rawIndex, hybridResult.Matches)
	}
}
