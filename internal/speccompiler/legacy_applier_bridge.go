package speccompiler

import (
	"encoding/json"
	"fmt"
)

// ExecutionPayloadProjection is the temporary ABI consumed by the current
// applier. It is derived only from a compiler-valid canonical Execution Spec
// and is deleted when PASS-3 migrates the applier to ExecutionProjection.
type ExecutionPayloadProjection struct {
	DeterministicOperations []ProjectedDeterministicOperation
	OperationGroups         []ProjectedOperationGroup
	ValidationCommands      []ProjectedValidationCommand
}

type ProjectedDeterministicOperation struct {
	ID                  string
	Kind                string
	Mode                string
	Paths               []string
	Guards              json.RawMessage
	ExpectedOccurrences int
	DependsOn           []string
	Group               string
	OnFailure           string
	Payload             json.RawMessage
}

type ProjectedOperationGroup struct {
	ID     string
	Atomic bool
}

func ProjectExecutionPayload(raw []byte) (ExecutionPayloadProjection, []Diagnostic) {
	root, lexicalErrors := parseDocument(raw)
	if len(lexicalErrors) != 0 {
		return ExecutionPayloadProjection{}, normalizeDiagnostics(lexicalErrors)
	}
	if root != nil && root.kind == nodeObject {
		_, hasSchemaVersion := root.objectMember("schema_version")
		_, hasFeatureSlug := root.objectMember("feature_slug")
		_, hasSteps := root.objectMember("steps")
		_, hasAuthoredPayload := root.objectMember("execution_payload")
		if !hasSchemaVersion && !hasFeatureSlug && !hasSteps && !hasAuthoredPayload {
			return ExecutionPayloadProjection{DeterministicOperations: []ProjectedDeterministicOperation{}, OperationGroups: []ProjectedOperationGroup{}, ValidationCommands: []ProjectedValidationCommand{}}, nil
		}
	}
	filenameBasename := "bridge.execution-spec.json"
	if root != nil && root.kind == nodeObject {
		if member, ok := root.objectMember("feature_slug"); ok && member.value.kind == nodeString && validFeatureSlug(member.value.text) {
			filenameBasename = member.value.text + executionSpecSuffix
		}
	}
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return ExecutionPayloadProjection{}, normalizeDiagnostics(filenameErrors)
	}
	result, document := compileExecutionDocument(filename, root, raw)
	if len(result.Errors) != 0 || document == nil {
		return ExecutionPayloadProjection{}, result.Errors
	}
	projection, diagnostics := ProjectExecutionSpec(document)
	if len(diagnostics) != 0 {
		return ExecutionPayloadProjection{}, diagnostics
	}
	if document.SchemaVersion == "2.0" {
		return ExecutionPayloadProjection{DeterministicOperations: []ProjectedDeterministicOperation{}, OperationGroups: []ProjectedOperationGroup{}, ValidationCommands: projection.ValidationCommands}, nil
	}
	return flattenLegacyProjection(projection)
}

func flattenLegacyProjection(projection ExecutionProjection) (ExecutionPayloadProjection, []Diagnostic) {
	legacy := ExecutionPayloadProjection{
		DeterministicOperations: []ProjectedDeterministicOperation{},
		OperationGroups:         []ProjectedOperationGroup{},
		ValidationCommands:      projection.ValidationCommands,
	}
	firstOperationBySubstep := map[string]string{}
	lastOperationBySubstep := map[string]string{}
	operationIndexByID := map[string]int{}

	for _, work := range projection.FileWork {
		operations, err := legacyOperationsForFileWork(work)
		if err != nil {
			return ExecutionPayloadProjection{}, []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: err.Error()}}
		}
		for index := range operations {
			if index > 0 {
				operations[index].DependsOn = append(operations[index].DependsOn, operations[index-1].ID)
			}
			if _, exists := operationIndexByID[operations[index].ID]; exists {
				return ExecutionPayloadProjection{}, []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("Duplicate legacy operation reference %q.", operations[index].ID)}}
			}
			operationIndexByID[operations[index].ID] = len(legacy.DeterministicOperations)
			legacy.DeterministicOperations = append(legacy.DeterministicOperations, operations[index])
		}
		if len(operations) == 0 {
			continue
		}
		if _, exists := firstOperationBySubstep[work.SubstepRef]; !exists {
			firstOperationBySubstep[work.SubstepRef] = operations[0].ID
		}
		lastOperationBySubstep[work.SubstepRef] = operations[len(operations)-1].ID
	}

	for _, substep := range projection.Substeps {
		firstID := firstOperationBySubstep[substep.Ref]
		if firstID == "" {
			continue
		}
		operationIndex := operationIndexByID[firstID]
		for _, dependency := range substep.Dependencies {
			lastID := lastOperationBySubstep[dependency]
			if lastID == "" {
				return ExecutionPayloadProjection{}, []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("Dependency substep %q has no legacy operation.", dependency)}}
			}
			legacy.DeterministicOperations[operationIndex].DependsOn = appendUnique(legacy.DeterministicOperations[operationIndex].DependsOn, lastID)
		}
	}
	return legacy, nil
}

func legacyOperationsForFileWork(work ProjectedFileWork) ([]ProjectedDeterministicOperation, error) {
	operation := func(id, kind string, paths []string, expected int, payload any) (ProjectedDeterministicOperation, error) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return ProjectedDeterministicOperation{}, err
		}
		return ProjectedDeterministicOperation{
			ID:                  id,
			Kind:                kind,
			Mode:                "exact",
			Paths:               append([]string(nil), paths...),
			ExpectedOccurrences: expected,
			DependsOn:           []string{},
			Payload:             raw,
		}, nil
	}

	switch work.Operation {
	case "modify":
		operations := make([]ProjectedDeterministicOperation, 0, len(work.Directives))
		for _, directive := range work.Directives {
			var payload any
			switch directive.Kind {
			case "replace":
				payload = struct {
					OldText             string `json:"old_text"`
					NewText             string `json:"new_text"`
					ExpectedOccurrences int    `json:"expected_occurrences"`
				}{directive.OldText, directive.NewText, directive.ExpectedOccurrences}
			case "insert_before", "insert_after":
				payload = struct {
					Anchor              string `json:"anchor"`
					Content             string `json:"content"`
					ExpectedOccurrences int    `json:"expected_occurrences"`
				}{directive.Anchor, directive.Content, directive.ExpectedOccurrences}
			case "remove":
				payload = struct {
					OldText             string `json:"old_text"`
					ExpectedOccurrences int    `json:"expected_occurrences"`
				}{directive.OldText, directive.ExpectedOccurrences}
			case "replace_file":
				payload = struct {
					Content string `json:"content"`
				}{directive.Content}
			default:
				return nil, fmt.Errorf("unsupported projected directive %q", directive.Kind)
			}
			op, err := operation(directive.Ref, directive.Kind, []string{work.Path}, directive.ExpectedOccurrences, payload)
			if err != nil {
				return nil, err
			}
			operations = append(operations, op)
		}
		return operations, nil
	case "create":
		op, err := operation(work.Ref, "create", []string{work.Path}, 0, struct {
			Content string `json:"content"`
		}{work.Content})
		return []ProjectedDeterministicOperation{op}, err
	case "delete":
		op, err := operation(work.Ref, "delete", []string{work.Path}, 0, struct {
			DeleteFile bool `json:"delete_file"`
		}{work.DeleteFile})
		return []ProjectedDeterministicOperation{op}, err
	case "rename":
		op, err := operation(work.Ref, "rename", []string{work.Path, work.DestinationPath}, 0, struct {
			DestinationPath string `json:"destination_path"`
			Content         string `json:"content,omitempty"`
		}{work.DestinationPath, work.Content})
		if !work.PreserveContent {
			op.Mode = "model_required"
		}
		return []ProjectedDeterministicOperation{op}, err
	default:
		return nil, fmt.Errorf("unsupported projected file operation %q", work.Operation)
	}
}
