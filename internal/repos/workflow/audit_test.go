package workflowrepos

import (
	"context"
	"errors"
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
			"diff --numstat -z --find-renames --find-copies " + rangeSpec:     nul("12", "4", "internal/a.go", "5", "0", "internal/b.go"),
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
			numstat: nul("5", "0", "pkg/new.go"),
			want: []AuditFileChange{
				{Path: "pkg/new.go", ChangeType: "added", Additions: 5, Deletions: 0},
			},
		},
		{
			name:    "modified file with exact counts",
			status:  nul("M", "pkg/existing.go"),
			numstat: nul("12", "4", "pkg/existing.go"),
			want: []AuditFileChange{
				{Path: "pkg/existing.go", ChangeType: "modified", Additions: 12, Deletions: 4},
			},
		},
		{
			name:    "deleted file with exact counts",
			status:  nul("D", "pkg/removed.go"),
			numstat: nul("0", "10", "pkg/removed.go"),
			want: []AuditFileChange{
				{Path: "pkg/removed.go", ChangeType: "deleted", Additions: 0, Deletions: 10},
			},
		},
		{
			name:    "type-changed file",
			status:  nul("T", "pkg/symlink.go"),
			numstat: nul("0", "0", "pkg/symlink.go"),
			want: []AuditFileChange{
				{Path: "pkg/symlink.go", ChangeType: "type_changed", Additions: 0, Deletions: 0},
			},
		},
		{
			name:    "rename with old path and counts",
			status:  nul("R100", "pkg/old.go", "pkg/new.go"),
			numstat: nul("1", "2", "pkg/old.go", "pkg/new.go"),
			want: []AuditFileChange{
				{Path: "pkg/new.go", PreviousPath: "pkg/old.go", ChangeType: "renamed", Additions: 1, Deletions: 2},
			},
		},
		{
			name:    "copy with old path and counts",
			status:  nul("C100", "pkg/original.go", "pkg/copied.go"),
			numstat: nul("3", "0", "pkg/original.go", "pkg/copied.go"),
			want: []AuditFileChange{
				{Path: "pkg/copied.go", PreviousPath: "pkg/original.go", ChangeType: "copied", Additions: 3, Deletions: 0},
			},
		},
		{
			name:   "mixed changes return deterministic path order",
			status: nul("A", "new/b.go", "M", "new/a.go", "R100", "old.go", "new/z.go"),
			numstat: nul(
				"0", "0", "new/b.go",
				"5", "1", "new/a.go",
				"2", "3", "old.go", "new/z.go",
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
			numstat: nul("-", "-", "assets/icon.png"),
			want: []AuditFileChange{
				{Path: "assets/icon.png", ChangeType: "added", Additions: 0, Deletions: 0},
			},
		},
		{
			name:    "file names containing spaces are supported",
			status:  nul("A", "my file with spaces.txt"),
			numstat: nul("1", "0", "my file with spaces.txt"),
			want: []AuditFileChange{
				{Path: "my file with spaces.txt", ChangeType: "added", Additions: 1, Deletions: 0},
			},
		},
		{
			name:        "malformed NUL-delimited status is rejected",
			status:      nul("M"),
			numstat:     nul("1", "0", "a.go"),
			wantErr:     true,
			errContains: "missing resulting path",
		},
		{
			name:        "empty status token is rejected",
			status:      nul("", "a.go"),
			numstat:     nul("1", "0", "a.go"),
			wantErr:     true,
			errContains: "empty status token",
		},
		{
			name:        "malformed numstat output is rejected",
			status:      nul("M", "a.go"),
			numstat:     nul("not-a-number", "0", "a.go"),
			wantErr:     true,
			errContains: "malformed addition count",
		},
		{
			name:        "unsupported status is rejected",
			status:      nul("X", "a.go"),
			numstat:     nul("1", "0", "a.go"),
			wantErr:     true,
			errContains: "unsupported status",
		},
		{
			name:        "combined status is rejected",
			status:      nul("AM", "a.go"),
			numstat:     nul("1", "0", "a.go"),
			wantErr:     true,
			errContains: "unsupported status",
		},
		{
			name:        "malformed rename score is rejected",
			status:      nul("Rabc", "old.go", "new.go"),
			numstat:     nul("1", "0", "old.go", "new.go"),
			wantErr:     true,
			errContains: "malformed R score",
		},
		{
			name:        "rename score out of range is rejected",
			status:      nul("R101", "old.go", "new.go"),
			numstat:     nul("1", "0", "old.go", "new.go"),
			wantErr:     true,
			errContains: "out of valid range",
		},
		{
			name:        "missing numstat record is rejected",
			status:      nul("M", "a.go", "A", "b.go"),
			numstat:     nul("1", "0", "a.go"),
			wantErr:     true,
			errContains: "missing numstat entry",
		},
		{
			name:        "extra numstat record is rejected",
			status:      nul("M", "a.go"),
			numstat:     nul("1", "0", "a.go", "2", "0", "b.go"),
			wantErr:     true,
			errContains: "extra numstat entries",
		},
		{
			name:        "duplicate resulting path is rejected",
			status:      nul("A", "dup.go", "A", "dup.go"),
			numstat:     nul("1", "0", "dup.go", "2", "0", "dup.go"),
			wantErr:     true,
			errContains: "duplicate resulting path",
		},
		{
			name:        "rename path disagreement is rejected",
			status:      nul("R100", "old.go", "new.go"),
			numstat:     nul("1", "0", "other.go", "new.go"),
			wantErr:     true,
			errContains: "path disagreement",
		},
		{
			name:        "unsafe resulting path is rejected",
			status:      nul("A", "/etc/passwd"),
			numstat:     nul("1", "0", "/etc/passwd"),
			wantErr:     true,
			errContains: "leading separator",
		},
		{
			name:        "unsafe previous path is rejected",
			status:      nul("R100", "../old.go", "new.go"),
			numstat:     nul("1", "0", "../old.go", "new.go"),
			wantErr:     true,
			errContains: "contains \"..\" segment",
		},
		{
			name:        "path with backslash is rejected",
			status:      nul("A", "pkg\\win.go"),
			numstat:     nul("1", "0", "pkg\\win.go"),
			wantErr:     true,
			errContains: "contains backslash",
		},
		{
			name:        "numeric overflow is rejected",
			status:      nul("A", "a.go"),
			numstat:     nul("999999999999999999999999999", "0", "a.go"),
			wantErr:     true,
			errContains: "malformed addition count",
		},
		{
			name:    "empty inputs produce empty changes",
			status:  nil,
			numstat: nil,
			want:    []AuditFileChange{},
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
