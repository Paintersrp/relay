package indexer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"relay/internal/sourceindex"
	"relay/internal/sourceindex/indexerprotocol"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(gitEnv(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, err := c.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(b))
}

type fixture struct {
	repo, commit, tree string
	request            indexerprotocol.BuildRequest
}

func makeFixture(t *testing.T, files map[string][]byte) fixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	repo := filepath.Join(root, "repo.git")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "--quiet")
	for name, content := range files {
		path := filepath.Join(work, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, work, "add", "--all")
	git(t, work, "commit", "--quiet", "-m", "fixture")
	commit := git(t, work, "rev-parse", "HEAD")
	tree := git(t, work, "rev-parse", "HEAD^{tree}")
	git(t, work, "clone", "--quiet", "--bare", ".", repo)
	options := sourceindex.DefaultBuildOptions()
	od, err := sourceindex.BuildOptionsSHA256(options)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := sourceindex.NewGenerationIdentity("vault", commit, tree, od)
	if err != nil {
		t.Fatal(err)
	}
	id, err := sourceindex.GenerationID(identity)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{repo: repo, commit: commit, tree: tree, request: indexerprotocol.BuildRequest{
		Version: indexerprotocol.ProtocolVersion, GenerationID: id, Identity: identity, BuildOptions: options,
		RepositoryPath: repo, IndexRoot: filepath.Join(root, "index"), StagingNonce: strings.Repeat("a", 32),
	}}
}

func TestRepositoryAuthorityExactObjects(t *testing.T) {
	f := makeFixture(t, map[string][]byte{"a.txt": []byte("hello")})
	if err := repository(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(filepath.Dir(f.repo), "authority-work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, work, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "a.txt")
	git(t, work, "commit", "--quiet", "-m", "authority")
	tree := git(t, work, "rev-parse", "HEAD^{tree}")
	blob := git(t, work, "rev-parse", "HEAD:a.txt")
	tagCommit := git(t, work, "tag", "-a", "commit-tag", "-m", "commit-tag")
	_ = tagCommit
	tagCommitOID := git(t, work, "rev-parse", "refs/tags/commit-tag")
	tagTreeOID := makeAnnotatedTag(t, work, tree, "tree-tag")
	nonBare := filepath.Join(filepath.Dir(f.repo), "non-bare")
	if err := os.Rename(work, nonBare); err != nil {
		t.Fatal(err)
	}
	fetch := exec.Command("git", "--git-dir="+f.repo, "fetch", "--quiet", "--tags", nonBare)
	fetch.Env = gitEnv()
	if output, err := fetch.CombinedOutput(); err != nil {
		t.Fatalf("fetch authority objects: %v\n%s", err, output)
	}
	cases := []struct {
		name string
		edit func(*indexerprotocol.BuildRequest)
		code string
	}{
		{"valid bare repository", func(r *indexerprotocol.BuildRequest) {}, ""},
		{"non-bare repository", func(r *indexerprotocol.BuildRequest) { r.RepositoryPath = nonBare }, "source_unavailable"},
		{"annotated commit tag", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = tagCommitOID }, "source_mismatch"},
		{"blob supplied as commit", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = blob }, "source_mismatch"},
		{"tree supplied as commit", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = tree }, "source_mismatch"},
		{"annotated tree tag", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = tagTreeOID }, "source_mismatch"},
		{"commit supplied as tree", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = f.commit }, "source_mismatch"},
		{"blob supplied as tree", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = blob }, "source_mismatch"},
		{"exact commit and tree", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = f.commit; r.Identity.TreeOID = f.tree }, ""},
		{"missing commit", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = strings.Repeat("0", 40) }, "source_mismatch"},
		{"missing tree", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = strings.Repeat("0", 40) }, "source_mismatch"},
		{"commit tree mismatch", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = f.commit }, "source_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request
			tc.edit(&r)
			if tc.code == "" {
				if err := repository(context.Background(), r); err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *Failure
			if !errors.As(repository(context.Background(), r), &failure) || failure.Code != tc.code {
				t.Fatalf("got %v, want %s", failure, tc.code)
			}
		})
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "file")} {
		if strings.HasSuffix(path, "file") {
			if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		r := f.request
		r.RepositoryPath = path
		if err := repository(context.Background(), r); err == nil {
			t.Fatal("accepted unavailable repository")
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(f.repo, link); err == nil {
		r := f.request
		r.RepositoryPath = link
		if err := repository(context.Background(), r); err == nil {
			t.Fatal("accepted symlink repository")
		}
	}
}

func makeAnnotatedTag(t *testing.T, dir, object, name string) string {
	t.Helper()
	content := fmt.Sprintf("object %s\ntype tree\ntag %s\ntagger test <test@example.invalid> 0 +0000\n\n", object, name)
	c := exec.Command("git", "mktag")
	c.Dir = dir
	c.Env = gitEnv()
	c.Stdin = strings.NewReader(content)
	b, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestTraverseAndClassifyCoverage(t *testing.T) {
	f := makeFixture(t, map[string][]byte{
		"indexed.txt": []byte("one two three"), "short.txt": []byte("é"), "runes.txt": []byte("猫犬鳥"),
		"invalid.bin": {0xff, 0xfe}, "zero-content.bin": {'a', 0, 'b'}, ".gitignore": []byte("ignored"),
		".zoektignore": []byte("ignored"), ".sourcegraph/ignore": []byte("ignored"),
	})
	entries, err := traverse(context.Background(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	coverage, docs, err := classify(context.Background(), f.request, entries)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]sourceindex.CoverageStatus{}
	for _, c := range coverage {
		p, err := c.Path.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		statuses[string(p)] = c.Status
	}
	for path, want := range map[string]sourceindex.CoverageStatus{
		"indexed.txt": sourceindex.CoverageIndexedText, "short.txt": sourceindex.CoverageShortText,
		"runes.txt": sourceindex.CoverageIndexedText, "invalid.bin": sourceindex.CoverageTextIneligible,
		"zero-content.bin": sourceindex.CoverageTextIneligible, ".gitignore": sourceindex.CoverageIndexedText,
		".zoektignore": sourceindex.CoverageIndexedText, ".sourcegraph/ignore": sourceindex.CoverageIndexedText,
	} {
		if statuses[path] != want {
			t.Errorf("%s: got %s, want %s", path, statuses[path], want)
		}
	}
	if len(docs) != 5 {
		t.Fatalf("got %d indexed documents, want 5", len(docs))
	}
	if statuses["indexed.txt"] != sourceindex.CoverageIndexedText || !utf8.Valid([]byte("猫犬鳥")) {
		t.Fatal("text classification failed")
	}
	// Path and size fallbacks are decided before the batch reader is asked for content.
	entries = []treeEntry{{path: []byte{0xff, 'x'}, mode: "100644", typ: "blob", oid: strings.Repeat("1", 40), size: 1}, {path: []byte("large"), mode: "100644", typ: "blob", oid: strings.Repeat("2", 40), size: sourceindex.DefaultFileLimitBytes + 1}, {path: []byte("tree"), mode: "040000", typ: "tree", oid: strings.Repeat("3", 40)}}
	coverage, docs, err = classify(context.Background(), f.request, entries)
	if err != nil || len(docs) != 0 || coverage[0].Status != sourceindex.CoverageFallbackPath || coverage[1].Status != sourceindex.CoverageFallbackSize || coverage[2].Status != sourceindex.CoverageNonBlob {
		t.Fatalf("fallback classification: %v, %#v, %#v", err, coverage, docs)
	}
}

func TestClassificationBoundariesAndContentRequests(t *testing.T) {
	old := commandContext
	defer func() { commandContext = old }()
	entries := []treeEntry{
		{path: []byte("empty"), mode: "100644", typ: "blob", oid: strings.Repeat("1", 40), size: 0},
		{path: []byte("one"), mode: "100644", typ: "blob", oid: strings.Repeat("2", 40), size: 1},
		{path: []byte("two"), mode: "100644", typ: "blob", oid: strings.Repeat("3", 40), size: 2},
		{path: []byte("three"), mode: "100644", typ: "blob", oid: strings.Repeat("4", 40), size: 3},
		{path: []byte("unicode"), mode: "100644", typ: "blob", oid: strings.Repeat("5", 40), size: 6},
		{path: []byte("invalid"), mode: "100644", typ: "blob", oid: strings.Repeat("6", 40), size: 2},
		{path: []byte("nul"), mode: "100644", typ: "blob", oid: strings.Repeat("7", 40), size: 3},
		{path: []byte("exact-limit"), mode: "100644", typ: "blob", oid: strings.Repeat("8", 40), size: sourceindex.DefaultFileLimitBytes},
		{path: []byte("above-limit"), mode: "100644", typ: "blob", oid: strings.Repeat("9", 40), size: sourceindex.DefaultFileLimitBytes + 1},
		{path: []byte{0xff, 'p'}, mode: "100644", typ: "blob", oid: strings.Repeat("a", 40), size: 1},
		{path: []byte("tree"), mode: "040000", typ: "tree", oid: strings.Repeat("b", 40)},
		{path: []byte("submodule"), mode: "160000", typ: "commit", oid: strings.Repeat("c", 40)},
	}
	request := map[string]string{}
	for oid, content := range map[string][]byte{
		strings.Repeat("1", 40): {}, strings.Repeat("2", 40): []byte("a"), strings.Repeat("3", 40): []byte("ab"), strings.Repeat("4", 40): []byte("abc"),
		strings.Repeat("5", 40): []byte("猫犬"), strings.Repeat("6", 40): {0xff, 0xfe}, strings.Repeat("7", 40): {'a', 0, 'b'},
	} {
		request[oid] = base64.StdEncoding.EncodeToString(content)
	}
	request[strings.Repeat("8", 40)] = "size:" + strconv.FormatInt(sourceindex.DefaultFileLimitBytes, 10)
	commandContext = helperCommand("batch", request)
	f := fixture{request: indexerprotocol.BuildRequest{BuildOptions: sourceindex.DefaultBuildOptions()}}
	coverage, docs, err := classify(context.Background(), f.request, entries)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]sourceindex.CoverageStatus{}
	for _, entry := range coverage {
		path, _ := entry.Path.Bytes()
		got[string(path)] = entry.Status
	}
	want := map[string]sourceindex.CoverageStatus{
		"empty": sourceindex.CoverageShortText, "one": sourceindex.CoverageShortText, "two": sourceindex.CoverageShortText, "three": sourceindex.CoverageIndexedText,
		"unicode": sourceindex.CoverageShortText, "invalid": sourceindex.CoverageTextIneligible, "nul": sourceindex.CoverageTextIneligible,
		"exact-limit": sourceindex.CoverageIndexedText, "above-limit": sourceindex.CoverageFallbackSize, string([]byte{0xff, 'p'}): sourceindex.CoverageFallbackPath,
		"tree": sourceindex.CoverageNonBlob, "submodule": sourceindex.CoverageNonBlob,
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%q: got %s, want %s", path, got[path], status)
		}
	}
	if len(docs) != 2 {
		t.Fatalf("got %d indexed documents, want 2", len(docs))
	}
}

func helperCommand(mode string, values map[string]string) func(context.Context, string, ...string) *exec.Cmd {
	args := []string{"-test.run=TestIndexerHelper", "--", mode}
	for key, value := range values {
		args = append(args, key+"="+value)
	}
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], args...)
	}
}

func TestRetainedTreeParserAndOrdering(t *testing.T) {
	old := commandContext
	defer func() { commandContext = old }()
	blobOID, treeOID, linkOID := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	records := []byte("100644 blob " + blobOID + " 3\troot\x00" + "100755 blob " + blobOID + " 3\texec\x00" + "120000 blob " + blobOID + " 3\tlink\x00" + "040000 tree " + treeOID + " -\tnested\x00" + "160000 commit " + linkOID + " -\tsubmodule\x00" + "100644 blob " + blobOID + " 3\t" + string([]byte{0xff, 'x'}) + "\x00")
	commandContext = helperCommand("tree", map[string]string{"output": base64.StdEncoding.EncodeToString(records)})
	entries, err := traverse(context.Background(), indexerprotocol.BuildRequest{RepositoryPath: "ignored", Identity: sourceindex.GenerationIdentity{TreeOID: treeOID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 6 {
		t.Fatalf("got %d entries, want 6", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if bytes.Compare(entries[i-1].path, entries[i].path) >= 0 {
			t.Fatal("entries are not raw-byte ordered")
		}
	}
	for _, entry := range entries {
		if entry.typ == "tree" || entry.typ == "commit" {
			if entry.size != 0 {
				t.Errorf("%s size %d, want zero", entry.typ, entry.size)
			}
		}
		if !hexOID(entry.oid) {
			t.Errorf("invalid parsed oid %q", entry.oid)
		}
	}
	for _, tc := range []struct{ name, record string }{
		{"malformed record", "100644 blob " + blobOID + " 3\tno-nul"},
		{"malformed oid", "100644 blob bad 3\tx\x00"},
		{"malformed size", "100644 blob " + blobOID + " nope\tx\x00"},
		{"unsupported mode", "100600 blob " + blobOID + " 3\tx\x00"},
		{"mode type disagreement", "040000 blob " + blobOID + " 3\tx\x00"},
		{"duplicate path", "100644 blob " + blobOID + " 3\tx\x00100755 blob " + blobOID + " 3\tx\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commandContext = helperCommand("tree", map[string]string{"output": base64.StdEncoding.EncodeToString([]byte(tc.record))})
			if _, err := traverse(context.Background(), indexerprotocol.BuildRequest{RepositoryPath: "ignored", Identity: sourceindex.GenerationIdentity{TreeOID: treeOID}}); err == nil {
				t.Fatal("accepted malformed retained tree")
			}
		})
	}
}

func TestBuildVerifyTamperingAndDeterminism(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pinned Zoekt builder is unsupported on Windows")
	}
	f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma"), "b.txt": []byte("delta epsilon zeta")})
	first := f.request
	first.IndexRoot = filepath.Join(t.TempDir(), "one", "nested")
	result1, err := Build(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	second := f.request
	second.IndexRoot = filepath.Join(t.TempDir(), "two", "nested")
	second.StagingNonce = strings.Repeat("b", 32)
	result2, err := Build(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if result1.GenerationManifestSHA256 != result2.GenerationManifestSHA256 || result1.CoverageManifestSHA256 != result2.CoverageManifestSHA256 || result1.ArtifactManifestSHA256 != result2.ArtifactManifestSHA256 || result1.CoverageCounts != result2.CoverageCounts || result1.ShardCount != result2.ShardCount {
		t.Fatalf("deterministic fields differ: %#v %#v", result1, result2)
	}
	if result1.StagingRelativeDirectory == result2.StagingRelativeDirectory {
		t.Fatal("staging directories did not differ")
	}
	want1, _ := sourceindex.StagingRelativeDirectory(first.GenerationID, first.StagingNonce)
	want2, _ := sourceindex.StagingRelativeDirectory(second.GenerationID, second.StagingNonce)
	if result1.StagingRelativeDirectory != want1 || result2.StagingRelativeDirectory != want2 {
		t.Fatalf("noncanonical staging directories: %q %q", result1.StagingRelativeDirectory, result2.StagingRelativeDirectory)
	}
	read := func(r indexerprotocol.BuildRequest, result indexerprotocol.BuildResult) map[string][]byte {
		out := map[string][]byte{}
		root := filepath.Join(r.IndexRoot, filepath.FromSlash(result.StagingRelativeDirectory))
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)], err = os.ReadFile(path)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	a, b := read(first, result1), read(second, result2)
	if len(a) != len(b) {
		t.Fatalf("artifact sets differ: %d %d", len(a), len(b))
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			t.Fatalf("artifact %s missing from second build", name)
		}
		if !bytes.Equal(a[name], b[name]) {
			t.Fatalf("artifact %s is nondeterministic", name)
		}
	}
	root := filepath.Join(first.IndexRoot, filepath.FromSlash(result1.StagingRelativeDirectory))
	if err := Verify(root, first, result1.ShardCount); err != nil {
		t.Fatal(err)
	}
	coveragePath := filepath.Join(root, sourceindex.CoverageManifestFileName)
	original, _ := os.ReadFile(coveragePath)
	if err := os.WriteFile(coveragePath, append(original, 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(root, first, result1.ShardCount); err == nil {
		t.Fatal("accepted modified coverage")
	}
}

func TestBuildNoReplaceAndCancellationCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pinned Zoekt builder is unsupported on Windows")
	}
	f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
	if _, err := Build(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), f.request); err == nil {
		t.Fatal("replaced existing staging target")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r := f.request
	r.StagingNonce = strings.Repeat("c", 32)
	if _, err := Build(cancelled, r); err == nil {
		t.Fatal("cancelled build succeeded")
	}
	parent := filepath.Join(r.IndexRoot, sourceindex.StagingDirectoryName)
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".relay-build-") {
			t.Fatal("private build directory leaked")
		}
	}
}

func TestStagingSafetyAndFailedBuildLeavesNoTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic staging exposure is unsupported on Windows")
	}
	f := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma")})
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		root func() string
	}{
		{"symlink root", func() string {
			target := filepath.Join(root, "real")
			_ = os.Mkdir(target, 0700)
			link := filepath.Join(root, "root-link")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return link
		}},
		{"regular-file root", func() string {
			path := filepath.Join(root, "root-file")
			_ = os.WriteFile(path, []byte("x"), 0600)
			return path
		}},
		{"symlink intermediate", func() string {
			target := filepath.Join(root, "intermediate-real")
			_ = os.Mkdir(target, 0700)
			link := filepath.Join(root, "intermediate-link")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return filepath.Join(link, "child")
		}},
		{"regular-file intermediate", func() string {
			path := filepath.Join(root, "intermediate-file")
			_ = os.WriteFile(path, []byte("x"), 0600)
			return filepath.Join(path, "child")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request
			r.IndexRoot = tc.root()
			if _, err := Build(context.Background(), r); err == nil {
				t.Fatal("accepted unsafe staging root")
			}
		})
	}
	privateRoot := filepath.Join(root, "private")
	r := f.request
	r.IndexRoot = privateRoot
	r.Identity.TreeOID = strings.Repeat("0", 40)
	if _, err := Build(context.Background(), r); err == nil {
		t.Fatal("failed build succeeded")
	}
	if _, err := os.Stat(filepath.Join(privateRoot, sourceindex.StagingDirectoryName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed build left staging target: %v", err)
	}
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := exposeNoReplace(source, target); err == nil {
		t.Fatal("replaced pre-existing target")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("pre-existing target was removed")
	}
}

func TestVerifyRejectsIndependentGenerationTampering(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pinned Zoekt builder is unsupported on Windows")
	}
	base := makeFixture(t, map[string][]byte{"a.txt": []byte("alpha beta gamma"), "b.txt": []byte("delta epsilon zeta")})
	for _, tc := range []struct {
		name   string
		mutate func(string, indexerprotocol.BuildRequest) error
	}{
		{"modified coverage", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, sourceindex.CoverageManifestFileName), []byte("{}"), 0600)
		}},
		{"modified artifact manifest", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, sourceindex.ArtifactManifestFileName), []byte("{}"), 0600)
		}},
		{"modified generation", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, sourceindex.GenerationManifestFileName), []byte("{}"), 0600)
		}},
		{"wrong coverage digest", func(root string, _ indexerprotocol.BuildRequest) error {
			return editGeneration(root, func(m *sourceindex.GenerationManifest) { m.CoverageManifestSHA256 = strings.Repeat("0", 64) })
		}},
		{"wrong artifact digest", func(root string, _ indexerprotocol.BuildRequest) error {
			return editGeneration(root, func(m *sourceindex.GenerationManifest) { m.ArtifactManifestSHA256 = strings.Repeat("0", 64) })
		}},
		{"wrong coverage commit", func(root string, _ indexerprotocol.BuildRequest) error {
			return editCoverage(root, func(m *sourceindex.CoverageManifest) { m.CommitOID = strings.Repeat("0", 40) })
		}},
		{"wrong coverage tree", func(root string, _ indexerprotocol.BuildRequest) error {
			return editCoverage(root, func(m *sourceindex.CoverageManifest) { m.TreeOID = strings.Repeat("0", 40) })
		}},
		{"missing listed artifact", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.Remove(filepath.Join(root, "shards", "000000.zoekt"))
		}},
		{"unlisted artifact", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, "extra"), []byte("x"), 0600)
		}},
		{"modified shard", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, "shards", "000000.zoekt"), []byte("x"), 0600)
		}},
		{"truncated shard", func(root string, _ indexerprotocol.BuildRequest) error {
			p := filepath.Join(root, "shards", "000000.zoekt")
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			return os.WriteFile(p, b[:len(b)/2], 0600)
		}},
		{"noncontiguous shard", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.Rename(filepath.Join(root, "shards", "000000.zoekt"), filepath.Join(root, "shards", "000001.zoekt"))
		}},
		{"shard count mismatch", func(root string, r indexerprotocol.BuildRequest) error { return verifyWithExpected(root, r, 2) }},
		{"missing indexed document", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, "shards", "000000.zoekt"), []byte("missing"), 0600)
		}},
		{"duplicate indexed document", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, "shards", "000000.zoekt"), []byte("duplicate"), 0600)
		}},
		{"unexpected indexed document", func(root string, _ indexerprotocol.BuildRequest) error {
			return os.WriteFile(filepath.Join(root, "shards", "000000.zoekt"), []byte("unexpected"), 0600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base.request
			r.IndexRoot = filepath.Join(t.TempDir(), "index")
			r.StagingNonce = strings.Repeat("e", 32)
			result, err := Build(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(r.IndexRoot, filepath.FromSlash(result.StagingRelativeDirectory))
			if err := tc.mutate(root, r); err != nil {
				t.Fatal(err)
			}
			if tc.name == "shard count mismatch" {
				return
			}
			if err := Verify(root, r, result.ShardCount); err == nil {
				t.Fatal("accepted tampered generation")
			}
		})
	}
}

func editGeneration(root string, edit func(*sourceindex.GenerationManifest)) error {
	b, err := os.ReadFile(filepath.Join(root, sourceindex.GenerationManifestFileName))
	if err != nil {
		return err
	}
	m, err := sourceindex.ParseGenerationManifest(b)
	if err != nil {
		return err
	}
	edit(&m)
	b, err = sourceindex.MarshalGenerationManifest(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, sourceindex.GenerationManifestFileName), b, 0600)
}
func editCoverage(root string, edit func(*sourceindex.CoverageManifest)) error {
	b, err := os.ReadFile(filepath.Join(root, sourceindex.CoverageManifestFileName))
	if err != nil {
		return err
	}
	m, err := sourceindex.ParseCoverageManifest(b)
	if err != nil {
		return err
	}
	edit(&m)
	b, err = sourceindex.MarshalCoverageManifest(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, sourceindex.CoverageManifestFileName), b, 0600)
}
func verifyWithExpected(root string, r indexerprotocol.BuildRequest, expected int64) error {
	if err := Verify(root, r, expected); err == nil {
		return errors.New("accepted shard count mismatch")
	}
	return nil
}

func TestBatchReaderProtocolFailures(t *testing.T) {
	old := commandContext
	defer func() { commandContext = old }()
	for _, tc := range []struct{ name, output string }{{"bad header", "bad\n"}, {"oversized header", strings.Repeat("x", 4097) + "\n"}, {"bad delimiter", strings.Repeat("1", 40) + " blob 1\nxy"}, {"truncated", strings.Repeat("1", 40) + " blob 2\na\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				return exec.CommandContext(ctx, os.Args[0], "-test.run=TestBatchHelper", "--", tc.output)
			}
			reader, err := newBatchReader(context.Background(), "ignored")
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.blob(treeEntry{oid: strings.Repeat("1", 40), size: 1})
			if err == nil {
				t.Fatal("accepted malformed batch response")
			}
			_ = reader.Close()
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestBatchHelper", "--", "hold")
	}
	reader, err := newBatchReader(ctx, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := reader.Close(); err == nil {
		t.Fatal("cancellation was not reported")
	}
}

func TestBatchReaderSequentialSuccessAndFailures(t *testing.T) {
	old := commandContext
	defer func() { commandContext = old }()
	firstOID, secondOID := strings.Repeat("1", 40), strings.Repeat("2", 40)
	commandContext = helperCommand("batch", map[string]string{firstOID: "", secondOID: base64.StdEncoding.EncodeToString([]byte{0, 1, 255, '\n'})})
	reader, err := newBatchReader(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	first, err := reader.blob(treeEntry{oid: firstOID, size: 0})
	if err != nil || len(first) != 0 {
		t.Fatalf("empty blob: %v %q", err, first)
	}
	second, err := reader.blob(treeEntry{oid: secondOID, size: 4})
	if err != nil || !bytes.Equal(second, []byte{0, 1, 255, '\n'}) {
		t.Fatalf("binary blob: %v %v", err, second)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, response string }{
		{"oid mismatch", strings.Repeat("9", 40) + " blob 1\na\n"},
		{"type mismatch", firstOID + " tree 1\na\n"},
		{"size mismatch", firstOID + " blob 2\na\n"},
		{"missing object", firstOID + " missing\n"},
		{"invalid delimiter", firstOID + " blob 1\naX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commandContext = helperCommand("batch", map[string]string{firstOID: "raw:" + base64.StdEncoding.EncodeToString([]byte(tc.response))})
			reader, err := newBatchReader(context.Background(), "ignored")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reader.blob(treeEntry{oid: firstOID, size: 1}); err == nil {
				t.Fatal("accepted malformed batch response")
			}
			_ = reader.Close()
		})
	}
	commandContext = helperCommand("batch", map[string]string{firstOID: "exit"})
	reader, err = newBatchReader(context.Background(), "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.blob(treeEntry{oid: firstOID, size: 1}); err == nil {
		t.Fatal("accepted premature child exit")
	}
	_ = reader.Close()
}
func TestBatchHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=TestBatchHelper") {
		return
	}
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i+1 >= len(os.Args) {
		return
	}
	value := os.Args[i+1]
	if value == "hold" {
		time.Sleep(time.Hour)
		return
	}
	_, _ = os.Stdout.WriteString(value)
	if !strings.HasSuffix(value, "\n") {
		_, _ = os.Stdout.WriteString("\n")
	}
}

func TestIndexerHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=TestIndexerHelper") {
		return
	}
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if i+1 >= len(os.Args) {
		return
	}
	mode := os.Args[i+1]
	values := map[string]string{}
	for _, arg := range os.Args[i+2:] {
		if key, value, ok := strings.Cut(arg, "="); ok {
			values[key] = value
		}
	}
	if mode == "tree" {
		b, err := base64.StdEncoding.DecodeString(values["output"])
		if err != nil {
			b, _ = base64.RawStdEncoding.DecodeString(values["output"])
		}
		_, _ = os.Stdout.Write(b)
		os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		value, ok := values[scanner.Text()]
		if !ok {
			_, _ = os.Stdout.WriteString(scanner.Text() + " missing\n")
			continue
		}
		if value == "exit" {
			os.Exit(0)
		}
		if strings.HasPrefix(value, "raw:") {
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "raw:"))
			_, _ = os.Stdout.Write(raw)
			continue
		}
		if strings.HasPrefix(value, "size:") {
			n, _ := strconv.ParseInt(strings.TrimPrefix(value, "size:"), 10, 64)
			_, _ = fmt.Fprintf(os.Stdout, "%s blob %d\n", scanner.Text(), n)
			chunk := bytes.Repeat([]byte{'a'}, 1<<20)
			for remaining := n; remaining > 0; {
				count := int64(len(chunk))
				if count > remaining {
					count = remaining
				}
				_, _ = os.Stdout.Write(chunk[:count])
				remaining -= count
			}
			_, _ = os.Stdout.Write([]byte{'\n'})
			continue
		}
		content, _ := base64.StdEncoding.DecodeString(value)
		_, _ = fmt.Fprintf(os.Stdout, "%s blob %d\n", scanner.Text(), len(content))
		_, _ = os.Stdout.Write(content)
		_, _ = os.Stdout.Write([]byte{'\n'})
	}
}
