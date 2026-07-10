package workflowrepos

import (
	"context"
	"errors"
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

func validAuditRunner() *auditScriptRunner {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	return &auditScriptRunner{
		outputs: map[string][]byte{
			"symbolic-ref --quiet --short HEAD":                              []byte("feat/simplification\n"),
			"rev-parse --verify HEAD":                                        []byte(head + "\n"),
			"status --porcelain=v1 --untracked-files=all":                    nil,
			"rev-parse --absolute-git-dir":                                   []byte("/repo/.git\n"),
			"cat-file -e " + head + "^{commit}":                              nil,
			"merge-base --is-ancestor " + base + " " + head:                  nil,
			"diff --name-status --no-renames " + base + ".." + head:          []byte("M\tinternal/a.go\nA\tinternal/b.go\n"),
			"diff --stat --no-renames " + base + ".." + head:                 []byte("2 files changed\n"),
			"log --format=%H%x09%an%x09%aI%x09%s " + base + ".." + head:      []byte(head + "\tDev\t2026-07-06T00:00:00Z\tchange\n"),
			"diff --binary --no-ext-diff --no-renames " + base + ".." + head: []byte("diff --git a/internal/a.go b/internal/a.go\n"),
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
	if !strings.Contains(result.Diff, "diff --git") {
		t.Fatal("diff was not captured")
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
