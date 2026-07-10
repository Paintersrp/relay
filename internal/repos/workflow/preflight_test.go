package workflowrepos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type scriptedGitRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []string
}

func (r *scriptedGitRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if err := r.errors[key]; err != nil {
		return nil, err
	}
	return r.outputs[key], nil
}

func cleanRunner(gitDir string) *scriptedGitRunner {
	return &scriptedGitRunner{outputs: map[string][]byte{
		"symbolic-ref --quiet --short HEAD":           []byte("feat/simplification\n"),
		"rev-parse --verify HEAD":                     []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"),
		"status --porcelain=v1 --untracked-files=all": nil,
		"rev-parse --absolute-git-dir":                []byte(gitDir + "\n"),
	}, errors: map[string]error{}}
}

func TestVerifyExecutionPreflightCleanExactRepository(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := cleanRunner(gitDir)
	result := VerifyExecutionPreflightWithRunner(context.Background(), root, "feat/simplification", strings.Repeat("a", 40), runner)
	if !result.OK {
		t.Fatalf("unexpected blocker: %+v", result)
	}
	wantCalls := []string{
		"symbolic-ref --quiet --short HEAD",
		"rev-parse --verify HEAD",
		"status --porcelain=v1 --untracked-files=all",
		"rev-parse --absolute-git-dir",
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("Git calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestVerifyExecutionPreflightBlocksEveryDisallowedState(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(root, gitDir string, runner *scriptedGitRunner)
		wantCode   string
		wantDetail string
	}{
		{name: "branch mismatch", mutate: func(_, _ string, r *scriptedGitRunner) {
			r.outputs["symbolic-ref --quiet --short HEAD"] = []byte("main\n")
		}, wantCode: "branch_mismatch", wantDetail: "does not match"},
		{name: "detached head", mutate: func(_, _ string, r *scriptedGitRunner) {
			r.errors["symbolic-ref --quiet --short HEAD"] = fmt.Errorf("detached")
		}, wantCode: "branch_mismatch", wantDetail: "detached"},
		{name: "head mismatch", mutate: func(_, _ string, r *scriptedGitRunner) {
			r.outputs["rev-parse --verify HEAD"] = []byte(strings.Repeat("b", 40) + "\n")
		}, wantCode: "head_mismatch", wantDetail: "does not match"},
		{name: "dirty tracked", mutate: func(_, _ string, r *scriptedGitRunner) {
			r.outputs["status --porcelain=v1 --untracked-files=all"] = []byte(" M internal/file.go\n")
		}, wantCode: "repository_dirty", wantDetail: "not clean"},
		{name: "untracked", mutate: func(_, _ string, r *scriptedGitRunner) {
			r.outputs["status --porcelain=v1 --untracked-files=all"] = []byte("?? new.txt\n")
		}, wantCode: "repository_dirty", wantDetail: "not clean"},
		{name: "merge in progress", mutate: func(_, gitDir string, _ *scriptedGitRunner) {
			if err := os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantCode: "git_operation_in_progress", wantDetail: "merge"},
		{name: "rebase in progress", mutate: func(_, gitDir string, _ *scriptedGitRunner) {
			if err := os.Mkdir(filepath.Join(gitDir, "rebase-merge"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, wantCode: "git_operation_in_progress", wantDetail: "rebase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			gitDir := filepath.Join(root, ".git")
			if err := os.MkdirAll(gitDir, 0o755); err != nil {
				t.Fatal(err)
			}
			runner := cleanRunner(gitDir)
			tt.mutate(root, gitDir, runner)
			result := VerifyExecutionPreflightWithRunner(context.Background(), root, "feat/simplification", strings.Repeat("a", 40), runner)
			if result.OK || result.BlockerCode != tt.wantCode || !strings.Contains(result.BlockerText, tt.wantDetail) {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}
