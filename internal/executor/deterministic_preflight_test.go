package executor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"relay/internal/speccompiler"
)

func TestPreflightDeterministicNilDocument(t *testing.T) {
	result, err := PreflightDeterministicOperations(DeterministicPreflightInput{})
	if err != nil || result.Status != DeterministicPreflightNotPresent || result.Plan != nil || result.Failure != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestPreflightDeterministicOperationsVirtualWorktree(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		document   *speccompiler.DeterministicOperationsDocument
		want       DeterministicPreflightStatus
		failure    string
		assertPlan func(*testing.T, *DeterministicMutationPlan)
	}{
		{
			name:     "create partial coverage",
			document: document("partial", operation("new/a.txt", "create", implContent("created"))),
			want:     DeterministicPreflightReady,
			assertPlan: func(t *testing.T, plan *DeterministicMutationPlan) {
				if plan.Coverage != "partial" || string(plan.Operations[0].After.Bytes) != "created" || !equalStrings(plan.Operations[0].ParentDirectories, []string{"new"}) {
					t.Fatalf("plan=%#v", plan)
				}
			},
		},
		{
			name:  "modify every directive complete coverage",
			files: map[string]string{"a.txt": "one two two three"},
			document: document("complete", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{
				{Kind: "replace", OldText: "one", NewText: "zero", ExpectedOccurrences: 1},
				{Kind: "insert_before", Anchor: "two", Content: "pre-", ExpectedOccurrences: 2},
				{Kind: "insert_after", Anchor: "three", Content: "-post", ExpectedOccurrences: 1},
				{Kind: "remove", OldText: "pre-", ExpectedOccurrences: 2},
				{Kind: "replace_file", ExpectedContent: "zero two two three-post", Content: "done"},
			}})),
			want: DeterministicPreflightReady,
			assertPlan: func(t *testing.T, plan *DeterministicMutationPlan) {
				if got := string(plan.Operations[0].After.Bytes); got != "done" {
					t.Fatalf("after=%q", got)
				}
			},
		},
		{
			name: "create followed by modify",
			document: document("partial",
				operation("a.txt", "create", implContent("old")),
				operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}}),
			),
			want: DeterministicPreflightReady,
			assertPlan: func(t *testing.T, plan *DeterministicMutationPlan) {
				if got := string(plan.Operations[1].After.Bytes); got != "new" {
					t.Fatalf("after=%q", got)
				}
			},
		},
		{
			name:  "rename followed by modify",
			files: map[string]string{"old.txt": "old"},
			document: document("partial",
				rename("old.txt", "new.txt", "old", true, ""),
				operation("new.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}}),
			),
			want: DeterministicPreflightReady,
		},
		{
			name:     "delete followed by create",
			files:    map[string]string{"a.txt": "old"},
			document: document("partial", operation("a.txt", "delete", implExpected("old")), operation("a.txt", "create", implContent("new"))),
			want:     DeterministicPreflightReady,
		},
		{
			name:     "rename chain preserving then replacing content",
			files:    map[string]string{"a.txt": "old"},
			document: document("partial", rename("a.txt", "b.txt", "old", true, ""), rename("b.txt", "c.txt", "old", false, "new")),
			want:     DeterministicPreflightReady,
			assertPlan: func(t *testing.T, plan *DeterministicMutationPlan) {
				if got := string(plan.Operations[1].DestinationAfter.Bytes); got != "new" {
					t.Fatalf("destination after=%q", got)
				}
			},
		},
		{
			name:     "later failure drops entire plan",
			files:    map[string]string{"a.txt": "old"},
			document: document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}}), operation("missing.txt", "delete", implExpected("x"))),
			want:     DeterministicPreflightFailed, failure: "source_missing",
		},
		{
			name:     "missing source first failure",
			document: document("partial", operation("missing.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "remove", OldText: "x", ExpectedOccurrences: 1}}}), operation("also-missing.txt", "delete", implExpected("x"))),
			want:     DeterministicPreflightFailed, failure: "source_missing",
		},
		{
			name:  "existing create destination",
			files: map[string]string{"a.txt": "old"}, document: document("partial", operation("a.txt", "create", implContent("new"))), want: DeterministicPreflightFailed, failure: "destination_exists",
		},
		{
			name:  "existing rename destination",
			files: map[string]string{"a.txt": "old", "b.txt": "present"}, document: document("partial", rename("a.txt", "b.txt", "old", true, "")), want: DeterministicPreflightFailed, failure: "destination_exists",
		},
		{
			name:  "delete expected content mismatch",
			files: map[string]string{"a.txt": "old"}, document: document("partial", operation("a.txt", "delete", implExpected("different"))), want: DeterministicPreflightFailed, failure: "expected_content_mismatch",
		},
		{
			name:  "rename expected content mismatch",
			files: map[string]string{"a.txt": "old"}, document: document("partial", rename("a.txt", "b.txt", "different", true, "")), want: DeterministicPreflightFailed, failure: "expected_content_mismatch",
		},
		{
			name:  "selector occurrence mismatch",
			files: map[string]string{"a.txt": "one"}, document: document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "one", NewText: "two", ExpectedOccurrences: 2}}})), want: DeterministicPreflightFailed, failure: "selector_occurrence_mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newDeterministicPreflightRepo(t, test.files)
			input := preflightInput(t, repo, test.document)
			before := snapshotRepo(t, repo)
			result, err := PreflightDeterministicOperations(input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.want {
				t.Fatalf("status=%q result=%#v", result.Status, result)
			}
			if test.failure != "" && (result.Failure == nil || result.Failure.Code != test.failure) {
				t.Fatalf("failure=%#v", result.Failure)
			}
			if result.Status == DeterministicPreflightReady && test.assertPlan != nil {
				test.assertPlan(t, result.Plan)
			}
			if result.Status == DeterministicPreflightFailed && result.Plan != nil {
				t.Fatalf("failure must not return a plan: %#v", result.Plan)
			}
			assertSnapshotRepo(t, repo, before)
		})
	}
}

func TestPreflightDeterministicFilesystemSafety(t *testing.T) {
	t.Run("invalid UTF-8 text directive", func(t *testing.T) {
		repo := newDeterministicPreflightRepo(t, map[string]string{"a.txt": string([]byte{0xff})})
		result, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "remove", OldText: "x", ExpectedOccurrences: 1}}}))))
		if err != nil || result.Failure == nil || result.Failure.Code != "invalid_utf8" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	t.Run("directory source", func(t *testing.T) {
		repo := newDeterministicPreflightRepo(t, map[string]string{})
		if err := os.Mkdir(filepath.Join(repo, "dir"), 0o755); err != nil {
			t.Fatal(err)
		}
		gitRun(t, repo, "add", "dir")
		gitRun(t, repo, "commit", "--allow-empty", "-m", "directory")
		result, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("dir", "delete", implExpected("x")))))
		if err != nil || result.Failure == nil || result.Failure.Code != "source_not_regular" {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	if runtime.GOOS != "windows" {
		t.Run("symlink source", func(t *testing.T) {
			repo := newDeterministicPreflightRepo(t, map[string]string{"target.txt": "x"})
			if err := os.Symlink("target.txt", filepath.Join(repo, "link.txt")); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", "link.txt")
			gitRun(t, repo, "commit", "-m", "link")
			_, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("link.txt", "delete", implExpected("x")))))
			if err == nil {
				t.Fatal("expected unsafe symlink error")
			}
		})
		t.Run("symlink parent", func(t *testing.T) {
			repo := newDeterministicPreflightRepo(t, map[string]string{"target/a.txt": "x"})
			if err := os.Symlink("target", filepath.Join(repo, "link")); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", "link")
			gitRun(t, repo, "commit", "-m", "link parent")
			_, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("link/a.txt", "delete", implExpected("x")))))
			if err == nil {
				t.Fatal("expected unsafe symlink parent error")
			}
		})
	}
}

func TestPreflightDeterministicRepositoryAdmission(t *testing.T) {
	doc := document("partial", operation("a.txt", "delete", implExpected("x")))
	tests := []struct {
		name  string
		alter func(*testing.T, string)
		input func(*testing.T, string) DeterministicPreflightInput
	}{
		{"branch mismatch", func(*testing.T, string) {}, func(t *testing.T, repo string) DeterministicPreflightInput {
			in := preflightInput(t, repo, doc)
			in.ExpectedBranch = "other"
			return in
		}},
		{"commit mismatch", func(*testing.T, string) {}, func(t *testing.T, repo string) DeterministicPreflightInput {
			in := preflightInput(t, repo, doc)
			in.ExpectedCommit = strings.Repeat("0", 40)
			return in
		}},
		{"dirty index", func(t *testing.T, repo string) {
			writePreflightFile(t, repo, "a.txt", "changed")
			gitRun(t, repo, "add", "a.txt")
		}, preflightInputFor(doc)},
		{"dirty worktree", func(t *testing.T, repo string) { writePreflightFile(t, repo, "a.txt", "changed") }, preflightInputFor(doc)},
		{"untracked file", func(t *testing.T, repo string) { writePreflightFile(t, repo, "untracked.txt", "x") }, preflightInputFor(doc)},
		{"active Git operation", func(t *testing.T, repo string) {
			gitDir := strings.TrimSpace(gitOutput(t, repo, "rev-parse", "--absolute-git-dir"))
			writePreflightFile(t, gitDir, "MERGE_HEAD", "deadbeef")
		}, preflightInputFor(doc)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			local := newDeterministicPreflightRepo(t, map[string]string{"a.txt": "x"})
			test.alter(t, local)
			_, err := PreflightDeterministicOperations(test.input(t, local))
			if !errors.Is(err, ErrDeterministicRepositoryBasis) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestPreflightDeterministicPlanDefensiveBytes(t *testing.T) {
	repo := newDeterministicPreflightRepo(t, map[string]string{"a.txt": "old"})
	input := preflightInput(t, repo, document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}})))
	first, err := PreflightDeterministicOperations(input)
	if err != nil {
		t.Fatal(err)
	}
	first.Plan.Operations[0].Before.Bytes[0] = 'X'
	first.Plan.Operations[0].After.Bytes[0] = 'Y'
	second, err := PreflightDeterministicOperations(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Plan.Operations[0].Before.Bytes) != "old" || string(second.Plan.Operations[0].After.Bytes) != "new" {
		t.Fatalf("plan aliases internal bytes: %#v", second.Plan)
	}
}

func document(coverage string, operations ...speccompiler.DeterministicOperation) *speccompiler.DeterministicOperationsDocument {
	return &speccompiler.DeterministicOperationsDocument{Coverage: coverage, Operations: operations}
}
func operation(path, kind string, implementation speccompiler.DeterministicImplementation) speccompiler.DeterministicOperation {
	return speccompiler.DeterministicOperation{Path: path, Operation: kind, Implementation: implementation}
}
func rename(path, destination, expected string, preserve bool, content string) speccompiler.DeterministicOperation {
	return speccompiler.DeterministicOperation{Path: path, DestinationPath: destination, Operation: "rename", Implementation: speccompiler.DeterministicImplementation{ExpectedContent: expected, PreserveContent: &preserve, Content: content}}
}
func implContent(content string) speccompiler.DeterministicImplementation {
	return speccompiler.DeterministicImplementation{Content: content}
}
func implExpected(content string) speccompiler.DeterministicImplementation {
	return speccompiler.DeterministicImplementation{ExpectedContent: content}
}

func newDeterministicPreflightRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-b", "main")
	gitRun(t, repo, "config", "user.email", "preflight@example.test")
	gitRun(t, repo, "config", "user.name", "Preflight")
	for path, content := range files {
		writePreflightFile(t, repo, path, content)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "--allow-empty", "-m", "initial")
	return repo
}
func preflightInput(t *testing.T, repo string, doc *speccompiler.DeterministicOperationsDocument) DeterministicPreflightInput {
	t.Helper()
	return DeterministicPreflightInput{RepositoryRoot: repo, ExpectedBranch: "main", ExpectedCommit: strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD")), Document: doc}
}
func preflightInputFor(doc *speccompiler.DeterministicOperationsDocument) func(*testing.T, string) DeterministicPreflightInput {
	return func(t *testing.T, repo string) DeterministicPreflightInput { return preflightInput(t, repo, doc) }
}
func writePreflightFile(t *testing.T, root, path, content string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
func gitRun(t *testing.T, repo string, args ...string) {
	t.Helper()
	if output := gitOutput(t, repo, args...); output != "" {
		t.Log(output)
	}
}
func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func snapshotRepo(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snapshot := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			if path != root {
				info, statErr := entry.Info()
				if statErr != nil {
					return statErr
				}
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				snapshot[filepath.ToSlash(relative)+"/"] = []byte(info.Mode().String())
			}
			return nil
		}
		bytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		snapshot[filepath.ToSlash(relative)] = append([]byte(info.Mode().String()+"\x00"), bytes...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot[".git/index"] = []byte(gitOutput(t, root, "hash-object", ".git/index"))
	snapshot[".git/HEAD"] = []byte(gitOutput(t, root, "rev-parse", "HEAD"))
	return snapshot
}
func assertSnapshotRepo(t *testing.T, root string, want map[string][]byte) {
	t.Helper()
	got := snapshotRepo(t, root)
	if len(got) != len(want) {
		t.Fatalf("snapshot length got=%d want=%d\ngot=%v\nwant=%v", len(got), len(want), snapshotKeys(got), snapshotKeys(want))
	}
	for path, expected := range want {
		if !bytes.Equal(got[path], expected) {
			t.Fatalf("repository changed at %s", path)
		}
	}
}
func snapshotKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func equalStrings(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}
