package planningartifacts

import (
	"reflect"
	"testing"

	"relay/internal/speccompiler"
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

func TestValidateRejectsRemovedTicketDesignBriefKind(t *testing.T) {
	// The Ticket Design Brief artifact kind is retired: planning validation no
	// longer recognizes any brief content, even the smallest heading-shaped
	// body, and reports the unsupported kind instead of validating it.
	got := Validate(speccompiler.ArtifactKind("ticket_design_brief"), []byte("# Ticket Design Brief\n\n## Selected Ticket\n"))
	if len(got) != 1 || got[0].Code != "unsupported_artifact_kind" {
		t.Fatalf("diagnostics = %+v, want unsupported_artifact_kind", got)
	}
}

func TestValidateReturnsConcreteEmptyDiagnostics(t *testing.T) {
	got := Validate(speccompiler.ArtifactRequirements, []byte("# Requirements\n## Goal\n## Scope\n## Requirements\n## Acceptance Criteria\n"))
	if got == nil || !reflect.DeepEqual(got, []speccompiler.Diagnostic{}) {
		t.Fatalf("diagnostics = %#v, want concrete empty slice", got)
	}
}
