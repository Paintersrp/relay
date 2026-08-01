// Package indexer builds and verifies one staged source-index generation.
package indexer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexerprotocol"
	"relay/internal/sourceindex/zoektbuild"
)

const ShardContentLimitBytes int64 = 256 << 20

type Failure struct{ Code, Message string }

func (e *Failure) Error() string      { return e.Code + ": " + e.Message }
func fail(code, message string) error { return &Failure{code, message} }
func hexOID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type treeEntry struct {
	path           []byte
	mode, typ, oid string
	size           int64
}
type document struct{ path, content []byte }

func gitEnv() []string {
	out := make([]string, 0)
	for _, e := range os.Environ() {
		name := e[:strings.IndexByte(e, '=')]
		if !strings.HasPrefix(name, "GIT_") && name != "HOME" && name != "XDG_CONFIG_HOME" && name != "XDG_DATA_HOME" {
			out = append(out, e)
		}
	}
	return append(out, "HOME=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME=", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
}
func runGit(ctx context.Context, repo string, args ...string) ([]byte, error) {
	a := append([]string{"--no-replace-objects", "-c", "credential.helper=", "--git-dir=" + repo}, args...)
	c := exec.CommandContext(ctx, "git", a...)
	c.Env = gitEnv()
	return c.Output()
}
func repository(ctx context.Context, r indexerprotocol.BuildRequest) error {
	i, e := os.Lstat(r.RepositoryPath)
	if e != nil || i.Mode()&os.ModeSymlink != 0 || !i.IsDir() {
		return fail("source_unavailable", "retained repository is unavailable")
	}
	b, e := runGit(ctx, r.RepositoryPath, "rev-parse", "--is-bare-repository")
	if e != nil || strings.TrimSpace(string(b)) != "true" {
		return fail("source_unavailable", "repository is not bare")
	}
	b, e = runGit(ctx, r.RepositoryPath, "cat-file", "-t", r.Identity.CommitOID)
	if e != nil || strings.TrimSpace(string(b)) != "commit" {
		return fail("source_mismatch", "commit is unavailable")
	}
	b, e = runGit(ctx, r.RepositoryPath, "cat-file", "-t", r.Identity.TreeOID)
	if e != nil || strings.TrimSpace(string(b)) != "tree" {
		return fail("source_mismatch", "tree is unavailable")
	}
	b, e = runGit(ctx, r.RepositoryPath, "rev-parse", r.Identity.CommitOID+"^{tree}")
	if e != nil || strings.TrimSpace(string(b)) != r.Identity.TreeOID {
		return fail("source_mismatch", "commit tree does not match identity")
	}
	return nil
}
func traverse(ctx context.Context, r indexerprotocol.BuildRequest) ([]treeEntry, error) {
	b, e := runGit(ctx, r.RepositoryPath, "ls-tree", "-r", "-t", "-z", "-l", "--full-tree", r.Identity.TreeOID)
	if e != nil {
		return nil, fail("tree_invalid", "cannot traverse retained tree")
	}
	var out []treeEntry
	for len(b) > 0 {
		n := bytes.IndexByte(b, 0)
		if n < 0 {
			return nil, fail("tree_invalid", "malformed tree record")
		}
		x := b[:n]
		b = b[n+1:]
		tab := bytes.IndexByte(x, '\t')
		if tab < 0 {
			return nil, fail("tree_invalid", "malformed tree record")
		}
		f := bytes.Fields(x[:tab])
		if len(f) != 4 {
			return nil, fail("tree_invalid", "malformed tree fields")
		}
		size := int64(0)
		if string(f[2]) == "blob" {
			var er error
			size, er = strconv.ParseInt(string(f[3]), 10, 64)
			if er != nil || size < 0 {
				return nil, fail("tree_invalid", "invalid blob size")
			}
		} else if string(f[3]) != "-" {
			return nil, fail("tree_invalid", "invalid tree size")
		}
		en := treeEntry{append([]byte(nil), x[tab+1:]...), string(f[0]), string(f[1]), string(f[2]), size}
		if !hexOID(en.oid) || !validEntry(en) {
			return nil, fail("tree_invalid", "invalid retained tree entry")
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].path, out[j].path) < 0 })
	for i := 1; i < len(out); i++ {
		if bytes.Equal(out[i-1].path, out[i].path) {
			return nil, fail("tree_invalid", "duplicate retained tree path")
		}
	}
	return out, nil
}
func validEntry(e treeEntry) bool {
	if len(e.path) == 0 || e.path[0] == '/' {
		return false
	}
	for _, p := range bytes.Split(e.path, []byte{'/'}) {
		if len(p) == 0 || bytes.Equal(p, []byte(".")) || bytes.Equal(p, []byte("..")) {
			return false
		}
	}
	return (e.mode == "040000" && e.typ == "tree") || (e.mode == "100644" && e.typ == "blob") || (e.mode == "100755" && e.typ == "blob") || (e.mode == "120000" && e.typ == "blob") || (e.mode == "160000" && e.typ == "commit")
}

type limitedBuffer struct{ b []byte }

func (w *limitedBuffer) Write(p []byte) (int, error) {
	const limit = 4096
	if len(w.b) < limit {
		w.b = append(w.b, p[:min(len(p), limit-len(w.b))]...)
	}
	return len(p), nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type batchReader struct {
	ctx context.Context
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func newBatchReader(ctx context.Context, repo string) (*batchReader, error) {
	c := exec.CommandContext(ctx, "git", "--no-replace-objects", "-c", "credential.helper=", "--git-dir="+repo, "cat-file", "--batch")
	c.Env = gitEnv()
	in, e := c.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := c.StdoutPipe()
	if e != nil {
		return nil, e
	}
	c.Stderr = &limitedBuffer{}
	if e = c.Start(); e != nil {
		return nil, e
	}
	return &batchReader{ctx, c, in, bufio.NewReaderSize(out, 8192)}, nil
}
func (b *batchReader) Close() error {
	_ = b.in.Close()
	e := b.cmd.Wait()
	if b.ctx.Err() != nil {
		return b.ctx.Err()
	}
	return e
}
func (b *batchReader) readLine() ([]byte, error) {
	v, e := b.out.ReadSlice('\n')
	if e != nil || len(v) > 4096 {
		return nil, errors.New("invalid batch header")
	}
	return v[:len(v)-1], nil
}
func (b *batchReader) blob(e treeEntry) ([]byte, error) {
	if b.ctx.Err() != nil {
		return nil, b.ctx.Err()
	}
	if _, er := io.WriteString(b.in, e.oid+"\n"); er != nil {
		return nil, er
	}
	h, er := b.readLine()
	if er != nil {
		return nil, er
	}
	f := bytes.Fields(h)
	if len(f) != 3 || string(f[0]) != e.oid || string(f[1]) != "blob" {
		return nil, errors.New("invalid batch object")
	}
	n, er := strconv.ParseInt(string(f[2]), 10, 64)
	if er != nil || n != e.size || n < 0 {
		return nil, errors.New("invalid batch size")
	}
	content := make([]byte, n)
	if _, er = io.ReadFull(b.out, content); er != nil {
		return nil, er
	}
	var delimiter [1]byte
	if _, er = io.ReadFull(b.out, delimiter[:]); er != nil || delimiter[0] != '\n' {
		return nil, errors.New("invalid batch delimiter")
	}
	return content, nil
}
func classify(ctx context.Context, r indexerprotocol.BuildRequest, entries []treeEntry) ([]sourceindex.CoverageEntry, []document, error) {
	reader, er := newBatchReader(ctx, r.RepositoryPath)
	if er != nil {
		return nil, nil, fail("content_read_failed", "cannot start retained blob reader")
	}
	cs := make([]sourceindex.CoverageEntry, 0, len(entries))
	var docs []document
	for _, e := range entries {
		p, er := sourceindex.NewPathIdentity(e.path)
		if er != nil {
			_ = reader.Close()
			return nil, nil, fail("tree_invalid", "invalid retained path")
		}
		c := sourceindex.CoverageEntry{Path: p, Mode: e.mode, ObjectType: e.typ, ObjectOID: e.oid, SizeBytes: e.size, Status: sourceindex.CoverageNonBlob}
		if e.typ != "blob" {
			cs = append(cs, c)
			continue
		}
		if !utf8.Valid(e.path) {
			c.Status = sourceindex.CoverageFallbackPath
			cs = append(cs, c)
			continue
		}
		if e.size > r.BuildOptions.FileLimitBytes {
			c.Status = sourceindex.CoverageFallbackSize
			cs = append(cs, c)
			continue
		}
		b, er := reader.blob(e)
		if er != nil {
			_ = reader.Close()
			return nil, nil, fail("content_read_failed", "retained blob content is unavailable")
		}
		if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
			c.Status = sourceindex.CoverageTextIneligible
		} else if utf8.RuneCount(b) < 3 {
			c.Status = sourceindex.CoverageShortText
		} else {
			c.Status = sourceindex.CoverageIndexedText
			docs = append(docs, document{e.path, b})
		}
		cs = append(cs, c)
	}
	if er := reader.Close(); er != nil {
		return nil, nil, fail("content_read_failed", "retained blob reader failed")
	}
	return cs, docs, nil
}
func digest(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func write(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	x := f.Close()
	if e != nil {
		return e
	}
	return x
}
func metadata(r indexerprotocol.BuildRequest) (zoektbuild.Metadata, error) {
	name, e := sourceindex.GenerationRepositoryName(r.GenerationID)
	if e != nil {
		return zoektbuild.Metadata{}, e
	}
	return zoektbuild.Metadata{RepositoryName: name, Branch: sourceindex.GenerationBranchName, Version: r.Identity.CommitOID, IndexOptions: r.Identity.BuildOptionsSHA256, Values: map[string]string{"relay_generation_id": r.GenerationID, "relay_vault_id": r.Identity.VaultID, "relay_commit_oid": r.Identity.CommitOID, "relay_tree_oid": r.Identity.TreeOID, "relay_engine_revision": r.Identity.EngineRevision, "relay_build_contract_version": r.Identity.BuildContractVersion, "relay_build_options_sha256": r.Identity.BuildOptionsSHA256}}, nil
}
func shards(root string, r indexerprotocol.BuildRequest, docs []document, limit int64) (int64, error) {
	m, e := metadata(r)
	if e != nil {
		return 0, e
	}
	d := filepath.Join(root, sourceindex.ShardDirectoryName)
	if e = os.Mkdir(d, 0700); e != nil {
		return 0, e
	}
	seq := 0
	var group []zoektbuild.Document
	var total int64
	flush := func() error {
		p := filepath.Join(d, fmt.Sprintf("%06d.zoekt", seq))
		if e := zoektbuild.Write(p, r.GenerationID, seq, m, group); e != nil {
			return e
		}
		seq++
		group = nil
		total = 0
		return nil
	}
	for _, x := range docs {
		n := int64(len(x.path) + len(x.content))
		if len(group) > 0 && total+n > limit {
			if e := flush(); e != nil {
				return 0, e
			}
		}
		group = append(group, zoektbuild.Document{Name: string(x.path), Content: x.content})
		total += n
	}
	if e := flush(); e != nil {
		return 0, e
	}
	return int64(seq), nil
}
func artifactFiles(root string) ([]sourceindex.ArtifactFile, error) {
	var fs []sourceindex.ArtifactFile
	e := filepath.Walk(root, func(p string, i os.FileInfo, er error) error {
		if er != nil {
			return er
		}
		if p == root {
			return nil
		}
		if i.Mode()&os.ModeSymlink != 0 || !i.Mode().IsRegular() && !i.IsDir() {
			return errors.New("unsafe output")
		}
		if i.IsDir() {
			if filepath.ToSlash(p) != filepath.ToSlash(filepath.Join(root, sourceindex.ShardDirectoryName)) {
				return errors.New("unexpected directory")
			}
			return nil
		}
		rel, er := filepath.Rel(root, p)
		if er != nil {
			return er
		}
		rel = filepath.ToSlash(rel)
		if rel == sourceindex.ArtifactManifestFileName || rel == sourceindex.GenerationManifestFileName {
			return nil
		}
		if rel != sourceindex.CoverageManifestFileName && (!strings.HasPrefix(rel, "shards/") || strings.Contains(strings.TrimPrefix(rel, "shards/"), "/")) {
			return errors.New("unexpected output")
		}
		if strings.HasPrefix(rel, "shards/") {
			if len(rel) != len("shards/000000.zoekt") || !strings.HasSuffix(rel, ".zoekt") {
				return errors.New("unexpected output")
			}
			for _, digit := range rel[len("shards/") : len("shards/")+6] {
				if digit < '0' || digit > '9' {
					return errors.New("unexpected output")
				}
			}
		}
		b, er := os.ReadFile(p)
		if er != nil {
			return er
		}
		kind := sourceindex.ArtifactZoektMetadata
		if rel == sourceindex.CoverageManifestFileName {
			kind = sourceindex.ArtifactCoverage
		} else if strings.HasSuffix(rel, ".zoekt") {
			kind = sourceindex.ArtifactZoektShard
		}
		fs = append(fs, sourceindex.ArtifactFile{Kind: kind, RelativePath: rel, SHA256: digest(b), SizeBytes: int64(len(b))})
		return nil
	})
	return fs, e
}
func Verify(root string, r indexerprotocol.BuildRequest, expectedShards int64) error {
	gb, e := os.ReadFile(filepath.Join(root, sourceindex.GenerationManifestFileName))
	if e != nil {
		return e
	}
	g, e := sourceindex.ParseGenerationManifest(gb)
	if e != nil {
		return e
	}
	cb, e := os.ReadFile(filepath.Join(root, sourceindex.CoverageManifestFileName))
	if e != nil {
		return e
	}
	c, e := sourceindex.ParseCoverageManifest(cb)
	if e != nil {
		return e
	}
	ab, e := os.ReadFile(filepath.Join(root, sourceindex.ArtifactManifestFileName))
	if e != nil {
		return e
	}
	a, e := sourceindex.ParseArtifactManifest(ab)
	if e != nil {
		return e
	}
	gd, e := sourceindex.GenerationManifestSHA256(g)
	if e != nil {
		return e
	}
	cd, e := sourceindex.CoverageManifestSHA256(c)
	if e != nil {
		return e
	}
	ad, e := sourceindex.ArtifactManifestSHA256(a)
	if e != nil {
		return e
	}
	if gd != digest(gb) || cd != digest(cb) || ad != digest(ab) || g.GenerationID != r.GenerationID || c.GenerationID != r.GenerationID || c.CommitOID != r.Identity.CommitOID || c.TreeOID != r.Identity.TreeOID || a.GenerationID != r.GenerationID || g.Identity != r.Identity || g.CoverageManifestSHA256 != digest(cb) || g.ArtifactManifestSHA256 != digest(ab) {
		return errors.New("manifest integrity")
	}
	files, e := artifactFiles(root)
	if e != nil {
		return e
	}
	listed, e := sourceindex.NewArtifactManifest(r.GenerationID, files)
	if e != nil || !sameArtifacts(a, listed) {
		return errors.New("artifact integrity")
	}
	m, e := metadata(r)
	if e != nil {
		return e
	}
	var n int64
	expectedDocuments := map[string]bool{}
	for _, entry := range c.Entries {
		path, er := entry.Path.Bytes()
		if er != nil {
			return er
		}
		if entry.Status == sourceindex.CoverageIndexedText {
			expectedDocuments[string(path)] = true
		}
	}
	seenDocuments := map[string]bool{}
	for _, f := range a.Files {
		if f.Kind == sourceindex.ArtifactZoektShard {
			n++
			seq := fmt.Sprintf("shards/%06d.zoekt", n-1)
			if f.RelativePath != seq {
				return errors.New("noncontiguous shard")
			}
			if e := zoektbuild.Verify(filepath.Join(root, filepath.FromSlash(f.RelativePath)), r.GenerationID, int(n-1), m); e != nil {
				return e
			}
			documents, er := zoektbuild.Documents(filepath.Join(root, filepath.FromSlash(f.RelativePath)), r.GenerationID, int(n-1), m)
			if er != nil {
				return er
			}
			for _, document := range documents {
				if !expectedDocuments[document] || seenDocuments[document] {
					return errors.New("unexpected shard document")
				}
				seenDocuments[document] = true
			}
		}
	}
	if n != expectedShards {
		return errors.New("shard count")
	}
	if len(seenDocuments) != len(expectedDocuments) {
		return errors.New("missing shard document")
	}
	return nil
}

func safeDirectory(path string) error {
	i, e := os.Lstat(path)
	if e != nil || i.Mode()&os.ModeSymlink != 0 || !i.IsDir() {
		return errors.New("unsafe directory")
	}
	return nil
}
func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	e = f.Sync()
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
func syncBuild(root, parent string) error {
	for _, name := range []string{sourceindex.CoverageManifestFileName, sourceindex.ArtifactManifestFileName, sourceindex.GenerationManifestFileName} {
		f, e := os.Open(filepath.Join(root, name))
		if e != nil {
			return e
		}
		e = f.Sync()
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if e := syncDirectory(filepath.Join(root, sourceindex.ShardDirectoryName)); e != nil {
		return e
	}
	if e := syncDirectory(root); e != nil {
		return e
	}
	return syncDirectory(parent)
}
func sameArtifacts(a, b sourceindex.ArtifactManifest) bool {
	if len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		if a.Files[i] != b.Files[i] {
			return false
		}
	}
	return true
}
func Build(ctx context.Context, r indexerprotocol.BuildRequest) (indexerprotocol.BuildResult, error) {
	if e := repository(ctx, r); e != nil {
		return indexerprotocol.BuildResult{}, e
	}
	target, e := sourceindex.StagingDirectory(r.IndexRoot, r.GenerationID, r.StagingNonce)
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("unsafe_path", "unsafe staging path")
	}
	if e = os.MkdirAll(r.IndexRoot, 0700); e != nil || safeDirectory(r.IndexRoot) != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot create index root")
	}
	parent := filepath.Dir(target)
	if e = os.MkdirAll(parent, 0700); e != nil || safeDirectory(parent) != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot create staging directory")
	}
	if _, e = os.Lstat(target); e == nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "staging target exists")
	} else if !errors.Is(e, os.ErrNotExist) {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot inspect staging target")
	}
	tmp, e := os.MkdirTemp(parent, ".relay-build-")
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot create private build directory")
	}
	if safeDirectory(parent) != nil || safeDirectory(tmp) != nil {
		_ = os.RemoveAll(tmp)
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "unsafe staging directory")
	}
	defer os.RemoveAll(tmp)
	entries, e := traverse(ctx, r)
	if e != nil {
		return indexerprotocol.BuildResult{}, e
	}
	coverage, docs, e := classify(ctx, r, entries)
	if e != nil {
		return indexerprotocol.BuildResult{}, e
	}
	cm, e := sourceindex.NewCoverageManifest(r.GenerationID, r.Identity.CommitOID, r.Identity.TreeOID, coverage)
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("internal", "coverage construction failed")
	}
	cb, _ := sourceindex.MarshalCoverageManifest(cm)
	if e = write(filepath.Join(tmp, sourceindex.CoverageManifestFileName), cb); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot write coverage manifest")
	}
	count, e := shards(tmp, r, docs, ShardContentLimitBytes)
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("index_build_failed", "cannot build zoekt shard")
	}
	af, e := artifactFiles(tmp)
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot enumerate artifacts")
	}
	am, e := sourceindex.NewArtifactManifest(r.GenerationID, af)
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("internal", "artifact manifest construction failed")
	}
	ab, _ := sourceindex.MarshalArtifactManifest(am)
	if e = write(filepath.Join(tmp, sourceindex.ArtifactManifestFileName), ab); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot write artifact manifest")
	}
	gm, e := sourceindex.NewGenerationManifest(r.Identity, digest(cb), digest(ab))
	if e != nil {
		return indexerprotocol.BuildResult{}, fail("internal", "generation manifest construction failed")
	}
	gb, _ := sourceindex.MarshalGenerationManifest(gm)
	if e = write(filepath.Join(tmp, sourceindex.GenerationManifestFileName), gb); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot write generation manifest")
	}
	if e = Verify(tmp, r, count); e != nil {
		return indexerprotocol.BuildResult{}, fail("verification_failed", "staged generation verification failed")
	}
	if e = ctx.Err(); e != nil {
		return indexerprotocol.BuildResult{}, fail("cancelled", "build cancelled")
	}
	if e = syncBuild(tmp, parent); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot durably prepare staging generation")
	}
	if e = exposeNoReplace(tmp, target); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot expose staging generation")
	}
	if e = syncDirectory(parent); e != nil {
		return indexerprotocol.BuildResult{}, fail("artifact_write_failed", "cannot durably expose staging generation")
	}
	rel, _ := sourceindex.StagingRelativeDirectory(r.GenerationID, r.StagingNonce)
	return indexerprotocol.BuildResult{StagingRelativeDirectory: rel, GenerationManifestSHA256: digest(gb), CoverageManifestSHA256: digest(cb), ArtifactManifestSHA256: digest(ab), CoverageCounts: cm.Counts, ShardCount: count}, nil
}

var _ = io.EOF
