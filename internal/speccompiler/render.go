package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

const derivedNotice = "> Derived from canonical JSON. Do not edit this Markdown independently."

func renderExecutionSpec(raw []byte) (string, error) {
	var spec executionSpecModel
	if err := json.Unmarshal(raw, &spec); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Executor Brief\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")

	b.WriteString("## Target\n\n")
	fmt.Fprintf(&b, "- Repository: `%s`\n", spec.RepoTarget)
	fmt.Fprintf(&b, "- Branch: `%s`\n", spec.Branch)
	fmt.Fprintf(&b, "- Base commit: `%s`\n\n", spec.BaseCommit)

	writeTextSection(&b, "## Goal", spec.Goal)
	writeTextSection(&b, "## Context", spec.Context)
	b.WriteString("## Scope\n\n")
	writeBulletSection(&b, "### In Scope", spec.Scope.InScope)
	writeBulletSection(&b, "### Out of Scope", spec.Scope.OutOfScope)

	b.WriteString("## Implementation\n\n")
	for _, step := range spec.Steps {
		fmt.Fprintf(&b, "### Step %d: %s\n\n", step.Number, trimHuman(step.Goal))
		for _, substep := range step.Substeps {
			fmt.Fprintf(&b, "#### Substep %d.%d\n\n", step.Number, substep.Number)
			b.WriteString("##### Files\n\n")
			for _, file := range substep.Files {
				if file.Operation == "rename" {
					fmt.Fprintf(&b, "- `%s` `%s` -> `%s` - %s\n", file.Operation, file.Path, file.DestinationPath, trimHuman(file.Purpose))
				} else {
					fmt.Fprintf(&b, "- `%s` `%s` - %s\n", file.Operation, file.Path, trimHuman(file.Purpose))
				}
			}
			b.WriteString("\n##### Instruction\n\n")
			b.WriteString(trimHuman(substep.Instruction))
			b.WriteString("\n\n##### Implementation\n\n")
			for _, file := range substep.Files {
				if err := renderFileImplementation(&b, file); err != nil {
					return "", err
				}
			}
			writeBulletSection(&b, "##### Completion Criteria", substep.Completion)
		}
		writeBulletSection(&b, "#### Step Completion Criteria", step.Completion)
	}

	b.WriteString("## Validation\n\n")
	b.WriteString("### Commands\n\n")
	for i, command := range spec.Validation.Commands {
		fmt.Fprintf(&b, "%d. Command:\n\n", i+1)
		writeFence(&b, "   ", command.Command)
		b.WriteString("\n")
		if command.WorkingDirectory != "" {
			fmt.Fprintf(&b, "   - Working directory: `%s`\n", command.WorkingDirectory)
		}
		fmt.Fprintf(&b, "   - Expected: %s\n\n", trimHuman(command.Expected))
	}
	if len(spec.Validation.ExecutorChecks) != 0 {
		writeBulletSection(&b, "### Executor Checks", spec.Validation.ExecutorChecks)
	}
	writeBulletSection(&b, "## Completion Criteria", spec.Completion)

	b.WriteString("## Execution Instructions\n\n")
	b.WriteString("- Treat this Executor Brief as the implementation authority for the assigned execution.\n")
	b.WriteString("- Complete the stated goal, implementation work, completion criteria, and validation.\n")
	b.WriteString("- Make any repository changes necessary to complete the specification.\n")
	b.WriteString("- Keep changes relevant to the specification and avoid unrelated cleanup or refactoring.\n")
	b.WriteString("- Preserve unrelated local changes. Do not reset, discard, or overwrite them.\n")
	b.WriteString("- Run the specified validation and report the results.\n")
	b.WriteString("- Report validation results, any incomplete work, and any technical blocker that prevents completion.\n")
	return oneFinalNewline(b.String()), nil
}

func renderFileImplementation(b *strings.Builder, file fileModel) error {
	if file.Operation == "rename" {
		fmt.Fprintf(b, "###### `%s` `%s` -> `%s`\n\n", file.Operation, file.Path, file.DestinationPath)
	} else {
		fmt.Fprintf(b, "###### `%s` `%s`\n\n", file.Operation, file.Path)
	}
	switch file.Operation {
	case "modify":
		var implementation modifyImplementationModel
		if err := json.Unmarshal(file.Implementation, &implementation); err != nil {
			return err
		}
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
		var implementation createImplementationModel
		if err := json.Unmarshal(file.Implementation, &implementation); err != nil {
			return err
		}
		b.WriteString("Content:\n\n")
		writeFence(b, "", implementation.Content)
		b.WriteString("\n")
	case "delete":
		var implementation deleteImplementationModel
		if err := json.Unmarshal(file.Implementation, &implementation); err != nil {
			return err
		}
		fmt.Fprintf(b, "Delete file: %t\n\n", implementation.DeleteFile)
	case "rename":
		var implementation renameImplementationModel
		if err := json.Unmarshal(file.Implementation, &implementation); err != nil {
			return err
		}
		if implementation.PreserveContent {
			b.WriteString("Preserve content: true\n\n")
		} else {
			b.WriteString("Content:\n\n")
			writeFence(b, "", implementation.Content)
			b.WriteString("\n")
		}
	}
	return nil
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
