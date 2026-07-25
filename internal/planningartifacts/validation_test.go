package planningartifacts

import (
	"reflect"
	"strings"
	"testing"

	"relay/internal/speccompiler"
	"relay/internal/testfixtures"
)

func TestValidateRequiresContractHeadings(t *testing.T) {
	tests := []struct {
		name string
		kind speccompiler.ArtifactKind
		body string
		want []string
	}{
		{name: "requirements", kind: speccompiler.ArtifactRequirements, body: "# Requirements\n\n## Goal\n\n## Scope\n\n## Requirements\n\n## Acceptance Criteria\n"},
		{name: "shared design", kind: speccompiler.ArtifactSharedDesign, body: "# Shared Design\n\n## Context\n\n## Design\n\n## Risks\n\n## Validation\n"},
		{name: "ticket design brief", kind: speccompiler.ArtifactTicketDesignBrief, body: testfixtures.TicketDesignBrief},
		{name: "missing headings are concrete and ignore fenced examples", kind: speccompiler.ArtifactRequirements, body: "# Requirements\n\n## Goal\n\n```markdown\n## Scope\n## Requirements\n## Acceptance Criteria\n```\n", want: []string{"## Scope", "## Requirements", "## Acceptance Criteria"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Validate(test.kind, []byte(test.body))
			if len(got) != len(test.want) {
				t.Fatalf("diagnostics = %+v, want %d", got, len(test.want))
			}
			for index, label := range test.want {
				if got[index].Code != "missing_required_heading" || got[index].Path != "/headings" || got[index].Message != "Required heading \""+label+"\" is missing." {
					t.Fatalf("diagnostics[%d] = %+v", index, got[index])
				}
			}
		})
	}
}

func TestValidateTicketDesignBriefAcceptsCurrentContractAndExactValidationEntry(t *testing.T) {
	got := Validate(speccompiler.ArtifactTicketDesignBrief, []byte(testfixtures.TicketDesignBrief))
	if len(got) != 0 {
		t.Fatalf("diagnostics = %+v, want none", got)
	}
	if !strings.Contains(testfixtures.TicketDesignBrief, "Working directory: .") ||
		!strings.Contains(testfixtures.TicketDesignBrief, "Command: go test ./internal/planningartifacts/...") ||
		!strings.Contains(testfixtures.TicketDesignBrief, "Expected: all tests pass.") {
		t.Fatal("fixture does not contain a complete validation entry")
	}
}

func TestValidateTicketDesignBriefAcceptsConciseNotApplicableSections(t *testing.T) {
	if got := Validate(speccompiler.ArtifactTicketDesignBrief, []byte(testfixtures.TicketDesignBrief)); len(got) != 0 {
		t.Fatalf("diagnostics = %+v, want concise Not applicable fixture to be accepted", got)
	}
}

func TestValidateTicketDesignBriefAcceptsValidationFieldsSplitAcrossBullets(t *testing.T) {
	body := strings.Replace(testfixtures.TicketDesignBrief,
		"- Working directory: .\n  Command: go test ./internal/planningartifacts/...\n  Expected: all tests pass.",
		"- Working directory: .\n- Command: go test ./internal/planningartifacts/...\n- Expected: all tests pass.", 1)
	if got := Validate(speccompiler.ArtifactTicketDesignBrief, []byte(body)); len(got) != 0 {
		t.Fatalf("diagnostics = %+v, want split validation fields to be accepted", got)
	}
}

func TestValidateTicketDesignBriefRejectsInvalidStructure(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing required heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Source Contracts\n\nNot applicable.\n\n", "", 1)},
		{name: "duplicated required heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Source Contracts\n", "## Source Contracts\n\n## Source Contracts\n", 1)},
		{name: "reordered headings", body: strings.Replace(testfixtures.TicketDesignBrief, "## Selected Ticket\n\nNot applicable.\n\n## Package Authority and Scope\n\nNot applicable.", "## Package Authority and Scope\n\nNot applicable.\n\n## Selected Ticket\n\nNot applicable.", 1)},
		{name: "obsolete Ticket Identity heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Selected Ticket", "## Ticket Identity", 1)},
		{name: "obsolete Implementation Notes heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Implementation Guidance", "## Implementation Notes", 1)},
		{name: "obsolete Validation heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Validation Commands", "## Validation", 1)},
		{name: "obsolete Validation Plan heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Validation Commands", "## Validation Plan", 1)},
		{name: "extra level two heading", body: strings.Replace(testfixtures.TicketDesignBrief, "## Selected Ticket\n", "## Extra Heading\n\nNot applicable.\n\n## Selected Ticket\n", 1)},
		{name: "frontmatter", body: "---\ntitle: brief\n---\n" + testfixtures.TicketDesignBrief},
		{name: "empty Validation Commands", body: strings.Replace(testfixtures.TicketDesignBrief, "- Working directory: .\n  Command: go test ./internal/planningartifacts/...\n  Expected: all tests pass.\n", "", 1)},
		{name: "missing working directory", body: strings.Replace(testfixtures.TicketDesignBrief, "- Working directory: .", "- Working directory: ", 1)},
		{name: "missing command", body: strings.Replace(testfixtures.TicketDesignBrief, "Command: go test ./internal/planningartifacts/...", "Command: ", 1)},
		{name: "missing expected result", body: strings.Replace(testfixtures.TicketDesignBrief, "Expected: all tests pass.", "Expected: ", 1)},
		{name: "unresolved placeholder", body: strings.Replace(testfixtures.TicketDesignBrief, "go test ./internal/planningartifacts/...", "<command>", 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Validate(speccompiler.ArtifactTicketDesignBrief, []byte(test.body)); len(got) == 0 {
				t.Fatal("invalid Ticket Design Brief was accepted")
			}
		})
	}
}

func TestValidateReturnsConcreteEmptyDiagnostics(t *testing.T) {
	got := Validate(speccompiler.ArtifactRequirements, []byte("# Requirements\n## Goal\n## Scope\n## Requirements\n## Acceptance Criteria\n"))
	if got == nil || !reflect.DeepEqual(got, []speccompiler.Diagnostic{}) {
		t.Fatalf("diagnostics = %#v, want concrete empty slice", got)
	}
}
