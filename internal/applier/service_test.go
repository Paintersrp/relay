package applier

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/speccompiler"
)

type failingEvidenceWriter struct{}

func (failingEvidenceWriter) WriteEvidence(context.Context, EvidenceFile) (EvidenceArtifact, error) {
	return EvidenceArtifact{}, errors.New("disk unavailable")
}

func TestApplyReplaySemantics(t *testing.T) {
	t.Run("v2 evolving selector succeeds", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "a.txt"), "a\n")
		projection := oneFileProjection(speccompiler.ReplayEvolvingPathChain, modifyWork("1.1.file.1", "1.1", "chain.a", "a.txt",
			directive("1.1.file.1.change.1", "replace", "a", "b", 1),
			directive("1.1.file.1.change.2", "replace", "b", "c", 1),
		))
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeCompleted || string(mustRead(t, filepath.Join(root, "a.txt"))) != "c\n" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("v1 immutable base ordered replay succeeds", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "a.txt"), "a b\n")
		projection := oneFileProjection(speccompiler.ReplayImmutableBase, modifyWork("1.1.file.1", "1.1", "chain.a", "a.txt",
			directive("1.1.file.1.change.1", "replace", "a", "x", 1),
			directive("1.1.file.1.change.2", "replace", "b", "y", 1),
		))
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeCompleted || string(mustRead(t, filepath.Join(root, "a.txt"))) != "x y\n" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("v1 replay contradiction blocks before mutation", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "a.txt"), "a b\n")
		projection := oneFileProjection(speccompiler.ReplayImmutableBase, modifyWork("1.1.file.1", "1.1", "chain.a", "a.txt",
			directive("1.1.file.1.change.1", "replace", "a", "b", 1),
			directive("1.1.file.1.change.2", "replace", "b", "c", 1),
		))
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || string(mustRead(t, filepath.Join(root, "a.txt"))) != "a b\n" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("v2 selector cannot use another or later chain producer", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "consumer.txt"), "base\n")
		projection := projection(
			[]speccompiler.ProjectedSubstep{
				{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}},
				{Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}},
			},
			[]speccompiler.ProjectedPathChain{
				chain("chain.consumer", 0, []string{"1.1.file.1"}, []string{"consumer.txt"}, []string{"1.1"}),
				chain("chain.producer", 1, []string{"1.2.file.1"}, []string{"producer.txt"}, []string{"1.2"}),
			},
			[]speccompiler.ProjectedFileWork{
				modifyWork("1.1.file.1", "1.1", "chain.consumer", "consumer.txt", directive("1.1.file.1.change.1", "replace", "generated", "used", 1)),
				{Ref: "1.2.file.1", SubstepRef: "1.2", PathChainRef: "chain.producer", Path: "producer.txt", Operation: "create", Content: "generated\n"},
			},
		)
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || string(mustRead(t, filepath.Join(root, "consumer.txt"))) != "base\n" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("occurrence mismatch blocks", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "a.txt"), "a\n")
		projection := oneFileProjection(speccompiler.ReplayEvolvingPathChain, modifyWork("1.1.file.1", "1.1", "chain.a", "a.txt", directive("1.1.file.1.change.1", "replace", "a", "b", 2)))
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || string(mustRead(t, filepath.Join(root, "a.txt"))) != "a\n" {
			t.Fatalf("result = %+v", result)
		}
	})
}

func TestApplySupportedFilesystemOperations(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "modify.txt"), "old\n")
	mustWrite(t, filepath.Join(root, "delete.txt"), "delete\n")
	mustWrite(t, filepath.Join(root, "rename.txt"), "rename\n")
	projection := projection(
		[]speccompiler.ProjectedSubstep{
			{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}},
			{Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}},
			{Ref: "1.3", FileWorkRefs: []string{"1.3.file.1"}},
			{Ref: "1.4", FileWorkRefs: []string{"1.4.file.1"}},
		},
		[]speccompiler.ProjectedPathChain{
			chain("chain.modify", 0, []string{"1.1.file.1"}, []string{"modify.txt"}, []string{"1.1"}),
			chain("chain.create", 1, []string{"1.2.file.1"}, []string{"create.txt"}, []string{"1.2"}),
			chain("chain.delete", 2, []string{"1.3.file.1"}, []string{"delete.txt"}, []string{"1.3"}),
			chain("chain.rename", 3, []string{"1.4.file.1"}, []string{"rename.txt", "renamed.txt"}, []string{"1.4"}),
		},
		[]speccompiler.ProjectedFileWork{
			modifyWork("1.1.file.1", "1.1", "chain.modify", "modify.txt", directive("1.1.file.1.change.1", "replace", "old", "new", 1)),
			{Ref: "1.2.file.1", SubstepRef: "1.2", PathChainRef: "chain.create", Path: "create.txt", Operation: "create", Content: "created\n"},
			{Ref: "1.3.file.1", SubstepRef: "1.3", PathChainRef: "chain.delete", Path: "delete.txt", Operation: "delete", DeleteFile: true},
			{Ref: "1.4.file.1", SubstepRef: "1.4", PathChainRef: "chain.rename", Path: "rename.txt", DestinationPath: "renamed.txt", Operation: "rename", PreserveContent: true},
		},
	)
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeCompleted {
		t.Fatalf("result = %+v", result)
	}
	if got := string(mustRead(t, filepath.Join(root, "modify.txt"))); got != "new\n" {
		t.Fatalf("modified content = %q", got)
	}
	if got := string(mustRead(t, filepath.Join(root, "create.txt"))); got != "created\n" {
		t.Fatalf("created content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete path still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "rename.txt")); !os.IsNotExist(err) {
		t.Fatalf("rename source still exists: %v", err)
	}
	if got := string(mustRead(t, filepath.Join(root, "renamed.txt"))); got != "rename\n" {
		t.Fatalf("renamed content = %q", got)
	}
}

func TestApplyMaterialBlockerPreventsAllMutation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "safe.txt"), "old\n")
	projection := projection(
		[]speccompiler.ProjectedSubstep{{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}}, {Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}}},
		[]speccompiler.ProjectedPathChain{
			chain("chain.safe", 0, []string{"1.1.file.1"}, []string{"safe.txt"}, []string{"1.1"}),
			chain("chain.blocked", 1, []string{"1.2.file.1"}, []string{"missing.txt"}, []string{"1.2"}),
		},
		[]speccompiler.ProjectedFileWork{
			modifyWork("1.1.file.1", "1.1", "chain.safe", "safe.txt", directive("1.1.file.1.change.1", "replace", "old", "new", 1)),
			{Ref: "1.2.file.1", SubstepRef: "1.2", PathChainRef: "chain.blocked", Path: "missing.txt", Operation: "delete", DeleteFile: true},
		},
	)
	result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeBlocked || string(mustRead(t, filepath.Join(root, "safe.txt"))) != "old\n" || len(result.ChangedFiles) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestApplyAtomicAndDependencyPropagation(t *testing.T) {
	t.Run("atomic residual propagates through dependency chain", func(t *testing.T) {
		root := t.TempDir()
		for _, path := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
			mustWrite(t, filepath.Join(root, path), strings.TrimSuffix(path, ".txt")+"\n")
		}
		projection := propagationProjection(false)
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeNotAttempted || len(result.Partition.DeterministicFileWork) != 0 || len(result.Partition.ResidualFileWork) != 4 {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("atomic blocker and dependencies block globally", func(t *testing.T) {
		root := t.TempDir()
		for _, path := range []string{"a.txt", "c.txt", "d.txt"} {
			mustWrite(t, filepath.Join(root, path), strings.TrimSuffix(path, ".txt")+"\n")
		}
		projection := propagationProjection(true)
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || len(result.ChangedFiles) != 0 {
			t.Fatalf("result = %+v", result)
		}
		if len(result.ImplementationResult.BlockedPathChains) != 4 {
			t.Fatalf("blocked propagation = %+v", result.ImplementationResult)
		}
	})
}

func TestBuildPartitionRejectsSplitBoundaries(t *testing.T) {
	t.Run("connected path chain", func(t *testing.T) {
		projection := speccompiler.ExecutionProjection{
			Substeps:   []speccompiler.ProjectedSubstep{{Ref: "1.1", FileWorkRefs: []string{"a", "b"}}},
			PathChains: []speccompiler.ProjectedPathChain{{Ref: "chain.shared", FileWorkRefs: []string{"a", "b"}}},
			FileWork:   []speccompiler.ProjectedFileWork{{Ref: "a", PathChainRef: "chain.shared"}, {Ref: "b", PathChainRef: "chain.shared"}},
		}
		plans := []pathChainPlan{
			{ref: "chain.shared", fileWorkRefs: []string{"a"}, disposition: DispositionDeterministic},
			{ref: "chain.shared", fileWorkRefs: []string{"b"}, disposition: DispositionResidual},
		}
		if _, reason := buildPartition(plans, projection); !strings.Contains(reason, "duplicate path-chain plan") {
			t.Fatalf("reason = %q", reason)
		}
	})

	t.Run("atomic substep", func(t *testing.T) {
		projection := speccompiler.ExecutionProjection{
			Substeps: []speccompiler.ProjectedSubstep{{Ref: "1.1", AtomicPresent: true, Atomic: true, FileWorkRefs: []string{"a", "b"}}},
			PathChains: []speccompiler.ProjectedPathChain{
				{Ref: "chain.a", FileWorkRefs: []string{"a"}},
				{Ref: "chain.b", FileWorkRefs: []string{"b"}},
			},
			FileWork: []speccompiler.ProjectedFileWork{{Ref: "a", PathChainRef: "chain.a"}, {Ref: "b", PathChainRef: "chain.b"}},
		}
		plans := []pathChainPlan{
			{ref: "chain.a", fileWorkRefs: []string{"a"}, disposition: DispositionDeterministic},
			{ref: "chain.b", fileWorkRefs: []string{"b"}, disposition: DispositionResidual},
		}
		if _, reason := buildPartition(plans, projection); !strings.Contains(reason, "atomic substep") {
			t.Fatalf("reason = %q", reason)
		}
	})
}

func TestApplyEnvironmentalFailureEvidenceIsTruthful(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"first.txt", "second.txt"} {
		mustWrite(t, filepath.Join(root, path), "old\n")
	}
	mustWrite(t, filepath.Join(root, "residual.txt"), "residual\n")
	projection := environmentalProjection()
	service := NewService()
	calls := 0
	service.applyMutations = func(actions []mutationAction) ([]string, error) {
		calls++
		if calls == 2 {
			if err := os.Remove(filepath.Join(root, "second.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "second.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return applyMutationActions(actions)
	}
	result, err := service.Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeBlocked || result.FailurePacket == nil {
		t.Fatalf("result = %+v", result)
	}
	assertStrings(t, "changed", result.ChangedFiles, []string{"first.txt"})
	assertStrings(t, "protected", result.Partition.ProtectedPaths, []string{"first.txt"})
	assertStrings(t, "uncertain chains", result.ImplementationResult.UncertainPathChains, []string{"chain.second"})
	assertStrings(t, "uncertain paths", result.ImplementationResult.UncertainPaths, []string{"second.txt"})
	assertStrings(t, "unattempted", result.ImplementationResult.UnattemptedPathChains, []string{"chain.third"})
	assertStrings(t, "residual", result.ImplementationResult.ResidualPathChains, []string{"chain.residual"})
	if contains(result.Partition.ProtectedPaths, "third.txt") || contains(result.Partition.ProtectedPaths, "second.txt") {
		t.Fatalf("protected paths include untouched or uncertain work: %+v", result.Partition)
	}
	if !strings.Contains(result.FailurePacket.Summary, "second.txt") || !reflect.DeepEqual(result.FailurePacket.UncertainPaths, []string{"second.txt"}) {
		t.Fatalf("failure packet = %+v", result.FailurePacket)
	}
}

func TestApplyEvidenceFailurePreservesMutationFailure(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"first.txt", "second.txt"} {
		mustWrite(t, filepath.Join(root, path), "old\n")
	}
	mustWrite(t, filepath.Join(root, "residual.txt"), "residual\n")
	service := NewService()
	calls := 0
	service.applyMutations = func(actions []mutationAction) ([]string, error) {
		calls++
		if calls == 2 {
			if err := os.Remove(filepath.Join(root, "second.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "second.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return applyMutationActions(actions)
	}
	result, err := service.Apply(context.Background(), Input{WorkspaceRoot: root, Projection: environmentalProjection(), EvidenceWriter: failingEvidenceWriter{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.FailurePacket == nil || !strings.Contains(result.FailurePacket.Summary, "second.txt") || !strings.Contains(result.FailurePacket.EvidenceFailureReason, "disk unavailable") {
		t.Fatalf("failure packet = %+v", result.FailurePacket)
	}
	assertStrings(t, "uncertain chains", result.FailurePacket.UncertainPathChains, []string{"chain.second"})
	assertStrings(t, "changed", result.FailurePacket.ChangedFiles, []string{"first.txt"})
	assertStrings(t, "protected", result.FailurePacket.ProtectedPaths, []string{"first.txt"})
}

func TestApplyCancellationStopsLaterWork(t *testing.T) {
	t.Run("during classification", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "first.txt"), "old\n")
		mustWrite(t, filepath.Join(root, "second.txt"), "old\n")
		projection := projection(
			[]speccompiler.ProjectedSubstep{{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}}, {Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}}},
			[]speccompiler.ProjectedPathChain{
				chain("chain.first", 0, []string{"1.1.file.1"}, []string{"first.txt"}, []string{"1.1"}),
				chain("chain.second", 1, []string{"1.2.file.1"}, []string{"second.txt"}, []string{"1.2"}),
			},
			[]speccompiler.ProjectedFileWork{
				modifyWork("1.1.file.1", "1.1", "chain.first", "first.txt", directive("1.1.file.1.change.1", "replace", "old", "new", 1)),
				modifyWork("1.2.file.1", "1.2", "chain.second", "second.txt", directive("1.2.file.1.change.1", "replace", "old", "new", 1)),
			},
		)
		ctx := &cancelDuringClassificationContext{Context: context.Background(), cancelAt: 3}
		result, err := NewService().Apply(ctx, Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || result.ImplementationResult.ModelExecutorRequired || len(result.ChangedFiles) != 0 {
			t.Fatalf("result = %+v", result)
		}
		if string(mustRead(t, filepath.Join(root, "first.txt"))) != "old\n" || string(mustRead(t, filepath.Join(root, "second.txt"))) != "old\n" {
			t.Fatal("classification cancellation mutated source")
		}
	})

	t.Run("between deterministic chains", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "first.txt"), "old\n")
		mustWrite(t, filepath.Join(root, "second.txt"), "old\n")
		projection := projection(
			[]speccompiler.ProjectedSubstep{{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}}, {Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}}},
			[]speccompiler.ProjectedPathChain{
				chain("chain.first", 0, []string{"1.1.file.1"}, []string{"first.txt"}, []string{"1.1"}),
				chain("chain.second", 1, []string{"1.2.file.1"}, []string{"second.txt"}, []string{"1.2"}),
			},
			[]speccompiler.ProjectedFileWork{
				modifyWork("1.1.file.1", "1.1", "chain.first", "first.txt", directive("1.1.file.1.change.1", "replace", "old", "new", 1)),
				modifyWork("1.2.file.1", "1.2", "chain.second", "second.txt", directive("1.2.file.1.change.1", "replace", "old", "new", 1)),
			},
		)
		service := NewService()
		service.applyMutations = func(actions []mutationAction) ([]string, error) {
			changed, err := applyMutationActions(actions)
			cancel()
			return changed, err
		}
		result, err := service.Apply(ctx, Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || string(mustRead(t, filepath.Join(root, "first.txt"))) != "new\n" || string(mustRead(t, filepath.Join(root, "second.txt"))) != "old\n" {
			t.Fatalf("result = %+v", result)
		}
		assertStrings(t, "protected", result.Partition.ProtectedPaths, []string{"first.txt"})
		assertStrings(t, "blocked", result.ImplementationResult.BlockedPathChains, []string{"chain.second"})
	})
}

type cancelDuringClassificationContext struct {
	context.Context
	calls    int
	cancelAt int
}

func (c *cancelDuringClassificationContext) Err() error {
	c.calls++
	if c.calls >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestApplyRejectsUnsafePathAndWritesCoherentEvidence(t *testing.T) {
	t.Run("unsafe path", func(t *testing.T) {
		root := t.TempDir()
		projection := oneFileProjection(speccompiler.ReplayEvolvingPathChain, speccompiler.ProjectedFileWork{Ref: "1.1.file.1", SubstepRef: "1.1", PathChainRef: "chain.a", Path: ".git/HEAD", Operation: "create", Content: "bad\n"})
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeBlocked || len(result.ChangedFiles) != 0 {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("successful evidence agrees", func(t *testing.T) {
		root := t.TempDir()
		writer := &MemoryEvidenceWriter{}
		projection := oneFileProjection(speccompiler.ReplayEvolvingPathChain, speccompiler.ProjectedFileWork{Ref: "1.1.file.1", SubstepRef: "1.1", PathChainRef: "chain.a", Path: "new.txt", Operation: "create", Content: "new\n"})
		result, err := NewService().Apply(context.Background(), Input{WorkspaceRoot: root, Projection: projection, EvidenceWriter: writer})
		if err != nil {
			t.Fatal(err)
		}
		if result.Outcome != OutcomeCompleted || len(writer.Files) != 4 {
			t.Fatalf("result = %+v evidence=%d", result, len(writer.Files))
		}
		assertStrings(t, "partition protected", result.Partition.ProtectedPaths, []string{"new.txt"})
		assertStrings(t, "result protected", result.ImplementationResult.ProtectedPaths, []string{"new.txt"})
		assertStrings(t, "ledger protected", result.Ledger.Entries[0].ProtectedPaths, []string{"new.txt"})
	})
}

func propagationProjection(block bool) speccompiler.ExecutionProjection {
	atomic := true
	second := speccompiler.ProjectedFileWork{Ref: "1.1.file.2", SubstepRef: "1.1", PathChainRef: "chain.b", Path: "b.txt", DestinationPath: "renamed.txt", Operation: "rename", Content: "replacement\n"}
	if block {
		second = speccompiler.ProjectedFileWork{Ref: "1.1.file.2", SubstepRef: "1.1", PathChainRef: "chain.b", Path: "b.txt", Operation: "delete", DeleteFile: true}
	}
	return projection(
		[]speccompiler.ProjectedSubstep{
			{Ref: "1.1", AtomicPresent: true, Atomic: atomic, FileWorkRefs: []string{"1.1.file.1", "1.1.file.2"}},
			{Ref: "1.2", Dependencies: []string{"1.1"}, FileWorkRefs: []string{"1.2.file.1"}},
			{Ref: "1.3", Dependencies: []string{"1.2"}, FileWorkRefs: []string{"1.3.file.1"}},
		},
		[]speccompiler.ProjectedPathChain{
			chain("chain.a", 0, []string{"1.1.file.1"}, []string{"a.txt"}, []string{"1.1"}),
			chain("chain.b", 1, []string{"1.1.file.2"}, endpoints(second), []string{"1.1"}),
			chain("chain.c", 2, []string{"1.2.file.1"}, []string{"c.txt"}, []string{"1.2"}),
			chain("chain.d", 3, []string{"1.3.file.1"}, []string{"d.txt"}, []string{"1.3"}),
		},
		[]speccompiler.ProjectedFileWork{
			modifyWork("1.1.file.1", "1.1", "chain.a", "a.txt", directive("1.1.file.1.change.1", "replace", "a", "aa", 1)),
			second,
			modifyWork("1.2.file.1", "1.2", "chain.c", "c.txt", directive("1.2.file.1.change.1", "replace", "c", "cc", 1)),
			modifyWork("1.3.file.1", "1.3", "chain.d", "d.txt", directive("1.3.file.1.change.1", "replace", "d", "dd", 1)),
		},
	)
}

func environmentalProjection() speccompiler.ExecutionProjection {
	residual := speccompiler.ProjectedFileWork{Ref: "1.4.file.1", SubstepRef: "1.4", PathChainRef: "chain.residual", Path: "residual.txt", DestinationPath: "residual-new.txt", Operation: "rename", Content: "replacement\n"}
	return projection(
		[]speccompiler.ProjectedSubstep{
			{Ref: "1.1", FileWorkRefs: []string{"1.1.file.1"}},
			{Ref: "1.2", FileWorkRefs: []string{"1.2.file.1"}},
			{Ref: "1.3", FileWorkRefs: []string{"1.3.file.1"}},
			{Ref: "1.4", FileWorkRefs: []string{"1.4.file.1"}},
		},
		[]speccompiler.ProjectedPathChain{
			chain("chain.first", 0, []string{"1.1.file.1"}, []string{"first.txt"}, []string{"1.1"}),
			chain("chain.second", 1, []string{"1.2.file.1"}, []string{"second.txt"}, []string{"1.2"}),
			chain("chain.third", 2, []string{"1.3.file.1"}, []string{"third.txt"}, []string{"1.3"}),
			chain("chain.residual", 3, []string{"1.4.file.1"}, endpoints(residual), []string{"1.4"}),
		},
		[]speccompiler.ProjectedFileWork{
			modifyWork("1.1.file.1", "1.1", "chain.first", "first.txt", directive("1.1.file.1.change.1", "replace", "old", "new", 1)),
			modifyWork("1.2.file.1", "1.2", "chain.second", "second.txt", directive("1.2.file.1.change.1", "replace", "old", "new", 1)),
			{Ref: "1.3.file.1", SubstepRef: "1.3", PathChainRef: "chain.third", Path: "third.txt", Operation: "create", Content: "third\n"},
			residual,
		},
	)
}

func projection(substeps []speccompiler.ProjectedSubstep, chains []speccompiler.ProjectedPathChain, works []speccompiler.ProjectedFileWork) speccompiler.ExecutionProjection {
	return speccompiler.ExecutionProjection{Replay: speccompiler.ReplayEvolvingPathChain, Substeps: substeps, PathChains: chains, FileWork: works}
}

func oneFileProjection(replay speccompiler.ReplaySemantics, work speccompiler.ProjectedFileWork) speccompiler.ExecutionProjection {
	return speccompiler.ExecutionProjection{
		Replay:   replay,
		Substeps: []speccompiler.ProjectedSubstep{{Ref: "1.1", FileWorkRefs: []string{work.Ref}}},
		PathChains: []speccompiler.ProjectedPathChain{{
			Ref:              work.PathChainRef,
			FileWorkRefs:     []string{work.Ref},
			PathEndpoints:    endpoints(work),
			SubstepRefs:      []string{"1.1"},
			Replay:           replay,
			FirstSourceOrder: 0,
		}},
		FileWork: []speccompiler.ProjectedFileWork{work},
	}
}

func chain(ref string, order int, fileRefs, paths, substeps []string) speccompiler.ProjectedPathChain {
	return speccompiler.ProjectedPathChain{Ref: ref, FileWorkRefs: fileRefs, PathEndpoints: paths, SubstepRefs: substeps, Replay: speccompiler.ReplayEvolvingPathChain, FirstSourceOrder: order}
}

func modifyWork(ref, substep, chainRef, path string, directives ...speccompiler.ProjectedDirective) speccompiler.ProjectedFileWork {
	return speccompiler.ProjectedFileWork{Ref: ref, SubstepRef: substep, PathChainRef: chainRef, Path: path, Operation: "modify", Directives: directives}
}

func directive(ref, kind, oldText, newText string, occurrences int) speccompiler.ProjectedDirective {
	return speccompiler.ProjectedDirective{Ref: ref, Kind: kind, OldText: oldText, NewText: newText, ExpectedOccurrences: occurrences}
}

func endpoints(work speccompiler.ProjectedFileWork) []string {
	if work.Operation == "rename" {
		return []string{work.Path, work.DestinationPath}
	}
	return []string{work.Path}
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
