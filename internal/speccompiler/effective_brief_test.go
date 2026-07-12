package speccompiler

import (
	"strings"
	"testing"
)

func TestRenderEffectiveExecutorBriefResidualOnly(t *testing.T) {
	document := effectiveBriefDocument(false, false)
	selection := EffectiveBriefSelection{
		Mode:                  EffectiveBriefResidual,
		ResidualFileWorkRefs:  []string{"1.2.file.1"},
		CompletedFileWorkRefs: []string{"1.1.file.1"},
		ProtectedPaths:        []string{"done.txt"},
	}
	first, err := RenderEffectiveExecutorBrief(document, selection)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderEffectiveExecutorBrief(document, selection)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasSuffix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Fatal("residual rendering is not byte deterministic with one final newline")
	}
	for _, required := range []string{"`remaining.txt`", "before", "after", "1.1.file.1", "`done.txt`", "go test ./internal/example", "The combined result is complete.", "sole implementation authority"} {
		if !strings.Contains(first, required) {
			t.Fatalf("residual brief missing %q:\n%s", required, first)
		}
	}
	for _, forbidden := range []string{"`done.txt` - Completed work.", "Old text:\n\n  ```text\n  old"} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("residual brief retained completed implementation %q:\n%s", forbidden, first)
		}
	}
}

func TestRenderEffectiveExecutorBriefRejectsPathChainSplit(t *testing.T) {
	document := effectiveBriefDocument(false, true)
	selection := EffectiveBriefSelection{
		Mode:                  EffectiveBriefResidual,
		ResidualFileWorkRefs:  []string{"1.2.file.1"},
		CompletedFileWorkRefs: []string{"1.1.file.1"},
		ProtectedPaths:        []string{"shared.txt"},
	}
	if _, err := RenderEffectiveExecutorBrief(document, selection); err == nil || !strings.Contains(err.Error(), "path chain") {
		t.Fatalf("expected path-chain split rejection, got %v", err)
	}
}

func TestRenderEffectiveExecutorBriefRejectsAtomicSplit(t *testing.T) {
	document := effectiveBriefDocument(true, false)
	selection := EffectiveBriefSelection{
		Mode:                  EffectiveBriefResidual,
		ResidualFileWorkRefs:  []string{"1.1.file.2"},
		CompletedFileWorkRefs: []string{"1.1.file.1"},
		ProtectedPaths:        []string{"done.txt"},
	}
	if _, err := RenderEffectiveExecutorBrief(document, selection); err == nil || !strings.Contains(err.Error(), "atomic substep") {
		t.Fatalf("expected atomic split rejection, got %v", err)
	}
}

func TestRenderEffectiveExecutorBriefRejectsInvalidSelection(t *testing.T) {
	document := &ExecutionDocument{
		SchemaVersion: "2.0",
		FeatureSlug:   "effective-brief",
		RepoTarget:    "relay",
		Branch:        "main",
		BaseCommit:    strings.Repeat("a", 40),
		Goal:          "Render one residual declaration.",
		Context:       "Selection validation.",
		Scope:         scopeModel{InScope: []string{"One file."}, OutOfScope: []string{"Nothing else."}},
		Steps:         []ExecutionStep{{Number: 1, Goal: "One step.", Substeps: []ExecutionSubstep{{Number: 1, Instruction: "Create one file.", Files: []ExecutionFile{{Path: "a.txt", Operation: "create", Purpose: "Create it.", Implementation: ExecutionFileImplementation{Content: "a\n"}}}, Completion: []string{"Created."}}}, Completion: []string{"Complete."}}},
		Validation:    ExecutionValidation{Commands: []ExecutionValidationCommand{{Command: "go test ./internal/example", Expected: "Tests pass."}}},
		Completion:    []string{"Complete."},
	}
	cases := []EffectiveBriefSelection{
		{Mode: EffectiveBriefFull, ResidualFileWorkRefs: []string{"1.1.file.1"}},
		{Mode: EffectiveBriefResidual},
		{Mode: EffectiveBriefResidual, ResidualFileWorkRefs: []string{"missing"}},
		{Mode: EffectiveBriefResidual, ResidualFileWorkRefs: []string{"1.1.file.1"}, CompletedFileWorkRefs: []string{"1.1.file.1"}},
		{Mode: EffectiveBriefResidual, ResidualFileWorkRefs: []string{"1.1.file.1"}, ProtectedPaths: []string{"z.txt", "a.txt"}},
	}
	for _, selection := range cases {
		if _, err := RenderEffectiveExecutorBrief(document, selection); err == nil {
			t.Fatalf("expected selection rejection: %+v", selection)
		}
	}
}

func effectiveBriefDocument(atomic, sharedPath bool) *ExecutionDocument {
	atomicValue := (*bool)(nil)
	if atomic {
		value := true
		atomicValue = &value
	}
	firstPath := "done.txt"
	secondPath := "remaining.txt"
	if sharedPath {
		firstPath = "shared.txt"
		secondPath = "shared.txt"
	}
	firstSubstep := ExecutionSubstep{
		Number:      1,
		Instruction: "Apply completed work.",
		Files: []ExecutionFile{{
			Path: firstPath, Operation: "modify", Purpose: "Completed work.",
			Implementation: ExecutionFileImplementation{Changes: []ExecutionDirective{{Kind: "replace", OldText: "old", NewText: "done", ExpectedOccurrences: 1}}},
		}},
		Completion: []string{"Completed declaration is satisfied."},
	}
	secondSubstep := ExecutionSubstep{
		Number:      2,
		Instruction: "Apply residual work.",
		Files: []ExecutionFile{{
			Path: secondPath, Operation: "modify", Purpose: "Residual work.",
			Implementation: ExecutionFileImplementation{Changes: []ExecutionDirective{{Kind: "replace", OldText: "before", NewText: "after", ExpectedOccurrences: 1}}},
		}},
		Completion: []string{"Residual declaration is satisfied."},
	}
	if atomic {
		firstSubstep.Atomic = atomicValue
		firstSubstep.Files = append(firstSubstep.Files, secondSubstep.Files[0])
		secondSubstep = ExecutionSubstep{}
	}
	substeps := []ExecutionSubstep{firstSubstep}
	if !atomic {
		substeps = append(substeps, secondSubstep)
	}
	return &ExecutionDocument{
		SchemaVersion: "2.0",
		FeatureSlug:   "effective-brief",
		RepoTarget:    "relay",
		Branch:        "main",
		BaseCommit:    strings.Repeat("a", 40),
		Goal:          "Apply deterministic work and execute only the residual work.",
		Context:       "Residual rendering test context.",
		Scope:         scopeModel{InScope: []string{"Implement the declared work."}, OutOfScope: []string{"No unrelated changes."}},
		Steps: []ExecutionStep{{
			Number:     1,
			Goal:       "Complete both declarations.",
			Substeps:   substeps,
			Completion: []string{"The step is complete."},
		}},
		Validation: ExecutionValidation{
			Commands:       []ExecutionValidationCommand{{Command: "go test ./internal/example", Expected: "Tests pass."}},
			ExecutorChecks: []string{"Confirm the protected path remains unchanged."},
		},
		Completion: []string{"The combined result is complete."},
	}
}
