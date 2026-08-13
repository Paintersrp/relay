package workflowrepos

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestVerifyIntegrationRepositoryRequiresExactPreservation(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "one")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "two.txt"), []byte("two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "two")
	two := git("rev-parse", "HEAD")
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, two, []string{two}, nil, "clean", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, strings.Repeat("f", 40), []string{two}, nil, "clean", ""); err == nil {
		t.Fatal("unknown integrated commit passed")
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, base, []string{two}, nil, "clean", ""); err == nil {
		t.Fatal("non-preserving integrated commit passed")
	}
	git("checkout", "-b", "feature", base)
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "feature")
	feature := git("rev-parse", "HEAD")
	git("checkout", "main")
	git("merge", "--no-ff", "feature", "-m", "merge feature")
	merge := git("rev-parse", "HEAD")
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, merge, []string{two, feature}, nil, "clean", ""); err != nil {
		t.Fatalf("clean multi-parent merge failed: %v", err)
	}
	for _, evidence := range []string{"arbitrary", "mechanically_resolved:" + merge} {
		if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, merge, []string{two, feature}, nil, "mechanically_resolved", evidence); err == nil {
			t.Fatalf("fabricated conflict evidence passed: %q", evidence)
		}
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, merge, []string{two, feature}, nil, "material_conflict", "material conflict"); err == nil {
		t.Fatal("material conflict passed")
	}
}

func integrationGitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func marshalEvidence(t *testing.T, evidence IntegrationConflictEvidence) string {
	t.Helper()
	bytes, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

// seedContentConflict builds a repository with three commits: base on main, an
// ours commit that rewrites conflict.txt on main, and a theirs commit on the
// feature branch rooted at base that rewrites conflict.txt differently.
func seedContentConflict(t *testing.T, root string) (base, ours, theirs string) {
	t.Helper()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "conflict.txt", "base\n")
	run("add", ".")
	run("commit", "-m", "base")
	base = run("rev-parse", "HEAD")
	writeRepoFile(t, root, "conflict.txt", "ours\n")
	run("add", ".")
	run("commit", "-m", "ours")
	ours = run("rev-parse", "HEAD")
	run("checkout", "-b", "feature", base)
	writeRepoFile(t, root, "conflict.txt", "theirs\n")
	run("add", ".")
	run("commit", "-m", "theirs")
	theirs = run("rev-parse", "HEAD")
	run("checkout", "main")
	return base, ours, theirs
}

// captureConflictStages merges the feature branch into main (already at ours),
// captures the exact conflict-stage tuples Git placed in the index, resolves
// the conflict, and returns the stages plus the resolved integrated commit and
// its parents.
func captureConflictStages(t *testing.T, root, base, ours, theirs string) (stages []IntegrationConflictStage, integrated string, parents []string) {
	t.Helper()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	mergeCommand := exec.Command("git", "merge", "feature", "-m", "merge feature")
	mergeCommand.Dir = root
	if out, err := mergeCommand.CombinedOutput(); err == nil {
		t.Fatalf("expected merge conflict, merge succeeded: %s", out)
	}
	lines := strings.Split(strings.TrimSpace(run("ls-files", "-u")), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("merge produced no conflicted stages")
	}
	stages = parseUnmergedIndex(t, lines)
	run("add", "-A")
	run("commit", "-m", "resolve conflict")
	integrated = run("rev-parse", "HEAD")
	parents = strings.Fields(run("rev-list", "--parents", "-n", "1", integrated))[1:]
	if len(parents) < 2 || parents[0] != ours || !containsString(parents[1:], theirs) {
		t.Fatalf("integrated parents = %v, want %s and %s", parents, ours, theirs)
	}
	return stages, integrated, parents
}

// parseUnmergedIndex parses `git ls-files -u` output ("<mode> <oid> <stage>\t<path>")
// into the exact conflict-stage tuples, independently of the merge-tree parser
// under test.
func parseUnmergedIndex(t *testing.T, lines []string) []IntegrationConflictStage {
	t.Helper()
	stages := make([]IntegrationConflictStage, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			t.Fatalf("unmerged entry = %q", line)
		}
		fields := strings.Fields(parts[0])
		if len(fields) != 3 {
			t.Fatalf("unmerged fields = %q", parts[0])
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil || stage < 1 || stage > 3 {
			t.Fatalf("unmerged stage = %q", fields[2])
		}
		stages = append(stages, IntegrationConflictStage{Stage: stage, Path: parts[1], Mode: fields[0], OID: fields[1]})
	}
	return stages
}

func resolvedTreeEntry(t *testing.T, run func(args ...string) string, commit, path string) IntegrationResolvedEntry {
	t.Helper()
	fields := strings.Fields(run("ls-tree", commit, "--", path))
	if len(fields) != 4 {
		t.Fatalf("tree entry for %s at %s = %q", path, commit, fields)
	}
	return IntegrationResolvedEntry{Path: path, Mode: fields[0], OID: fields[2], Commit: commit}
}

func TestVerifyIntegrationRepositoryVerifiesFactualMechanicalConflictEvidence(t *testing.T) {
	root := t.TempDir()
	base, ours, theirs := seedContentConflict(t, root)
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	if len(stages) != 3 || stages[0].Stage != 1 || stages[1].Stage != 2 || stages[2].Stage != 3 {
		t.Fatalf("content conflict stages = %+v", stages)
	}
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   stages,
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "conflict.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err != nil {
		t.Fatalf("valid factual conflict evidence failed: %v", err)
	}
}

func TestVerifyIntegrationRepositoryRejectsFabricatedOrMismatchedStageEvidence(t *testing.T) {
	root := t.TempDir()
	base, ours, theirs := seedContentConflict(t, root)
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	resolved := []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "conflict.txt")}
	mutations := []struct {
		name   string
		mutate func(*IntegrationConflictEvidence)
	}{
		{"wrong assignment", func(e *IntegrationConflictEvidence) { e.AssignmentID = "wrong-assignment" }},
		{"wrong base", func(e *IntegrationConflictEvidence) { e.BaseCommit = strings.Repeat("a", 40) }},
		{"wrong constituent", func(e *IntegrationConflictEvidence) { e.ConstituentCommits = []string{ours} }},
		{"wrong parents", func(e *IntegrationConflictEvidence) { e.IntegratedParents = []string{theirs, ours} }},
		{"wrong integrated commit", func(e *IntegrationConflictEvidence) { e.IntegratedCommit = base }},
		{"wrong ours relation", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Ours = strings.Repeat("e", 40) }},
		{"wrong theirs relation", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Theirs = strings.Repeat("f", 40) }},
		{"wrong stage-1 oid", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Stages[0].OID = strings.Repeat("1", 40) }},
		{"wrong stage-2 oid", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Stages[1].OID = strings.Repeat("2", 40) }},
		{"wrong stage-3 oid", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Stages[2].OID = strings.Repeat("3", 40) }},
		{"wrong stage mode", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Stages[1].Mode = "100755" }},
		{"wrong stage path", func(e *IntegrationConflictEvidence) { e.Conflicts[0].Stages[2].Path = "other.txt" }},
		{"omitted git stage", func(e *IntegrationConflictEvidence) {
			e.Conflicts[0].Stages = []IntegrationConflictStage{e.Conflicts[0].Stages[0], e.Conflicts[0].Stages[2]}
		}},
		{"fabricated extra stage", func(e *IntegrationConflictEvidence) {
			e.Conflicts[0].Stages = append(e.Conflicts[0].Stages, IntegrationConflictStage{Stage: 1, Path: "fabricated.txt", Mode: "100644", OID: strings.Repeat("c", 40)})
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			evidence := IntegrationConflictEvidence{
				Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
				ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
				IntegratedParents: parents,
				Conflicts: []IntegrationMergeConflict{{
					Ours: ours, Theirs: theirs,
					Stages:   append([]IntegrationConflictStage(nil), stages...),
					Resolved: append([]IntegrationResolvedEntry(nil), resolved...),
				}},
			}
			tc.mutate(&evidence)
			if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err == nil {
				t.Fatalf("%s evidence passed", tc.name)
			}
		})
	}
}

func TestVerifyIntegrationRepositoryRejectsMismatchedResolvedEntry(t *testing.T) {
	root := t.TempDir()
	base, ours, theirs := seedContentConflict(t, root)
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	resolved := resolvedTreeEntry(t, run, integrated, "conflict.txt")
	for _, mutate := range []func(*IntegrationResolvedEntry){
		func(e *IntegrationResolvedEntry) { e.OID = strings.Repeat("0", 40) },
		func(e *IntegrationResolvedEntry) { e.Mode = "100755" },
		func(e *IntegrationResolvedEntry) { e.Path = "missing.txt" },
		func(e *IntegrationResolvedEntry) { e.Commit = base },
	} {
		mutated := resolved
		mutate(&mutated)
		evidence := IntegrationConflictEvidence{
			Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
			ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
			IntegratedParents: parents,
			Conflicts: []IntegrationMergeConflict{{
				Ours: ours, Theirs: theirs,
				Stages:   append([]IntegrationConflictStage(nil), stages...),
				Resolved: []IntegrationResolvedEntry{mutated},
			}},
		}
		if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err == nil {
			t.Fatalf("resolved mutation passed: %+v", mutated)
		}
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   stages,
			Resolved: []IntegrationResolvedEntry{resolved},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err != nil {
		t.Fatalf("valid resolved entry failed: %v", err)
	}
}

func TestVerifyIntegrationRepositoryVerifiesAddAddWithoutStageOne(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "base.txt", "base\n")
	run("add", ".")
	run("commit", "-m", "base")
	base := run("rev-parse", "HEAD")
	writeRepoFile(t, root, "add.txt", "ours-add\n")
	run("add", ".")
	run("commit", "-m", "add ours")
	ours := run("rev-parse", "HEAD")
	run("checkout", "-b", "feature", base)
	writeRepoFile(t, root, "add.txt", "theirs-add\n")
	run("add", ".")
	run("commit", "-m", "add theirs")
	theirs := run("rev-parse", "HEAD")
	run("checkout", "main")
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	if len(stages) != 2 || stages[0].Stage != 2 || stages[1].Stage != 3 {
		t.Fatalf("add/add stages = %+v", stages)
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   stages,
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "add.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err != nil {
		t.Fatalf("add/add conflict evidence failed: %v", err)
	}
}

func TestVerifyIntegrationRepositoryVerifiesDeleteModifyWithAbsentSide(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "dm.txt", "base-dm\n")
	run("add", ".")
	run("commit", "-m", "base")
	base := run("rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(root, "dm.txt")); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "delete ours")
	ours := run("rev-parse", "HEAD")
	run("checkout", "-b", "feature", base)
	writeRepoFile(t, root, "dm.txt", "modified-theirs\n")
	run("add", ".")
	run("commit", "-m", "modify theirs")
	theirs := run("rev-parse", "HEAD")
	run("checkout", "main")
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	if len(stages) != 2 || stages[0].Stage != 1 || stages[1].Stage != 3 {
		t.Fatalf("delete/modify stages = %+v", stages)
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   stages,
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "dm.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err != nil {
		t.Fatalf("delete/modify conflict evidence failed: %v", err)
	}
}

func TestVerifyIntegrationRepositoryVerifiesAsymmetricRenameConflictPaths(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "original.txt", "l1\nl2\nl3\nl4\nl5\nl6\n")
	run("add", ".")
	run("commit", "-m", "base")
	base := run("rev-parse", "HEAD")
	run("mv", "original.txt", "rr-a.txt")
	run("commit", "-m", "rename a")
	ours := run("rev-parse", "HEAD")
	run("checkout", "-b", "feature", base)
	run("mv", "original.txt", "rr-b.txt")
	run("commit", "-m", "rename b")
	theirs := run("rev-parse", "HEAD")
	run("checkout", "main")
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	if len(stages) != 3 || stages[0].Stage != 1 || stages[1].Stage != 2 || stages[2].Stage != 3 {
		t.Fatalf("rename/rename stages = %+v", stages)
	}
	// The rename/rename conflict is path-asymmetric: stage 1 stays at the base
	// path while stages 2 and 3 use the two distinct rename targets. It must
	// never be collapsed into a single same-path representation.
	if stages[0].Path != "original.txt" || stages[1].Path != "rr-a.txt" || stages[2].Path != "rr-b.txt" {
		t.Fatalf("rename/rename stages are not path-asymmetric: %+v", stages)
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   stages,
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "rr-b.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err != nil {
		t.Fatalf("rename/rename conflict evidence failed: %v", err)
	}
}

func TestVerifyIntegrationRepositoryRejectsEvidenceOmittingAReportedConflictPath(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "a.txt", "base-a\n")
	writeRepoFile(t, root, "b.txt", "base-b\n")
	run("add", ".")
	run("commit", "-m", "base")
	base := run("rev-parse", "HEAD")
	writeRepoFile(t, root, "a.txt", "ours-a\n")
	writeRepoFile(t, root, "b.txt", "ours-b\n")
	run("add", ".")
	run("commit", "-m", "ours")
	ours := run("rev-parse", "HEAD")
	run("checkout", "-b", "feature", base)
	writeRepoFile(t, root, "a.txt", "theirs-a\n")
	writeRepoFile(t, root, "b.txt", "theirs-b\n")
	run("add", ".")
	run("commit", "-m", "theirs")
	theirs := run("rev-parse", "HEAD")
	run("checkout", "main")
	stages, integrated, parents := captureConflictStages(t, root, base, ours, theirs)
	var onlyA []IntegrationConflictStage
	for _, stage := range stages {
		if stage.Path == "a.txt" {
			onlyA = append(onlyA, stage)
		}
	}
	if len(onlyA) != 3 {
		t.Fatalf("two-path conflict stages = %+v", stages)
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages:   onlyA,
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "a.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err == nil {
		t.Fatal("evidence hiding a Git-reported conflict path passed")
	}
}

func TestVerifyIntegrationRepositoryRejectsDivergentButCleanSamePathEvidence(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) string { return integrationGitRun(t, root, args...) }
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	writeRepoFile(t, root, "same.txt", "base-a\ncontext-b\ncontext-c\ncontext-d\ncontext-e\nbase-f\n")
	run("add", ".")
	run("commit", "-m", "base")
	base := run("rev-parse", "HEAD")
	run("checkout", "-b", "ours")
	writeRepoFile(t, root, "same.txt", "ours-a\ncontext-b\ncontext-c\ncontext-d\ncontext-e\nbase-f\n")
	run("commit", "-am", "ours")
	ours := run("rev-parse", "HEAD")
	run("checkout", "-b", "theirs", base)
	writeRepoFile(t, root, "same.txt", "base-a\ncontext-b\ncontext-c\ncontext-d\ncontext-e\ntheirs-f\n")
	run("commit", "-am", "theirs")
	theirs := run("rev-parse", "HEAD")
	run("checkout", "-b", "integrated", ours)
	run("merge", "--no-ff", "theirs", "-m", "merge theirs")
	run("branch", "-f", "main", "integrated")
	run("checkout", "main")
	integrated := run("rev-parse", "HEAD")
	parents := strings.Fields(run("rev-list", "--parents", "-n", "1", integrated))[1:]
	blob := func(commit string) IntegrationConflictStage {
		fields := strings.Fields(run("ls-tree", commit, "--", "same.txt"))
		if len(fields) != 4 {
			t.Fatalf("tree entry %s = %q", commit, fields)
		}
		return IntegrationConflictStage{Path: "same.txt", Mode: fields[0], OID: fields[2]}
	}
	baseEntry := blob(base)
	oursEntry := blob(ours)
	theirsEntry := blob(theirs)
	if baseEntry.OID == oursEntry.OID || baseEntry.OID == theirsEntry.OID || oursEntry.OID == theirsEntry.OID {
		t.Fatal("clean divergent test blobs must all differ")
	}
	evidence := IntegrationConflictEvidence{
		Version: 1, AssignmentID: "assignment-1", BaseCommit: base,
		ConstituentCommits: []string{ours, theirs}, IntegratedCommit: integrated,
		IntegratedParents: parents,
		Conflicts: []IntegrationMergeConflict{{
			Ours: ours, Theirs: theirs,
			Stages: []IntegrationConflictStage{
				{Stage: 1, Path: baseEntry.Path, Mode: baseEntry.Mode, OID: baseEntry.OID},
				{Stage: 2, Path: oursEntry.Path, Mode: oursEntry.Mode, OID: oursEntry.OID},
				{Stage: 3, Path: theirsEntry.Path, Mode: theirsEntry.Mode, OID: theirsEntry.OID},
			},
			Resolved: []IntegrationResolvedEntry{resolvedTreeEntry(t, run, integrated, "same.txt")},
		}},
	}
	if _, err := VerifyIntegrationRepository(context.Background(), root, "assignment-1", "main", base, integrated, []string{ours, theirs}, nil, "mechanically_resolved", marshalEvidence(t, evidence)); err == nil {
		t.Fatal("clean divergent same-path merge was accepted as a conflict")
	}
}

func jsonEvidence(t *testing.T, encoded string) IntegrationConflictEvidence {
	t.Helper()
	var evidence IntegrationConflictEvidence
	if err := json.Unmarshal([]byte(encoded), &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
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
