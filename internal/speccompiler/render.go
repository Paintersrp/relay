package speccompiler

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const derivedNotice = "> Derived from canonical JSON. Do not edit this Markdown independently."

func renderExecutionSpec(spec *ExecutionDocument) (string, error) {
	var b strings.Builder
	b.WriteString("# Executor Brief\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")

	writeExecutionTarget(&b, spec)
	writeTextSection(&b, "## Goal", spec.Goal)
	writeTextSection(&b, "## Context", spec.Context)
	writeExecutionScope(&b, spec)
	writeExecutionImplementation(&b, spec, nil)
	writeExecutionValidation(&b, spec)
	writeBulletSection(&b, "## Completion Criteria", spec.Completion)
	writeFullExecutionInstructions(&b)
	return oneFinalNewline(b.String()), nil
}

func RenderEffectiveExecutorBrief(spec *ExecutionDocument, selection EffectiveBriefSelection) (string, error) {
	if spec == nil {
		return "", fmt.Errorf("execution document is required")
	}
	if selection.Mode != EffectiveBriefResidual {
		return "", fmt.Errorf("effective brief renderer requires residual mode")
	}
	selected, err := validateEffectiveBriefSelection(spec, selection)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Executor Brief\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")
	writeExecutionTarget(&b, spec)
	writeTextSection(&b, "## Goal", spec.Goal)
	writeTextSection(&b, "## Context", spec.Context)
	writeExecutionScope(&b, spec)
	writeResidualDetails(&b, selection)
	writeExecutionImplementation(&b, spec, selected)
	writeExecutionValidation(&b, spec)
	writeBulletSection(&b, "## Completion Criteria", spec.Completion)
	writeResidualExecutionInstructions(&b)
	return oneFinalNewline(b.String()), nil
}

func validateEffectiveBriefSelection(spec *ExecutionDocument, selection EffectiveBriefSelection) (map[string]struct{}, error) {
	if len(selection.ResidualFileWorkRefs) == 0 {
		return nil, fmt.Errorf("residual effective brief requires at least one residual file-work reference")
	}
	known := make([]string, 0)
	knownSet := map[string]struct{}{}
	for _, step := range spec.Steps {
		for _, substep := range step.Substeps {
			for fileIndex := range substep.Files {
				ref := fmt.Sprintf("%d.%d.file.%d", step.Number, substep.Number, fileIndex+1)
				known = append(known, ref)
				knownSet[ref] = struct{}{}
			}
		}
	}
	residual, err := checkedReferenceSet("residual", selection.ResidualFileWorkRefs, knownSet)
	if err != nil {
		return nil, err
	}
	completed, err := checkedReferenceSet("completed", selection.CompletedFileWorkRefs, knownSet)
	if err != nil {
		return nil, err
	}
	for ref := range residual {
		if _, overlap := completed[ref]; overlap {
			return nil, fmt.Errorf("file-work reference %q is both residual and completed", ref)
		}
	}
	if len(residual)+len(completed) != len(known) {
		return nil, fmt.Errorf("effective brief selection does not cover every canonical file-work reference")
	}
	if !sameReferenceOrder(selection.ResidualFileWorkRefs, known, residual) {
		return nil, fmt.Errorf("residual file-work references are not in canonical order")
	}
	if !sameReferenceOrder(selection.CompletedFileWorkRefs, known, completed) {
		return nil, fmt.Errorf("completed file-work references are not in canonical order")
	}
	projection, diagnostics := ProjectExecutionSpec(spec)
	if len(diagnostics) != 0 {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, fmt.Sprintf("%s at %s: %s", diagnostic.Code, diagnostic.Path, diagnostic.Message))
		}
		return nil, fmt.Errorf("project residual effective brief selection: %s", strings.Join(messages, "; "))
	}
	if err := validateEffectiveBriefPartition(projection, residual, completed); err != nil {
		return nil, err
	}
	if !sort.StringsAreSorted(selection.ProtectedPaths) {
		return nil, fmt.Errorf("protected paths are not in deterministic order")
	}
	for index := 1; index < len(selection.ProtectedPaths); index++ {
		if selection.ProtectedPaths[index] == selection.ProtectedPaths[index-1] {
			return nil, fmt.Errorf("protected path %q is duplicated", selection.ProtectedPaths[index])
		}
	}
	return residual, nil
}

func validateEffectiveBriefPartition(projection ExecutionProjection, residual, completed map[string]struct{}) error {
	selectionSide := func(ref string) (string, error) {
		if _, ok := residual[ref]; ok {
			return "residual", nil
		}
		if _, ok := completed[ref]; ok {
			return "completed", nil
		}
		return "", fmt.Errorf("projected file-work reference %q is not selected", ref)
	}
	for _, chain := range projection.PathChains {
		side := ""
		for _, ref := range chain.FileWorkRefs {
			current, err := selectionSide(ref)
			if err != nil {
				return err
			}
			if side != "" && side != current {
				return fmt.Errorf("path chain %q is split between completed and residual file work", chain.Ref)
			}
			side = current
		}
	}
	for _, substep := range projection.Substeps {
		if !substep.AtomicPresent || !substep.Atomic {
			continue
		}
		side := ""
		for _, ref := range substep.FileWorkRefs {
			current, err := selectionSide(ref)
			if err != nil {
				return err
			}
			if side != "" && side != current {
				return fmt.Errorf("atomic substep %q is split between completed and residual file work", substep.Ref)
			}
			side = current
		}
	}
	return nil
}

func checkedReferenceSet(label string, refs []string, known map[string]struct{}) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, exists := known[ref]; !exists {
			return nil, fmt.Errorf("unknown %s file-work reference %q", label, ref)
		}
		if _, duplicate := set[ref]; duplicate {
			return nil, fmt.Errorf("duplicate %s file-work reference %q", label, ref)
		}
		set[ref] = struct{}{}
	}
	return set, nil
}

func sameReferenceOrder(selected, canonical []string, membership map[string]struct{}) bool {
	expected := make([]string, 0, len(selected))
	for _, ref := range canonical {
		if _, ok := membership[ref]; ok {
			expected = append(expected, ref)
		}
	}
	if len(expected) != len(selected) {
		return false
	}
	for index := range expected {
		if expected[index] != selected[index] {
			return false
		}
	}
	return true
}

func writeExecutionTarget(b *strings.Builder, spec *ExecutionDocument) {
	b.WriteString("## Target\n\n")
	fmt.Fprintf(b, "- Repository: `%s`\n", spec.RepoTarget)
	fmt.Fprintf(b, "- Branch: `%s`\n", spec.Branch)
	fmt.Fprintf(b, "- Base commit: `%s`\n\n", spec.BaseCommit)
}

func writeExecutionScope(b *strings.Builder, spec *ExecutionDocument) {
	b.WriteString("## Scope\n\n")
	writeBulletSection(b, "### In Scope", spec.Scope.InScope)
	writeBulletSection(b, "### Out of Scope", spec.Scope.OutOfScope)
}

func writeExecutionImplementation(b *strings.Builder, spec *ExecutionDocument, selected map[string]struct{}) {
	b.WriteString("## Implementation\n\n")
	for _, step := range spec.Steps {
		includedSubsteps := make([]ExecutionSubstep, 0, len(step.Substeps))
		for _, substep := range step.Substeps {
			files := make([]ExecutionFile, 0, len(substep.Files))
			for fileIndex, file := range substep.Files {
				ref := fmt.Sprintf("%d.%d.file.%d", step.Number, substep.Number, fileIndex+1)
				if selected == nil {
					files = append(files, file)
					continue
				}
				if _, ok := selected[ref]; ok {
					files = append(files, file)
				}
			}
			if len(files) == 0 {
				continue
			}
			copySubstep := substep
			copySubstep.Files = files
			includedSubsteps = append(includedSubsteps, copySubstep)
		}
		if len(includedSubsteps) == 0 {
			continue
		}
		fmt.Fprintf(b, "### Step %d: %s\n\n", step.Number, trimHuman(step.Goal))
		for _, substep := range includedSubsteps {
			writeExecutionSubstep(b, step.Number, substep)
		}
		writeBulletSection(b, "#### Step Completion Criteria", step.Completion)
	}
}

func writeExecutionSubstep(b *strings.Builder, stepNumber int, substep ExecutionSubstep) {
	fmt.Fprintf(b, "#### Substep %d.%d\n\n", stepNumber, substep.Number)
	b.WriteString("##### Files\n\n")
	for _, file := range substep.Files {
		if file.Operation == "rename" {
			fmt.Fprintf(b, "- `%s` `%s` -> `%s` - %s\n", file.Operation, file.Path, file.DestinationPath, trimHuman(file.Purpose))
		} else {
			fmt.Fprintf(b, "- `%s` `%s` - %s\n", file.Operation, file.Path, trimHuman(file.Purpose))
		}
	}
	b.WriteString("\n##### Instruction\n\n")
	b.WriteString(trimHuman(substep.Instruction))
	b.WriteString("\n\n")
	if substep.DependsOn != nil || substep.Atomic != nil {
		b.WriteString("##### Execution Constraints\n\n")
		if substep.DependsOn != nil {
			b.WriteString("- Depends on: ")
			for index, dependency := range substep.DependsOn {
				if index != 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(b, "`%s`", dependency)
			}
			b.WriteString("\n")
		}
		if substep.Atomic != nil {
			value := "not required"
			if *substep.Atomic {
				value = "required"
			}
			fmt.Fprintf(b, "- Atomic deterministic preflight: %s\n", value)
		}
		b.WriteString("\n")
	}
	b.WriteString("##### Implementation\n\n")
	for _, file := range substep.Files {
		renderFileImplementation(b, file)
	}
	writeBulletSection(b, "##### Completion Criteria", substep.Completion)
}

func writeResidualDetails(b *strings.Builder, selection EffectiveBriefSelection) {
	b.WriteString("## Relay Deterministic Pre-Application\n\n")
	b.WriteString("- Effective brief mode: `residual`\n")
	b.WriteString("- Completed canonical file-work references:\n")
	writeIndentedReferenceList(b, selection.CompletedFileWorkRefs)
	b.WriteString("- Protected changed paths:\n")
	writeIndentedReferenceList(b, selection.ProtectedPaths)
	b.WriteString("\n")
}

func writeIndentedReferenceList(b *strings.Builder, values []string) {
	if len(values) == 0 {
		b.WriteString("  - none\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(b, "  - `%s`\n", value)
	}
}

func writeExecutionValidation(b *strings.Builder, spec *ExecutionDocument) {
	b.WriteString("## Validation\n\n")
	b.WriteString("### Commands\n\n")
	for index, command := range spec.Validation.Commands {
		fmt.Fprintf(b, "%d. Command:\n\n", index+1)
		writeFence(b, "   ", command.Command)
		b.WriteString("\n")
		if command.WorkingDirectory != "" {
			fmt.Fprintf(b, "   - Working directory: `%s`\n", command.WorkingDirectory)
		}
		fmt.Fprintf(b, "   - Expected: %s\n\n", trimHuman(command.Expected))
	}
	if len(spec.Validation.ExecutorChecks) != 0 {
		writeBulletSection(b, "### Executor Checks", spec.Validation.ExecutorChecks)
	}
}

func writeFullExecutionInstructions(b *strings.Builder) {
	b.WriteString("## Execution Instructions\n\n")
	b.WriteString("- Treat this effective brief as the sole implementation authority for this attempt.\n")
	b.WriteString("- This canonical brief is full mode; every declared implementation directive remains required.\n")
	b.WriteString("- Apply the declared implementation exactly, using only necessary source-compatible adaptation that preserves behavior, architecture, scope, and material code shape.\n")
	b.WriteString("- Preserve unrelated work and avoid unrelated cleanup or refactoring.\n")
	b.WriteString("- Run the specified validation and report exact results, blockers, or incomplete work.\n")
}

func writeResidualExecutionInstructions(b *strings.Builder) {
	b.WriteString("## Execution Instructions\n\n")
	b.WriteString("- Treat this residual effective brief as the sole implementation authority for this attempt.\n")
	b.WriteString("- Only the implementation work present in this brief remains. Omitted canonical work is already satisfied by Relay deterministic pre-application.\n")
	b.WriteString("- Do not repeat, revert, reconstruct, or invalidate Relay-completed work or protected changed paths.\n")
	b.WriteString("- A selector absent because Relay already completed the corresponding work is not a blocker; exact-directive blocker rules apply only to directives present in this brief.\n")
	b.WriteString("- Apply the remaining declared implementation exactly, using only necessary source-compatible adaptation that preserves behavior, architecture, scope, and material code shape.\n")
	b.WriteString("- Preserve unrelated work and avoid unrelated cleanup or refactoring.\n")
	b.WriteString("- Run every specified validation command and applicable Executor check against the combined deterministic and residual result, then report exact results, blockers, or incomplete work.\n")
}

func renderFileImplementation(b *strings.Builder, file ExecutionFile) {
	if file.Operation == "rename" {
		fmt.Fprintf(b, "###### `%s` `%s` -> `%s`\n\n", file.Operation, file.Path, file.DestinationPath)
	} else {
		fmt.Fprintf(b, "###### `%s` `%s`\n\n", file.Operation, file.Path)
	}
	implementation := file.Implementation
	switch file.Operation {
	case "modify":
		for _, change := range implementation.Changes {
			switch change.Kind {
			case "replace":
				fmt.Fprintf(b, "- replace, expected occurrences: %d\n\n", change.ExpectedOccurrences)
				b.WriteString("  Old text:\n\n")
				writeFence(b, "  ", change.OldText)
				b.WriteString("\n  New text:\n\n")
				writeFence(b, "  ", change.NewText)
				b.WriteString("\n")
			case "insert_before", "insert_after":
				fmt.Fprintf(b, "- %s, expected occurrences: %d\n\n", change.Kind, change.ExpectedOccurrences)
				b.WriteString("  Anchor:\n\n")
				writeFence(b, "  ", change.Anchor)
				b.WriteString("\n  Content:\n\n")
				writeFence(b, "  ", change.Content)
				b.WriteString("\n")
			case "remove":
				fmt.Fprintf(b, "- remove, expected occurrences: %d\n\n", change.ExpectedOccurrences)
				b.WriteString("  Old text:\n\n")
				writeFence(b, "  ", change.OldText)
				b.WriteString("\n")
			case "replace_file":
				b.WriteString("- replace_file\n\n")
				b.WriteString("  Content:\n\n")
				writeFence(b, "  ", change.Content)
				b.WriteString("\n")
			}
		}
	case "create":
		b.WriteString("Content:\n\n")
		writeFence(b, "", implementation.Content)
		b.WriteString("\n")
	case "delete":
		fmt.Fprintf(b, "Delete file: %t\n\n", implementation.DeleteFile)
	case "rename":
		if implementation.PreserveContent {
			b.WriteString("Preserve content: true\n\n")
		} else {
			b.WriteString("Content:\n\n")
			writeFence(b, "", implementation.Content)
			b.WriteString("\n")
		}
	}
}

func renderPlan(raw []byte) (string, error) {
	var plan planModel
	if err := json.Unmarshal(raw, &plan); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Plan of Passes\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")
	writeTextSection(&b, "## Goal", plan.Goal)
	writeTextSection(&b, "## Context", plan.Context)
	b.WriteString("## Scope\n\n")
	writeBulletSection(&b, "### In Scope", plan.Scope.InScope)
	writeBulletSection(&b, "### Out of Scope", plan.Scope.OutOfScope)

	b.WriteString("## Repository Targets\n\n")
	for _, target := range plan.RepoTargets {
		fmt.Fprintf(&b, "### `%s`\n\n", target.RepoTarget)
		fmt.Fprintf(&b, "- Branch: `%s`\n", target.Branch)
		fmt.Fprintf(&b, "- Planning base commit: `%s`\n\n", target.PlanningBaseCommit)
	}

	b.WriteString("## Passes\n\n")
	for _, pass := range plan.Passes {
		fmt.Fprintf(&b, "### Pass %d: %s\n\n", pass.Number, trimHuman(pass.Name))
		b.WriteString("#### Repository Target\n\n")
		fmt.Fprintf(&b, "`%s`\n\n", pass.RepoTarget)
		writeTextSection(&b, "#### Goal", pass.Goal)
		writeTextSection(&b, "#### Context", pass.Context)
		b.WriteString("#### Scope\n\n")
		writeBulletSection(&b, "##### In Scope", pass.Scope.InScope)
		writeBulletSection(&b, "##### Out of Scope", pass.Scope.OutOfScope)
		b.WriteString("#### Dependencies\n\n")
		if len(pass.DependsOn) == 0 {
			b.WriteString("None\n\n")
		} else {
			for _, dependency := range pass.DependsOn {
				fmt.Fprintf(&b, "- Pass %d\n", dependency)
			}
			b.WriteString("\n")
		}
		writeBulletSection(&b, "#### Outcomes", pass.Outcomes)
		b.WriteString("#### Source Targets\n\n")
		for _, target := range pass.SourceTargets {
			fmt.Fprintf(&b, "- `%s` - %s\n", target.Path, trimHuman(target.Purpose))
		}
		b.WriteString("\n")
		writeBulletSection(&b, "#### Validation Intent", pass.ValidationIntent)
		writeBulletSection(&b, "#### Completion Criteria", pass.Completion)
	}
	writeBulletSection(&b, "## Plan Completion Criteria", plan.Completion)
	return oneFinalNewline(b.String()), nil
}

func writeTextSection(b *strings.Builder, heading, text string) {
	b.WriteString(heading)
	b.WriteString("\n\n")
	b.WriteString(trimHuman(text))
	b.WriteString("\n\n")
}

func writeBulletSection(b *strings.Builder, heading string, values []string) {
	b.WriteString(heading)
	b.WriteString("\n\n")
	for _, value := range values {
		b.WriteString("- ")
		b.WriteString(trimHuman(value))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeFence(b *strings.Builder, indent, content string) {
	content = normalizeLF(content)
	fence := strings.Repeat("`", maxBacktickRun(content)+1)
	if len(fence) < 3 {
		fence = "```"
	}
	b.WriteString(indent)
	b.WriteString(fence)
	b.WriteString("text\n")
	writeIndentedRaw(b, indent, content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(indent)
	b.WriteString(fence)
	b.WriteString("\n")
}

func writeIndentedRaw(b *strings.Builder, indent, content string) {
	for len(content) != 0 {
		index := strings.IndexByte(content, '\n')
		b.WriteString(indent)
		if index == -1 {
			b.WriteString(content)
			return
		}
		b.WriteString(content[:index+1])
		content = content[index+1:]
	}
}

func maxBacktickRun(value string) int {
	max, current := 0, 0
	for _, r := range value {
		if r == '`' {
			current++
			if current > max {
				max = current
			}
		} else {
			current = 0
		}
	}
	return max
}

func trimHuman(value string) string {
	return strings.TrimSpace(normalizeLF(value))
}

func normalizeLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func oneFinalNewline(value string) string {
	return strings.TrimRight(normalizeLF(value), "\n") + "\n"
}
