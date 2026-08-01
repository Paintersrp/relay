package sourceindex

import (
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
	if _, err := NewGenerationIdentity("vault", "ABC", testTree, optionsDigest); err == nil {
		t.Fatal("invalid OID accepted")
	}
	if _, err := MarshalBuildOptions(BuildOptions{}); err == nil {
		t.Fatal("invalid options accepted")
	}
}

func TestPathIdentityPreservesRawBytes(t *testing.T) {
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
