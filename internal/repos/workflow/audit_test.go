package workflowrepos

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type auditScriptRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []string
}

func (r *auditScriptRunner) Run(_ context.Context, _ string, _ int, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if err := r.errors[key]; err != nil {
		return nil, err
	}
	return append([]byte(nil), r.outputs[key]...), nil
}

func nul(parts ...string) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, []byte(p)...)
		b = append(b, 0)
	}
	return b
}

func numstat(path, additions, deletions string) []byte {
	return []byte(additions + "\t" + deletions + "\t" + path + "\x00")
}

func renameNumstat(previous, path, additions, deletions string) []byte {
	return []byte(
		additions + "\t" + deletions + "\t\x00" +
			previous + "\x00" +
			path + "\x00",
	)
}

func validAuditRunner() *auditScriptRunner {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	rangeSpec := base + ".." + head
	return &auditScriptRunner{
		outputs: map[string][]byte{
			"symbolic-ref --quiet --short HEAD":                               []byte("feat/simplification\n"),
			"rev-parse --verify HEAD":                                         []byte(head + "\n"),
			"status --porcelain=v1 --untracked-files=all":                     nil,
			"rev-parse --absolute-git-dir":                                    []byte("/repo/.git\n"),
			"cat-file -e " + head + "^{commit}":                               nil,
			"merge-base --is-ancestor " + base + " " + head:                   nil,
			"diff --name-status --no-renames " + rangeSpec:                    []byte("M\tinternal/a.go\nA\tinternal/b.go\n"),
			"diff --stat --no-renames " + rangeSpec:                           []byte("2 files changed\n"),
			"log --format=%H%x09%an%x09%aI%x09%s " + rangeSpec:                []byte(head + "\tDev\t2026-07-06T00:00:00Z\tchange\n"),
			"diff --binary --no-ext-diff --no-renames " + rangeSpec:           []byte("diff --git a/internal/a.go b/internal/a.go\n"),
			"diff --name-status -z --find-renames --find-copies " + rangeSpec: nul("M", "internal/a.go", "A", "internal/b.go"),
			"diff --numstat -z --find-renames --find-copies " + rangeSpec: append(
				numstat("internal/a.go", "12", "4"),
				numstat("internal/b.go", "5", "0")...,
			),
		},
		errors: map[string]error{},
	}
}

func TestInspectAuditCommitCapturesExactRange(t *testing.T) {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	runner := validAuditRunner()
	result, err := InspectAuditCommitWithRunner(context.Background(), t.TempDir(), "feat/simplification", base, head, runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.BaseCommit != base || result.AuditedCommit != head || result.Branch != "feat/simplification" {
		t.Fatalf("identity = %+v", result)
	}
	if strings.Join(result.ChangedFiles, ",") != "internal/a.go,internal/b.go" {
		t.Fatalf("changed files = %v", result.ChangedFiles)
	}
	if result.NameStatus != "M\tinternal/a.go\nA\tinternal/b.go" {
		t.Fatalf("name_status = %q", result.NameStatus)
	}
	if result.DiffStat != "2 files changed" {
		t.Fatalf("diff_stat = %q", result.DiffStat)
	}
	if !strings.Contains(result.CommitLog, head) {
		t.Fatalf("commit_log = %q", result.CommitLog)
	}
	if !strings.Contains(result.Diff, "diff --git") {
		t.Fatal("diff was not captured")
	}
	if len(result.FileChanges) != 2 {
		t.Fatalf("file changes = %+v", result.FileChanges)
	}
	if got := result.FileChanges[0]; got.Path != "internal/a.go" || got.PreviousPath != "" || got.ChangeType != "modified" || got.Additions != 12 || got.Deletions != 4 {
		t.Fatalf("first file change = %+v", got)
	}
	if got := result.FileChanges[1]; got.Path != "internal/b.go" || got.PreviousPath != "" || got.ChangeType != "added" || got.Additions != 5 || got.Deletions != 0 {
		t.Fatalf("second file change = %+v", got)
	}
	wantStatus := "diff --name-status -z --find-renames --find-copies " + base + ".." + head
	wantNumstat := "diff --numstat -z --find-renames --find-copies " + base + ".." + head
	if !sliceContains(runner.calls, wantStatus) {
		t.Fatalf("structured status command not issued; calls = %v", runner.calls)
	}
	if !sliceContains(runner.calls, wantNumstat) {
		t.Fatalf("structured numstat command not issued; calls = %v", runner.calls)
	}
	for _, call := range runner.calls {
		verb := strings.Fields(call)
		if len(verb) == 0 {
			continue
		}
		for _, forbidden := range []string{"checkout", "switch", "fetch", "pull", "add", "commit", "push", "reset", "restore", "clean", "stash"} {
			if verb[0] == forbidden {
				t.Fatalf("mutating Git command used: %s", call)
			}
		}
	}
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestInspectAuditCommitBlocksInvalidAuthority(t *testing.T) {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	tests := []struct {
		name   string
		mutate func(*auditScriptRunner)
		want   string
	}{
		{name: "dirty", mutate: func(r *auditScriptRunner) {
			r.outputs["status --porcelain=v1 --untracked-files=all"] = []byte(" M file.go\n")
		}, want: "repository_dirty"},
		{name: "missing commit", mutate: func(r *auditScriptRunner) {
			r.errors["cat-file -e "+head+"^{commit}"] = errors.New("missing")
		}, want: "does not exist"},
		{name: "not descendant", mutate: func(r *auditScriptRunner) {
			r.errors["merge-base --is-ancestor "+base+" "+head] = errors.New("not ancestor")
		}, want: "not descended"},
		{name: "empty range", mutate: func(r *auditScriptRunner) {
			r.outputs["diff --name-status --no-renames "+base+".."+head] = nil
		}, want: "contains no changes"},
		{name: "oversized diff", mutate: func(r *auditScriptRunner) {
			r.errors["diff --binary --no-ext-diff --no-renames "+base+".."+head] = ErrAuditGitOutputTooLarge
		}, want: "configured bound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := validAuditRunner()
			tt.mutate(runner)
			_, err := InspectAuditCommitWithRunner(context.Background(), t.TempDir(), "feat/simplification", base, head, runner)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseAuditFileChanges(t *testing.T) {
	tests := []struct {
		name        string
		status      []byte
		numstat     []byte
		want        []AuditFileChange
		wantErr     bool
		errContains string
	}{
		{
			name:    "added file with exact counts",
			status:  nul("A", "pkg/new.go"),
			numstat: numstat("pkg/new.go", "5", "0"),
			want: []AuditFileChange{
				{Path: "pkg/new.go", ChangeType: "added", Additions: 5, Deletions: 0},
			},
		},
		{
			name:    "modified file with exact counts",
			status:  nul("M", "pkg/existing.go"),
			numstat: numstat("pkg/existing.go", "12", "4"),
			want: []AuditFileChange{
				{Path: "pkg/existing.go", ChangeType: "modified", Additions: 12, Deletions: 4},
			},
		},
		{
			name:    "deleted file with exact counts",
			status:  nul("D", "pkg/removed.go"),
			numstat: numstat("pkg/removed.go", "0", "10"),
			want: []AuditFileChange{
				{Path: "pkg/removed.go", ChangeType: "deleted", Additions: 0, Deletions: 10},
			},
		},
		{
			name:    "type-changed file",
			status:  nul("T", "pkg/symlink.go"),
			numstat: numstat("pkg/symlink.go", "0", "0"),
			want: []AuditFileChange{
				{Path: "pkg/symlink.go", ChangeType: "type_changed", Additions: 0, Deletions: 0},
			},
		},
		{
			name:    "rename with old path and counts",
			status:  nul("R100", "pkg/old.go", "pkg/new.go"),
			numstat: renameNumstat("pkg/old.go", "pkg/new.go", "1", "2"),
			want: []AuditFileChange{
				{Path: "pkg/new.go", PreviousPath: "pkg/old.go", ChangeType: "renamed", Additions: 1, Deletions: 2},
			},
		},
		{
			name:    "copy with old path and counts",
			status:  nul("C100", "pkg/original.go", "pkg/copied.go"),
			numstat: renameNumstat("pkg/original.go", "pkg/copied.go", "3", "0"),
			want: []AuditFileChange{
				{Path: "pkg/copied.go", PreviousPath: "pkg/original.go", ChangeType: "copied", Additions: 3, Deletions: 0},
			},
		},
		{
			name:   "mixed changes return deterministic path order",
			status: nul("A", "new/b.go", "M", "new/a.go", "R100", "old.go", "new/z.go"),
			numstat: append(
				append(
					renameNumstat("old.go", "new/z.go", "2", "3"),
					numstat("new/b.go", "0", "0")...,
				),
				numstat("new/a.go", "5", "1")...,
			),
			want: []AuditFileChange{
				{Path: "new/a.go", ChangeType: "modified", Additions: 5, Deletions: 1},
				{Path: "new/b.go", ChangeType: "added", Additions: 0, Deletions: 0},
				{Path: "new/z.go", PreviousPath: "old.go", ChangeType: "renamed", Additions: 2, Deletions: 3},
			},
		},
		{
			name:    "binary file counts are represented as zero",
			status:  nul("A", "assets/icon.png"),
			numstat: numstat("assets/icon.png", "-", "-"),
			want: []AuditFileChange{
				{Path: "assets/icon.png", ChangeType: "added", Additions: 0, Deletions: 0},
			},
		},
		{
			name:    "file names containing spaces are supported",
			status:  nul("A", "my file with spaces.txt"),
			numstat: numstat("my file with spaces.txt", "1", "0"),
			want: []AuditFileChange{
				{Path: "my file with spaces.txt", ChangeType: "added", Additions: 1, Deletions: 0},
			},
		},
		{
			name:        "name-status path containing tab is rejected",
			status:      nul("A", "my\tfile.txt"),
			numstat:     numstat("valid.txt", "1", "0"),
			wantErr:     true,
			errContains: "control character",
		},
		{
			name:        "numstat path containing tab is rejected",
			status:      nul("A", "valid.txt"),
			numstat:     numstat("my\tfile.txt", "1", "0"),
			wantErr:     true,
			errContains: "control character",
		},
		{
			name:        "rename previous path containing tab is rejected",
			status:      nul("R100", "old\tpath.go", "new.go"),
			numstat:     renameNumstat("old.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "control character",
		},
		{
			name:        "rename resulting path containing tab is rejected",
			status:      nul("R100", "old.go", "new\tpath.go"),
			numstat:     renameNumstat("old.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "control character",
		},
		{
			name:        "mixed binary/numeric count forms are rejected",
			status:      nul("A", "a.go"),
			numstat:     []byte("-\t5\ta.go\x00"),
			wantErr:     true,
			errContains: "mixed binary/numeric count forms",
		},
		{
			name:        "missing first tab is rejected",
			status:      nul("A", "a.go"),
			numstat:     []byte("50a.go\x00"),
			wantErr:     true,
			errContains: "missing first tab",
		},
		{
			name:        "missing second tab is rejected",
			status:      nul("A", "a.go"),
			numstat:     []byte("5\t0a.go\x00"),
			wantErr:     true,
			errContains: "missing second tab",
		},
		{
			name:        "empty ordinary path is rejected",
			status:      nul("A", "a.go"),
			numstat:     []byte("5\t0\t\x00"),
			wantErr:     true,
			errContains: "missing previous path",
		},
		{
			name:        "rename marker without both following paths is rejected",
			status:      nul("A", "a.go"),
			numstat:     []byte("5\t0\t\x00old.go\x00"),
			wantErr:     true,
			errContains: "missing resulting path",
		},
		{
			name:        "duplicate numstat identity is rejected",
			status:      nul("A", "a.go"),
			numstat:     append(numstat("a.go", "1", "0"), numstat("a.go", "2", "0")...),
			wantErr:     true,
			errContains: "duplicate numstat",
		},
		{
			name:        "missing numstat identity is rejected",
			status:      nul("M", "a.go"),
			numstat:     numstat("b.go", "1", "0"),
			wantErr:     true,
			errContains: "missing numstat entry",
		},
		{
			name:        "extra numstat record is rejected",
			status:      nul("M", "a.go"),
			numstat:     append(numstat("a.go", "1", "0"), numstat("b.go", "2", "0")...),
			wantErr:     true,
			errContains: "extra numstat entries",
		},
		{
			name:        "rename identity disagreement is rejected",
			status:      nul("R100", "old.go", "new.go"),
			numstat:     renameNumstat("other.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "missing numstat entry",
		},
		{
			name:        "unsafe resulting path is rejected",
			status:      nul("A", "/etc/passwd"),
			numstat:     numstat("/etc/passwd", "1", "0"),
			wantErr:     true,
			errContains: "leading separator",
		},
		{
			name:        "unsafe previous path is rejected",
			status:      nul("R100", "../old.go", "new.go"),
			numstat:     renameNumstat("../old.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "contains \"..\" segment",
		},
		{
			name:        "path with backslash is rejected",
			status:      nul("A", "pkg\\win.go"),
			numstat:     numstat("pkg\\win.go", "1", "0"),
			wantErr:     true,
			errContains: "contains backslash",
		},
		{
			name:        "invalid UTF-8 path is rejected",
			status:      nul("A", "\xff"),
			numstat:     numstat("\xff", "1", "0"),
			wantErr:     true,
			errContains: "invalid UTF-8",
		},
		{
			name:        "numeric overflow is rejected",
			status:      nul("A", "a.go"),
			numstat:     numstat("a.go", "999999999999999999999999999", "0"),
			wantErr:     true,
			errContains: "malformed addition count",
		},
		{
			name:        "empty structured status evidence is rejected",
			status:      nil,
			numstat:     numstat("a.go", "1", "0"),
			wantErr:     true,
			errContains: "structured status evidence is empty",
		},
		{
			name:        "empty structured numstat evidence is rejected",
			status:      nul("M", "a.go"),
			numstat:     nil,
			wantErr:     true,
			errContains: "structured numstat evidence is empty",
		},
		{
			name:        "malformed NUL-delimited status is rejected",
			status:      nul("M"),
			numstat:     numstat("a.go", "1", "0"),
			wantErr:     true,
			errContains: "missing resulting path",
		},
		{
			name:        "empty status token is rejected",
			status:      nul("", "a.go"),
			numstat:     numstat("a.go", "1", "0"),
			wantErr:     true,
			errContains: "empty status token",
		},
		{
			name:        "malformed numstat output is rejected",
			status:      nul("M", "a.go"),
			numstat:     numstat("a.go", "not-a-number", "0"),
			wantErr:     true,
			errContains: "malformed addition count",
		},
		{
			name:        "unsupported status is rejected",
			status:      nul("X", "a.go"),
			numstat:     numstat("a.go", "1", "0"),
			wantErr:     true,
			errContains: "unsupported status",
		},
		{
			name:        "combined status is rejected",
			status:      nul("AM", "a.go"),
			numstat:     numstat("a.go", "1", "0"),
			wantErr:     true,
			errContains: "unsupported status",
		},
		{
			name:        "malformed rename score is rejected",
			status:      nul("Rabc", "old.go", "new.go"),
			numstat:     renameNumstat("old.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "malformed R score",
		},
		{
			name:        "rename score out of range is rejected",
			status:      nul("R101", "old.go", "new.go"),
			numstat:     renameNumstat("old.go", "new.go", "1", "0"),
			wantErr:     true,
			errContains: "out of valid range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAuditFileChanges(tt.status, tt.numstat)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("changes = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseChangedFilesUnchanged(t *testing.T) {
	nameStatus := "M\tinternal/a.go\nA\tinternal/b.go\nM\tinternal/a.go"
	want := []string{"internal/a.go", "internal/b.go"}
	got := parseChangedFiles(nameStatus)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseChangedFiles = %v, want %v", got, want)
	}
}

func TestInspectAuditCommitStructuredErrorsFailClosed(t *testing.T) {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	runner := validAuditRunner()
	runner.errors["diff --name-status -z --find-renames --find-copies "+base+".."+head] = errors.New("git crashed")
	_, err := InspectAuditCommitWithRunner(context.Background(), t.TempDir(), "feat/simplification", base, head, runner)
	if err == nil || !strings.Contains(err.Error(), "capture structured changed-file statuses") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseAuditFileChangesRealGit(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable not available")
	}

	dir := t.TempDir()
	run := func(args ...string) []byte {
		cmd := exec.Command(gitPath, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	writeFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("checkout", "-b", "audit-test-branch")

	writeFile("original.go", "package main\n")
	run("add", "original.go")
	run("commit", "-m", "base")
	base := strings.TrimSpace(string(run("rev-parse", "HEAD")))

	writeFile("added.go", "package added\n")
	if err := os.Rename(filepath.Join(dir, "original.go"), filepath.Join(dir, "renamed.go")); err != nil {
		t.Fatalf("rename original.go: %v", err)
	}
	run("add", "-A")
	run("commit", "-m", "changes")
	head := strings.TrimSpace(string(run("rev-parse", "HEAD")))

	runner := boundedGitRunner{}
	evidence, err := InspectAuditCommitWithRunner(context.Background(), dir, "audit-test-branch", base, head, runner)
	if err != nil {
		t.Fatalf("InspectAuditCommitWithRunner: %v", err)
	}

	if len(evidence.FileChanges) != 2 {
		t.Fatalf("FileChanges = %+v", evidence.FileChanges)
	}
	want := map[string]AuditFileChange{
		"added.go":   {Path: "added.go", ChangeType: "added"},
		"renamed.go": {Path: "renamed.go", PreviousPath: "original.go", ChangeType: "renamed"},
	}
	for _, fc := range evidence.FileChanges {
		wantChange, ok := want[fc.Path]
		if !ok {
			t.Fatalf("unexpected path %q", fc.Path)
		}
		if fc.PreviousPath != wantChange.PreviousPath || fc.ChangeType != wantChange.ChangeType {
			t.Fatalf("change for %q = %+v, want %+v", fc.Path, fc, wantChange)
		}
	}

	if len(evidence.ChangedFiles) == 0 {
		t.Fatal("legacy ChangedFiles is empty")
	}
	if evidence.NameStatus == "" {
		t.Fatal("legacy NameStatus is empty")
	}
	if evidence.DiffStat == "" {
		t.Fatal("legacy DiffStat is empty")
	}
	if evidence.CommitLog == "" {
		t.Fatal("legacy CommitLog is empty")
	}
	if evidence.Diff == "" {
		t.Fatal("legacy Diff is empty")
	}
}
