package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/artifactschema"
)

// DeliveryPlanDocument is the decoded canonical Delivery Plan v1.0 model. The
// Plan owns planned unit boundaries and the planned semantic dependency
// topology only; it is never execution, integration, lifecycle, or roadmap
// authority and never becomes a selected-package member.
type DeliveryPlanDocument struct {
	SchemaVersion json.RawMessage     `json:"schema_version"`
	FeatureSlug   string              `json:"feature_slug"`
	Goal          string              `json:"goal"`
	Context       string              `json:"context"`
	Scope         scopeModel          `json:"scope"`
	Units         []DeliveryPlanUnit  `json:"units"`
}

type DeliveryPlanUnit struct {
	UnitID    string   `json:"unit_id"`
	Goal      string   `json:"goal"`
	DependsOn []string `json:"depends_on"`
}

// CompileDeliveryPlan validates one canonical Delivery Plan JSON artifact and
// renders its deterministic Markdown form. The caller owns the exact filename
// basename and exact bytes; a failure never produces a partial result.
func CompileDeliveryPlan(filenameBasename string, rawJSON []byte) (Result, *DeliveryPlanDocument) {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil), nil
	}
	if filename.Kind != ArtifactDeliveryPlan || !strings.HasSuffix(filenameBasename, deliveryPlanJSONSuffix) {
		return failed([]Diagnostic{{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must identify a Delivery Plan JSON artifact."}}, nil), nil
	}
	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil), nil
	}
	return compileDeliveryPlanDocument(filename, root, rawJSON)
}

func compileDeliveryPlanDocument(filename FilenameInfo, root *jsonNode, rawJSON []byte) (Result, *DeliveryPlanDocument) {
	definition, _ := currentDefinition(filename.Kind)
	notices := schemaVersionNotice(root, definition)
	schemaValid, schemaErr := artifactschema.Validate(definition.SchemaKind, rawJSON)
	errors := validateDeliveryPlan(root, filename.FeatureSlug)
	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded current %s schema validation failed: %v", definition.Kind, schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: fmt.Sprintf("Artifact does not satisfy the embedded current %s JSON Schema.", definition.Kind)})
	}
	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices), nil
	}
	document, err := decodeDeliveryPlanDocument(rawJSON)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Delivery Plan: %v", err)}}, notices), nil
	}
	markdown, err := renderDeliveryPlan(document)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Render validated Delivery Plan: %v", err)}}, notices), nil
	}
	output := filename.OutputStem + ".delivery-plan.md"
	return Result{OutputFilename: &output, Markdown: &markdown, Errors: []Diagnostic{}, Notices: notices}, document
}

func decodeDeliveryPlanDocument(raw []byte) (*DeliveryPlanDocument, error) {
	var document DeliveryPlanDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

// validateDeliveryPlan enforces the Delivery Plan checks owned by
// contracts/compiler.md: canonical top-level and unit property order, a
// nonempty ordered units array, keyed planned-unit identity uniqueness,
// dependency-reference validity, and an acyclic planned semantic dependency
// graph. Cycle detection evaluates the complete Plan and reports every defect
// without repair, inferred edges, or partial interpretation.
func validateDeliveryPlan(root *jsonNode, filenameSlug string) []Diagnostic {
	v := &validator{}
	if !v.objectShape(root, "", []string{"schema_version", "feature_slug", "goal", "context", "scope", "units"}, []string{"feature_slug", "goal", "context", "scope", "units"}) {
		return v.diagnostics
	}
	if slug, ok := v.stringMember(root, "feature_slug", "/feature_slug", stringFeatureSlug); ok && slug != filenameSlug {
		v.add("filename_slug_mismatch", "/feature_slug", fmt.Sprintf("feature_slug %q does not match filename slug %q.", slug, filenameSlug))
	}
	v.stringMember(root, "goal", "/goal", stringSingleLine)
	v.stringMember(root, "context", "/context", stringMultiline)
	if member, ok := root.objectMember("scope"); ok {
		v.validateScope(member.value, "/scope")
	}

	units, ok := root.objectMember("units")
	if !ok {
		return v.diagnostics
	}
	if units.value.kind != nodeArray {
		v.add("invalid_value_type", "/units", "units must be an array.")
		return v.diagnostics
	}
	if len(units.value.array) == 0 {
		v.add("empty_required_value", "/units", "units must not be empty.")
		return v.diagnostics
	}
	declared := make(map[string]struct{}, len(units.value.array))
	unitIndexByName := make(map[string]int, len(units.value.array))
	dependencies := make([][]string, len(units.value.array))
	for index, unit := range units.value.array {
		path := joinPointer("/units", fmt.Sprint(index))
		if unit.kind != nodeObject {
			v.add("invalid_value_type", path, "Planned unit must be an object.")
			continue
		}
		if !v.objectShape(unit, path, []string{"unit_id", "goal", "depends_on"}, []string{"unit_id", "goal", "depends_on"}) {
			continue
		}
		unitID, idOK := v.stringMember(unit, "unit_id", path+"/unit_id", stringUnitID)
		if idOK {
			if _, duplicate := declared[unitID]; duplicate {
				v.add("duplicate_planned_unit_id", path+"/unit_id", fmt.Sprintf("Planned unit %q is declared by more than one unit.", unitID))
			} else {
				unitIndexByName[unitID] = index
			}
			declared[unitID] = struct{}{}
		}
		v.stringMember(unit, "goal", path+"/goal", stringSingleLine)
		dependencyMember, hasDependencies := unit.objectMember("depends_on")
		if !hasDependencies {
			continue
		}
		if dependencyMember.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/depends_on", "depends_on must be an array.")
			continue
		}
		seen := map[string]struct{}{}
		for dependencyIndex, dependency := range dependencyMember.value.array {
			dependencyPath := joinPointer(path+"/depends_on", fmt.Sprint(dependencyIndex))
			dependencyID, dependencyOK := v.stringNode(dependency, dependencyPath, stringUnitID)
			if !dependencyOK {
				continue
			}
			if _, duplicate := seen[dependencyID]; duplicate {
				v.add("duplicate_dependency", dependencyPath, fmt.Sprintf("Dependency %q appears more than once.", dependencyID))
			}
			seen[dependencyID] = struct{}{}
			if idOK && dependencyID == unitID {
				v.add("self_dependency", dependencyPath, "A planned unit cannot depend on itself.")
			}
			if _, exists := declared[dependencyID]; !exists {
				v.add("unknown_dependency", dependencyPath, fmt.Sprintf("Dependency %q does not name a planned unit of this Plan.", dependencyID))
			}
			dependencies[index] = append(dependencies[index], dependencyID)
		}
	}
	v.validatePlannedDependencyCycles(dependencies, unitIndexByName)
	return v.diagnostics
}

// validatePlannedDependencyCycles reports every cycle in the planned semantic
// dependency graph through the transitive closure of planned dependency
// references. A unit is reachable from itself when any chain of dependency
// references returns to its starting unit, including indirect multi-unit
// cycles.
func (v *validator) validatePlannedDependencyCycles(dependencies [][]string, unitIndexByName map[string]int) {
	state := make([]uint8, len(dependencies))
	inStack := make([]bool, len(dependencies))
	for start := range dependencies {
		if state[start] != 0 {
			continue
		}
		var visit func(int)
		visit = func(index int) {
			state[index] = 1
			inStack[index] = true
			for _, dependency := range dependencies[index] {
				target, exists := unitIndexByName[dependency]
				if !exists || state[target] == 2 {
					continue
				}
				if state[target] == 1 && inStack[target] {
					v.add("circular_dependency", joinPointer("/units", fmt.Sprint(index)), fmt.Sprintf("Planned dependency %q participates in a dependency cycle.", dependency))
					continue
				}
				if state[target] == 0 {
					visit(target)
				}
			}
			inStack[index] = false
			state[index] = 2
		}
		visit(start)
	}
}

func renderDeliveryPlan(plan *DeliveryPlanDocument) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("delivery plan document is required")
	}
	var b strings.Builder
	b.WriteString("# Delivery Plan\n\n")
	b.WriteString(derivedNotice)
	b.WriteString("\n\n")

	b.WriteString("## Identity\n\n")
	fmt.Fprintf(&b, "Feature: %s\n\n", plan.FeatureSlug)
	writeTextSection(&b, "## Goal", plan.Goal)
	writeTextSection(&b, "## Context", plan.Context)
	b.WriteString("## Scope\n\n")
	writeBulletSection(&b, "### In Scope", plan.Scope.InScope)
	writeBulletSection(&b, "### Out of Scope", plan.Scope.OutOfScope)

	b.WriteString("## Planned Units\n\n")
	for index, unit := range plan.Units {
		fmt.Fprintf(&b, "%d. %s: %s\n", index+1, unit.UnitID, trimHuman(unit.Goal))
	}
	b.WriteString("\n")

	b.WriteString("## Planned Dependency Topology\n\n")
	for _, unit := range plan.Units {
		if len(unit.DependsOn) == 0 {
			fmt.Fprintf(&b, "- %s depends on: None\n", unit.UnitID)
			continue
		}
		fmt.Fprintf(&b, "- %s depends on: %s\n", unit.UnitID, strings.Join(unit.DependsOn, ", "))
	}
	b.WriteString("\n")
	return oneFinalNewline(b.String()), nil
}
