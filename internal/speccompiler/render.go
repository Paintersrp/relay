package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

const derivedNotice = "> Derived from canonical JSON. Do not edit this Markdown independently."

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

func renderDeliveryTicket(ticket *DeliveryTicketDocument) (string, error) {
	if ticket == nil {
		return "", fmt.Errorf("delivery ticket document is required")
	}
	var b strings.Builder
	b.WriteString("# Delivery Ticket\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")

	b.WriteString("## Identity\n\n")
	fmt.Fprintf(&b, "- Ticket: `%s`\n", ticket.TicketID)
	fmt.Fprintf(&b, "- Revision: `%d`\n\n", ticket.Revision)

	b.WriteString("## Target\n\n")
	fmt.Fprintf(&b, "- Repository: `%s`\n", ticket.RepoTarget)
	fmt.Fprintf(&b, "- Branch: `%s`\n", ticket.Branch)
	fmt.Fprintf(&b, "- Base commit: `%s`\n\n", ticket.BaseCommit)
	writeTextSection(&b, "## Goal", ticket.Goal)
	writeTextSection(&b, "## Context", ticket.Context)

	b.WriteString("## Scope\n\n")
	writeBulletSection(&b, "### In Scope", ticket.Scope.InScope)
	writeBulletSection(&b, "### Out of Scope", ticket.Scope.OutOfScope)

	writeTicketDependencies(&b, ticket.DependsOn)
	writeSharedDesignConstraints(&b, ticket.SharedDesignConstraints)
	writeBulletOrNone(&b, "## Required Invariants", ticket.RequiredInvariants)
	writeBulletOrNone(&b, "## Forbidden Behaviors", ticket.ForbiddenBehaviors)
	writeImplementationObligations(&b, ticket.ImplementationObligations)
	writeBulletOrNone(&b, "## Proof Obligations", ticket.ProofObligations)
	writeValidationCommands(&b, ticket.ValidationCommands)

	b.WriteString("## Transition Applicability\n\n")
	fmt.Fprintf(&b, "%s\n\n", ticket.TransitionApplicability)
	writeBulletOrNone(&b, "## Explicit Deferrals", ticket.ExplicitDeferrals)

	b.WriteString("## Replacement\n\n")
	if ticket.ReplacesRevision == nil {
		b.WriteString("None\n\n")
	} else {
		fmt.Fprintf(&b, "Replaces revision %d.\n\n", *ticket.ReplacesRevision)
	}
	b.WriteString("## Cancellation\n\n")
	if ticket.Cancellation == nil {
		b.WriteString("None\n\n")
	} else {
		b.WriteString(trimHuman(ticket.Cancellation.Reason))
		b.WriteString("\n\n")
	}
	writeBulletSection(&b, "## Completion Criteria", ticket.Completion)
	return oneFinalNewline(b.String()), nil
}

func writeTicketDependencies(b *strings.Builder, dependencies []DeliveryTicketDependency) {
	b.WriteString("## Dependencies\n\n")
	if len(dependencies) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for _, dependency := range dependencies {
		fmt.Fprintf(b, "- `%s` revision %d\n", dependency.TicketID, dependency.Revision)
	}
	b.WriteString("\n")
}

func writeSharedDesignConstraints(b *strings.Builder, constraints []DeliveryTicketCrossMemberConstraint) {
	b.WriteString("## Shared Design Constraints\n\n")
	if len(constraints) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for _, constraint := range constraints {
		fmt.Fprintf(b, "- %s requires:\n", constraint.Kind)
		for _, required := range constraint.Requires {
			fmt.Fprintf(b, "  - `%s` revision %d\n", required.TicketID, required.Revision)
		}
	}
	b.WriteString("\n")
}

func writeBulletOrNone(b *strings.Builder, heading string, values []string) {
	b.WriteString(heading)
	b.WriteString("\n\n")
	if len(values) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for _, value := range values {
		b.WriteString("- ")
		b.WriteString(trimHuman(value))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeImplementationObligations(b *strings.Builder, obligations []DeliveryTicketObligation) {
	b.WriteString("## Implementation Obligations\n\n")
	if len(obligations) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for index, obligation := range obligations {
		sourceArea := "None"
		if obligation.SourceArea != nil {
			sourceArea = *obligation.SourceArea
		}
		fmt.Fprintf(b, "%d. Source area: %s\n", index+1, sourceArea)
		fmt.Fprintf(b, "   Obligation: %s\n", obligation.Obligation)
		if len(obligation.Prerequisites) == 0 {
			b.WriteString("   Prerequisites: None\n")
		} else {
			b.WriteString("   Prerequisites:\n")
			for _, prerequisite := range obligation.Prerequisites {
				fmt.Fprintf(b, "   - %s\n", prerequisite)
			}
		}
	}
	b.WriteString("\n")
}

func writeValidationCommands(b *strings.Builder, commands []DeliveryTicketValidationCommand) {
	b.WriteString("## Validation Commands\n\n")
	if len(commands) == 0 {
		b.WriteString("None\n\n")
		return
	}
	for index, command := range commands {
		directory := command.WorkingDirectory
		if directory == "" {
			directory = "."
		}
		fmt.Fprintf(b, "%d. Working directory: %s\n", index+1, directory)
		fmt.Fprintf(b, "   Command: %s\n", command.Command)
		fmt.Fprintf(b, "   Expected: %s\n", command.Expected)
	}
	b.WriteString("\n")
}

func renderTransitionPlan(plan *TransitionPlanDocument) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("transition plan document is required")
	}
	var b strings.Builder
	b.WriteString("# Transition Plan\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")

	b.WriteString("## Ticket Identity\n\n")
	fmt.Fprintf(&b, "- Ticket: `%s`\n", plan.TicketID)
	fmt.Fprintf(&b, "- Revision: `%d`\n\n", plan.TicketRevision)

	writeBulletSection(&b, "## Cutover Prerequisites", plan.CutoverPrerequisites)
	writeBulletSection(&b, "## Activation Obligations", plan.ActivationObligations)

	b.WriteString("## Rollback\n\n")
	eligibility := plan.RollbackEligibility
	if eligibility == "not_eligible" {
		eligibility = "not eligible"
	}
	fmt.Fprintf(&b, "- Eligibility: %s\n\n", eligibility)
	if len(plan.RollbackObligations) == 0 {
		b.WriteString("None\n\n")
	} else {
		for _, obligation := range plan.RollbackObligations {
			b.WriteString("- ")
			b.WriteString(trimHuman(obligation))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	writeBulletSection(&b, "## Completion Criteria", plan.CompletionCriteria)
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
