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

// manifestLimitBytes bounds every manifest read from a bound descriptor.
const manifestLimitBytes int64 = 1 << 30

// dirHandle is an opaque directory descriptor owned by the platform dirFS.
type dirHandle int

// dirEntry describes one directory entry as seen without following symlinks.
type dirEntry struct {
	Name string
	Mode os.FileMode
}

// dirFS anchors every reader filesystem operation to open directory
// descriptors. The supported implementation refuses symlinks, traversal, and
// cross-filesystem components; unsupported platforms fail closed.
type dirFS interface {
	OpenRoot(path string) (dirHandle, indexer.FileIdentity, error)
	OpenChild(parent dirHandle, name string) (dirHandle, indexer.FileIdentity, error)
	Identity(dir dirHandle) (indexer.FileIdentity, error)
	ReadFile(dir dirHandle, name string, limit int64) ([]byte, indexer.FileIdentity, error)
	OpenFile(dir dirHandle, name string) (*os.File, indexer.FileIdentity, error)
	List(dir dirHandle) ([]dirEntry, error)
	Close(dir dirHandle) error
}

// The filesystem boundary is a package seam so deterministic tests can bind
// the reader to a fake descriptor space on every platform.
var boundFS dirFS = defaultDirFS()

var zoektSupported = zoektread.Supported

// verifyGenerationFiles is the shared verification boundary; the seam keeps
// deterministic tests able to complete reader opening without the pinned
// Zoekt builder on every platform.
var verifyGenerationFiles = indexer.VerifyGenerationFiles

type shard interface {
	Search(ctx context.Context, literal string, limit int) (zoektread.Result, error)
	Close() error
}

var openShard = func(f *os.File, generation string, sequence int, want zoektread.Metadata) (shard, error) {
	return zoektread.Open(f, generation, sequence, want)
}

type boundDirectory struct {
	handle   dirHandle
	identity indexer.FileIdentity
	name     string
}

func (d *boundDirectory) Close() error { return boundFS.Close(d.handle) }

// boundGenerationFiles reads one generation exclusively through its bound
// generation-directory descriptor.
type boundGenerationFiles struct {
	dir   *boundDirectory
	cache map[string][]byte
	ids   map[string]indexer.FileIdentity
}

func (b *boundGenerationFiles) ReadManifest(name string) ([]byte, indexer.FileIdentity, error) {
	if raw, ok := b.cache[name]; ok {
		return raw, b.ids[name], nil
	}
	if name != sourceindex.GenerationManifestFileName && name != sourceindex.CoverageManifestFileName && name != sourceindex.ArtifactManifestFileName {
		return nil, indexer.FileIdentity{}, errors.New("unexpected manifest")
	}
	raw, id, err := boundFS.ReadFile(b.dir.handle, name, manifestLimitBytes)
	if err != nil {
		return nil, indexer.FileIdentity{}, err
	}
	b.cache[name] = raw
	b.ids[name] = id
	return raw, id, nil
}

func (b *boundGenerationFiles) ListArtifacts() ([]indexer.OpenedArtifact, error) {
	raw, ok := b.cache[sourceindex.ArtifactManifestFileName]
	if !ok {
		return nil, errors.New("artifact manifest not read")
	}
	am, err := sourceindex.ParseArtifactManifest(raw)
	if err != nil {
		return nil, err
	}
	seenIdentity := make(map[indexer.FileIdentity]string, len(b.ids)+8)
	for name, id := range b.ids {
		seenIdentity[id] = name
	}
	var out []indexer.OpenedArtifact
	if cb, ok := b.cache[sourceindex.CoverageManifestFileName]; ok {
		out = append(out, indexer.OpenedArtifact{Kind: sourceindex.ArtifactCoverage, RelativePath: sourceindex.CoverageManifestFileName, SHA256: digest(cb), SizeBytes: int64(len(cb)), Identity: b.ids[sourceindex.CoverageManifestFileName]})
	}
	shardsDir, _, err := boundFS.OpenChild(b.dir.handle, sourceindex.ShardDirectoryName)
	if err != nil {
		return nil, err
	}
	var opened []*os.File
	shardsClosed := false
	closeFiles := func() error {
		var errs []error
		for _, f := range opened {
			errs = append(errs, f.Close())
		}
		return errors.Join(errs...)
	}
	closeAll := func() error {
		var errs []error
		errs = append(errs, closeFiles())
		if !shardsClosed {
			shardsClosed = true
			errs = append(errs, boundFS.Close(shardsDir))
		}
		return errors.Join(errs...)
	}
	fail := func(e error) ([]indexer.OpenedArtifact, error) {
		return nil, errors.Join(e, closeAll())
	}
	expected := make(map[string]bool)
	var expectedOrder []string
	shardNumber := 0
	for _, want := range am.Files {
		if want.Kind != sourceindex.ArtifactZoektShard {
			continue
		}
		seq := fmt.Sprintf("%06d.zoekt", shardNumber)
		shardNumber++
		if want.RelativePath != sourceindex.ShardDirectoryName+"/"+seq {
			return fail(errors.New("noncontiguous shard"))
		}
		expected[seq] = true
		expectedOrder = append(expectedOrder, seq)
	}
	record := func(file *os.File, id indexer.FileIdentity, rel string, want *sourceindex.ArtifactFile) (indexer.OpenedArtifact, error) {
		if prior, ok := seenIdentity[id]; ok {
			return indexer.OpenedArtifact{}, fmt.Errorf("artifact aliases %q", prior)
		}
		seenIdentity[id] = rel
		info, e := file.Stat()
		if e != nil {
			return indexer.OpenedArtifact{}, e
		}
		if _, e := file.Seek(0, 0); e != nil {
			return indexer.OpenedArtifact{}, e
		}
		h := sha256.New()
		if _, e := io.Copy(h, file); e != nil {
			return indexer.OpenedArtifact{}, e
		}
		if _, e := file.Seek(0, 0); e != nil {
			return indexer.OpenedArtifact{}, e
		}
		sha := hex.EncodeToString(h.Sum(nil))
		if want != nil && (info.Size() != want.SizeBytes || sha != want.SHA256) {
			return indexer.OpenedArtifact{}, errors.New("shard integrity")
		}
		return indexer.OpenedArtifact{Kind: sourceindex.ArtifactZoektShard, RelativePath: rel, SHA256: sha, SizeBytes: info.Size(), Identity: id, File: file}, nil
	}
	wantBySeq := make(map[string]*sourceindex.ArtifactFile, len(expectedOrder))
	for i := range am.Files {
		if am.Files[i].Kind == sourceindex.ArtifactZoektShard {
			wantBySeq[filepath.Base(am.Files[i].RelativePath)] = &am.Files[i]
		}
	}
	for _, seq := range expectedOrder {
		file, id, e := boundFS.OpenFile(shardsDir, seq)
		if e != nil {
			return fail(e)
		}
		opened = append(opened, file)
		o, e := record(file, id, sourceindex.ShardDirectoryName+"/"+seq, wantBySeq[seq])
		if e != nil {
			return fail(e)
		}
		out = append(out, o)
	}
	shardEntries, e := boundFS.List(shardsDir)
	if e != nil {
		return fail(e)
	}
	for _, entry := range shardEntries {
		if entry.Mode&os.ModeSymlink != 0 || !entry.Mode.IsRegular() {
			return fail(errors.New("unsafe shard entry"))
		}
		if entry.Mode.IsDir() {
			return fail(errors.New("unexpected directory"))
		}
		if expected[entry.Name] {
			continue
		}
		file, id, e := boundFS.OpenFile(shardsDir, entry.Name)
		if e != nil {
			return fail(e)
		}
		opened = append(opened, file)
		o, e := record(file, id, sourceindex.ShardDirectoryName+"/"+entry.Name, nil)
		if e != nil {
			return fail(e)
		}
		out = append(out, o)
	}
	if e := boundFS.Close(shardsDir); e != nil {
		shardsClosed = true
		return nil, errors.Join(e, closeFiles())
	}
	shardsClosed = true
	genEntries, e := boundFS.List(b.dir.handle)
	if e != nil {
		return fail(e)
	}
	shardsSeen := false
	for _, entry := range genEntries {
		if entry.Mode&os.ModeSymlink != 0 || !entry.Mode.IsRegular() && !entry.Mode.IsDir() {
			return fail(errors.New("unsafe entry"))
		}
		if entry.Mode.IsDir() {
			if entry.Name != sourceindex.ShardDirectoryName {
				return fail(errors.New("unexpected directory"))
			}
			if shardsSeen {
				return fail(errors.New("repeated shards entry"))
			}
			shardsSeen = true
			continue
		}
		if entry.Name == sourceindex.GenerationManifestFileName || entry.Name == sourceindex.ArtifactManifestFileName || entry.Name == sourceindex.CoverageManifestFileName {
			continue
		}
		file, id, e := boundFS.OpenFile(b.dir.handle, entry.Name)
		if e != nil {
			return fail(e)
		}
		opened = append(opened, file)
		o, e := record(file, id, entry.Name, nil)
		if e != nil {
			return fail(e)
		}
		out = append(out, o)
	}
	if !shardsSeen {
		return fail(errors.New("missing shards directory"))
	}
	return out, nil
}

type Reader struct {
	descriptor           Descriptor
	coverage             map[string]sourceindex.CoverageStatus
	fallback             [][]byte
	shards               []shard
	limit                int
	indexedDocumentCount int64
	dir                  *boundDirectory
	mu                   sync.Mutex
	closed               bool
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

// VerifyPublishedGeneration verifies one published generation through bound
// descriptors and closes every descriptor opened by verification before it
// returns. The returned descriptor is derived from the verified manifests.
func VerifyPublishedGeneration(ctx context.Context, config Config, identity sourceindex.GenerationIdentity) (descriptor Descriptor, err error) {
	if err := ctx.Err(); err != nil {
		return Descriptor{}, err
	}
	if config.IndexRoot == "" || !filepath.IsAbs(config.IndexRoot) || filepath.Clean(config.IndexRoot) != config.IndexRoot {
		return Descriptor{}, invalidConfig()
	}
	if _, err := sourceindex.GenerationID(identity); err != nil {
		return Descriptor{}, fmt.Errorf("%w: identity", ErrInvalidConfiguration)
	}
	wantOptions, _ := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if identity.BuildOptionsSHA256 != wantOptions {
		return Descriptor{}, fmt.Errorf("%w: build options", ErrInvalidConfiguration)
	}
	if err := sourceindex.ValidateIndexRoot(config.IndexRoot, config.ProtectedStorage); err != nil {
		return Descriptor{}, fmt.Errorf("%w: index root", ErrInvalidConfiguration)
	}
	if !zoektSupported() {
		return Descriptor{}, ErrUnsupportedPlatform
	}
	id, _ := sourceindex.GenerationID(identity)
	dir, err := anchorDirectory(config, id)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%w: directory", ErrGenerationIntegrity)
	}
	files := &boundGenerationFiles{dir: dir, cache: map[string][]byte{}, ids: map[string]indexer.FileIdentity{}}
	verified, err := verifyGenerationFiles(files, indexerprotocol.BuildRequest{
		GenerationID: id,
		Identity:     identity,
		BuildOptions: sourceindex.DefaultBuildOptions(),
	})
	if err != nil {
		_ = dir.Close()
		return Descriptor{}, fmt.Errorf("%w: artifacts", ErrGenerationIntegrity)
	}
	if err := ctx.Err(); err != nil {
		_ = indexer.CloseArtifacts(verified.Opened)
		_ = dir.Close()
		return Descriptor{}, err
	}
	defer func() {
		err = errors.Join(err, indexer.CloseArtifacts(verified.Opened), dir.Close())
	}()
	if verified.Generation.GenerationID != id || verified.Generation.Identity != identity {
		return Descriptor{}, ErrGenerationIntegrity
	}
	return Descriptor{
		GenerationID:             id,
		Identity:                 identity,
		GenerationManifestSHA256: verified.GenerationRawSHA256,
		CoverageManifestSHA256:   verified.CoverageRawSHA256,
		ArtifactManifestSHA256:   verified.ArtifactRawSHA256,
	}, nil
}

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
	if !zoektSupported() {
		return nil, ErrUnsupportedPlatform
	}
	row, err := store.GetSourceIndexGenerationByIdentity(ctx, identity)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		switch {
		case errors.Is(err, workflow.ErrInvalidSourceIndexGeneration), errors.Is(err, workflow.ErrSourceIndexGenerationIntegrity):
			return nil, fmt.Errorf("%w: generation", ErrGenerationIntegrity)
		case errors.Is(err, workflow.ErrSourceIndexGenerationNotFound):
			return nil, fmt.Errorf("%w: generation", ErrGenerationUnavailable)
		default:
			return nil, err
		}
	}
	id, _ := sourceindex.GenerationID(identity)
	if row.Identity != identity || row.GenerationID != id || row.State != workflow.SourceIndexGenerationReady || row.FailureCode != "" || row.FailureMessage != "" || !validDigest(row.GenerationManifestSHA256) || !validDigest(row.CoverageManifestSHA256) || !validDigest(row.ArtifactManifestSHA256) {
		return nil, ErrGenerationUnavailable
	}
	dir, err := anchorDirectory(config, row.GenerationID)
	if err != nil {
		return nil, fmt.Errorf("%w: directory", ErrGenerationIntegrity)
	}
	files := &boundGenerationFiles{dir: dir, cache: map[string][]byte{}, ids: map[string]indexer.FileIdentity{}}
	gm, _, err := files.ReadManifest(sourceindex.GenerationManifestFileName)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	g, err := sourceindex.ParseGenerationManifest(gm)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	cm, _, err := files.ReadManifest(sourceindex.CoverageManifestFileName)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	c, err := sourceindex.ParseCoverageManifest(cm)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	am, _, err := files.ReadManifest(sourceindex.ArtifactManifestFileName)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	a, err := sourceindex.ParseArtifactManifest(am)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: manifests", ErrGenerationIntegrity)
	}
	gd, _ := sourceindex.GenerationManifestSHA256(g)
	cd, _ := sourceindex.CoverageManifestSHA256(c)
	ad, _ := sourceindex.ArtifactManifestSHA256(a)
	if g.GenerationID != id || g.Identity != identity || c.GenerationID != id || c.CommitOID != identity.CommitOID || c.TreeOID != identity.TreeOID || a.GenerationID != id || g.CoverageManifestSHA256 != cd || g.ArtifactManifestSHA256 != ad || gd != row.GenerationManifestSHA256 || cd != row.CoverageManifestSHA256 || ad != row.ArtifactManifestSHA256 {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	var shardCount int64
	for _, f := range a.Files {
		if f.Kind == sourceindex.ArtifactZoektShard {
			shardCount++
		}
	}
	req := indexerprotocol.BuildRequest{GenerationID: id, Identity: identity, BuildOptions: sourceindex.DefaultBuildOptions()}
	verified, err := verifyGenerationFiles(files, req)
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("%w: artifacts", ErrGenerationIntegrity)
	}
	consumed := false
	defer func() {
		if !consumed {
			for _, o := range verified.Opened {
				if o.File != nil {
					_ = o.File.Close()
				}
			}
		}
	}()
	if verified.ShardCount != shardCount {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	if verified.GenerationRawSHA256 != row.GenerationManifestSHA256 || verified.CoverageRawSHA256 != row.CoverageManifestSHA256 || verified.ArtifactRawSHA256 != row.ArtifactManifestSHA256 {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	again, err := store.GetSourceIndexGenerationByIdentity(ctx, identity)
	if err != nil {
		_ = dir.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		switch {
		case errors.Is(err, workflow.ErrInvalidSourceIndexGeneration), errors.Is(err, workflow.ErrSourceIndexGenerationIntegrity):
			return nil, fmt.Errorf("%w: generation", ErrGenerationIntegrity)
		case errors.Is(err, workflow.ErrSourceIndexGenerationNotFound):
			return nil, fmt.Errorf("%w: generation", ErrGenerationUnavailable)
		default:
			return nil, err
		}
	}
	if again.State != workflow.SourceIndexGenerationReady || again.Identity != identity || again.GenerationID != id || again.GenerationManifestSHA256 != row.GenerationManifestSHA256 || again.CoverageManifestSHA256 != row.CoverageManifestSHA256 || again.ArtifactManifestSHA256 != row.ArtifactManifestSHA256 {
		_ = dir.Close()
		return nil, ErrGenerationUnavailable
	}
	if againID, err := boundFS.Identity(dir.handle); err != nil || againID != dir.identity {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	shardArtifacts := make([]indexer.OpenedArtifact, 0, shardCount)
	for _, o := range verified.Opened {
		if o.Kind == sourceindex.ArtifactZoektShard {
			shardArtifacts = append(shardArtifacts, o)
		}
	}
	if int64(len(shardArtifacts)) != shardCount {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	for i, o := range shardArtifacts {
		if o.RelativePath != fmt.Sprintf("shards/%06d.zoekt", i) {
			_ = dir.Close()
			return nil, ErrGenerationIntegrity
		}
	}
	limit, err := searchLimit(c.Counts.IndexedText)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	coverage, err := coverageMap(c)
	if err != nil {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	fallback, err := fallbackPaths(c)
	if err != nil {
		_ = dir.Close()
		return nil, ErrGenerationIntegrity
	}
	r := &Reader{descriptor: Descriptor{id, identity, gd, cd, ad}, coverage: coverage, fallback: fallback, limit: limit, indexedDocumentCount: c.Counts.IndexedText, dir: dir}
	meta := zoektread.Metadata{RepositoryName: g.RepositoryName, Branch: g.BranchName, Version: identity.CommitOID, IndexOptions: identity.BuildOptionsSHA256, Values: map[string]string{"relay_generation_id": id, "relay_vault_id": identity.VaultID, "relay_commit_oid": identity.CommitOID, "relay_tree_oid": identity.TreeOID, "relay_engine_revision": identity.EngineRevision, "relay_build_contract_version": identity.BuildContractVersion, "relay_build_options_sha256": identity.BuildOptionsSHA256}}
	for i, artifact := range shardArtifacts {
		z, e := openShard(artifact.File, id, i, meta)
		if e != nil {
			_ = r.Close()
			if errors.Is(e, zoektread.ErrUnsupported) {
				return nil, ErrUnsupportedPlatform
			}
			return nil, fmt.Errorf("%w: shard", ErrGenerationIntegrity)
		}
		r.shards = append(r.shards, z)
	}
	consumed = true
	return r, nil
}

// searchLimit performs the checked conversion of the exact verified
// indexed-document count to the explicit positive Zoekt search limit.
func searchLimit(indexed int64) (int, error) {
	if indexed < 0 {
		return 0, ErrGenerationIntegrity
	}
	n, err := checkedInt(uint64(indexed))
	if err != nil {
		return 0, ErrGenerationIntegrity
	}
	if n < 1 {
		n = 1
	}
	return n, nil
}

func checkedInt(v uint64) (int, error) {
	if v > uint64(^uint(0)>>1) {
		return 0, ErrGenerationIntegrity
	}
	return int(v), nil
}

func anchorDirectory(config Config, generationID string) (*boundDirectory, error) {
	root, _, err := boundFS.OpenRoot(config.IndexRoot)
	if err != nil {
		return nil, err
	}
	gens, _, err := boundFS.OpenChild(root, sourceindex.GenerationDirectoryName)
	if err != nil {
		_ = boundFS.Close(root)
		return nil, err
	}
	gen, id, err := boundFS.OpenChild(gens, generationID)
	if err != nil {
		_ = boundFS.Close(gens)
		_ = boundFS.Close(root)
		return nil, err
	}
	if err := boundFS.Close(gens); err != nil {
		_ = boundFS.Close(gen)
		_ = boundFS.Close(root)
		return nil, err
	}
	if err := boundFS.Close(root); err != nil {
		_ = boundFS.Close(gen)
		return nil, err
	}
	return &boundDirectory{handle: gen, identity: id, name: sourceindex.GenerationDirectoryName + "/" + generationID}, nil
}

func coverageMap(c sourceindex.CoverageManifest) (map[string]sourceindex.CoverageStatus, error) {
	out := make(map[string]sourceindex.CoverageStatus, len(c.Entries))
	for _, e := range c.Entries {
		p, err := e.Path.Bytes()
		if err != nil {
			return nil, err
		}
		out[string(p)] = e.Status
	}
	return out, nil
}

func fallbackPaths(c sourceindex.CoverageManifest) ([][]byte, error) {
	var out [][]byte
	for _, e := range c.Entries {
		if e.Status != sourceindex.CoverageFallbackPath && e.Status != sourceindex.CoverageFallbackSize {
			continue
		}
		p, err := e.Path.Bytes()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i], out[j]) < 0 })
	return out, nil
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	if literal == "" || !utf8.ValidString(literal) || utf8.RuneCountInString(literal) < 3 {
		return nil, ErrQueryIneligible
	}
	seen := map[string]bool{}
	for _, s := range r.shards {
		x, e := s.Search(ctx, literal, r.limit)
		if e != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, ErrQueryIncomplete
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if e := r.validateResult(x); e != nil {
			return nil, e
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
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
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

// validateResult classifies one shard's completion evidence, failing closed on
// crashes, skipped files or shards, limit or size flushes, inconsistent
// statistics, and results beyond the verified corpus bound. Trigram-filter
// skips are an ordinary negative-query outcome.
func (r *Reader) validateResult(x zoektread.Result) error {
	if r.indexedDocumentCount == 0 && len(x.Matches) > 0 {
		return ErrGenerationIntegrity
	}
	if int64(len(x.Matches)) > r.indexedDocumentCount {
		return ErrQueryIncomplete
	}
	if x.Crashes > 0 || x.FilesSkipped > 0 || x.ShardsSkipped > 0 {
		return ErrQueryIncomplete
	}
	switch x.FlushReason {
	case zoektread.FlushReasonNone, zoektread.FlushReasonFinalFlush:
	default:
		return ErrQueryIncomplete
	}
	if x.FileCount != len(x.Matches) || x.MatchCount != len(x.Matches) {
		return ErrQueryIncomplete
	}
	if x.ShardsSkippedFilter > 0 && len(x.Matches) > 0 {
		return ErrQueryIncomplete
	}
	return nil
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
	var errs []error
	for _, s := range r.shards {
		if e := s.Close(); e != nil {
			errs = append(errs, e)
		}
	}
	if r.dir != nil {
		if e := r.dir.Close(); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
