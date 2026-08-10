package speccompiler

import (
	"strings"
	"testing"
)

const v2ActiveDeliveryTicket = `{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P1-T1","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Deliver the outcome.","context":"Carried context.","scope":{"in_scope":["Deliver."],"out_of_scope":["Other."]},"depends_on":[],"required_invariants":["Invariant."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":null,"obligation":"Implement it.","prerequisites":[]}],"proof_obligations":["Prove it."],"validation_commands":[{"working_directory":"","command":"go test ./internal/example","expected":"Tests pass."}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["Complete."]}`

const v2CancellationDeliveryTicket = `{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P1-T1","revision":2,"replaces_revision":1,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Cancel the outcome.","context":"Cancellation context.","scope":{"in_scope":["Record the cancellation."],"out_of_scope":["No execution."]},"depends_on":[],"required_invariants":[],"forbidden_behaviors":[],"implementation_obligations":[],"proof_obligations":[],"validation_commands":[],"transition_applicability":"not_required","explicit_deferrals":[],"cancellation":{"reason":"Superseded."},"completion_criteria":["Cancellation is recorded."]}`

func TestDeliveryTicketV2CompilesAndRendersDeterministically(t *testing.T) {
	result, document := CompileDeliveryTicket("checkout.ticket-P1-T1.r1.delivery-ticket.json", []byte(v2ActiveDeliveryTicket))
	if len(result.Errors) != 0 || document == nil {
		t.Fatalf("errors=%v document=%+v", result.Errors, document)
	}
	if result.OutputFilename == nil || *result.OutputFilename != "checkout.ticket-P1-T1.r1.delivery-ticket.md" || result.Markdown == nil {
		t.Fatalf("output=%+v", result)
	}
	if document.FeatureSlug != "checkout" || document.TicketID != "P1-T1" || document.Revision != 1 ||
		len(document.RequiredInvariants) != 1 || len(document.ProofObligations) != 1 ||
		len(document.ValidationCommands) != 1 || document.ValidationCommands[0].WorkingDirectory != "" ||
		document.ImplementationObligations[0].SourceArea != nil || len(document.ImplementationObligations[0].Prerequisites) != 0 {
		t.Fatalf("document=%+v", document)
	}
	first, err := renderDeliveryTicket(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderDeliveryTicket(document)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasSuffix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Fatal("v2 rendering is not byte deterministic with one final newline")
	}
	headings := []string{
		"# Delivery Ticket", "## Identity", "## Target", "## Goal", "## Context", "## Scope",
		"### In Scope", "### Out of Scope", "## Dependencies", "## Required Invariants",
		"## Forbidden Behaviors", "## Implementation Obligations", "## Proof Obligations",
		"## Validation Commands", "## Transition Applicability", "## Explicit Deferrals",
		"## Replacement", "## Cancellation", "## Completion Criteria",
	}
	last := -1
	for _, heading := range headings {
		index := strings.Index(first, heading)
		if index < 0 {
			t.Fatalf("rendered ticket missing %q:\n%s", heading, first)
		}
		if index < last {
			t.Fatalf("rendered ticket heading %q is out of order:\n%s", heading, first)
		}
		last = index
	}
	for _, expected := range []string{
		"1. Source area: None", "   Obligation: Implement it.", "   Prerequisites: None",
		"1. Working directory: .", "   Command: go test ./internal/example", "   Expected: Tests pass.",
		"- Invariant.", "- Prove it.", "None", "not_required",
	} {
		if !strings.Contains(first, expected) {
			t.Fatalf("rendered ticket missing %q:\n%s", expected, first)
		}
	}
	if strings.Contains(first, "schema_version") || strings.Contains(first, "feature_slug") {
		t.Fatalf("rendered ticket leaks producer metadata:\n%s", first)
	}
}

func TestDeliveryTicketV2CancellationCompilesAndRenders(t *testing.T) {
	result, document := CompileDeliveryTicket("checkout.ticket-P1-T1.r2.delivery-ticket.json", []byte(v2CancellationDeliveryTicket))
	if len(result.Errors) != 0 || document == nil {
		t.Fatalf("errors=%v document=%+v", result.Errors, document)
	}
	if document.Cancellation == nil || document.Cancellation.Reason != "Superseded." {
		t.Fatalf("document=%+v", document)
	}
	markdown := *result.Markdown
	for _, expected := range []string{"Replaces revision 1.", "Superseded.", "## Implementation Obligations\n\nNone", "## Proof Obligations\n\nNone", "## Validation Commands\n\nNone", "## Explicit Deferrals\n\nNone"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("cancellation ticket missing %q:\n%s", expected, markdown)
		}
	}
	if strings.Contains(markdown, "Obligation:") || strings.Contains(markdown, "Command:") {
		t.Fatalf("cancellation ticket renders execution fields:\n%s", markdown)
	}
}

func TestDeliveryTicketV2ProjectionIsDeterministicAndComplete(t *testing.T) {
	result, document := CompileDeliveryTicket("checkout.ticket-P1-T1.r1.delivery-ticket.json", []byte(v2ActiveDeliveryTicket))
	if len(result.Errors) != 0 || document == nil {
		t.Fatalf("errors=%v", result.Errors)
	}
	first, diagnostics := ProjectDeliveryTicket(document)
	if len(diagnostics) != 0 {
		t.Fatalf("active projection diagnostics=%v", diagnostics)
	}
	second, diagnostics := ProjectDeliveryTicket(document)
	if len(diagnostics) != 0 || !projectionEqual(first, second) {
		t.Fatalf("projection is not deterministic: %+v vs %+v, diagnostics=%v", first, second, diagnostics)
	}
	if first.TicketID != "P1-T1" || first.Revision != 1 || len(first.ImplementationObligations) != 1 ||
		len(first.ValidationCommands) != 1 || len(first.ProofObligations) != 1 || first.Cancellation != nil {
		t.Fatalf("projection=%+v", first)
	}
	if _, diagnostics := ProjectDeliveryTicket(nil); len(diagnostics) == 0 || diagnostics[0].Code != "projection_invariant" {
		t.Fatalf("nil projection diagnostics=%v", diagnostics)
	}
	cancelled, diagnostics := ProjectDeliveryTicket(&DeliveryTicketDocument{
		TicketID: "P1-T1", Revision: 2, TransitionApplicability: "not_required",
		ImplementationObligations: []DeliveryTicketObligation{},
		ProofObligations:          []string{},
		ValidationCommands:        []DeliveryTicketValidationCommand{},
		Cancellation:              &DeliveryTicketCancellation{Reason: "Superseded."},
	})
	if len(diagnostics) != 0 {
		t.Fatalf("cancellation projection diagnostics=%v", diagnostics)
	}
	if cancelled.Cancellation == nil {
		t.Fatal("cancellation projection lost cancellation")
	}
	incomplete := &DeliveryTicketDocument{
		TicketID: "P1-T1", Revision: 1, TransitionApplicability: "not_required",
		ImplementationObligations: []DeliveryTicketObligation{},
		ProofObligations:          []string{"Prove it."},
		ValidationCommands:        []DeliveryTicketValidationCommand{{Command: "go test ./...", Expected: "pass"}},
	}
	if _, diagnostics := ProjectDeliveryTicket(incomplete); len(diagnostics) == 0 || diagnostics[0].Code != "projection_invariant" {
		t.Fatalf("incomplete projection diagnostics=%v", diagnostics)
	}
}

func projectionEqual(a, b DeliveryTicketProjection) bool {
	if a.TicketID != b.TicketID || a.Revision != b.Revision || len(a.ImplementationObligations) != len(b.ImplementationObligations) || len(a.ValidationCommands) != len(b.ValidationCommands) {
		return false
	}
	for index := range a.ValidationCommands {
		if a.ValidationCommands[index] != b.ValidationCommands[index] {
			return false
		}
	}
	return true
}

func TestDeliveryTicketV2Rejections(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		code string
	}{
		{"retired validation_intent property", strings.Replace(v2ActiveDeliveryTicket, `"depends_on":[],`, `"depends_on":[],"validation_intent":["x"],`, 1), "unknown_property"},
		{"noncanonical top-level order", strings.Replace(v2ActiveDeliveryTicket, `"goal":"Deliver the outcome.","context":"Carried context."`, `"context":"Carried context.","goal":"Deliver the outcome."`, 1), "noncanonical_property_order"},
		{"noncanonical obligation order", strings.Replace(v2ActiveDeliveryTicket, `{"source_area":null,"obligation":"Implement it.","prerequisites":[]}`, `{"obligation":"Implement it.","source_area":null,"prerequisites":[]}`, 1), "noncanonical_property_order"},
		{"noncanonical validation command order", strings.Replace(v2ActiveDeliveryTicket, `{"working_directory":"","command":"go test ./internal/example","expected":"Tests pass."}`, `{"command":"go test ./internal/example","working_directory":"","expected":"Tests pass."}`, 1), "noncanonical_property_order"},
		{"unsafe source_area", strings.Replace(v2ActiveDeliveryTicket, `"source_area":null`, `"source_area":"../escape"`, 1), "unsafe_repository_path"},
		{"unsafe working_directory", strings.Replace(v2ActiveDeliveryTicket, `"working_directory":""`, `"working_directory":"C:\\Windows"`, 1), "unsafe_working_directory"},
		{"active empty implementation obligations", strings.Replace(v2ActiveDeliveryTicket, `"implementation_obligations":[{"source_area":null,"obligation":"Implement it.","prerequisites":[]}]`, `"implementation_obligations":[]`, 1), "empty_required_value"},
		{"active empty proof obligations", strings.Replace(v2ActiveDeliveryTicket, `"proof_obligations":["Prove it."]`, `"proof_obligations":[]`, 1), "empty_required_value"},
		{"active empty validation commands", strings.Replace(v2ActiveDeliveryTicket, `"validation_commands":[{"working_directory":"","command":"go test ./internal/example","expected":"Tests pass."}]`, `"validation_commands":[]`, 1), "empty_required_value"},
		{"missing required property", strings.Replace(v2ActiveDeliveryTicket, `"explicit_deferrals":[],`, ``, 1), "missing_required_property"},
		{"cancellation with obligations", strings.Replace(v2CancellationDeliveryTicket, `"implementation_obligations":[]`, `"implementation_obligations":[{"source_area":"internal/example","obligation":"Do it.","prerequisites":[]}]`, 1), "cancellation_has_obligations"},
		{"cancellation with transition required", strings.Replace(v2CancellationDeliveryTicket, `"transition_applicability":"not_required"`, `"transition_applicability":"required"`, 1), "cancellation_requires_no_transition"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, _ := CompileDeliveryTicket("checkout.ticket-P1-T1.r1.delivery-ticket.json", []byte(test.raw))
			for _, diagnostic := range result.Errors {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("errors=%v; missing %s", result.Errors, test.code)
		})
	}
}

func TestTicketDesignBriefNoLongerRecognizedOrCompiled(t *testing.T) {
	filename := "checkout.ticket-P1-T1.r1.design-brief.md"
	if _, diagnostics := ParseFilename(filename); len(diagnostics) == 0 {
		t.Fatal("Ticket Design Brief filename was still recognized")
	}
	if result := Compile(filename, []byte("# Ticket Design Brief\n")); len(result.Errors) == 0 {
		t.Fatal("Ticket Design Brief compiled despite removed recognition")
	}
}
