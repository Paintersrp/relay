package speccompiler

import (
	"strings"
	"testing"
)

const validDeliveryPlan = `{"schema_version":"1.0","feature_slug":"checkout","goal":"Deliver the approved bounded planning outcome.","context":"Planning context for the planned delivery outcome.","scope":{"in_scope":["Plan the approved bounded delivery outcome."],"out_of_scope":["Do not plan execution, integration, lifecycle, or roadmap content."]},"units":[{"unit_id":"P1-T1","goal":"Deliver the planned bounded outcome of the unit.","depends_on":[]},{"unit_id":"P1-T2","goal":"Deliver the second planned bounded outcome.","depends_on":["P1-T1"]}]}`

func TestDeliveryPlanCompilesAndRendersDeterministically(t *testing.T) {
	result, document := CompileDeliveryPlan("checkout.delivery-plan.json", []byte(validDeliveryPlan))
	if len(result.Errors) != 0 || document == nil {
		t.Fatalf("errors=%v document=%+v", result.Errors, document)
	}
	if result.OutputFilename == nil || *result.OutputFilename != "checkout.delivery-plan.md" || result.Markdown == nil {
		t.Fatalf("output=%+v", result)
	}
	if document.FeatureSlug != "checkout" || len(document.Units) != 2 || document.Units[1].UnitID != "P1-T2" || len(document.Units[1].DependsOn) != 1 || document.Units[1].DependsOn[0] != "P1-T1" {
		t.Fatalf("document=%+v", document)
	}
	first, err := renderDeliveryPlan(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderDeliveryPlan(document)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasSuffix(first, "\n") || strings.HasSuffix(first, "\n\n") {
		t.Fatal("delivery plan rendering is not byte deterministic with one final newline")
	}
	headings := []string{
		"# Delivery Plan", "## Identity", "## Goal", "## Context", "## Scope",
		"### In Scope", "### Out of Scope", "## Planned Units", "## Planned Dependency Topology",
	}
	last := -1
	for _, heading := range headings {
		index := strings.Index(first, heading)
		if index < 0 {
			t.Fatalf("rendered plan missing %q:\n%s", heading, first)
		}
		if index < last {
			t.Fatalf("rendered plan heading %q is out of order:\n%s", heading, first)
		}
		last = index
	}
	for _, expected := range []string{
		"Feature: checkout",
		"1. P1-T1: Deliver the planned bounded outcome of the unit.",
		"2. P1-T2: Deliver the second planned bounded outcome.",
		"- P1-T1 depends on: None",
		"- P1-T2 depends on: P1-T1",
	} {
		if !strings.Contains(first, expected) {
			t.Fatalf("rendered plan missing %q:\n%s", expected, first)
		}
	}
	if strings.Contains(first, "schema_version") || strings.Contains(first, "feature_slug") || strings.Contains(first, "P1-T1 depends on: None, ") {
		t.Fatalf("rendered plan leaks producer metadata or topology:\n%s", first)
	}
}

func TestDeliveryPlanFilenameDispatchAndSchemaValidation(t *testing.T) {
	filename, diagnostics := ParseFilename("checkout.delivery-plan.json")
	if len(diagnostics) != 0 || filename.Kind != ArtifactDeliveryPlan || filename.FeatureSlug != "checkout" {
		t.Fatalf("filename=%+v diagnostics=%v", filename, diagnostics)
	}
	filename, diagnostics = ParseFilename("checkout.delivery-plan.md")
	if len(diagnostics) != 0 || filename.Kind != ArtifactDeliveryPlan {
		t.Fatalf("markdown filename=%+v diagnostics=%v", filename, diagnostics)
	}
	result := Compile("checkout.delivery-plan.md", []byte(validDeliveryPlan))
	if len(result.Errors) != 1 || result.Errors[0].Code != "unsupported_artifact_kind" {
		t.Fatalf("markdown dispatch errors=%v", result.Errors)
	}
	if _, diagnostics := ParseFilename("checkout.delivery-plan.json.extra"); len(diagnostics) == 0 {
		t.Fatal("noncanonical delivery plan filename was recognized")
	}
	provenance := SourceProvenance()
	found := false
	for _, schema := range provenance.Schemas {
		if schema.ArtifactKind == ArtifactDeliveryPlan && schema.Version == "1.0" && schema.Path == "schemas/delivery-plan.schema.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("delivery plan schema provenance missing: %+v", provenance.Schemas)
	}
}

func TestDeliveryPlanRejectsStructuralAndTopologyDefects(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		code string
	}{
		{"duplicate unit id", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":[]},{"unit_id":"P1-T1","goal":"b","depends_on":[]}]}`, "duplicate_planned_unit_id"},
		{"self dependency", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":["P1-T1"]}]}`, "self_dependency"},
		{"unknown dependency", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":["P1-T9"]}]}`, "unknown_dependency"},
		{"duplicate dependency", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":[]},{"unit_id":"P1-T2","goal":"b","depends_on":["P1-T1","P1-T1"]}]}`, "duplicate_dependency"},
		{"direct cycle", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":["P1-T2"]},{"unit_id":"P1-T2","goal":"b","depends_on":["P1-T1"]}]}`, "circular_dependency"},
		{"indirect cycle", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":["P1-T2"]},{"unit_id":"P1-T2","goal":"b","depends_on":["P1-T3"]},{"unit_id":"P1-T3","goal":"c","depends_on":["P1-T1"]}]}`, "circular_dependency"},
		{"noncanonical top-level order", `{"feature_slug":"checkout","schema_version":"1.0","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":[]}]}`, "noncanonical_property_order"},
		{"noncanonical unit order", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","depends_on":[],"goal":"a"}]}`, "noncanonical_property_order"},
		{"empty units", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[]}`, "empty_required_value"},
		{"empty depends_on scope", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":[],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":[]}]}`, "empty_required_value"},
		{"filename slug mismatch", `{"schema_version":"1.0","feature_slug":"other","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"P1-T1","goal":"a","depends_on":[]}]}`, "filename_slug_mismatch"},
		{"invalid unit id", `{"schema_version":"1.0","feature_slug":"checkout","goal":"g","context":"c","scope":{"in_scope":["a"],"out_of_scope":["b"]},"units":[{"unit_id":"p1-t1","goal":"a","depends_on":[]}]}`, "invalid_unit_id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, _ := CompileDeliveryPlan("checkout.delivery-plan.json", []byte(test.raw))
			for _, diagnostic := range result.Errors {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("errors=%v; missing %s", result.Errors, test.code)
		})
	}
}
