package executor

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/speccompiler"
)

func TestApplyDeterministicSuccessfulPlans(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		doc     *speccompiler.DeterministicOperationsDocument
		want    map[string]string
		changed []string
	}{
		{"modify", map[string]string{"a.txt": "old"}, document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}})), map[string]string{"a.txt": "new"}, []string{"a.txt"}},
		{"create then modify", nil, document("complete", operation("new/a.txt", "create", implContent("old")), operation("new/a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}})), map[string]string{"new/a.txt": "new"}, []string{"new/a.txt"}},
		{"delete then create", map[string]string{"a.txt": "old"}, document("partial", operation("a.txt", "delete", implExpected("old")), operation("a.txt", "create", implContent("new"))), map[string]string{"a.txt": "new"}, []string{"a.txt"}},
		{"rename replacing content", map[string]string{"a.txt": "old"}, document("partial", rename("a.txt", "dir/b.txt", "old", false, "new")), map[string]string{"dir/b.txt": "new"}, []string{"a.txt", "dir/b.txt"}},
		{"file to directory", map[string]string{"node": "old"}, document("partial", operation("node", "delete", implExpected("old")), operation("node/child.txt", "create", implContent("new"))), map[string]string{"node/child.txt": "new"}, []string{"node", "node/child.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newDeterministicPreflightRepo(t, test.files)
			preflight, err := PreflightDeterministicOperations(preflightInput(t, repo, test.doc))
			if err != nil || preflight.Plan == nil {
				t.Fatalf("preflight=%#v err=%v", preflight, err)
			}
			result, err := ApplyDeterministicMutationPlan(applyInput(t, repo, preflight.Plan))
			if err != nil {
				t.Fatal(err)
			}
			if result.Coverage != test.doc.Coverage || !reflect.DeepEqual(result.ChangedPaths, test.changed) {
				t.Fatalf("result=%#v", result)
			}
			for path, want := range test.want {
				got, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
				if readErr != nil || string(got) != want {
					t.Fatalf("%s: got=%q err=%v", path, got, readErr)
				}
			}
			if _, err := os.Stat(filepath.Join(repo, "a.txt")); test.name == "rename replacing content" && !os.IsNotExist(err) {
				t.Fatalf("rename source remains: %v", err)
			}
		})
	}
}

func TestApplyDeterministicRejectsInvalidOrStalePlanBeforeMutation(t *testing.T) {
	repo := newDeterministicPreflightRepo(t, map[string]string{"a.txt": "old"})
	preflight, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("a.txt", "delete", implExpected("old")))))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		alter func(*DeterministicMutationPlan)
		want  error
	}{
		{"nil", func(plan *DeterministicMutationPlan) {}, ErrDeterministicPlanInvalid},
		{"bad hash", func(plan *DeterministicMutationPlan) { plan.Operations[0].Before.SHA256 = strings.Repeat("0", 64) }, ErrDeterministicPlanInvalid},
		{"unsafe path", func(plan *DeterministicMutationPlan) { plan.Operations[0].SourcePath = "../a.txt" }, ErrDeterministicPlanInvalid},
		{"bad operation index", func(plan *DeterministicMutationPlan) { plan.Operations[0].Index = 2 }, ErrDeterministicPlanInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := snapshotRepo(t, repo)
			plan := cloneDeterministicPlan(preflight.Plan)
			if test.name == "nil" {
				plan = nil
			} else {
				test.alter(plan)
			}
			_, applyErr := ApplyDeterministicMutationPlan(applyInput(t, repo, plan))
			if !errors.Is(applyErr, test.want) {
				t.Fatalf("err=%v", applyErr)
			}
			assertSnapshotRepo(t, repo, before)
		})
	}
	t.Run("changed after preflight", func(t *testing.T) {
		repo := newDeterministicPreflightRepo(t, map[string]string{"a.txt": "old"})
		preflight, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("a.txt", "delete", implExpected("old")))))
		if err != nil {
			t.Fatal(err)
		}
		plan := cloneDeterministicPlan(preflight.Plan)
		plan.Operations[0].Before = newFileState([]byte("changed"))
		before := snapshotRepo(t, repo)
		_, err = ApplyDeterministicMutationPlan(applyInput(t, repo, plan))
		if !errors.Is(err, ErrDeterministicPlanStale) {
			t.Fatalf("err=%v", err)
		}
		assertSnapshotRepo(t, repo, before)
	})
}

func TestApplyDeterministicRollback(t *testing.T) {
	repo := newDeterministicPreflightRepo(t, map[string]string{"node": "old"})
	preflight, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("node", "delete", implExpected("old")), operation("node/child.txt", "create", implContent("new")))))
	if err != nil || preflight.Plan == nil {
		t.Fatalf("preflight=%#v err=%v", preflight, err)
	}
	before := snapshotRepo(t, repo)
	deterministicApplyFailureHook = func(phase string, index int) error {
		if phase == "after_mutation" && index == 2 {
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { deterministicApplyFailureHook = nil })
	_, err = ApplyDeterministicMutationPlan(applyInput(t, repo, preflight.Plan))
	if !errors.Is(err, ErrDeterministicApplicationFailed) {
		t.Fatalf("err=%v", err)
	}
	assertSnapshotRepo(t, repo, before)
}

func TestApplyDeterministicRollbackFailureIsReconciliation(t *testing.T) {
	repo := newDeterministicPreflightRepo(t, map[string]string{"a.txt": "old"})
	preflight, err := PreflightDeterministicOperations(preflightInput(t, repo, document("partial", operation("a.txt", "modify", speccompiler.DeterministicImplementation{Changes: []speccompiler.DeterministicChange{{Kind: "replace", OldText: "old", NewText: "new", ExpectedOccurrences: 1}}}))))
	if err != nil {
		t.Fatal(err)
	}
	deterministicApplyFailureHook = func(phase string, index int) error {
		if phase == "after_mutation" {
			return errors.New("application")
		}
		if phase == "rollback" {
			return errors.New("rollback")
		}
		return nil
	}
	t.Cleanup(func() { deterministicApplyFailureHook = nil })
	_, err = ApplyDeterministicMutationPlan(applyInput(t, repo, preflight.Plan))
	if !errors.Is(err, ErrDeterministicMutationReconciliation) {
		t.Fatalf("err=%v", err)
	}
}

func applyInput(t *testing.T, repo string, plan *DeterministicMutationPlan) DeterministicApplyInput {
	t.Helper()
	return DeterministicApplyInput{RepositoryRoot: repo, ExpectedBranch: "main", ExpectedCommit: strings.TrimSpace(gitOutput(t, repo, "rev-parse", "HEAD")), Plan: plan}
}
