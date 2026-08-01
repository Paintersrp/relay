package reader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexer"
	"relay/internal/sourceindex/indexerprotocol"
	"relay/internal/sourceindex/zoektread"
	workflow "relay/internal/store/workflow"
)

const (
	testOID   = "0123456789abcdef0123456789abcdef01234567"
	testTree  = "89abcdef0123456789abcdef0123456789abcdef"
	testOID2  = "fedcba9876543210fedcba9876543210fedcba98"
	testTree2 = "0123456789abcdef0123456789abcdef01234567"
)

func testIdentity(t *testing.T, vault string) (sourceindex.GenerationIdentity, string) {
	t.Helper()
	options, err := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity(vault, testOID, testTree, options)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return identity, id
}

// fakeStore serves one persisted row and records cancellation.
type fakeStore struct {
	row workflow.SourceIndexGeneration
	err error
}

func (s *fakeStore) GetSourceIndexGenerationByIdentity(ctx context.Context, _ sourceindex.GenerationIdentity) (workflow.SourceIndexGeneration, error) {
	if err := ctx.Err(); err != nil {
		return workflow.SourceIndexGeneration{}, err
	}
	if s.err != nil {
		return workflow.SourceIndexGeneration{}, s.err
	}
	return s.row, nil
}

// fakeFS is an in-memory descriptor space that enforces the same anchored
// semantics as the platform implementation: symlinks and cross-filesystem
// components fail, and opened handles stay bound to their original objects.
type fakeFS struct {
	t           *testing.T
	tmp         string
	nextHandle  int
	seq         uint64
	handles     map[int]*fakeDir
	closed      map[int]bool
	roots       map[string]*fakeEntry
	filesOpened int
	filesClosed int
	closedOrder []int
}

type fakeDir struct {
	device   uint64
	entries  map[string]*fakeEntry
	identity indexer.FileIdentity
}

type fakeFile struct {
	content   []byte
	identity  indexer.FileIdentity
	oversized bool
}

type fakeEntry struct {
	symlink bool
	dir     *fakeDir
	file    *fakeFile
}

func newFakeFS(t *testing.T) *fakeFS {
	return &fakeFS{t: t, tmp: t.TempDir(), handles: map[int]*fakeDir{}, closed: map[int]bool{}, roots: map[string]*fakeEntry{}}
}

func (fs *fakeFS) identity() indexer.FileIdentity {
	fs.seq++
	return indexer.FileIdentity{Device: 1, Inode: fs.seq}
}

func (fs *fakeFS) newDir(device uint64) *fakeDir {
	if device == 0 {
		device = 1
	}
	return &fakeDir{device: device, entries: map[string]*fakeEntry{}, identity: fs.identity()}
}

func (fs *fakeFS) registerRoot(path string, entry *fakeEntry) {
	fs.roots[filepath.Clean(path)] = entry
}

func (fs *fakeFS) mkdir(path string) {
	fs.mkdirDevice(path, 0)
}

func (fs *fakeFS) mkdirDevice(path string, device uint64) {
	clean := filepath.Clean(path)
	if _, ok := fs.roots[clean]; ok {
		fs.t.Fatalf("mkdir %s: already a root", path)
	}
	for root, rootEntry := range fs.roots {
		if rootEntry.symlink || rootEntry.file != nil {
			continue
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		cursor := rootEntry.dir
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			child, ok := cursor.entries[part]
			if !ok {
				child = &fakeEntry{dir: fs.newDir(device)}
				cursor.entries[part] = child
			} else if child.dir == nil {
				fs.t.Fatalf("mkdir %s: component %q is not a directory", path, part)
			}
			cursor = child.dir
		}
		return
	}
	fs.roots[clean] = &fakeEntry{dir: fs.newDir(device)}
}

func (fs *fakeFS) writeFile(path string, content []byte) {
	file := &fakeFile{content: append([]byte(nil), content...), identity: fs.identity()}
	fs.registerEntry(path, &fakeEntry{file: file})
}

func (fs *fakeFS) oversizedFile(path string) {
	file := &fakeFile{content: []byte("x"), identity: fs.identity(), oversized: true}
	fs.registerEntry(path, &fakeEntry{file: file})
}

func (fs *fakeFS) symlinkEntry(path string) {
	clean := filepath.Clean(path)
	if _, ok := fs.roots[clean]; ok {
		fs.roots[clean] = &fakeEntry{symlink: true}
		return
	}
	if dir, name, err := fs.split(clean); err == nil {
		dir.entries[name] = &fakeEntry{symlink: true}
		return
	}
	fs.roots[clean] = &fakeEntry{symlink: true}
}

func (fs *fakeFS) hardlink(dst, src string) {
	srcEntry, err := fs.lookup(filepath.Clean(src))
	if err != nil {
		fs.t.Fatalf("hardlink source %s: %v", src, err)
	}
	if srcEntry.file == nil {
		fs.t.Fatalf("hardlink source %s is not a file", src)
	}
	clean := filepath.Clean(dst)
	if _, ok := fs.roots[clean]; ok {
		fs.roots[clean] = &fakeEntry{file: srcEntry.file}
		return
	}
	dir, name, err := fs.split(clean)
	if err != nil {
		fs.t.Fatalf("hardlink target %s: %v", dst, err)
	}
	dir.entries[name] = &fakeEntry{file: srcEntry.file}
}

// replaceFile rewrites one file's content while retaining its identity, as a
// replacement in place would after the descriptor was opened.
func (fs *fakeFS) replaceFile(path string, content []byte) {
	entry, err := fs.lookup(filepath.Clean(path))
	if err != nil {
		fs.t.Fatalf("replace %s: %v", path, err)
	}
	if entry.file == nil {
		fs.t.Fatalf("replace %s is not a file", path)
	}
	entry.file.content = append([]byte(nil), content...)
}

func (fs *fakeFS) remove(path string) {
	clean := filepath.Clean(path)
	if _, ok := fs.roots[clean]; ok {
		delete(fs.roots, clean)
		return
	}
	dir, name, err := fs.split(clean)
	if err != nil {
		fs.t.Fatalf("remove %s: %v", path, err)
	}
	delete(dir.entries, name)
}

// replaceDir swaps the directory reachable at path with a new one; handles
// opened before the swap remain bound to the original directory.
func (fs *fakeFS) replaceDir(path string, dir *fakeDir) {
	entry, err := fs.lookup(filepath.Clean(path))
	if err != nil {
		fs.t.Fatalf("replace dir %s: %v", path, err)
	}
	if entry.dir == nil {
		fs.t.Fatalf("replace dir %s is not a directory", path)
	}
	entry.dir = dir
}

func (fs *fakeFS) registerEntry(path string, entry *fakeEntry) {
	clean := filepath.Clean(path)
	if _, ok := fs.roots[clean]; ok {
		fs.t.Fatalf("root %s already registered", clean)
	}
	if _, err := fs.lookup(clean); err == nil {
		fs.t.Fatalf("path %s already exists", clean)
	}
	dir, name, err := fs.split(clean)
	if err != nil {
		fs.t.Fatalf("register %s: %v", path, err)
	}
	dir.entries[name] = entry
}

func (fs *fakeFS) lookup(path string) (*fakeEntry, error) {
	root, ok := fs.roots[filepath.Clean(path)]
	if ok {
		return root, nil
	}
	dir, name, err := fs.split(path)
	if err != nil {
		return nil, err
	}
	entry, ok := dir.entries[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entry, nil
}

func (fs *fakeFS) split(path string) (*fakeDir, string, error) {
	clean := filepath.Clean(path)
	for root, entry := range fs.roots {
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if entry.symlink || entry.file != nil {
			return nil, "", errors.New("not a directory")
		}
		cursor := entry.dir
		parts := strings.Split(rel, string(filepath.Separator))
		for _, part := range parts[:len(parts)-1] {
			child, ok := cursor.entries[part]
			if !ok {
				return nil, "", os.ErrNotExist
			}
			if child.symlink || child.file != nil || child.dir == nil {
				return nil, "", errors.New("not a directory")
			}
			cursor = child.dir
		}
		return cursor, parts[len(parts)-1], nil
	}
	return nil, "", os.ErrNotExist
}

func (fs *fakeFS) handleFor(dir *fakeDir) int {
	fs.nextHandle++
	fs.handles[fs.nextHandle] = dir
	return fs.nextHandle
}

func (fs *fakeFS) OpenRoot(path string) (dirHandle, indexer.FileIdentity, error) {
	entry, ok := fs.roots[filepath.Clean(path)]
	if !ok {
		return 0, indexer.FileIdentity{}, os.ErrNotExist
	}
	if entry.symlink {
		return 0, indexer.FileIdentity{}, errors.New("symlink root")
	}
	if entry.file != nil {
		return 0, indexer.FileIdentity{}, errors.New("not a directory")
	}
	return dirHandle(fs.handleFor(entry.dir)), entry.dir.identity, nil
}

func (fs *fakeFS) OpenChild(parent dirHandle, name string) (dirHandle, indexer.FileIdentity, error) {
	dir, ok := fs.handles[int(parent)]
	if !ok || fs.closed[int(parent)] {
		return 0, indexer.FileIdentity{}, os.ErrInvalid
	}
	entry, ok := dir.entries[name]
	if !ok {
		return 0, indexer.FileIdentity{}, os.ErrNotExist
	}
	if entry.symlink {
		return 0, indexer.FileIdentity{}, errors.New("symlink component")
	}
	if entry.file != nil {
		return 0, indexer.FileIdentity{}, errors.New("not a directory")
	}
	if entry.dir.device != dir.device {
		return 0, indexer.FileIdentity{}, errors.New("cross-filesystem component")
	}
	return dirHandle(fs.handleFor(entry.dir)), entry.dir.identity, nil
}

func (fs *fakeFS) Identity(dir dirHandle) (indexer.FileIdentity, error) {
	handle, ok := fs.handles[int(dir)]
	if !ok || fs.closed[int(dir)] {
		return indexer.FileIdentity{}, os.ErrInvalid
	}
	return handle.identity, nil
}

func (fs *fakeFS) ReadFile(dir dirHandle, name string, limit int64) ([]byte, indexer.FileIdentity, error) {
	handle, ok := fs.handles[int(dir)]
	if !ok || fs.closed[int(dir)] {
		return nil, indexer.FileIdentity{}, os.ErrInvalid
	}
	entry, ok := handle.entries[name]
	if !ok {
		return nil, indexer.FileIdentity{}, os.ErrNotExist
	}
	if entry.symlink {
		return nil, indexer.FileIdentity{}, errors.New("symlink file")
	}
	if entry.file == nil {
		return nil, indexer.FileIdentity{}, errors.New("not a file")
	}
	if entry.file.oversized || int64(len(entry.file.content)) > limit {
		return nil, indexer.FileIdentity{}, os.ErrInvalid
	}
	return append([]byte(nil), entry.file.content...), entry.file.identity, nil
}

func (fs *fakeFS) OpenFile(dir dirHandle, name string) (*os.File, indexer.FileIdentity, error) {
	handle, ok := fs.handles[int(dir)]
	if !ok || fs.closed[int(dir)] {
		return nil, indexer.FileIdentity{}, os.ErrInvalid
	}
	entry, ok := handle.entries[name]
	if !ok {
		return nil, indexer.FileIdentity{}, os.ErrNotExist
	}
	if entry.symlink {
		return nil, indexer.FileIdentity{}, errors.New("symlink file")
	}
	if entry.file == nil {
		return nil, indexer.FileIdentity{}, errors.New("not a file")
	}
	path := filepath.Join(fs.tmp, fmt.Sprintf("open-%d", fs.filesOpened))
	if err := os.WriteFile(path, entry.file.content, 0600); err != nil {
		fs.t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		fs.t.Fatal(err)
	}
	fs.filesOpened++
	return file, entry.file.identity, nil
}

func (fs *fakeFS) List(dir dirHandle) ([]dirEntry, error) {
	handle, ok := fs.handles[int(dir)]
	if !ok || fs.closed[int(dir)] {
		return nil, os.ErrInvalid
	}
	var out []dirEntry
	for name, entry := range handle.entries {
		mode := os.FileMode(0)
		switch {
		case entry.symlink:
			mode = os.ModeSymlink
		case entry.dir != nil:
			mode = os.ModeDir
		}
		out = append(out, dirEntry{Name: name, Mode: mode})
	}
	return out, nil
}

func (fs *fakeFS) Close(dir dirHandle) error {
	handle, ok := fs.handles[int(dir)]
	if !ok || fs.closed[int(dir)] {
		return os.ErrInvalid
	}
	delete(fs.handles, int(dir))
	fs.closed[int(dir)] = true
	fs.closedOrder = append(fs.closedOrder, int(dir))
	_ = handle
	return nil
}

func (fs *fakeFS) assertBalancedFiles(t *testing.T) {
	t.Helper()
	if fs.filesOpened != fs.filesClosed {
		t.Fatalf("opened %d files, closed %d", fs.filesOpened, fs.filesClosed)
	}
}

// withFakeFS swaps the filesystem seam for the duration of the test.
func withFakeFS(fs *fakeFS) {
	old := boundFS
	boundFS = fs
	oldOpenShard := openShard
	oldSupported := zoektSupported
	oldVerify := verifyGenerationFiles
	zoektSupported = func() bool { return true }
	verifyGenerationFiles = fakeVerifiedGenerationFiles
	openShard = func(f *os.File, generation string, sequence int, want zoektread.Metadata) (shard, error) {
		fs.filesClosed++
		_ = f.Close()
		return &fakeShard{seq: sequence, meta: want}, nil
	}
	restore := func() {
		boundFS = old
		openShard = oldOpenShard
		zoektSupported = oldSupported
		verifyGenerationFiles = oldVerify
	}
	fs.t.Cleanup(restore)
}

// fakeVerifiedGenerationFiles completes verification of a bound generation
// without the pinned Zoekt builder: canonical manifests, digest chains,
// complete artifact membership, and shard integrity, skipping only the
// platform-blocked document enumeration.
func fakeVerifiedGenerationFiles(files indexer.VerifiedGenerationFiles, r indexerprotocol.BuildRequest) (*indexer.VerifiedGeneration, error) {
	gb, _, err := files.ReadManifest(sourceindex.GenerationManifestFileName)
	if err != nil {
		return nil, err
	}
	g, err := sourceindex.ParseGenerationManifest(gb)
	if err != nil {
		return nil, err
	}
	cb, _, err := files.ReadManifest(sourceindex.CoverageManifestFileName)
	if err != nil {
		return nil, err
	}
	c, err := sourceindex.ParseCoverageManifest(cb)
	if err != nil {
		return nil, err
	}
	ab, _, err := files.ReadManifest(sourceindex.ArtifactManifestFileName)
	if err != nil {
		return nil, err
	}
	a, err := sourceindex.ParseArtifactManifest(ab)
	if err != nil {
		return nil, err
	}
	gd, err := sourceindex.GenerationManifestSHA256(g)
	if err != nil {
		return nil, err
	}
	cd, err := sourceindex.CoverageManifestSHA256(c)
	if err != nil {
		return nil, err
	}
	ad, err := sourceindex.ArtifactManifestSHA256(a)
	if err != nil {
		return nil, err
	}
	if gd != digest(gb) || cd != digest(cb) || ad != digest(ab) || g.GenerationID != r.GenerationID || c.GenerationID != r.GenerationID || c.CommitOID != r.Identity.CommitOID || c.TreeOID != r.Identity.TreeOID || a.GenerationID != r.GenerationID || g.Identity != r.Identity || g.CoverageManifestSHA256 != digest(cb) || g.ArtifactManifestSHA256 != digest(ab) {
		return nil, errors.New("manifest integrity")
	}
	artifacts, err := files.ListArtifacts()
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			for _, o := range artifacts {
				if o.File != nil {
					_ = o.File.Close()
				}
			}
		}
	}()
	byPath := make(map[string]indexer.OpenedArtifact, len(artifacts))
	for _, o := range artifacts {
		if _, ok := byPath[o.RelativePath]; ok {
			return nil, errors.New("repeated artifact path")
		}
		if o.RelativePath != sourceindex.CoverageManifestFileName && o.Kind != sourceindex.ArtifactZoektShard {
			return nil, errors.New("unexpected artifact")
		}
		byPath[o.RelativePath] = o
	}
	if len(byPath) != len(a.Files) {
		return nil, errors.New("artifact membership")
	}
	var shardCount int64
	for _, want := range a.Files {
		o, ok := byPath[want.RelativePath]
		if !ok {
			return nil, errors.New("missing artifact")
		}
		if o.Kind != want.Kind || o.SizeBytes != want.SizeBytes || o.SHA256 != want.SHA256 {
			return nil, errors.New("artifact integrity")
		}
		if want.Kind == sourceindex.ArtifactZoektShard {
			shardCount++
		}
	}
	complete = true
	return &indexer.VerifiedGeneration{
		Generation:          g,
		Coverage:            c,
		Artifacts:           a,
		Opened:              artifacts,
		GenerationRawSHA256: digest(gb),
		CoverageRawSHA256:   digest(cb),
		ArtifactRawSHA256:   digest(ab),
		ShardCount:          shardCount,
	}, nil
}

// fakeShard records the exact query inputs and serves canned results.
type fakeShard struct {
	seq      int
	meta     zoektread.Metadata
	literal  string
	limit    int
	results  []zoektread.Result
	err      error
	closeErr error
	closed   bool
}

func (f *fakeShard) Search(ctx context.Context, literal string, limit int) (zoektread.Result, error) {
	if err := ctx.Err(); err != nil {
		return zoektread.Result{}, err
	}
	f.literal = literal
	f.limit = limit
	if f.err != nil {
		return zoektread.Result{}, f.err
	}
	if len(f.results) == 0 {
		return zoektread.Result{}, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func (f *fakeShard) Close() error {
	f.closed = true
	return f.closeErr
}

func completeResult(matches ...zoektread.Match) zoektread.Result {
	return zoektread.Result{Matches: matches, FlushReason: zoektread.FlushReasonNone, FileCount: len(matches), MatchCount: len(matches)}
}

func matchFor(t *testing.T, identity sourceindex.GenerationIdentity, id, path string) zoektread.Match {
	t.Helper()
	repo, err := sourceindex.GenerationRepositoryName(id)
	if err != nil {
		t.Fatal(err)
	}
	return zoektread.Match{FileName: path, Repository: repo, Version: identity.CommitOID, Branches: []string{sourceindex.GenerationBranchName}}
}

// generationSpec describes one fake generation tree.
type generationSpec struct {
	identity     sourceindex.GenerationIdentity
	commit       string
	tree         string
	indexed      []string
	fallbackPath []string
	fallbackSize []string
	short        []string
	ineligible   []string
	nonBlob      []string
	shards       map[int][]byte
	shardNames   map[int]string
	extraFiles   map[string][]byte
	extraDirs    []string
}

func buildGeneration(t *testing.T, fs *fakeFS, root string, spec generationSpec) (workflow.SourceIndexGeneration, string, string) {
	t.Helper()
	identity := spec.identity
	commit, tree := identity.CommitOID, identity.TreeOID
	if spec.commit != "" {
		commit = spec.commit
	}
	if spec.tree != "" {
		tree = spec.tree
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	fs.mkdir(root)
	fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName))
	genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, id)
	fs.mkdir(genDir)
	fs.mkdir(filepath.Join(genDir, sourceindex.ShardDirectoryName))
	var entries []sourceindex.CoverageEntry
	add := func(path, mode, objectType, oid string, size int64, status sourceindex.CoverageStatus) {
		p, err := sourceindex.NewPathIdentity([]byte(path))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, sourceindex.CoverageEntry{Path: p, Mode: mode, ObjectType: objectType, ObjectOID: oid, SizeBytes: size, Status: status})
	}
	for _, path := range spec.indexed {
		add(path, "100644", "blob", testOID, 1, sourceindex.CoverageIndexedText)
	}
	for _, path := range spec.fallbackPath {
		add(path, "100644", "blob", testOID, 1, sourceindex.CoverageFallbackPath)
	}
	for _, path := range spec.fallbackSize {
		add(path, "100644", "blob", testOID, 1, sourceindex.CoverageFallbackSize)
	}
	for _, path := range spec.short {
		add(path, "100644", "blob", testOID, 1, sourceindex.CoverageShortText)
	}
	for _, path := range spec.ineligible {
		add(path, "100644", "blob", testOID, 1, sourceindex.CoverageTextIneligible)
	}
	for _, path := range spec.nonBlob {
		add(path, "040000", "tree", testTree, 0, sourceindex.CoverageNonBlob)
	}
	cm, err := sourceindex.NewCoverageManifest(id, commit, tree, entries)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := sourceindex.MarshalCoverageManifest(cm)
	if err != nil {
		t.Fatal(err)
	}
	coverageDigest := sha256sum(cb)
	var artifacts []sourceindex.ArtifactFile
	seqs := make([]int, 0, len(spec.shards))
	for seq := range spec.shards {
		seqs = append(seqs, seq)
	}
	sort.Ints(seqs)
	for _, seq := range seqs {
		content := spec.shards[seq]
		name := fmt.Sprintf("shards/%06d.zoekt", seq)
		if override, ok := spec.shardNames[seq]; ok {
			name = "shards/" + override
		}
		fs.writeFile(filepath.Join(genDir, filepath.FromSlash(name)), content)
		artifacts = append(artifacts, sourceindex.ArtifactFile{Kind: sourceindex.ArtifactZoektShard, RelativePath: name, SHA256: sha256sum(content), SizeBytes: int64(len(content))})
	}
	artifacts = append(artifacts, sourceindex.ArtifactFile{Kind: sourceindex.ArtifactCoverage, RelativePath: sourceindex.CoverageManifestFileName, SHA256: coverageDigest, SizeBytes: int64(len(cb))})
	for rel, content := range spec.extraFiles {
		fs.writeFile(filepath.Join(genDir, filepath.FromSlash(rel)), content)
	}
	for _, dir := range spec.extraDirs {
		fs.mkdir(filepath.Join(genDir, filepath.FromSlash(dir)))
	}
	am, err := sourceindex.NewArtifactManifest(id, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	ab, err := sourceindex.MarshalArtifactManifest(am)
	if err != nil {
		t.Fatal(err)
	}
	artifactDigest := sha256sum(ab)
	gm, err := sourceindex.NewGenerationManifest(identity, coverageDigest, artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	gb, err := sourceindex.MarshalGenerationManifest(gm)
	if err != nil {
		t.Fatal(err)
	}
	fs.writeFile(filepath.Join(genDir, sourceindex.CoverageManifestFileName), cb)
	fs.writeFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), ab)
	fs.writeFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb)
	row := workflow.SourceIndexGeneration{
		GenerationID:             id,
		Identity:                 identity,
		State:                    workflow.SourceIndexGenerationReady,
		AttemptCount:             1,
		GenerationManifestSHA256: sha256sum(gb),
		CoverageManifestSHA256:   coverageDigest,
		ArtifactManifestSHA256:   artifactDigest,
		BuildingStartedAt:        "2026-01-01T00:00:00.000Z",
		ReadyAt:                  "2026-01-01T00:00:01.000Z",
	}
	return row, id, genDir
}

func sha256sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// openGeneration opens a built generation with a fake store and seams.
func openGeneration(t *testing.T, fs *fakeFS, store GenerationStore, config Config, identity sourceindex.GenerationIdentity) (*Reader, error) {
	t.Helper()
	return Open(context.Background(), store, config, identity)
}

func TestOpenConfigurationValidation(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	config := Config{IndexRoot: filepath.Join(t.TempDir(), "index")}
	options, _ := sourceindex.BuildOptionsSHA256(sourceindex.DefaultBuildOptions())
	cases := []struct {
		name     string
		store    GenerationStore
		config   Config
		identity sourceindex.GenerationIdentity
	}{
		{"nil store", nil, config, identity},
		{"relative root", &fakeStore{}, Config{IndexRoot: "relative"}, identity},
		{"unclean root", &fakeStore{}, Config{IndexRoot: config.IndexRoot + string(filepath.Separator)}, identity},
		{"empty root", &fakeStore{}, Config{}, identity},
		{"invalid identity", &fakeStore{}, config, sourceindex.GenerationIdentity{Version: "bad"}},
		{"unsupported engine", &fakeStore{}, config, sourceindex.GenerationIdentity{Version: sourceindex.GenerationIdentityVersion, VaultID: "vault", CommitOID: testOID, TreeOID: testTree, Engine: "other", EngineRevision: sourceindex.PinnedZoektRevision, BuildContractVersion: sourceindex.BuildContractVersion, BuildOptionsSHA256: options}},
		{"unsupported revision", &fakeStore{}, config, sourceindex.GenerationIdentity{Version: sourceindex.GenerationIdentityVersion, VaultID: "vault", CommitOID: testOID, TreeOID: testTree, Engine: sourceindex.EngineZoekt, EngineRevision: "other", BuildContractVersion: sourceindex.BuildContractVersion, BuildOptionsSHA256: options}},
		{"unsupported build contract", &fakeStore{}, config, sourceindex.GenerationIdentity{Version: sourceindex.GenerationIdentityVersion, VaultID: "vault", CommitOID: testOID, TreeOID: testTree, Engine: sourceindex.EngineZoekt, EngineRevision: sourceindex.PinnedZoektRevision, BuildContractVersion: "other", BuildOptionsSHA256: options}},
		{"wrong build options digest", &fakeStore{}, config, sourceindex.GenerationIdentity{Version: sourceindex.GenerationIdentityVersion, VaultID: "vault", CommitOID: testOID, TreeOID: testTree, Engine: sourceindex.EngineZoekt, EngineRevision: sourceindex.PinnedZoektRevision, BuildContractVersion: sourceindex.BuildContractVersion, BuildOptionsSHA256: strings.Repeat("0", 64)}},
		{"protected storage overlap", &fakeStore{}, Config{IndexRoot: config.IndexRoot, ProtectedStorage: sourceindex.ProtectedStorage{SourceVaultRoot: filepath.Join(config.IndexRoot, "protected")}}, identity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Open(context.Background(), tc.store, tc.config, tc.identity); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("error = %v, want invalid configuration", err)
			}
		})
	}
	t.Run("unsupported platform", func(t *testing.T) {
		old := zoektSupported
		zoektSupported = func() bool { return false }
		defer func() { zoektSupported = old }()
		if _, err := Open(context.Background(), &fakeStore{}, config, identity); !errors.Is(err, ErrUnsupportedPlatform) {
			t.Fatalf("error = %v, want unsupported platform", err)
		}
	})
}

func TestOpenGenerationResolution(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	fs := newFakeFS(t)
	withFakeFS(fs)
	root := filepath.Join(t.TempDir(), "index")
	row, _, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
	config := Config{IndexRoot: root}

	t.Run("missing generation", func(t *testing.T) {
		store := &fakeStore{err: errors.New("not found")}
		if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	for _, state := range []workflow.SourceIndexGenerationState{
		workflow.SourceIndexGenerationPending,
		workflow.SourceIndexGenerationBuilding,
		workflow.SourceIndexGenerationFailed,
		workflow.SourceIndexGenerationRetired,
	} {
		t.Run(string(state), func(t *testing.T) {
			bad := row
			bad.State = state
			store := &fakeStore{row: bad}
			if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	t.Run("identity mismatch", func(t *testing.T) {
		other, _ := testIdentity(t, "other-vault")
		bad := row
		bad.Identity = other
		store := &fakeStore{row: bad}
		if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("generation id mismatch", func(t *testing.T) {
		bad := row
		bad.GenerationID = strings.Repeat("f", 64)
		store := &fakeStore{row: bad}
		if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	for _, mutate := range []func(*workflow.SourceIndexGeneration){
		func(r *workflow.SourceIndexGeneration) { r.GenerationManifestSHA256 = "bad" },
		func(r *workflow.SourceIndexGeneration) { r.CoverageManifestSHA256 = "bad" },
		func(r *workflow.SourceIndexGeneration) { r.ArtifactManifestSHA256 = strings.Repeat("0", 63) },
	} {
		t.Run("malformed persisted digest", func(t *testing.T) {
			bad := row
			mutate(&bad)
			store := &fakeStore{row: bad}
			if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	t.Run("failure fields on ready row", func(t *testing.T) {
		bad := row
		bad.FailureCode = "build_failed"
		bad.FailureMessage = "boom"
		store := &fakeStore{row: bad}
		if _, err := Open(context.Background(), store, config, identity); !errors.Is(err, ErrGenerationUnavailable) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("cancelled store resolution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Open(ctx, &fakeStore{row: row}, config, identity); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

func TestOpenBoundDirectorySafety(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	root := filepath.Join(t.TempDir(), "index")

	t.Run("index root symlink", func(t *testing.T) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		row, _, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		fs.symlinkEntry(root)
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("generations symlink", func(t *testing.T) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		row, id, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		fs.remove(filepath.Join(root, sourceindex.GenerationDirectoryName))
		fs.symlinkEntry(filepath.Join(root, sourceindex.GenerationDirectoryName))
		_ = row
		_ = id
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("generation directory symlink", func(t *testing.T) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		row, id, genDir := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		fs.remove(genDir)
		fs.symlinkEntry(genDir)
		_ = id
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("traversal via symlinked shard", func(t *testing.T) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		row, _, genDir := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		fs.symlinkEntry(filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("cross-filesystem generations", func(t *testing.T) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		row, id, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		fs.remove(filepath.Join(root, sourceindex.GenerationDirectoryName))
		fs.mkdirDevice(filepath.Join(root, sourceindex.GenerationDirectoryName), 2)
		fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName, id))
		_ = row
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("generation pathname replacement after opening", func(t *testing.T) {
		fs := newFakeFS(t)
		old := boundFS
		boundFS = fs
		defer func() { boundFS = old }()
		row, id, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		_ = row
		handle, _, err := fs.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		gens, _, err := fs.OpenChild(handle, sourceindex.GenerationDirectoryName)
		if err != nil {
			t.Fatal(err)
		}
		gen, _, err := fs.OpenChild(gens, id)
		if err != nil {
			t.Fatal(err)
		}
		original, _, err := fs.ReadFile(gen, sourceindex.GenerationManifestFileName, manifestLimitBytes)
		if err != nil {
			t.Fatal(err)
		}
		replacement := fs.newDir(0)
		fs.replaceDir(filepath.Join(root, sourceindex.GenerationDirectoryName, id), replacement)
		replacementGen, _, err := fs.OpenChild(gens, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := fs.ReadFile(replacementGen, sourceindex.GenerationManifestFileName, manifestLimitBytes); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replacement read error = %v", err)
		}
		still, _, err := fs.ReadFile(gen, sourceindex.GenerationManifestFileName, manifestLimitBytes)
		if err != nil || string(still) != string(original) {
			t.Fatalf("bound read after replacement: %v", err)
		}
	})
	t.Run("generations replacement after opening", func(t *testing.T) {
		fs := newFakeFS(t)
		old := boundFS
		boundFS = fs
		defer func() { boundFS = old }()
		row, id, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		_ = row
		handle, _, err := fs.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		gens, _, err := fs.OpenChild(handle, sourceindex.GenerationDirectoryName)
		if err != nil {
			t.Fatal(err)
		}
		gen, _, err := fs.OpenChild(gens, id)
		if err != nil {
			t.Fatal(err)
		}
		original, _, err := fs.ReadFile(gen, sourceindex.GenerationManifestFileName, manifestLimitBytes)
		if err != nil {
			t.Fatal(err)
		}
		fs.replaceDir(filepath.Join(root, sourceindex.GenerationDirectoryName), fs.newDir(0))
		still, _, err := fs.ReadFile(gen, sourceindex.GenerationManifestFileName, manifestLimitBytes)
		if err != nil || string(still) != string(original) {
			t.Fatalf("bound read after generations replacement: %v", err)
		}
	})
}

func TestOpenManifestValidation(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration
	}{
		{"missing generation manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.GenerationManifestFileName))
			return row
		}},
		{"missing coverage manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			return row
		}},
		{"missing artifact manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.ArtifactManifestFileName))
			return row
		}},
		{"symlink generation manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.GenerationManifestFileName))
			fs.symlinkEntry(filepath.Join(genDir, sourceindex.GenerationManifestFileName))
			return row
		}},
		{"symlink coverage manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			fs.symlinkEntry(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			return row
		}},
		{"non-regular manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			fs.mkdir(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			return row
		}},
		{"hard-linked manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.hardlink(filepath.Join(genDir, sourceindex.CoverageManifestFileName), filepath.Join(genDir, sourceindex.GenerationManifestFileName))
			return row
		}},
		{"repeated manifest identity", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.hardlink(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), filepath.Join(genDir, sourceindex.GenerationManifestFileName))
			return row
		}},
		{"oversized manifest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.remove(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			fs.oversizedFile(filepath.Join(genDir, sourceindex.CoverageManifestFileName))
			return row
		}},
		{"noncanonical coverage", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.CoverageManifestFileName)
			fs.replaceFile(path, append(readFake(t, fs, path), ' '))
			return row
		}},
		{"trailing coverage json", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.CoverageManifestFileName)
			fs.replaceFile(path, append(readFake(t, fs, path), []byte(" {}")...))
			return row
		}},
		{"extra coverage field", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.CoverageManifestFileName)
			raw := readFake(t, fs, path)
			fs.replaceFile(path, []byte(strings.Replace(string(raw), `"version"`, `"unknown":true,"version"`, 1)))
			return row
		}},
		{"wrong coverage digest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.GenerationManifestFileName)
			gm, err := sourceindex.ParseGenerationManifest(readFake(t, fs, path))
			if err != nil {
				t.Fatal(err)
			}
			gm.CoverageManifestSHA256 = strings.Repeat("0", 64)
			b, err := sourceindex.MarshalGenerationManifest(gm)
			if err != nil {
				t.Fatal(err)
			}
			fs.replaceFile(path, b)
			return row
		}},
		{"wrong artifact digest", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			path := filepath.Join(genDir, sourceindex.GenerationManifestFileName)
			gm, err := sourceindex.ParseGenerationManifest(readFake(t, fs, path))
			if err != nil {
				t.Fatal(err)
			}
			gm.ArtifactManifestSHA256 = strings.Repeat("0", 64)
			b, err := sourceindex.MarshalGenerationManifest(gm)
			if err != nil {
				t.Fatal(err)
			}
			fs.replaceFile(path, b)
			return row
		}},
		{"ready row coverage digest mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			row.CoverageManifestSHA256 = strings.Repeat("0", 64)
			return row
		}},
		{"ready row generation digest mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			row.GenerationManifestSHA256 = strings.Repeat("0", 64)
			return row
		}},
		{"ready row artifact digest mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			row.ArtifactManifestSHA256 = strings.Repeat("0", 64)
			return row
		}},
		{"manifest identity mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			other, _ := testIdentity(t, "other")
			original, err := sourceindex.ParseGenerationManifest(readFake(t, fs, filepath.Join(genDir, sourceindex.GenerationManifestFileName)))
			if err != nil {
				t.Fatal(err)
			}
			replacement, err := sourceindex.NewGenerationManifest(other, original.CoverageManifestSHA256, original.ArtifactManifestSHA256)
			if err != nil {
				t.Fatal(err)
			}
			gb, err := sourceindex.MarshalGenerationManifest(replacement)
			if err != nil {
				t.Fatal(err)
			}
			fs.replaceFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb)
			row.GenerationManifestSHA256 = sha256sum(gb)
			return row
		}},
		{"commit mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.replaceFile(filepath.Join(genDir, sourceindex.CoverageManifestFileName), reparseCoverage(t, fs, genDir, func(c *sourceindex.CoverageManifest) { c.CommitOID = testOID2 }))
			return row
		}},
		{"tree mismatch", func(t *testing.T, fs *fakeFS, root, genDir string, row workflow.SourceIndexGeneration) workflow.SourceIndexGeneration {
			fs.replaceFile(filepath.Join(genDir, sourceindex.CoverageManifestFileName), reparseCoverage(t, fs, genDir, func(c *sourceindex.CoverageManifest) { c.TreeOID = testTree2 }))
			return row
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeFS(t)
			withFakeFS(fs)
			root := filepath.Join(t.TempDir(), "index")
			row, _, genDir := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
			row = tc.mutate(t, fs, root, genDir, row)
			if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
				t.Fatalf("error = %v, want generation integrity", err)
			}
		})
	}
}

func readFake(t *testing.T, fs *fakeFS, path string) []byte {
	t.Helper()
	entry, err := fs.lookup(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), entry.file.content...)
}

func reparseCoverage(t *testing.T, fs *fakeFS, genDir string, mutate func(*sourceindex.CoverageManifest)) []byte {
	t.Helper()
	b := readFake(t, fs, filepath.Join(genDir, sourceindex.CoverageManifestFileName))
	c, err := sourceindex.ParseCoverageManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&c)
	out, err := sourceindex.MarshalCoverageManifest(c)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenArtifactValidation(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	base := func(t *testing.T, spec generationSpec) (fs *fakeFS, row workflow.SourceIndexGeneration, root string) {
		fs = newFakeFS(t)
		withFakeFS(fs)
		root = filepath.Join(t.TempDir(), "index")
		row, _, _ = buildGeneration(t, fs, root, spec)
		return fs, row, root
	}
	valid := generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}}
	t.Run("missing shard", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.remove(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.ShardDirectoryName, "000000.zoekt"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unlisted shard", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.writeFile(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.ShardDirectoryName, "000001.zoekt"), []byte("extra"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("extra file", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.writeFile(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), "extra"), []byte("x"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unexpected directory", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), "unexpected"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unexpected shard directory", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.ShardDirectoryName, "nested"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("noncontiguous shard", func(t *testing.T) {
		fs, row, root := base(t, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}, shardNames: map[int]string{0: "000001.zoekt"}})
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("symlink shard", func(t *testing.T) {
		fs, row, root := base(t, valid)
		fs.remove(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.ShardDirectoryName, "000000.zoekt"))
		fs.symlinkEntry(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.ShardDirectoryName, "000000.zoekt"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("hard-linked shard", func(t *testing.T) {
		fs, row, root := base(t, valid)
		genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity))
		fs.hardlink(filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"), filepath.Join(genDir, sourceindex.CoverageManifestFileName))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("repeated shard identity", func(t *testing.T) {
		spec := generationSpec{identity: identity, indexed: []string{"a.txt", "b.txt"}, shards: map[int][]byte{0: []byte("shard"), 1: []byte("shard")}}
		fs, row, root := base(t, spec)
		genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity))
		fs.hardlink(filepath.Join(genDir, sourceindex.ShardDirectoryName, "000001.zoekt"), filepath.Join(genDir, sourceindex.ShardDirectoryName, "000000.zoekt"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong shard size", func(t *testing.T) {
		fs, row, root := base(t, valid)
		genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity))
		path := filepath.Join(genDir, sourceindex.ArtifactManifestFileName)
		am, err := sourceindex.ParseArtifactManifest(readFake(t, fs, path))
		if err != nil {
			t.Fatal(err)
		}
		for i := range am.Files {
			if am.Files[i].Kind == sourceindex.ArtifactZoektShard {
				am.Files[i].SizeBytes++
			}
		}
		row = publishArtifacts(t, fs, genDir, row, am)
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong shard digest", func(t *testing.T) {
		fs, row, root := base(t, valid)
		genDir := filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity))
		path := filepath.Join(genDir, sourceindex.ArtifactManifestFileName)
		am, err := sourceindex.ParseArtifactManifest(readFake(t, fs, path))
		if err != nil {
			t.Fatal(err)
		}
		for i := range am.Files {
			if am.Files[i].Kind == sourceindex.ArtifactZoektShard {
				am.Files[i].SHA256 = strings.Repeat("0", 64)
			}
		}
		row = publishArtifacts(t, fs, genDir, row, am)
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
}

func identityID(t *testing.T, identity sourceindex.GenerationIdentity) string {
	t.Helper()
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// publishArtifacts rewrites the artifact manifest and repairs the generation
// manifest and ready row digests.
func publishArtifacts(t *testing.T, fs *fakeFS, genDir string, row workflow.SourceIndexGeneration, am sourceindex.ArtifactManifest) workflow.SourceIndexGeneration {
	t.Helper()
	ab, err := sourceindex.MarshalArtifactManifest(am)
	if err != nil {
		t.Fatal(err)
	}
	fs.replaceFile(filepath.Join(genDir, sourceindex.ArtifactManifestFileName), ab)
	gm, err := sourceindex.ParseGenerationManifest(readFake(t, fs, filepath.Join(genDir, sourceindex.GenerationManifestFileName)))
	if err != nil {
		t.Fatal(err)
	}
	gm.ArtifactManifestSHA256 = sha256sum(ab)
	gb, err := sourceindex.MarshalGenerationManifest(gm)
	if err != nil {
		t.Fatal(err)
	}
	fs.replaceFile(filepath.Join(genDir, sourceindex.GenerationManifestFileName), gb)
	row.ArtifactManifestSHA256 = sha256sum(ab)
	row.GenerationManifestSHA256 = sha256sum(gb)
	return row
}

func TestSearchLimitConversion(t *testing.T) {
	maxInt := int64(^uint(0) >> 1)
	if got, err := searchLimit(0); err != nil || got != 1 {
		t.Fatalf("searchLimit(0) = %d, %v", got, err)
	}
	if got, err := searchLimit(3); err != nil || got != 3 {
		t.Fatalf("searchLimit(3) = %d, %v", got, err)
	}
	if got, err := searchLimit(maxInt); err != nil || int64(got) != maxInt {
		t.Fatalf("searchLimit(maxInt) = %d, %v", got, err)
	}
	if _, err := checkedInt(uint64(maxInt)); err != nil {
		t.Fatal(err)
	}
	if _, err := checkedInt(uint64(maxInt) + 1); !errors.Is(err, ErrGenerationIntegrity) {
		t.Fatalf("overflow error = %v", err)
	}
	if _, err := searchLimit(-1); !errors.Is(err, ErrGenerationIntegrity) {
		t.Fatalf("negative error = %v", err)
	}
}

func TestQueryEligibilityAndClosure(t *testing.T) {
	r := &Reader{}
	for _, literal := range []string{"", "a", "ab", string([]byte{0xff, 0xff, 0xff})} {
		if _, err := r.IndexedTextCandidates(context.Background(), literal); !errors.Is(err, ErrQueryIneligible) {
			t.Fatalf("%q error = %v", literal, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{"", "ab", "abc", string([]byte{0xff, 0xff, 0xff})} {
		if _, err := r.IndexedTextCandidates(context.Background(), literal); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed %q error = %v", literal, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestQueryLimits(t *testing.T) {
	identity, id := testIdentity(t, "vault")
	coverage := indexedCoverage("a.txt", "b.txt", "猫犬鳥.txt")
	descriptor := Descriptor{GenerationID: id, Identity: identity}
	t.Run("zero documents use positive explicit options", func(t *testing.T) {
		record := &fakeShard{}
		r := &Reader{coverage: map[string]sourceindex.CoverageStatus{}, shards: []shard{record}, limit: 1, indexedDocumentCount: 0, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 0 {
			t.Fatalf("candidates = %v, %v", got, err)
		}
		if record.limit < 1 {
			t.Fatalf("shard limit = %d, want positive", record.limit)
		}
		if record.literal != "abc" {
			t.Fatalf("literal = %q", record.literal)
		}
	})
	t.Run("zero documents accept an empty result", func(t *testing.T) {
		r := &Reader{coverage: map[string]sourceindex.CoverageStatus{}, shards: []shard{&fakeShard{}}, limit: 1, indexedDocumentCount: 0, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("any result for zero documents is corruption", func(t *testing.T) {
		r := &Reader{coverage: map[string]sourceindex.CoverageStatus{}, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"))}}}, limit: 1, indexedDocumentCount: 0, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("excessive result count", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"), matchFor(t, identity, id, "b.txt"))}}}, limit: 1, indexedDocumentCount: 1, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrQueryIncomplete) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("exact corpus bound accepted", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"), matchFor(t, identity, id, "b.txt"))}}}, limit: 2, indexedDocumentCount: 2, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 2 {
			t.Fatalf("candidates = %v, %v", got, err)
		}
	})
}

func TestQuerySemantics(t *testing.T) {
	identity, id := testIdentity(t, "vault")
	descriptor := Descriptor{GenerationID: id, Identity: identity}
	coverage := indexedCoverage("a.txt", "b.txt", "猫犬鳥.txt")

	t.Run("literal forwarded verbatim", func(t *testing.T) {
		record := &fakeShard{}
		r := &Reader{coverage: coverage, shards: []shard{record}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "AbC猫"); err != nil {
			t.Fatal(err)
		}
		if record.literal != "AbC猫" {
			t.Fatalf("literal = %q", record.literal)
		}
	})
	t.Run("exactly three code points eligible", func(t *testing.T) {
		record := &fakeShard{}
		r := &Reader{coverage: coverage, shards: []shard{record}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "猫犬鳥"); err != nil {
			t.Fatalf("three runes rejected: %v", err)
		}
		if record.literal != "猫犬鳥" {
			t.Fatalf("literal = %q", record.literal)
		}
	})
	t.Run("repeated filename is corruption", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"), matchFor(t, identity, id, "a.txt"))}}}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("no match returns empty candidates", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{}}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 0 {
			t.Fatalf("candidates = %v, %v", got, err)
		}
	})
	t.Run("raw byte canonical ordering", func(t *testing.T) {
		raw := "ü.txt"
		rawCoverage := indexedCoverage("a.txt", "b.txt", raw)
		r := &Reader{coverage: rawCoverage, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "b.txt"), matchFor(t, identity, id, raw), matchFor(t, identity, id, "a.txt"))}}}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 3 {
			t.Fatalf("candidates = %v, %v", got, err)
		}
		want := []string{"a.txt", "b.txt", raw}
		for i, w := range want {
			if string(got[i].Path) != w {
				t.Fatalf("order[%d] = %q, want %q", i, got[i].Path, w)
			}
		}
	})
	t.Run("unicode candidate", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "猫犬鳥.txt"))}}}, limit: 3, indexedDocumentCount: 3, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 1 || string(got[0].Path) != "猫犬鳥.txt" {
			t.Fatalf("candidates = %v, %v", got, err)
		}
	})
}

func TestQueryCompletenessClassification(t *testing.T) {
	identity, id := testIdentity(t, "vault")
	descriptor := Descriptor{GenerationID: id, Identity: identity}
	coverage := indexedCoverage("a.txt", "b.txt")
	reader := func(results ...zoektread.Result) *Reader {
		return &Reader{coverage: coverage, shards: []shard{&fakeShard{results: results}}, limit: 2, indexedDocumentCount: 2, descriptor: descriptor}
	}
	rejected := func(name string, result zoektread.Result, want error) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if _, err := reader(result).IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}
	match := matchFor(t, identity, id, "a.txt")
	rejected("crash count", zoektread.Result{Crashes: 1, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("files skipped", zoektread.Result{FilesSkipped: 1, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("ordinary shards skipped", zoektread.Result{ShardsSkipped: 1, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("trigram filter skip with matches", zoektread.Result{ShardsSkippedFilter: 1, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("limit flush", zoektread.Result{FlushReason: zoektread.FlushReasonTimerExpired, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("size flush", zoektread.Result{FlushReason: zoektread.FlushReasonMaxSize, FileCount: 1, MatchCount: 1}, ErrQueryIncomplete)
	rejected("inconsistent file count", zoektread.Result{Matches: []zoektread.Match{match}, FlushReason: zoektread.FlushReasonNone, FileCount: 2, MatchCount: 1}, ErrQueryIncomplete)
	rejected("inconsistent match count", zoektread.Result{Matches: []zoektread.Match{match}, FlushReason: zoektread.FlushReasonNone, FileCount: 1, MatchCount: 2}, ErrQueryIncomplete)

	t.Run("trigram filter skip is a negative outcome", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{{ShardsSkippedFilter: 1, FlushReason: zoektread.FlushReasonNone}}}}, limit: 2, indexedDocumentCount: 2, descriptor: descriptor}
		got, err := r.IndexedTextCandidates(context.Background(), "abc")
		if err != nil || len(got) != 0 {
			t.Fatalf("candidates = %v, %v", got, err)
		}
	})
	t.Run("final flush accepted", func(t *testing.T) {
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{results: []zoektread.Result{{Matches: []zoektread.Match{match}, FlushReason: zoektread.FlushReasonFinalFlush, FileCount: 1, MatchCount: 1}}}}, limit: 2, indexedDocumentCount: 2, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("duplicate filename", func(t *testing.T) {
		r := reader(completeResult(matchFor(t, identity, id, "a.txt"), matchFor(t, identity, id, "a.txt")))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unknown filename", func(t *testing.T) {
		r := reader(completeResult(matchFor(t, identity, id, "unknown.txt")))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("non-indexed coverage filename", func(t *testing.T) {
		r := &Reader{coverage: map[string]sourceindex.CoverageStatus{"a.txt": sourceindex.CoverageShortText}, shards: []shard{&fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"))}}}, limit: 2, indexedDocumentCount: 2, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong repository", func(t *testing.T) {
		wrong := matchFor(t, identity, id, "a.txt")
		wrong.Repository = "other"
		r := reader(completeResult(wrong))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong version", func(t *testing.T) {
		wrong := matchFor(t, identity, id, "a.txt")
		wrong.Version = testOID2
		r := reader(completeResult(wrong))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong branch", func(t *testing.T) {
		wrong := matchFor(t, identity, id, "a.txt")
		wrong.Branches = []string{"other"}
		r := reader(completeResult(wrong))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("additional branch", func(t *testing.T) {
		wrong := matchFor(t, identity, id, "a.txt")
		wrong.Branches = []string{sourceindex.GenerationBranchName, "other"}
		r := reader(completeResult(wrong))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("non-utf8 filename", func(t *testing.T) {
		r := reader(completeResult(matchFor(t, identity, id, string([]byte{0xff, 'a'}))))
		if _, err := r.IndexedTextCandidates(context.Background(), "abc"); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestQueryCancellation(t *testing.T) {
	identity, id := testIdentity(t, "vault")
	descriptor := Descriptor{GenerationID: id, Identity: identity}
	coverage := indexedCoverage("a.txt")
	t.Run("cancelled search returns context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := &Reader{coverage: coverage, shards: []shard{&fakeShard{}}, limit: 1, indexedDocumentCount: 1, descriptor: descriptor}
		if _, err := r.IndexedTextCandidates(ctx, "abc"); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("cancellation mid-search returns no partial candidates", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		first := &fakeShard{results: []zoektread.Result{completeResult(matchFor(t, identity, id, "a.txt"))}}
		blocked := &fakeShard{err: errors.New("shard unavailable")}
		r := &Reader{coverage: coverage, shards: []shard{first, blocked}, limit: 1, indexedDocumentCount: 1, descriptor: descriptor}
		cancel()
		if _, err := r.IndexedTextCandidates(ctx, "abc"); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestCloseVisitsEveryShardAndGenerationDirectory(t *testing.T) {
	first := &fakeShard{}
	second := &fakeShard{}
	fs := newFakeFS(t)
	withFakeFS(fs)
	root := filepath.Join(t.TempDir(), "index")
	fs.mkdir(root)
	dir, _, err := fs.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	r := &Reader{shards: []shard{first, second}, dir: &boundDirectory{handle: dir, name: "generations"}}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if !first.closed || !second.closed {
		t.Fatal("not every shard was closed")
	}
	if !fs.closed[int(dir)] {
		t.Fatal("generation directory handle was not closed")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

func TestCloseJoinsShardErrors(t *testing.T) {
	first := &fakeShard{}
	second := &fakeShard{closeErr: errors.New("close failed")}
	r := &Reader{shards: []shard{first, second}}
	if err := r.Close(); err == nil || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("error = %v", err)
	}
	if !first.closed || !second.closed {
		t.Fatal("not every shard was visited")
	}
}

func TestFallbackPathsContract(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	raw := string([]byte{0xff, 'z'})
	fs := newFakeFS(t)
	withFakeFS(fs)
	root := filepath.Join(t.TempDir(), "index")
	spec := generationSpec{
		identity: identity, indexed: []string{"indexed.txt"},
		fallbackPath: []string{raw, "fallback.txt"}, fallbackSize: []string{"large.txt"},
		short: []string{"short.txt"}, ineligible: []string{"binary.bin"}, nonBlob: []string{"tree"},
		shards: map[int][]byte{0: []byte("shard")},
	}
	row, _, _ := buildGeneration(t, fs, root, spec)
	_ = row
	_ = fs
	// Fallback extraction is a pure function over the parsed coverage manifest.
	b := readFake(t, fs, filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), sourceindex.CoverageManifestFileName))
	cm, err := sourceindex.ParseCoverageManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := fallbackPaths(cm)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fallback.txt", "large.txt", raw}
	if len(paths) != len(want) {
		t.Fatalf("fallback = %#v", paths)
	}
	for i, w := range want {
		if string(paths[i]) != w {
			t.Fatalf("fallback[%d] = %q, want %q", i, paths[i], w)
		}
	}
	for _, p := range paths {
		if p[0] == 0xff {
			if len(p) != 2 {
				t.Fatalf("raw bytes altered: %v", p)
			}
			p[0] = 'x'
		}
	}
	again, _ := fallbackPaths(cm)
	for _, p := range again {
		if len(p) == 2 && p[1] == 'z' {
			if p[0] != 0xff {
				t.Fatal("fallback paths alias manifest state")
			}
		}
	}
}

func TestFallbackCandidatesAreSortedAndCopied(t *testing.T) {
	r := &Reader{fallback: [][]byte{{0xff, 'z'}, []byte("a")}}
	got := r.FallbackCandidates()
	if len(got) != 2 || string(got[0].Path) != "a" || len(got[1].Path) != 2 || got[1].Path[0] != 0xff {
		t.Fatalf("fallback = %#v", got)
	}
	got[0].Path[0] = 'x'
	if string(r.FallbackCandidates()[0].Path) != "a" {
		t.Fatal("fallback path aliases reader state")
	}
}

func indexedCoverage(paths ...string) map[string]sourceindex.CoverageStatus {
	out := make(map[string]sourceindex.CoverageStatus, len(paths))
	for _, p := range paths {
		out[p] = sourceindex.CoverageIndexedText
	}
	return out
}

// probeFS records every opened artifact descriptor and directory close, and
// injects deterministic close failures and enumeration mutations.
type probeFS struct {
	dirFS
	opened     []*os.File
	names      map[dirHandle]string
	closeCount map[dirHandle]int
	failClose  map[string]error
	mutateList func([]dirEntry) []dirEntry
	listCalls  int
}

func newProbeFS(fs *fakeFS) *probeFS {
	return &probeFS{dirFS: fs, names: map[dirHandle]string{}, closeCount: map[dirHandle]int{}, failClose: map[string]error{}}
}

func (p *probeFS) OpenChild(parent dirHandle, name string) (dirHandle, indexer.FileIdentity, error) {
	handle, id, err := p.dirFS.OpenChild(parent, name)
	if err == nil {
		p.names[handle] = name
	}
	return handle, id, err
}

func (p *probeFS) OpenFile(dir dirHandle, name string) (*os.File, indexer.FileIdentity, error) {
	file, id, err := p.dirFS.OpenFile(dir, name)
	if err == nil {
		p.opened = append(p.opened, file)
	}
	return file, id, err
}

func (p *probeFS) List(dir dirHandle) ([]dirEntry, error) {
	entries, err := p.dirFS.List(dir)
	if err != nil {
		return nil, err
	}
	p.listCalls++
	if p.listCalls == 2 && p.mutateList != nil {
		return p.mutateList(entries), nil
	}
	return entries, nil
}

func (p *probeFS) Close(dir dirHandle) error {
	p.closeCount[dir]++
	if err, ok := p.failClose[p.names[dir]]; ok {
		_ = p.dirFS.Close(dir)
		return err
	}
	return p.dirFS.Close(dir)
}

func (p *probeFS) handleNamed(name string) dirHandle {
	for h, n := range p.names {
		if n == name {
			return h
		}
	}
	return 0
}

func assertFilesClosed(t *testing.T, files []*os.File) {
	t.Helper()
	for _, f := range files {
		if err := f.Close(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("artifact %s still open: %v", f.Name(), err)
		}
	}
}

func TestRequiredShardsDirectoryAccepted(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	fs := newFakeFS(t)
	withFakeFS(fs)
	root := filepath.Join(t.TempDir(), "index")
	row, id, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
	handle, _, err := fs.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	gens, _, err := fs.OpenChild(handle, sourceindex.GenerationDirectoryName)
	if err != nil {
		t.Fatal(err)
	}
	gen, _, err := fs.OpenChild(gens, id)
	if err != nil {
		t.Fatal(err)
	}
	files := &boundGenerationFiles{dir: &boundDirectory{handle: gen, name: "generations/" + id}, cache: map[string][]byte{}, ids: map[string]indexer.FileIdentity{}}
	for _, name := range []string{sourceindex.GenerationManifestFileName, sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName} {
		if _, _, err := files.ReadManifest(name); err != nil {
			t.Fatal(err)
		}
	}
	artifacts, err := files.ListArtifacts()
	if err != nil {
		t.Fatalf("bound enumeration failed: %v", err)
	}
	if len(artifacts) != 2 || artifacts[0].RelativePath != sourceindex.CoverageManifestFileName || artifacts[0].Kind != sourceindex.ArtifactCoverage || artifacts[1].RelativePath != sourceindex.ShardDirectoryName+"/000000.zoekt" || artifacts[1].Kind != sourceindex.ArtifactZoektShard || artifacts[1].SHA256 != sha256sum([]byte("shard")) || artifacts[1].SizeBytes != int64(len("shard")) {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	for _, o := range artifacts {
		if o.File != nil {
			_ = o.File.Close()
		}
	}
	_ = boundFS.Close(gen)
	_ = boundFS.Close(gens)
	_ = boundFS.Close(handle)
	// The reader completes opening with fake verified shards.
	r, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer r.Close()
	if d := r.Descriptor(); d.GenerationID != id || d.Identity != identity {
		t.Fatalf("descriptor = %#v", d)
	}
}

func TestShardsDirectoryRejection(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	base := func(t *testing.T) (*fakeFS, workflow.SourceIndexGeneration, string, string) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		root := filepath.Join(t.TempDir(), "index")
		row, _, genDir := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		return fs, row, root, genDir
	}
	reject := func(name string, setup func(t *testing.T, fs *fakeFS, row workflow.SourceIndexGeneration, root, genDir string)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			fs, row, root, genDir := base(t)
			setup(t, fs, row, root, genDir)
			if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
				t.Fatalf("error = %v, want generation integrity", err)
			}
		})
	}
	reject("missing shards directory", func(t *testing.T, fs *fakeFS, _ workflow.SourceIndexGeneration, _, genDir string) {
		fs.remove(filepath.Join(genDir, sourceindex.ShardDirectoryName))
	})
	reject("second unexpected directory", func(t *testing.T, fs *fakeFS, _ workflow.SourceIndexGeneration, _, genDir string) {
		fs.mkdir(filepath.Join(genDir, "unexpected"))
	})
	reject("symlink named shards", func(t *testing.T, fs *fakeFS, _ workflow.SourceIndexGeneration, _, genDir string) {
		fs.remove(filepath.Join(genDir, sourceindex.ShardDirectoryName))
		fs.symlinkEntry(filepath.Join(genDir, sourceindex.ShardDirectoryName))
	})
	reject("regular file named shards", func(t *testing.T, fs *fakeFS, _ workflow.SourceIndexGeneration, _, genDir string) {
		fs.remove(filepath.Join(genDir, sourceindex.ShardDirectoryName))
		fs.writeFile(filepath.Join(genDir, sourceindex.ShardDirectoryName), []byte("x"))
	})
	t.Run("missing shards entry after descriptor opened", func(t *testing.T) {
		fs, row, root, _ := base(t)
		probe := newProbeFS(fs)
		probe.mutateList = func(entries []dirEntry) []dirEntry {
			filtered := entries[:0]
			for _, e := range entries {
				if e.Name != sourceindex.ShardDirectoryName {
					filtered = append(filtered, e)
				}
			}
			return filtered
		}
		old := boundFS
		boundFS = probe
		defer func() { boundFS = old }()
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		assertFilesClosed(t, probe.opened)
		if n := probe.closeCount[probe.handleNamed(sourceindex.ShardDirectoryName)]; n != 1 {
			t.Fatalf("shards directory closed %d times, want once", n)
		}
	})
	t.Run("duplicate reported shards entries", func(t *testing.T) {
		fs, row, root, _ := base(t)
		probe := newProbeFS(fs)
		probe.mutateList = func(entries []dirEntry) []dirEntry {
			return append(entries, dirEntry{Name: sourceindex.ShardDirectoryName, Mode: os.ModeDir})
		}
		old := boundFS
		boundFS = probe
		defer func() { boundFS = old }()
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		assertFilesClosed(t, probe.opened)
	})
}

func TestVerificationCleanup(t *testing.T) {
	identity, _ := testIdentity(t, "vault")
	build := func(t *testing.T) (*fakeFS, workflow.SourceIndexGeneration, string) {
		fs := newFakeFS(t)
		withFakeFS(fs)
		root := filepath.Join(t.TempDir(), "index")
		row, _, _ := buildGeneration(t, fs, root, generationSpec{identity: identity, indexed: []string{"a.txt"}, shards: map[int][]byte{0: []byte("shard")}})
		return fs, row, root
	}
	attach := func(t *testing.T, fs *fakeFS) *probeFS {
		probe := newProbeFS(fs)
		old := boundFS
		boundFS = probe
		t.Cleanup(func() { boundFS = old })
		return probe
	}
	t.Run("shard count mismatch closes every verified artifact", func(t *testing.T) {
		fs, row, root := build(t)
		probe := attach(t, fs)
		old := verifyGenerationFiles
		verifyGenerationFiles = func(files indexer.VerifiedGenerationFiles, r indexerprotocol.BuildRequest) (*indexer.VerifiedGeneration, error) {
			verified, err := fakeVerifiedGenerationFiles(files, r)
			if err != nil {
				return nil, err
			}
			verified.ShardCount++
			return verified, nil
		}
		t.Cleanup(func() { verifyGenerationFiles = old })
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		assertFilesClosed(t, probe.opened)
	})
	t.Run("enumeration failure closes every opened artifact", func(t *testing.T) {
		fs, row, root := build(t)
		probe := attach(t, fs)
		fs.mkdir(filepath.Join(root, sourceindex.GenerationDirectoryName, identityID(t, identity), "unexpected"))
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		assertFilesClosed(t, probe.opened)
	})
	t.Run("successful verification closes each artifact exactly once", func(t *testing.T) {
		fs, row, root := build(t)
		probe := attach(t, fs)
		r, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		assertFilesClosed(t, probe.opened)
		fs.assertBalancedFiles(t)
	})
	t.Run("shard directory close error is not retried", func(t *testing.T) {
		fs, row, root := build(t)
		probe := attach(t, fs)
		probe.failClose[sourceindex.ShardDirectoryName] = errors.New("close failed")
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		if n := probe.closeCount[probe.handleNamed(sourceindex.ShardDirectoryName)]; n != 1 {
			t.Fatalf("shards directory closed %d times, want once", n)
		}
		assertFilesClosed(t, probe.opened)
	})
	t.Run("cleanup continues across multiple close errors", func(t *testing.T) {
		fs, row, root := build(t)
		probe := attach(t, fs)
		id, _ := sourceindex.GenerationID(identity)
		probe.failClose[sourceindex.ShardDirectoryName] = errors.New("shards close failed")
		probe.failClose[id] = errors.New("generation close failed")
		if _, err := openGeneration(t, fs, &fakeStore{row: row}, Config{IndexRoot: root}, identity); !errors.Is(err, ErrGenerationIntegrity) {
			t.Fatalf("error = %v, want generation integrity", err)
		}
		if n := probe.closeCount[probe.handleNamed(sourceindex.ShardDirectoryName)]; n != 1 {
			t.Fatalf("shards directory closed %d times, want once", n)
		}
		if n := probe.closeCount[probe.handleNamed(id)]; n != 1 {
			t.Fatalf("generation directory closed %d times, want once", n)
		}
		assertFilesClosed(t, probe.opened)
	})
}
