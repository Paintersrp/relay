// Package sourceindex defines the durable, deterministic contracts for source indexes.
package sourceindex

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	GenerationIdentityVersion        = "relay.source-index-generation-identity.v1"
	GenerationManifestVersion        = "relay.source-index-generation-manifest.v1"
	CoverageManifestVersion          = "relay.source-index-coverage-manifest.v1"
	ArtifactManifestVersion          = "relay.source-index-artifact-manifest.v1"
	BuildContractVersion             = "relay.source-index-build.v1"
	BuildOptionsVersion              = "relay.source-index-build-options.v1"
	EngineZoekt                      = "zoekt"
	PinnedZoektRevision              = "2b2ce2e398e6bee68d67143f567b6c6199340c7f"
	GenerationRepositoryPrefix       = "relay-generation/"
	GenerationBranchName             = "relay-revision"
	DefaultFileLimitBytes      int64 = 64 << 20
	DefaultMaxTrigramCount           = 64 << 20
	StagingDirectoryName             = "staging"
	GenerationDirectoryName          = "generations"
	GenerationManifestFileName       = "generation.json"
	CoverageManifestFileName         = "coverage.json"
	ArtifactManifestFileName         = "manifest.json"
	ShardDirectoryName               = "shards"
)

type ErrorCode string

const (
	InvalidIdentity      ErrorCode = "invalid_identity"
	InvalidManifest      ErrorCode = "invalid_manifest"
	DigestMismatch       ErrorCode = "digest_mismatch"
	UnsupportedVersion   ErrorCode = "unsupported_version"
	InvalidCoverage      ErrorCode = "invalid_coverage"
	InvalidArtifact      ErrorCode = "invalid_artifact"
	UnsafePath           ErrorCode = "unsafe_path"
	StorageOverlap       ErrorCode = "storage_overlap"
	NoncanonicalEncoding ErrorCode = "noncanonical_encoding"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }
func fail(code ErrorCode, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type BuildOptions struct {
	Version               string `json:"version"`
	FileLimitBytes        int64  `json:"file_limit_bytes"`
	MaxTrigramCount       int    `json:"max_trigram_count"`
	DisableCTags          bool   `json:"disable_ctags"`
	IncludeSubmodules     bool   `json:"include_submodules"`
	ApplyRepositoryIgnore bool   `json:"apply_repository_ignore"`
	BranchName            string `json:"branch_name"`
}

func DefaultBuildOptions() BuildOptions {
	return BuildOptions{BuildOptionsVersion, DefaultFileLimitBytes, DefaultMaxTrigramCount, true, false, false, GenerationBranchName}
}
func validateBuildOptions(v BuildOptions) error {
	if v.Version != BuildOptionsVersion {
		return fail(UnsupportedVersion, "build options version")
	}
	if v.FileLimitBytes != DefaultFileLimitBytes || v.MaxTrigramCount != DefaultMaxTrigramCount || !v.DisableCTags || v.IncludeSubmodules || v.ApplyRepositoryIgnore || v.BranchName != GenerationBranchName {
		return fail(InvalidIdentity, "unsupported build options")
	}
	return nil
}
func MarshalBuildOptions(v BuildOptions) ([]byte, error) {
	if err := validateBuildOptions(v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func BuildOptionsSHA256(v BuildOptions) (string, error) {
	b, e := MarshalBuildOptions(v)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}

type GenerationIdentity struct {
	Version              string `json:"version"`
	VaultID              string `json:"vault_id"`
	CommitOID            string `json:"commit_oid"`
	TreeOID              string `json:"tree_oid"`
	Engine               string `json:"engine"`
	EngineRevision       string `json:"engine_revision"`
	BuildContractVersion string `json:"build_contract_version"`
	BuildOptionsSHA256   string `json:"build_options_sha256"`
}

func NewGenerationIdentity(vaultID, commitOID, treeOID, options string) (GenerationIdentity, error) {
	v := GenerationIdentity{GenerationIdentityVersion, vaultID, commitOID, treeOID, EngineZoekt, PinnedZoektRevision, BuildContractVersion, options}
	return v, validateIdentity(v)
}
func validateIdentity(v GenerationIdentity) error {
	if v.Version != GenerationIdentityVersion {
		return fail(UnsupportedVersion, "identity version")
	}
	if v.VaultID == "" || !lowerHex(v.CommitOID, 40) || !lowerHex(v.TreeOID, 40) || v.Engine != EngineZoekt || v.EngineRevision != PinnedZoektRevision || v.BuildContractVersion != BuildContractVersion || !lowerHex(v.BuildOptionsSHA256, 64) {
		return fail(InvalidIdentity, "invalid generation identity")
	}
	return nil
}
func MarshalGenerationIdentity(v GenerationIdentity) ([]byte, error) {
	if e := validateIdentity(v); e != nil {
		return nil, e
	}
	return json.Marshal(v)
}
func GenerationID(v GenerationIdentity) (string, error) {
	b, e := MarshalGenerationIdentity(v)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}
func GenerationRepositoryName(id string) (string, error) {
	if !lowerHex(id, 64) {
		return "", fail(InvalidIdentity, "generation id")
	}
	return GenerationRepositoryPrefix + id, nil
}
func GenerationBranch() string { return GenerationBranchName }

type CoverageStatus string

const (
	CoverageIndexedText    CoverageStatus = "indexed_text"
	CoverageShortText      CoverageStatus = "short_text"
	CoverageTextIneligible CoverageStatus = "text_ineligible"
	CoverageFallbackPath   CoverageStatus = "fallback_path"
	CoverageFallbackSize   CoverageStatus = "fallback_size"
	CoverageNonBlob        CoverageStatus = "non_blob"
)

type PathIdentity struct {
	Base64     string `json:"base64"`
	SHA256     string `json:"sha256"`
	ByteLength int64  `json:"byte_length"`
}

func NewPathIdentity(path []byte) (PathIdentity, error) {
	if e := validRawPath(path); e != nil {
		return PathIdentity{}, e
	}
	p := PathIdentity{base64.StdEncoding.EncodeToString(path), digest(path), int64(len(path))}
	return p, nil
}
func (p PathIdentity) Bytes() ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(p.Base64)
	if e != nil || base64.StdEncoding.EncodeToString(b) != p.Base64 {
		return nil, fail(InvalidCoverage, "path base64")
	}
	if e := validRawPath(b); e != nil {
		return nil, e
	}
	if p.ByteLength != int64(len(b)) || p.SHA256 != digest(b) {
		return nil, fail(DigestMismatch, "path identity")
	}
	return append([]byte(nil), b...), nil
}
func validRawPath(p []byte) error {
	if len(p) == 0 || p[0] == '/' {
		return fail(UnsafePath, "path")
	}
	for _, part := range bytes.Split(p, []byte{'/'}) {
		if len(part) == 0 || bytes.Equal(part, []byte(".")) || bytes.Equal(part, []byte("..")) {
			return fail(UnsafePath, "path component")
		}
	}
	return nil
}

type CoverageEntry struct {
	Path       PathIdentity   `json:"path"`
	Mode       string         `json:"mode"`
	ObjectType string         `json:"object_type"`
	ObjectOID  string         `json:"object_oid"`
	SizeBytes  int64          `json:"size_bytes"`
	Status     CoverageStatus `json:"status"`
}
type CoverageCounts struct {
	IndexedText    int64 `json:"indexed_text"`
	ShortText      int64 `json:"short_text"`
	TextIneligible int64 `json:"text_ineligible"`
	FallbackPath   int64 `json:"fallback_path"`
	FallbackSize   int64 `json:"fallback_size"`
	NonBlob        int64 `json:"non_blob"`
	Total          int64 `json:"total"`
}
type CoverageManifest struct {
	Version      string          `json:"version"`
	GenerationID string          `json:"generation_id"`
	CommitOID    string          `json:"commit_oid"`
	TreeOID      string          `json:"tree_oid"`
	Entries      []CoverageEntry `json:"entries"`
	Counts       CoverageCounts  `json:"counts"`
}

func validateEntry(e CoverageEntry) ([]byte, error) {
	p, x := e.Path.Bytes()
	if x != nil {
		return nil, x
	}
	if !lowerHex(e.ObjectOID, 40) {
		return nil, fail(InvalidCoverage, "object oid")
	}
	blob := e.ObjectType == "blob"
	if (e.Mode == "100644" || e.Mode == "100755" || e.Mode == "120000") != blob || (e.Mode == "040000" && e.ObjectType != "tree") || (e.Mode == "160000" && e.ObjectType != "commit") || (e.Mode != "040000" && e.Mode != "100644" && e.Mode != "100755" && e.Mode != "120000" && e.Mode != "160000") {
		return nil, fail(InvalidCoverage, "mode and object type")
	}
	if blob {
		if e.SizeBytes < 0 || e.Status == CoverageNonBlob {
			return nil, fail(InvalidCoverage, "blob entry")
		}
	} else if e.SizeBytes != 0 || e.Status != CoverageNonBlob {
		return nil, fail(InvalidCoverage, "non-blob entry")
	}
	if e.Status != CoverageIndexedText && e.Status != CoverageShortText && e.Status != CoverageTextIneligible && e.Status != CoverageFallbackPath && e.Status != CoverageFallbackSize && e.Status != CoverageNonBlob {
		return nil, fail(InvalidCoverage, "status")
	}
	return p, nil
}
func coverageCounts(entries []CoverageEntry) CoverageCounts {
	var c CoverageCounts
	for _, e := range entries {
		switch e.Status {
		case CoverageIndexedText:
			c.IndexedText++
		case CoverageShortText:
			c.ShortText++
		case CoverageTextIneligible:
			c.TextIneligible++
		case CoverageFallbackPath:
			c.FallbackPath++
		case CoverageFallbackSize:
			c.FallbackSize++
		case CoverageNonBlob:
			c.NonBlob++
		}
		c.Total++
	}
	return c
}
func normalizeEntries(entries []CoverageEntry) ([]CoverageEntry, error) {
	out := append([]CoverageEntry(nil), entries...)
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i].Path.Bytes()
		b, _ := out[j].Path.Bytes()
		return bytes.Compare(a, b) < 0
	})
	var prev []byte
	for i := range out {
		p, e := validateEntry(out[i])
		if e != nil {
			return nil, e
		}
		if i > 0 && bytes.Equal(prev, p) {
			return nil, fail(InvalidCoverage, "duplicate path")
		}
		prev = p
	}
	return out, nil
}
func NewCoverageManifest(id, commit, tree string, entries []CoverageEntry) (CoverageManifest, error) {
	if !lowerHex(id, 64) || !lowerHex(commit, 40) || !lowerHex(tree, 40) {
		return CoverageManifest{}, fail(InvalidIdentity, "coverage identity")
	}
	e, x := normalizeEntries(entries)
	if x != nil {
		return CoverageManifest{}, x
	}
	m := CoverageManifest{CoverageManifestVersion, id, commit, tree, e, coverageCounts(e)}
	return m, nil
}
func validateCoverage(m CoverageManifest, ordered bool) error {
	if m.Version != CoverageManifestVersion {
		return fail(UnsupportedVersion, "coverage version")
	}
	if !lowerHex(m.GenerationID, 64) || !lowerHex(m.CommitOID, 40) || !lowerHex(m.TreeOID, 40) {
		return fail(InvalidIdentity, "coverage identity")
	}
	e, x := normalizeEntries(m.Entries)
	if x != nil {
		return x
	}
	if ordered {
		for i := range e {
			a, _ := e[i].Path.Bytes()
			b, _ := m.Entries[i].Path.Bytes()
			if !bytes.Equal(a, b) {
				return fail(InvalidCoverage, "entries are not ordered")
			}
		}
	}
	if m.Counts != coverageCounts(e) {
		return fail(InvalidCoverage, "counts")
	}
	return nil
}
func MarshalCoverageManifest(m CoverageManifest) ([]byte, error) {
	if e := validateCoverage(m, false); e != nil {
		return nil, e
	}
	m.Entries, _ = normalizeEntries(m.Entries)
	return json.Marshal(m)
}
func ParseCoverageManifest(raw []byte) (CoverageManifest, error) {
	var m CoverageManifest
	if e := decode(raw, &m); e != nil {
		return m, e
	}
	if e := validateCoverage(m, true); e != nil {
		return m, e
	}
	b, e := MarshalCoverageManifest(m)
	if e != nil {
		return m, e
	}
	if !bytes.Equal(raw, b) {
		return m, fail(NoncanonicalEncoding, "coverage manifest")
	}
	return m, nil
}
func CoverageManifestSHA256(m CoverageManifest) (string, error) {
	b, e := MarshalCoverageManifest(m)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}

type ArtifactKind string

const (
	ArtifactCoverage      ArtifactKind = "coverage"
	ArtifactZoektShard    ArtifactKind = "zoekt_shard"
	ArtifactZoektMetadata ArtifactKind = "zoekt_metadata"
)

type ArtifactFile struct {
	Kind         ArtifactKind `json:"kind"`
	RelativePath string       `json:"relative_path"`
	SHA256       string       `json:"sha256"`
	SizeBytes    int64        `json:"size_bytes"`
}
type ArtifactManifest struct {
	Version      string         `json:"version"`
	GenerationID string         `json:"generation_id"`
	Files        []ArtifactFile `json:"files"`
}

func validRelative(p string) bool {
	if p == "" || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, x := range strings.Split(p, "/") {
		if x == "" || x == "." || x == ".." {
			return false
		}
	}
	return true
}
func normalizeFiles(files []ArtifactFile) ([]ArtifactFile, error) {
	o := append([]ArtifactFile(nil), files...)
	sort.Slice(o, func(i, j int) bool {
		if o[i].RelativePath == o[j].RelativePath {
			return o[i].Kind < o[j].Kind
		}
		return o[i].RelativePath < o[j].RelativePath
	})
	seen := map[string]bool{}
	for _, f := range o {
		if !validRelative(f.RelativePath) || !lowerHex(f.SHA256, 64) || f.SizeBytes < 0 || (f.Kind != ArtifactCoverage && f.Kind != ArtifactZoektShard && f.Kind != ArtifactZoektMetadata) || f.RelativePath == ArtifactManifestFileName || f.RelativePath == GenerationManifestFileName {
			return nil, fail(InvalidArtifact, "file")
		}
		if seen[f.RelativePath] {
			return nil, fail(InvalidArtifact, "duplicate path")
		}
		seen[f.RelativePath] = true
	}
	return o, nil
}
func NewArtifactManifest(id string, files []ArtifactFile) (ArtifactManifest, error) {
	if !lowerHex(id, 64) {
		return ArtifactManifest{}, fail(InvalidIdentity, "generation id")
	}
	f, e := normalizeFiles(files)
	if e != nil {
		return ArtifactManifest{}, e
	}
	return ArtifactManifest{ArtifactManifestVersion, id, f}, nil
}
func validateArtifact(m ArtifactManifest, ordered bool) error {
	if m.Version != ArtifactManifestVersion {
		return fail(UnsupportedVersion, "artifact version")
	}
	if !lowerHex(m.GenerationID, 64) {
		return fail(InvalidIdentity, "generation id")
	}
	f, e := normalizeFiles(m.Files)
	if e != nil {
		return e
	}
	if ordered {
		for i := range f {
			if f[i] != m.Files[i] {
				return fail(InvalidArtifact, "files are not ordered")
			}
		}
	}
	return nil
}
func MarshalArtifactManifest(m ArtifactManifest) ([]byte, error) {
	if e := validateArtifact(m, false); e != nil {
		return nil, e
	}
	m.Files, _ = normalizeFiles(m.Files)
	return json.Marshal(m)
}
func ParseArtifactManifest(raw []byte) (ArtifactManifest, error) {
	var m ArtifactManifest
	if e := decode(raw, &m); e != nil {
		return m, e
	}
	if e := validateArtifact(m, true); e != nil {
		return m, e
	}
	b, e := MarshalArtifactManifest(m)
	if e != nil {
		return m, e
	}
	if !bytes.Equal(raw, b) {
		return m, fail(NoncanonicalEncoding, "artifact manifest")
	}
	return m, nil
}
func ArtifactManifestSHA256(m ArtifactManifest) (string, error) {
	b, e := MarshalArtifactManifest(m)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}

type GenerationManifest struct {
	Version                string             `json:"version"`
	GenerationID           string             `json:"generation_id"`
	Identity               GenerationIdentity `json:"identity"`
	RepositoryName         string             `json:"repository_name"`
	BranchName             string             `json:"branch_name"`
	CoverageManifestSHA256 string             `json:"coverage_manifest_sha256"`
	ArtifactManifestSHA256 string             `json:"artifact_manifest_sha256"`
}

func NewGenerationManifest(i GenerationIdentity, coverage, artifact string) (GenerationManifest, error) {
	id, e := GenerationID(i)
	if e != nil {
		return GenerationManifest{}, e
	}
	r, _ := GenerationRepositoryName(id)
	m := GenerationManifest{GenerationManifestVersion, id, i, r, GenerationBranchName, coverage, artifact}
	return m, validateGeneration(m)
}
func validateGeneration(m GenerationManifest) error {
	if m.Version != GenerationManifestVersion {
		return fail(UnsupportedVersion, "generation manifest version")
	}
	id, e := GenerationID(m.Identity)
	if e != nil {
		return e
	}
	r, _ := GenerationRepositoryName(id)
	if m.GenerationID != id {
		return fail(DigestMismatch, "generation id")
	}
	if m.RepositoryName != r || m.BranchName != GenerationBranchName || !lowerHex(m.CoverageManifestSHA256, 64) || !lowerHex(m.ArtifactManifestSHA256, 64) {
		return fail(InvalidManifest, "generation manifest")
	}
	return nil
}
func MarshalGenerationManifest(m GenerationManifest) ([]byte, error) {
	if e := validateGeneration(m); e != nil {
		return nil, e
	}
	return json.Marshal(m)
}
func ParseGenerationManifest(raw []byte) (GenerationManifest, error) {
	var m GenerationManifest
	if e := decode(raw, &m); e != nil {
		return m, e
	}
	if e := validateGeneration(m); e != nil {
		return m, e
	}
	b, e := MarshalGenerationManifest(m)
	if e != nil {
		return m, e
	}
	if !bytes.Equal(raw, b) {
		return m, fail(NoncanonicalEncoding, "generation manifest")
	}
	return m, nil
}
func GenerationManifestSHA256(m GenerationManifest) (string, error) {
	b, e := MarshalGenerationManifest(m)
	if e != nil {
		return "", e
	}
	return digest(b), nil
}

func lowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func decode(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return fail(InvalidManifest, "json: %v", e)
	}
	var extra any
	if e := d.Decode(&extra); !errors.Is(e, io.EOF) {
		return fail(InvalidManifest, "trailing json")
	}
	return nil
}

func GenerationRelativeDirectory(id string) (string, error) {
	if !lowerHex(id, 64) {
		return "", fail(UnsafePath, "generation id")
	}
	return GenerationDirectoryName + "/" + id, nil
}
func StagingRelativeDirectory(id, nonce string) (string, error) {
	if !lowerHex(id, 64) || !lowerHex(nonce, 32) {
		return "", fail(UnsafePath, "generation id or nonce")
	}
	return StagingDirectoryName + "/" + id + "-" + nonce, nil
}
func GenerationDirectory(root, id string) (string, error) {
	rel, e := GenerationRelativeDirectory(id)
	if e != nil {
		return "", e
	}
	return safeStoragePath(root, rel)
}
func StagingDirectory(root, id, nonce string) (string, error) {
	rel, e := StagingRelativeDirectory(id, nonce)
	if e != nil {
		return "", e
	}
	return safeStoragePath(root, rel)
}
func safeStoragePath(root, rel string) (string, error) {
	canon, e := canonicalExisting(root)
	if e != nil {
		return "", e
	}
	info, e := os.Lstat(root)
	if e != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", fail(UnsafePath, "root")
	}
	out := filepath.Join(canon, filepath.FromSlash(rel))
	if !within(canon, out) {
		return "", fail(UnsafePath, "outside root")
	}
	cursor := canon
	for _, part := range strings.Split(rel, "/") {
		cursor = filepath.Join(cursor, part)
		if info, e := os.Lstat(cursor); e == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, x := filepath.EvalSymlinks(cursor)
			if x != nil || !within(canon, resolved) {
				return "", fail(UnsafePath, "symlink")
			}
		}
	}
	return out, nil
}

type ProtectedStorage struct {
	SourceVaultRoot       string
	WorkflowArtifactsRoot string
	WorkflowDatabasePath  string
	RepositoryRoots       []string
}

func ValidateIndexRoot(indexRoot string, p ProtectedStorage) error {
	index, e := canonicalPotential(indexRoot)
	if e != nil {
		return e
	}
	if info, e := os.Lstat(indexRoot); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return fail(UnsafePath, "index root symlink")
	}
	paths := []string{p.SourceVaultRoot, p.WorkflowArtifactsRoot, p.WorkflowDatabasePath}
	paths = append(paths, p.RepositoryRoots...)
	for _, v := range paths {
		if v == "" {
			continue
		}
		x, e := canonicalPotential(v)
		if e != nil {
			return e
		}
		if overlaps(index, x) {
			return fail(StorageOverlap, "index root overlaps protected storage")
		}
	}
	return nil
}
func canonicalExisting(p string) (string, error) {
	a, e := filepath.Abs(p)
	if e != nil {
		return "", fail(UnsafePath, "root")
	}
	r, e := filepath.EvalSymlinks(a)
	if e != nil {
		return "", fail(UnsafePath, "root")
	}
	return filepath.Clean(r), nil
}
func canonicalPotential(p string) (string, error) {
	a, e := filepath.Abs(p)
	if e != nil {
		return "", fail(UnsafePath, "path")
	}
	a = filepath.Clean(a)
	suffix := []string{}
	cursor := a
	for {
		if _, e := os.Lstat(cursor); e == nil {
			base, e := filepath.EvalSymlinks(cursor)
			if e != nil {
				return "", fail(UnsafePath, "symlink")
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				base = filepath.Join(base, suffix[i])
			}
			return filepath.Clean(base), nil
		}
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return "", fail(UnsafePath, "no existing ancestor")
		}
		suffix = append(suffix, filepath.Base(cursor))
		cursor = parent
	}
}
func within(root, p string) bool {
	r, e := filepath.Rel(root, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) && !filepath.IsAbs(r)
}
func overlaps(a, b string) bool { return within(a, b) || within(b, a) }
