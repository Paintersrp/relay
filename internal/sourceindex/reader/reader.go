// Package reader opens and queries one verified, ready source-index generation.
package reader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"unicode/utf8"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/indexerprotocol"
	"relay/internal/sourceindex/zoektread"
	workflow "relay/internal/store/workflow"
)

var (
	ErrInvalidConfiguration  = errors.New("invalid source-index reader configuration")
	ErrGenerationUnavailable = errors.New("source-index generation unavailable")
	ErrGenerationIntegrity   = errors.New("source-index generation integrity failure")
	ErrQueryIneligible       = errors.New("source-index query ineligible")
	ErrQueryIncomplete       = errors.New("source-index query incomplete")
	ErrClosed                = errors.New("source-index reader closed")
	ErrUnsupportedPlatform   = errors.New("source-index reading unsupported on this platform")
)

type GenerationStore interface {
	GetSourceIndexGenerationByIdentity(context.Context, sourceindex.GenerationIdentity) (workflow.SourceIndexGeneration, error)
}
type Config struct {
	IndexRoot        string
	ProtectedStorage sourceindex.ProtectedStorage
}
type Descriptor struct {
	GenerationID                                                             string
	Identity                                                                 sourceindex.GenerationIdentity
	GenerationManifestSHA256, CoverageManifestSHA256, ArtifactManifestSHA256 string
}
type Candidate struct{ Path []byte }
type Reader struct {
	descriptor Descriptor
	coverage   map[string]sourceindex.CoverageStatus
	fallback   [][]byte
	shards     []*zoektread.Reader
	limit      int
	mu         sync.Mutex
	closed     bool
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func invalidConfig() error   { return ErrInvalidConfiguration }

func Open(ctx context.Context, store GenerationStore, config Config, identity sourceindex.GenerationIdentity) (*Reader, error) {
	if store == nil || config.IndexRoot == "" || !filepath.IsAbs(config.IndexRoot) || filepath.Clean(config.IndexRoot) != config.IndexRoot {
		return nil, invalidConfig()
	}
	if _, err := sourceindex.GenerationID(identity); err != nil {
		return nil, fmt.Errorf("%w: identity", ErrInvalidConfiguration)
	}
	wantOptions, _ := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if identity.BuildOptionsSHA256 != wantOptions {
		return nil, fmt.Errorf("%w: build options", ErrInvalidConfiguration)
	}
	if err := sourceindex.ValidateIndexRoot(config.IndexRoot, config.ProtectedStorage); err != nil {
		return nil, fmt.Errorf("%w: index root", ErrInvalidConfiguration)
	}
	if !zoektread.Supported() {
		return nil, ErrUnsupportedPlatform
	}
	g, err := store.GetSourceIndexGenerationByIdentity(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("%w: generation", ErrGenerationUnavailable)
	}
	id, _ := sourceindex.GenerationID(identity)
	if g.Identity != identity || g.GenerationID != id || g.State != workflow.SourceIndexGenerationReady || g.FailureCode != "" || g.FailureMessage != "" || !validDigest(g.GenerationManifestSHA256) || !validDigest(g.CoverageManifestSHA256) || !validDigest(g.ArtifactManifestSHA256) {
		return nil, ErrGenerationUnavailable
	}
	root, err := sourceindex.GenerationDirectory(config.IndexRoot, g.GenerationID)
	if err != nil {
		return nil, fmt.Errorf("%w: generation directory", ErrGenerationIntegrity)
	}
	if err := safeDirs(config.IndexRoot, root); err != nil {
		return nil, fmt.Errorf("%w: directory", ErrGenerationIntegrity)
	}
	gm, cm, am, err := manifests(root)
	if err != nil {
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	gd, cd, ad := mustDigest(gm), mustDigest(cm), mustDigest(am)
	if gm.GenerationID != id || gm.Identity != identity || cm.GenerationID != id || cm.CommitOID != identity.CommitOID || cm.TreeOID != identity.TreeOID || am.GenerationID != id || gm.CoverageManifestSHA256 != cd || gm.ArtifactManifestSHA256 != ad || gd != g.GenerationManifestSHA256 || cd != g.CoverageManifestSHA256 || ad != g.ArtifactManifestSHA256 {
		return nil, ErrGenerationIntegrity
	}
	var shardCount int64
	for _, f := range am.Files {
		if f.Kind == sourceindex.ArtifactZoektShard {
			shardCount++
		}
	}
	req := indexerprotocol.BuildRequest{GenerationID: id, Identity: identity, BuildOptions: sourceindex.DefaultBuildOptions()}
	if err := indexer.Verify(root, req, shardCount); err != nil {
		return nil, fmt.Errorf("%w: artifacts", ErrGenerationIntegrity)
	}
	if cm.Counts.IndexedText > int64(^uint(0)>>1) {
		return nil, ErrGenerationIntegrity
	}
	r := &Reader{descriptor: Descriptor{id, identity, gd, cd, ad}, coverage: map[string]sourceindex.CoverageStatus{}, limit: int(cm.Counts.IndexedText)}
	for _, e := range cm.Entries {
		p, _ := e.Path.Bytes()
		r.coverage[string(p)] = e.Status
		if e.Status == sourceindex.CoverageFallbackPath || e.Status == sourceindex.CoverageFallbackSize {
			r.fallback = append(r.fallback, p)
		}
	}
	sort.Slice(r.fallback, func(i, j int) bool { return bytes.Compare(r.fallback[i], r.fallback[j]) < 0 })
	meta := zoektread.Metadata{RepositoryName: gm.RepositoryName, Branch: gm.BranchName, Version: identity.CommitOID, IndexOptions: identity.BuildOptionsSHA256, Values: map[string]string{"relay_generation_id": id, "relay_vault_id": identity.VaultID, "relay_commit_oid": identity.CommitOID, "relay_tree_oid": identity.TreeOID, "relay_engine_revision": identity.EngineRevision, "relay_build_contract_version": identity.BuildContractVersion, "relay_build_options_sha256": identity.BuildOptionsSHA256}}
	for _, f := range am.Files {
		if f.Kind != sourceindex.ArtifactZoektShard {
			continue
		}
		expected := fmt.Sprintf("shards/%06d.zoekt", len(r.shards))
		if f.RelativePath != expected {
			r.Close()
			return nil, ErrGenerationIntegrity
		}
		file, e := openShard(root, f)
		if e != nil {
			r.Close()
			return nil, fmt.Errorf("%w: shard", ErrGenerationIntegrity)
		}
		z, e := zoektread.Open(file, id, len(r.shards), meta)
		if e != nil {
			_ = file.Close()
			r.Close()
			if errors.Is(e, zoektread.ErrUnsupported) {
				return nil, ErrUnsupportedPlatform
			}
			return nil, ErrGenerationIntegrity
		}
		r.shards = append(r.shards, z)
	}
	return r, nil
}
func safeDirs(root, generation string) error {
	for _, p := range []string{root, filepath.Join(root, sourceindex.GenerationDirectoryName), generation} {
		i, e := os.Lstat(p)
		if e != nil || i.Mode()&os.ModeSymlink != 0 || !i.IsDir() {
			return os.ErrInvalid
		}
	}
	return nil
}
func manifests(root string) (sourceindex.GenerationManifest, sourceindex.CoverageManifest, sourceindex.ArtifactManifest, error) {
	var z sourceindex.GenerationManifest
	var c sourceindex.CoverageManifest
	var a sourceindex.ArtifactManifest
	read := func(n string) ([]byte, error) {
		p := filepath.Join(root, n)
		i, e := os.Lstat(p)
		if e != nil || i.Mode()&os.ModeSymlink != 0 || !i.Mode().IsRegular() {
			return nil, os.ErrInvalid
		}
		return os.ReadFile(p)
	}
	b, e := read(sourceindex.GenerationManifestFileName)
	if e != nil {
		return z, c, a, e
	}
	z, e = sourceindex.ParseGenerationManifest(b)
	if e != nil {
		return z, c, a, e
	}
	b, e = read(sourceindex.CoverageManifestFileName)
	if e != nil {
		return z, c, a, e
	}
	c, e = sourceindex.ParseCoverageManifest(b)
	if e != nil {
		return z, c, a, e
	}
	b, e = read(sourceindex.ArtifactManifestFileName)
	if e != nil {
		return z, c, a, e
	}
	a, e = sourceindex.ParseArtifactManifest(b)
	return z, c, a, e
}
func mustDigest(v any) string {
	switch x := v.(type) {
	case sourceindex.GenerationManifest:
		d, _ := sourceindex.GenerationManifestSHA256(x)
		return d
	case sourceindex.CoverageManifest:
		d, _ := sourceindex.CoverageManifestSHA256(x)
		return d
	case sourceindex.ArtifactManifest:
		d, _ := sourceindex.ArtifactManifestSHA256(x)
		return d
	}
	return ""
}
func openShard(root string, want sourceindex.ArtifactFile) (*os.File, error) {
	p := filepath.Join(root, filepath.FromSlash(want.RelativePath))
	if rel, e := filepath.Rel(root, p); e != nil || rel == ".." || filepath.IsAbs(rel) {
		return nil, os.ErrInvalid
	}
	i, e := os.Lstat(p)
	if e != nil || i.Mode()&os.ModeSymlink != 0 || !i.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	fi, e := f.Stat()
	if e != nil || !os.SameFile(i, fi) || fi.Size() != want.SizeBytes {
		f.Close()
		return nil, os.ErrInvalid
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		f.Close()
		return nil, os.ErrInvalid
	}
	if _, e = f.Seek(0, 0); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func (r *Reader) Descriptor() Descriptor { return r.descriptor }
func (r *Reader) FallbackCandidates() []Candidate {
	out := make([]Candidate, len(r.fallback))
	for i, p := range r.fallback {
		out[i].Path = append([]byte(nil), p...)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Path, out[j].Path) < 0 })
	return out
}
func (r *Reader) IndexedTextCandidates(ctx context.Context, literal string) ([]Candidate, error) {
	if literal == "" || !utf8.ValidString(literal) || utf8.RuneCountInString(literal) < 3 {
		return nil, ErrQueryIneligible
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	seen := map[string]bool{}
	for _, s := range r.shards {
		x, e := s.Search(ctx, literal, r.limit)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrQueryIncomplete
		}
		if x.Crashes > 0 || x.FilesSkipped > 0 || x.ShardsSkipped > 0 || x.Flush || len(x.Matches) > r.limit {
			return nil, ErrQueryIncomplete
		}
		for _, m := range x.Matches {
			p := []byte(m.FileName)
			if !utf8.ValidString(m.FileName) {
				return nil, ErrGenerationIntegrity
			}
			if _, e := sourceindex.NewPathIdentity(p); e != nil || r.coverage[string(p)] != sourceindex.CoverageIndexedText || seen[string(p)] || m.Repository != r.descriptor.IdentityStringRepository() || m.Version != r.descriptor.Identity.CommitOID || len(m.Branches) != 1 || m.Branches[0] != sourceindex.GenerationBranchName {
				return nil, ErrGenerationIntegrity
			}
			seen[string(p)] = true
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	paths := make([][]byte, 0, len(seen))
	for p := range seen {
		paths = append(paths, []byte(p))
	}
	sort.Slice(paths, func(i, j int) bool { return bytes.Compare(paths[i], paths[j]) < 0 })
	out := make([]Candidate, len(paths))
	for i, p := range paths {
		out[i].Path = append([]byte(nil), p...)
	}
	return out, nil
}
func (d Descriptor) IdentityStringRepository() string {
	x, _ := sourceindex.GenerationRepositoryName(d.GenerationID)
	return x
}
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	for _, s := range r.shards {
		s.Close()
	}
	return nil
}
