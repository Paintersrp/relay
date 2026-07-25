package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/artifactschema"
)

type DeterministicOperationsDocument struct {
	SchemaVersion any                      `json:"schema_version,omitempty"`
	FeatureSlug   string                   `json:"feature_slug"`
	RepoTarget    string                   `json:"repo_target"`
	Branch        string                   `json:"branch"`
	BaseCommit    string                   `json:"base_commit"`
	Coverage      string                   `json:"coverage"`
	Operations    []DeterministicOperation `json:"operations"`
}

type DeterministicOperation struct {
	Path            string                      `json:"path"`
	DestinationPath string                      `json:"destination_path,omitempty"`
	Operation       string                      `json:"operation"`
	Implementation  DeterministicImplementation `json:"implementation"`
}

type DeterministicImplementation struct {
	Changes         []DeterministicChange `json:"changes,omitempty"`
	Content         string                `json:"content,omitempty"`
	ExpectedContent string                `json:"expected_content,omitempty"`
	PreserveContent *bool                 `json:"preserve_content,omitempty"`
}

type DeterministicChange struct {
	Kind                string `json:"kind"`
	OldText             string `json:"old_text,omitempty"`
	NewText             string `json:"new_text,omitempty"`
	Anchor              string `json:"anchor,omitempty"`
	Content             string `json:"content,omitempty"`
	ExpectedOccurrences int    `json:"expected_occurrences,omitempty"`
	ExpectedContent     string `json:"expected_content,omitempty"`
}

func CompileDeterministicOperations(filenameBasename string, rawJSON []byte) (Result, *DeterministicOperationsDocument) {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil), nil
	}
	if filename.Kind != ArtifactDeterministicOperations {
		return failed([]Diagnostic{{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must identify a Deterministic Operations artifact."}}, nil), nil
	}
	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil), nil
	}
	definition, _ := currentDefinition(filename.Kind)
	notices := schemaVersionNotice(root, definition)
	schemaValid, schemaErr := artifactschema.Validate(definition.SchemaKind, rawJSON)
	errors := validateDeterministicOperations(root, filename.FeatureSlug)
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
	document, err := decodeDeterministicOperationsDocument(rawJSON)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Deterministic Operations: %v", err)}}, notices), nil
	}
	return Result{Errors: []Diagnostic{}, Notices: notices}, document
}

func decodeDeterministicOperationsDocument(raw []byte) (*DeterministicOperationsDocument, error) {
	var document DeterministicOperationsDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func validateDeterministicOperations(root *jsonNode, filenameSlug string) []Diagnostic {
	v := &validator{}
	if !v.objectShape(root, "", []string{"schema_version", "feature_slug", "repo_target", "branch", "base_commit", "coverage", "operations"}, []string{"feature_slug", "repo_target", "branch", "base_commit", "coverage", "operations"}) {
		return v.diagnostics
	}
	if slug, ok := v.stringMember(root, "feature_slug", "/feature_slug", stringFeatureSlug); ok && slug != filenameSlug {
		v.add("filename_slug_mismatch", "/feature_slug", fmt.Sprintf("feature_slug %q does not match filename slug %q.", slug, filenameSlug))
	}
	v.stringMember(root, "repo_target", "/repo_target", stringRepositoryKey)
	v.stringMember(root, "branch", "/branch", stringBranch)
	v.stringMember(root, "base_commit", "/base_commit", stringCommit)
	if coverage, ok := v.stringMember(root, "coverage", "/coverage", stringSingleLine); ok && coverage != "complete" && coverage != "partial" {
		v.add("invalid_coverage", "/coverage", "Coverage must be complete or partial.")
	}
	operations, ok := root.objectMember("operations")
	if !ok {
		return v.diagnostics
	}
	if operations.value.kind != nodeArray {
		v.add("invalid_value_type", "/operations", "Operations must be an array.")
		return v.diagnostics
	}
	if len(operations.value.array) == 0 {
		v.add("empty_required_value", "/operations", "Operations must not be empty.")
		return v.diagnostics
	}
	paths := map[string]struct{}{}
	destinations := map[string]struct{}{}
	for index, operation := range operations.value.array {
		path := joinPointer("/operations", fmt.Sprint(index))
		if !v.objectShape(operation, path, []string{"path", "destination_path", "operation", "implementation"}, []string{"path", "operation", "implementation"}) {
			continue
		}
		source, sourceOK := v.stringMember(operation, "path", path+"/path", stringRepositoryPath)
		op, opOK := v.stringMember(operation, "operation", path+"/operation", stringSingleLine)
		if sourceOK {
			if _, duplicate := paths[source]; duplicate {
				v.add("ambiguous_path_chain", path+"/path", fmt.Sprintf("Path %q occurs more than once.", source))
			}
			paths[source] = struct{}{}
		}
		if member, exists := operation.objectMember("destination_path"); exists {
			if opOK && op != "rename" {
				v.add("unexpected_destination_path", path+"/destination_path", "Only rename operations may declare destination_path.")
			}
			destination, destinationOK := v.stringNode(member.value, path+"/destination_path", stringRepositoryPath)
			if destinationOK {
				if _, duplicate := destinations[destination]; duplicate {
					v.add("ambiguous_rename_destination", path+"/destination_path", fmt.Sprintf("Destination path %q occurs more than once.", destination))
				}
				destinations[destination] = struct{}{}
			}
		} else if opOK && op == "rename" {
			v.add("missing_required_property", path+"/destination_path", "Rename operations require destination_path.")
		}
		implementation, exists := operation.objectMember("implementation")
		if !exists || implementation.value.kind != nodeObject {
			continue
		}
		switch op {
		case "modify":
			v.validateDeterministicModifyImplementation(implementation.value, path+"/implementation")
		case "create":
			v.validateImplementationShape(implementation.value, path+"/implementation", []string{"content"}, []string{"content"})
			if member, exists := implementation.value.objectMember("content"); exists {
				v.validateCompleteContent(member.value, path+"/implementation/content")
			}
		case "delete":
			v.validateImplementationShape(implementation.value, path+"/implementation", []string{"expected_content"}, []string{"expected_content"})
			if member, exists := implementation.value.objectMember("expected_content"); exists {
				v.validateCompleteContent(member.value, path+"/implementation/expected_content")
			}
		case "rename":
			v.validateDeterministicRenameImplementation(implementation.value, path+"/implementation")
		default:
			if opOK {
				v.add("unsupported_operation", path+"/operation", "Operation must be modify, create, delete, or rename.")
			}
		}
	}
	return v.diagnostics
}

func (v *validator) validateDeterministicModifyImplementation(node *jsonNode, path string) {
	if !v.validateImplementationShape(node, path, []string{"changes"}, []string{"changes"}) {
		return
	}
	member, _ := node.objectMember("changes")
	if member.value.kind != nodeArray {
		v.add("invalid_value_type", path+"/changes", "Changes must be an array.")
		return
	}
	if len(member.value.array) == 0 {
		v.add("empty_required_value", path+"/changes", "Changes must not be empty.")
		return
	}
	for index, change := range member.value.array {
		changePath := joinPointer(path+"/changes", fmt.Sprint(index))
		if change.kind != nodeObject {
			v.add("invalid_value_type", changePath, "Change must be an object.")
			continue
		}
		kindMember, exists := change.objectMember("kind")
		if !exists || kindMember.value.kind != nodeString {
			v.add("missing_required_property", changePath+"/kind", "Change kind is required.")
			continue
		}
		switch kindMember.value.text {
		case "replace":
			v.validateImplementationShape(change, changePath, []string{"kind", "old_text", "new_text", "expected_occurrences"}, []string{"kind", "old_text", "new_text", "expected_occurrences"})
			v.validateChangeContent(change, changePath, true)
		case "insert_before", "insert_after":
			v.validateImplementationShape(change, changePath, []string{"kind", "anchor", "content", "expected_occurrences"}, []string{"kind", "anchor", "content", "expected_occurrences"})
			v.validateChangeContent(change, changePath, true)
		case "remove":
			v.validateImplementationShape(change, changePath, []string{"kind", "old_text", "expected_occurrences"}, []string{"kind", "old_text", "expected_occurrences"})
			v.validateChangeContent(change, changePath, true)
		case "replace_file":
			v.validateImplementationShape(change, changePath, []string{"kind", "expected_content", "content"}, []string{"kind", "expected_content", "content"})
			v.validateChangeContent(change, changePath, false)
			if member, exists := change.objectMember("expected_content"); exists {
				v.validateCompleteContent(member.value, changePath+"/expected_content")
			}
		default:
			v.add("unsupported_directive", changePath+"/kind", "Directive must be replace, insert_before, insert_after, remove, or replace_file.")
		}
	}
}

func (v *validator) validateDeterministicRenameImplementation(node *jsonNode, path string) {
	if !v.validateImplementationShape(node, path, []string{"expected_content", "preserve_content", "content"}, []string{"expected_content"}) {
		return
	}
	if member, exists := node.objectMember("expected_content"); exists {
		v.validateCompleteContent(member.value, path+"/expected_content")
	}
	preserve, hasPreserve := node.objectMember("preserve_content")
	content, hasContent := node.objectMember("content")
	if hasPreserve == hasContent {
		v.add("invalid_rename_representation", path, "Rename must contain exactly one of preserve_content or content.")
	}
	if hasPreserve && (preserve.value.kind != nodeBool || !preserve.value.boolean) {
		v.add("invalid_rename_representation", path+"/preserve_content", "preserve_content must be true.")
	}
	if hasContent {
		v.validateCompleteContent(content.value, path+"/content")
	}
}

func (v *validator) validateImplementationShape(node *jsonNode, path string, order, required []string) bool {
	return v.objectShape(node, path, order, required)
}

func (v *validator) validateChangeContent(node *jsonNode, path string, count bool) {
	for _, key := range []string{"old_text", "anchor", "new_text", "content"} {
		if member, exists := node.objectMember(key); exists {
			v.validateCompleteContent(member.value, path+"/"+key)
		}
	}
	if count {
		member, exists := node.objectMember("expected_occurrences")
		if exists {
			v.integerNode(member.value, path+"/expected_occurrences", 1)
		}
	}
}

func (v *validator) validateCompleteContent(node *jsonNode, path string) {
	if node == nil || node.kind != nodeString {
		v.add("invalid_value_type", path, "Content must be a string.")
		return
	}
	if strings.TrimSpace(node.text) == "" {
		v.add("empty_required_value", path, "Content must not be blank.")
	}
	v.validateTargetContent(node.text, path)
}

func (v *validator) integerNode(node *jsonNode, path string, minimum int) {
	if node == nil || node.kind != nodeNumber {
		v.add("invalid_value_type", path, "Value must be an integer.")
		return
	}
	value, err := node.number.Int64()
	if err != nil || value < int64(minimum) {
		v.add("invalid_value_type", path, fmt.Sprintf("Value must be an integer of at least %d.", minimum))
	}
}
