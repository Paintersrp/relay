package sourceindex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const (
	testOID    = "0123456789abcdef0123456789abcdef01234567"
	testTree   = "89abcdef0123456789abcdef0123456789abcdef"
	testDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestIdentityVectorsAndValidation(t *testing.T) {
	options := DefaultBuildOptions()
	optionsDigest, err := BuildOptionsSHA256(options)
	if err != nil {
		t.Fatal(err)
	}
	if optionsDigest != "0a840b5ee0918c24f53913822d673a77fc17f22c0792199631ca7340aa8c31b5" {
		t.Fatalf("set build options vector to %s", optionsDigest)
	}
	identity, err := NewGenerationIdentity("vault", testOID, testTree, optionsDigest)
	if err != nil {
		t.Fatal(err)
	}
	id, err := GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	if id != "b48d0d1c1ed6500d5cdcfc9e19785e262a511a389d21f154096496847f14b302" {
		t.Fatalf("set generation id vector to %s", id)
	}
	if next, _ := GenerationID(identity); next != id {
		t.Fatal("generation ID was not deterministic")
	}
	for _, mutate := range []func(*GenerationIdentity){
		func(v *GenerationIdentity) { v.Version = "other" }, func(v *GenerationIdentity) { v.VaultID = "other" },
		func(v *GenerationIdentity) { v.CommitOID = testTree }, func(v *GenerationIdentity) { v.TreeOID = testOID },
		func(v *GenerationIdentity) { v.Engine = "other" }, func(v *GenerationIdentity) { v.EngineRevision = testOID },
		func(v *GenerationIdentity) { v.BuildContractVersion = "other" }, func(v *GenerationIdentity) { v.BuildOptionsSHA256 = testDigest },
	} {
		v := identity
		mutate(&v)
		changed, err := GenerationID(v)
		if err == nil && changed == id {
			t.Fatal("changed identity retained its generation ID")
		}
		if err != nil && (v.VaultID != identity.VaultID || v.CommitOID != identity.CommitOID || v.TreeOID != identity.TreeOID || v.BuildOptionsSHA256 != identity.BuildOptionsSHA256) {
			t.Fatal(err)
		}
	}
	for _, identity := range []GenerationIdentity{
		{GenerationIdentityVersion, "", testOID, testTree, EngineZoekt, PinnedZoektRevision, BuildContractVersion, optionsDigest},
		{GenerationIdentityVersion, "vault", "ABC", testTree, EngineZoekt, PinnedZoektRevision, BuildContractVersion, optionsDigest},
		{GenerationIdentityVersion, "vault", testOID, "ABC", EngineZoekt, PinnedZoektRevision, BuildContractVersion, optionsDigest},
		{GenerationIdentityVersion, "vault", testOID, testTree, "other", PinnedZoektRevision, BuildContractVersion, optionsDigest},
		{GenerationIdentityVersion, "vault", testOID, testTree, EngineZoekt, "other", BuildContractVersion, optionsDigest},
		{GenerationIdentityVersion, "vault", testOID, testTree, EngineZoekt, PinnedZoektRevision, "other", optionsDigest},
		{GenerationIdentityVersion, "vault", testOID, testTree, EngineZoekt, PinnedZoektRevision, BuildContractVersion, "bad"},
	} {
		if _, err := GenerationID(identity); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	if _, err := MarshalBuildOptions(BuildOptions{}); err == nil {
		t.Fatal("invalid options accepted")
	}
}

func TestPathIdentityPreservesRawBytes(t *testing.T) {
	ordinary, err := NewPathIdentity([]byte("dir/file.txt"))
	if err != nil || ordinary.Base64 != "ZGlyL2ZpbGUudHh0" {
		t.Fatalf("ordinary path encoding: %v, %q", err, ordinary.Base64)
	}
	raw := []byte{'d', 'i', 'r', '/', 0xff, 'x'}
	p, err := NewPathIdentity(raw)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := p.Bytes()
	if err != nil || string(roundTrip) != string(raw) {
		t.Fatalf("raw path did not round trip: %v", err)
	}
	roundTrip[0] = 'x'
	again, _ := p.Bytes()
	if again[0] != 'd' {
		t.Fatal("path bytes were not defensive")
	}
	for _, invalid := range [][]byte{nil, []byte(""), []byte("/x"), []byte("x//y"), []byte("x/./y"), []byte("x/../y")} {
		if _, err := NewPathIdentity(invalid); err == nil {
			t.Fatalf("accepted invalid path %q", invalid)
		}
	}
	p.ByteLength++
	if _, err := p.Bytes(); err == nil {
		t.Fatal("accepted path length mismatch")
	}
	p, _ = NewPathIdentity([]byte("a\\b"))
	if got, _ := p.Bytes(); !bytes.Equal(got, []byte("a\\b")) {
		t.Fatal("backslash was not retained as a Git path byte")
	}
	p.SHA256 = testDigest
	if _, err := p.Bytes(); err == nil {
		t.Fatal("accepted path digest mismatch")
	}
	p.Base64 = "!"
	if _, err := p.Bytes(); err == nil {
		t.Fatal("accepted invalid base64")
	}
}

func TestCoverageCanonicalValidation(t *testing.T) {
	first, _ := NewPathIdentity([]byte("z"))
	second, _ := NewPathIdentity([]byte("a"))
	entries := []CoverageEntry{
		{Path: first, Mode: "100644", ObjectType: "blob", ObjectOID: testOID, SizeBytes: 1, Status: CoverageIndexedText},
		{Path: second, Mode: "040000", ObjectType: "tree", ObjectOID: testTree, Status: CoverageNonBlob},
	}
	m, err := NewCoverageManifest(testDigest, testOID, testTree, entries)
	if err != nil {
		t.Fatal(err)
	}
	if m.Counts.IndexedText != 1 || m.Counts.NonBlob != 1 || m.Counts.Total != 2 {
		t.Fatal("counts not calculated")
	}
	b, err := MarshalCoverageManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCoverageManifest(b); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCoverageManifest(append(b, ' ')); err == nil {
		t.Fatal("accepted noncanonical bytes")
	}
	unsorted := m
	unsorted.Entries[0], unsorted.Entries[1] = unsorted.Entries[1], unsorted.Entries[0]
	if _, err := ParseCoverageManifest(mustJSON(t, unsorted)); err == nil {
		t.Fatal("accepted unsorted entries")
	}
	bad := m
	bad.Entries[0].Status = CoverageNonBlob
	if _, err := MarshalCoverageManifest(bad); err == nil {
		t.Fatal("accepted blob non_blob status")
	}
}

func TestArtifactAndGenerationManifests(t *testing.T) {
	files := []ArtifactFile{{Kind: ArtifactZoektShard, RelativePath: "shards/b", SHA256: testDigest, SizeBytes: 2}, {Kind: ArtifactCoverage, RelativePath: CoverageManifestFileName, SHA256: testDigest, SizeBytes: 1}}
	a, err := NewArtifactManifest(testDigest, files)
	if err != nil {
		t.Fatal(err)
	}
	if a.Files[0].RelativePath != CoverageManifestFileName {
		t.Fatal("files not ordered")
	}
	b, _ := MarshalArtifactManifest(a)
	if _, err := ParseArtifactManifest(b); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../x", "/x", "x\\y", ArtifactManifestFileName, GenerationManifestFileName} {
		_, err := NewArtifactManifest(testDigest, []ArtifactFile{{Kind: ArtifactCoverage, RelativePath: path, SHA256: testDigest}})
		if err == nil {
			t.Fatalf("accepted artifact path %q", path)
		}
	}
	options, _ := BuildOptionsSHA256(DefaultBuildOptions())
	i, _ := NewGenerationIdentity("vault", testOID, testTree, options)
	g, err := NewGenerationManifest(i, testDigest, testDigest)
	if err != nil {
		t.Fatal(err)
	}
	gb, _ := MarshalGenerationManifest(g)
	if _, err := ParseGenerationManifest(gb); err != nil {
		t.Fatal(err)
	}
	g.BranchName = "wrong"
	if _, err := MarshalGenerationManifest(g); err == nil {
		t.Fatal("accepted wrong branch")
	}
}

func TestStoragePathsAndOverlap(t *testing.T) {
	root := t.TempDir()
	id := hash("generation")
	nonce := hash("nonce")[:32]
	final, err := GenerationDirectory(root, id)
	if err != nil {
		t.Fatal(err)
	}
	staging, err := StagingDirectory(root, id, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if final != filepath.Join(root, GenerationDirectoryName, id) || staging != filepath.Join(root, StagingDirectoryName, id+"-"+nonce) {
		t.Fatal("unexpected storage paths")
	}
	if _, err := StagingRelativeDirectory(id, "not-a-nonce"); err == nil {
		t.Fatal("invalid nonce accepted")
	}
	if _, err := GenerationDirectory(root, "invalid"); err == nil {
		t.Fatal("invalid generation ID accepted")
	}
	missingRoot := filepath.Join(root, "missing", "index")
	if got, err := GenerationDirectory(missingRoot, id); err != nil || !within(missingRoot, got) {
		t.Fatalf("nonexistent directory root: %q, %v", got, err)
	}
	protected := filepath.Join(root, "protected")
	if err := os.Mkdir(protected, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndexRoot(protected, ProtectedStorage{SourceVaultRoot: protected}); err == nil {
		t.Fatal("equal roots accepted")
	}
	if err := ValidateIndexRoot(root, ProtectedStorage{SourceVaultRoot: protected}); err == nil {
		t.Fatal("ancestor root accepted")
	}
	if err := ValidateIndexRoot(filepath.Join(protected, "child"), ProtectedStorage{SourceVaultRoot: protected}); err == nil {
		t.Fatal("descendant root accepted")
	}
	if err := ValidateIndexRoot(filepath.Join(root, "safe", "missing"), ProtectedStorage{RepositoryRoots: []string{protected}}); err != nil {
		t.Fatal(err)
	}
	for _, storage := range []ProtectedStorage{
		{SourceVaultRoot: protected},
		{WorkflowArtifactsRoot: protected},
		{WorkflowDatabasePath: protected},
		{RepositoryRoots: []string{protected}},
	} {
		if err := ValidateIndexRoot(protected, storage); err == nil {
			t.Fatal("accepted protected root overlap")
		}
		if err := ValidateIndexRoot(root, storage); err == nil {
			t.Fatal("accepted protected descendant overlap")
		}
		if err := ValidateIndexRoot(filepath.Join(protected, "child"), storage); err == nil {
			t.Fatal("accepted protected ancestor overlap")
		}
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndexRoot(root, ProtectedStorage{WorkflowDatabasePath: file}); err == nil {
		t.Fatal("accepted workflow database overlap")
	}
	for _, unsafe := range []string{file, filepath.Join(file, "child")} {
		if _, err := GenerationDirectory(unsafe, id); err == nil {
			t.Fatal("accepted regular-file root or ancestor")
		}
	}
	intermediate := filepath.Join(root, GenerationDirectoryName)
	if err := os.WriteFile(intermediate, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerationDirectory(root, id); err == nil {
		t.Fatal("accepted regular-file storage component")
	}
	if err := os.Remove(intermediate); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := GenerationDirectory(link, id); err == nil {
		t.Fatal("accepted symlink root")
	}
	if err := os.Symlink(target, filepath.Join(root, GenerationDirectoryName)); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerationDirectory(root, id); err == nil {
		t.Fatal("accepted symlink storage component")
	}
	if err := os.Remove(filepath.Join(root, GenerationDirectoryName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protected, filepath.Join(root, "index-link")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndexRoot(filepath.Join(root, "index-link"), ProtectedStorage{SourceVaultRoot: protected}); err == nil {
		t.Fatal("accepted symlink-resolved overlap")
	}
}

func TestCoverageManifestContract(t *testing.T) {
	statuses := []CoverageStatus{CoverageIndexedText, CoverageShortText, CoverageTextIneligible, CoverageFallbackPath, CoverageFallbackSize, CoverageNonBlob}
	entries := make([]CoverageEntry, 0, len(statuses))
	for i, status := range statuses {
		path, _ := NewPathIdentity([]byte{byte('a' + i)})
		entry := CoverageEntry{Path: path, Mode: "100644", ObjectType: "blob", ObjectOID: testOID, SizeBytes: 1, Status: status}
		if status == CoverageNonBlob {
			entry = CoverageEntry{Path: path, Mode: "040000", ObjectType: "tree", ObjectOID: testTree, Status: status}
		}
		entries = append(entries, entry)
	}
	m, err := NewCoverageManifest(testDigest, testOID, testTree, entries)
	if err != nil || m.Counts != (CoverageCounts{1, 1, 1, 1, 1, 1, 6}) {
		t.Fatalf("coverage statuses: %v, %#v", err, m.Counts)
	}
	entries[0].Status = CoverageNonBlob
	if m.Entries[0].Status != CoverageIndexedText {
		t.Fatal("coverage entries were not copied")
	}
	raw, _ := MarshalCoverageManifest(m)
	for _, bad := range [][]byte{append(raw, []byte(" {}")...), append(raw, ' '), bytes.Replace(raw, []byte(`"version"`), []byte(`"unknown":true,"version"`), 1)} {
		if _, err := ParseCoverageManifest(bad); err == nil {
			t.Fatal("accepted malformed coverage JSON")
		}
	}
	for _, mutate := range []func(*CoverageManifest){
		func(v *CoverageManifest) { v.Counts.Total++ },
		func(v *CoverageManifest) { v.Entries[0].Status = "invalid" },
		func(v *CoverageManifest) { v.Entries[0].Status = CoverageNonBlob },
		func(v *CoverageManifest) { v.Entries[0].ObjectOID = "bad" },
		func(v *CoverageManifest) { v.Entries[0].SizeBytes = -1 },
		func(v *CoverageManifest) { v.Entries[5].SizeBytes = 1 },
		func(v *CoverageManifest) { v.Entries[5].ObjectType = "commit" },
		func(v *CoverageManifest) { v.Entries[5].Status = CoverageIndexedText },
		func(v *CoverageManifest) { v.Entries[0].Mode = "040000" },
	} {
		bad := m
		bad.Entries = append([]CoverageEntry(nil), m.Entries...)
		mutate(&bad)
		if _, err := MarshalCoverageManifest(bad); err == nil {
			t.Fatal("accepted invalid coverage entry")
		}
	}
	duplicate := m
	duplicate.Entries = append(append([]CoverageEntry(nil), m.Entries...), m.Entries[0])
	if _, err := MarshalCoverageManifest(duplicate); err == nil {
		t.Fatal("accepted duplicate coverage path")
	}
}

func TestArtifactManifestContract(t *testing.T) {
	files := []ArtifactFile{{ArtifactZoektShard, "shards/z", testDigest, 1}, {ArtifactCoverage, "coverage.json", testDigest, 2}}
	m, err := NewArtifactManifest(testDigest, files)
	if err != nil || m.Files[0].RelativePath != "coverage.json" {
		t.Fatalf("artifact ordering: %v", err)
	}
	files[0].RelativePath = "changed"
	if m.Files[1].RelativePath != "shards/z" {
		t.Fatal("artifact files were not copied")
	}
	raw, _ := MarshalArtifactManifest(m)
	for _, bad := range [][]byte{append(raw, []byte(" {}")...), append(raw, ' '), bytes.Replace(raw, []byte(`"version"`), []byte(`"unknown":true,"version"`), 1)} {
		if _, err := ParseArtifactManifest(bad); err == nil {
			t.Fatal("accepted malformed artifact JSON")
		}
	}
	for _, path := range []string{"", "/x", "x\\y", "x//y", "x/./y", "x/../y", ArtifactManifestFileName, GenerationManifestFileName} {
		if _, err := NewArtifactManifest(testDigest, []ArtifactFile{{ArtifactCoverage, path, testDigest, 0}}); err == nil {
			t.Fatalf("accepted artifact path %q", path)
		}
	}
	for _, file := range []ArtifactFile{{"bad", "x", testDigest, 0}, {ArtifactCoverage, "x", "bad", 0}, {ArtifactCoverage, "x", testDigest, -1}} {
		if _, err := NewArtifactManifest(testDigest, []ArtifactFile{file}); err == nil {
			t.Fatal("accepted invalid artifact file")
		}
	}
	duplicate := append([]ArtifactFile(nil), m.Files...)
	duplicate = append(duplicate, m.Files[0])
	if _, err := NewArtifactManifest(testDigest, duplicate); err == nil {
		t.Fatal("accepted duplicate artifact path")
	}
	unsorted := m
	unsorted.Files = []ArtifactFile{m.Files[1], m.Files[0]}
	if _, err := ParseArtifactManifest(mustJSON(t, unsorted)); err == nil {
		t.Fatal("accepted unsorted artifact files")
	}
}

func TestGenerationManifestContract(t *testing.T) {
	options, _ := BuildOptionsSHA256(DefaultBuildOptions())
	identity, _ := NewGenerationIdentity("vault", testOID, testTree, options)
	m, err := NewGenerationManifest(identity, testDigest, testDigest)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := MarshalGenerationManifest(m)
	if _, err := ParseGenerationManifest(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*GenerationManifest){
		func(v *GenerationManifest) { v.GenerationID = testDigest },
		func(v *GenerationManifest) { v.RepositoryName = "wrong" },
		func(v *GenerationManifest) { v.BranchName = "wrong" },
		func(v *GenerationManifest) { v.Identity.Engine = "wrong" },
		func(v *GenerationManifest) { v.Identity.EngineRevision = "wrong" },
		func(v *GenerationManifest) { v.Identity.BuildContractVersion = "wrong" },
		func(v *GenerationManifest) { v.Identity.BuildOptionsSHA256 = "wrong" },
		func(v *GenerationManifest) { v.CoverageManifestSHA256 = "wrong" },
		func(v *GenerationManifest) { v.ArtifactManifestSHA256 = "wrong" },
		func(v *GenerationManifest) { v.Version = "wrong" },
	} {
		bad := m
		mutate(&bad)
		if _, err := MarshalGenerationManifest(bad); err == nil {
			t.Fatal("accepted invalid generation manifest")
		}
	}
	for _, bad := range [][]byte{append(raw, []byte(" {}")...), append(raw, ' '), bytes.Replace(raw, []byte(`"version"`), []byte(`"unknown":true,"version"`), 1)} {
		if _, err := ParseGenerationManifest(bad); err == nil {
			t.Fatal("accepted malformed generation JSON")
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := jsonMarshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
