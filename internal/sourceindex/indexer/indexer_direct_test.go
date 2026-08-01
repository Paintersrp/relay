package indexer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	cases := []struct {
		name string
		edit func(*indexerprotocol.BuildRequest)
		code string
	}{
		{"missing commit", func(r *indexerprotocol.BuildRequest) { r.Identity.CommitOID = strings.Repeat("0", 40) }, "source_mismatch"},
		{"missing tree", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = strings.Repeat("0", 40) }, "source_mismatch"},
		{"commit tree mismatch", func(r *indexerprotocol.BuildRequest) { r.Identity.TreeOID = f.commit }, "source_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request
			tc.edit(&r)
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
	if result1 != result2 {
		t.Fatalf("build results differ: %#v %#v", result1, result2)
	}
	read := func(r indexerprotocol.BuildRequest, result indexerprotocol.BuildResult) map[string][]byte {
		out := map[string][]byte{}
		root := filepath.Join(r.IndexRoot, filepath.FromSlash(result.StagingRelativeDirectory))
		for _, name := range []string{"coverage.json", "manifest.json", "generation.json", "shards/000000.zoekt"} {
			out[name], _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		}
		return out
	}
	a, b := read(first, result1), read(second, result2)
	for name := range a {
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
