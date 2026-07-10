package speccompiler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	featureSlugPattern    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	templateMarkerPattern = regexp.MustCompile(`\{\{[\s\S]*?\}\}`)
)

type validator struct {
	diagnostics []Diagnostic
}

func (v *validator) add(code, path, message string) {
	v.diagnostics = append(v.diagnostics, Diagnostic{Code: code, Path: path, Message: message})
}

func validateExecutionSpec(root *jsonNode, filenameSlug string) []Diagnostic {
	v := &validator{}
	if !v.objectShape(root, "", []string{"schema_version", "feature_slug", "repo_target", "branch", "base_commit", "goal", "context", "scope", "steps", "validation", "completion_criteria"}, []string{"feature_slug", "repo_target", "branch", "base_commit", "goal", "context", "scope", "steps", "validation", "completion_criteria"}) {
		return v.diagnostics
	}

	if slug, ok := v.stringMember(root, "feature_slug", "/feature_slug", stringFeatureSlug); ok && slug != filenameSlug {
		v.add("filename_slug_mismatch", "/feature_slug", fmt.Sprintf("feature_slug %q does not match filename slug %q.", slug, filenameSlug))
	}
	v.stringMember(root, "repo_target", "/repo_target", stringRepositoryKey)
	v.stringMember(root, "branch", "/branch", stringBranch)
	v.stringMember(root, "base_commit", "/base_commit", stringCommit)
	v.stringMember(root, "goal", "/goal", stringSingleLine)
	v.stringMember(root, "context", "/context", stringMultiline)
	if member, ok := root.objectMember("scope"); ok {
		v.validateScope(member.value, "/scope")
	}

	fileCount := 0
	operations := map[string]string{}
	destinations := map[string]string{}
	if member, ok := root.objectMember("steps"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", "/steps", "steps must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", "/steps", "steps must not be empty.")
		} else {
			for i, step := range member.value.array {
				stepPath := joinPointer("/steps", strconv.Itoa(i))
				v.validateExecutionStep(step, stepPath, i+1, &fileCount, operations, destinations)
			}
		}
	}
	if fileCount == 0 {
		v.add("missing_file_declaration", "/steps", "At least one file declaration is required.")
	}

	if member, ok := root.objectMember("validation"); ok {
		v.validateValidation(member.value, "/validation")
	}
	if member, ok := root.objectMember("completion_criteria"); ok {
		v.validateStringArray(member.value, "/completion_criteria", false)
	}
	return v.diagnostics
}

func (v *validator) validateExecutionStep(node *jsonNode, path string, expectedNumber int, fileCount *int, operations, destinations map[string]string) {
	if !v.objectShape(node, path, []string{"number", "goal", "substeps", "completion_criteria"}, []string{"number", "goal", "substeps", "completion_criteria"}) {
		return
	}
	if number, ok := v.integerMember(node, "number", path+"/number", 1); ok && number != expectedNumber {
		v.add("nonsequential_step_number", path+"/number", fmt.Sprintf("Step number must be %d.", expectedNumber))
	}
	v.stringMember(node, "goal", path+"/goal", stringSingleLine)
	if member, ok := node.objectMember("substeps"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/substeps", "substeps must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", path+"/substeps", "substeps must not be empty.")
		} else {
			for i, substep := range member.value.array {
				subPath := joinPointer(path+"/substeps", strconv.Itoa(i))
				v.validateExecutionSubstep(substep, subPath, i+1, fileCount, operations, destinations)
			}
		}
	}
	if member, ok := node.objectMember("completion_criteria"); ok {
		v.validateStringArray(member.value, path+"/completion_criteria", false)
	}
}

func (v *validator) validateExecutionSubstep(node *jsonNode, path string, expectedNumber int, fileCount *int, operations, destinations map[string]string) {
	if !v.objectShape(node, path, []string{"number", "instruction", "files", "completion_criteria"}, []string{"number", "instruction", "files", "completion_criteria"}) {
		return
	}
	if number, ok := v.integerMember(node, "number", path+"/number", 1); ok && number != expectedNumber {
		v.add("nonsequential_substep_number", path+"/number", fmt.Sprintf("Substep number must be %d.", expectedNumber))
	}
	v.stringMember(node, "instruction", path+"/instruction", stringSingleLine)
	if member, ok := node.objectMember("files"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/files", "files must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", path+"/files", "files must not be empty.")
		} else {
			for i, file := range member.value.array {
				(*fileCount)++
				filePath := joinPointer(path+"/files", strconv.Itoa(i))
				v.validateFile(file, filePath, operations, destinations)
			}
		}
	}
	if member, ok := node.objectMember("completion_criteria"); ok {
		v.validateStringArray(member.value, path+"/completion_criteria", false)
	}
}

func (v *validator) validateFile(node *jsonNode, path string, operations, destinations map[string]string) {
	if node == nil || node.kind != nodeObject {
		v.add("invalid_value_type", path, "File declaration must be an object.")
		return
	}
	op := ""
	if member, ok := node.objectMember("operation"); ok && member.value.kind == nodeString {
		op = member.value.text
	}
	order := []string{"path", "operation", "purpose", "implementation"}
	required := []string{"path", "operation", "purpose", "implementation"}
	if op == "rename" {
		order = []string{"path", "destination_path", "operation", "purpose", "implementation"}
		required = []string{"path", "destination_path", "operation", "purpose", "implementation"}
	}
	v.objectShape(node, path, order, required)

	filePath, pathOK := v.stringMember(node, "path", path+"/path", stringRepositoryPath)
	operation, operationOK := v.stringMember(node, "operation", path+"/operation", stringSingleLine)
	if operationOK && operation != "modify" && operation != "create" && operation != "delete" && operation != "rename" {
		v.add("invalid_file_operation", path+"/operation", fmt.Sprintf("Unsupported file operation %q.", operation))
	}
	v.stringMember(node, "purpose", path+"/purpose", stringSingleLine)

	destination := ""
	if operation == "rename" {
		var ok bool
		destination, ok = v.stringMember(node, "destination_path", path+"/destination_path", stringRepositoryPath)
		if !ok {
			v.add("missing_rename_destination", path+"/destination_path", "Rename operations require destination_path.")
		}
	} else if _, ok := node.objectMember("destination_path"); ok {
		v.add("unexpected_rename_destination", path+"/destination_path", "destination_path is allowed only for rename operations.")
	}

	if pathOK && operationOK {
		if prior, exists := operations[filePath]; exists && prior != operation {
			v.add("conflicting_file_operation", path+"/operation", fmt.Sprintf("Path %q previously used operation %q.", filePath, prior))
		} else {
			operations[filePath] = operation
		}
		if operation == "rename" {
			if prior, exists := destinations[filePath]; exists && prior != destination {
				v.add("conflicting_rename_destination", path+"/destination_path", fmt.Sprintf("Path %q previously used rename destination %q.", filePath, prior))
			} else {
				destinations[filePath] = destination
			}
		}
	}

	implementation, ok := node.objectMember("implementation")
	if !ok {
		v.add("missing_file_implementation", path+"/implementation", "File declaration requires implementation content.")
		return
	}
	switch operation {
	case "modify":
		v.validateModifyImplementation(implementation.value, path+"/implementation")
	case "create":
		v.validateCreateImplementation(implementation.value, path+"/implementation")
	case "delete":
		v.validateDeleteImplementation(implementation.value, path+"/implementation")
	case "rename":
		v.validateRenameImplementation(implementation.value, path+"/implementation")
	default:
		if implementation.value.kind != nodeObject {
			v.add("operation_incompatible_file_implementation", path+"/implementation", "implementation must be an object compatible with the file operation.")
		}
	}
}

func (v *validator) validateModifyImplementation(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"changes"}, []string{"changes"}) {
		v.add("operation_incompatible_file_implementation", path, "modify implementation must contain changes.")
		return
	}
	member, ok := node.objectMember("changes")
	if !ok {
		return
	}
	if member.value.kind != nodeArray {
		v.add("operation_incompatible_file_implementation", path+"/changes", "changes must be an array.")
		return
	}
	if len(member.value.array) == 0 {
		v.add("empty_modify_changes", path+"/changes", "modify changes must not be empty.")
		return
	}
	for i, change := range member.value.array {
		v.validateModifyChange(change, joinPointer(path+"/changes", strconv.Itoa(i)))
	}
}

func (v *validator) validateModifyChange(node *jsonNode, path string) {
	if node == nil || node.kind != nodeObject {
		v.add("invalid_modify_directive", path, "Modify directive must be an object.")
		return
	}
	kind := ""
	if member, ok := node.objectMember("kind"); ok && member.value.kind == nodeString {
		kind = member.value.text
	}
	var order, required []string
	switch kind {
	case "replace":
		order = []string{"kind", "old_text", "new_text", "expected_occurrences"}
		required = order
	case "insert_before", "insert_after":
		order = []string{"kind", "anchor", "content", "expected_occurrences"}
		required = order
	case "remove":
		order = []string{"kind", "old_text", "expected_occurrences"}
		required = order
	case "replace_file":
		order = []string{"kind", "content"}
		required = order
	default:
		order = []string{"kind", "old_text", "anchor", "new_text", "content", "expected_occurrences"}
		required = []string{"kind"}
	}
	v.objectShape(node, path, order, required)
	if _, ok := v.stringMember(node, "kind", path+"/kind", stringSingleLine); ok {
		if kind != "replace" && kind != "insert_before" && kind != "insert_after" && kind != "remove" && kind != "replace_file" {
			v.add("invalid_modify_directive", path+"/kind", fmt.Sprintf("Unsupported modify directive %q.", kind))
		}
	}
	switch kind {
	case "replace":
		v.stringMember(node, "old_text", path+"/old_text", stringMultiline)
		if value, ok := v.stringMember(node, "new_text", path+"/new_text", stringMultiline); ok {
			v.validateTargetContent(value, path+"/new_text")
		}
		v.integerMemberWithCode(node, "expected_occurrences", path+"/expected_occurrences", 1, "invalid_expected_occurrences")
	case "insert_before", "insert_after":
		v.stringMember(node, "anchor", path+"/anchor", stringMultiline)
		if value, ok := v.stringMember(node, "content", path+"/content", stringMultiline); ok {
			v.validateTargetContent(value, path+"/content")
		}
		v.integerMemberWithCode(node, "expected_occurrences", path+"/expected_occurrences", 1, "invalid_expected_occurrences")
	case "remove":
		v.stringMember(node, "old_text", path+"/old_text", stringMultiline)
		v.integerMemberWithCode(node, "expected_occurrences", path+"/expected_occurrences", 1, "invalid_expected_occurrences")
	case "replace_file":
		if value, ok := v.stringMember(node, "content", path+"/content", stringMultiline); ok {
			v.validateTargetContent(value, path+"/content")
		}
	}
}

func (v *validator) validateCreateImplementation(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"content"}, []string{"content"}) {
		v.add("operation_incompatible_file_implementation", path, "create implementation must contain complete content.")
		return
	}
	if value, ok := v.stringMember(node, "content", path+"/content", stringMultiline); ok {
		v.validateTargetContent(value, path+"/content")
	}
}

func (v *validator) validateDeleteImplementation(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"delete_file"}, []string{"delete_file"}) {
		v.add("operation_incompatible_file_implementation", path, "delete implementation must contain delete_file: true.")
		return
	}
	member, ok := node.objectMember("delete_file")
	if ok && (member.value.kind != nodeBool || !member.value.boolean) {
		v.add("operation_incompatible_file_implementation", path+"/delete_file", "delete_file must be true.")
	}
}

func (v *validator) validateRenameImplementation(node *jsonNode, path string) {
	if node == nil || node.kind != nodeObject {
		v.add("operation_incompatible_file_implementation", path, "rename implementation must be an object.")
		return
	}
	v.objectShape(node, path, []string{"preserve_content", "content"}, nil)
	preserve, hasPreserve := node.objectMember("preserve_content")
	content, hasContent := node.objectMember("content")
	if hasPreserve == hasContent {
		v.add("invalid_rename_implementation", path, "Rename implementation requires exactly one of preserve_content or content.")
		return
	}
	if hasPreserve && (preserve.value.kind != nodeBool || !preserve.value.boolean) {
		v.add("invalid_rename_implementation", path+"/preserve_content", "preserve_content must be true.")
	}
	if hasContent {
		if value, ok := v.stringNode(content.value, path+"/content", stringMultiline); ok {
			v.validateTargetContent(value, path+"/content")
		}
	}
}

func (v *validator) validateValidation(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"commands", "executor_checks"}, []string{"commands"}) {
		return
	}
	if member, ok := node.objectMember("commands"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/commands", "commands must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("missing_validation_command", path+"/commands", "At least one validation command is required.")
		} else {
			for i, command := range member.value.array {
				commandPath := joinPointer(path+"/commands", strconv.Itoa(i))
				if !v.objectShape(command, commandPath, []string{"command", "working_directory", "expected"}, []string{"command", "expected"}) {
					continue
				}
				v.stringMember(command, "command", commandPath+"/command", stringSingleLine)
				if _, ok := command.objectMember("working_directory"); ok {
					v.stringMember(command, "working_directory", commandPath+"/working_directory", stringRepositoryPath)
				}
				v.stringMember(command, "expected", commandPath+"/expected", stringSingleLine)
			}
		}
	}
	if member, ok := node.objectMember("executor_checks"); ok {
		v.validateStringArray(member.value, path+"/executor_checks", false)
	}
}

func validatePlan(root *jsonNode, filenameSlug string) []Diagnostic {
	v := &validator{}
	if !v.objectShape(root, "", []string{"schema_version", "feature_slug", "goal", "context", "scope", "repo_targets", "passes", "completion_criteria"}, []string{"feature_slug", "goal", "context", "scope", "repo_targets", "passes", "completion_criteria"}) {
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

	repositories := map[string]struct{}{}
	if member, ok := root.objectMember("repo_targets"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", "/repo_targets", "repo_targets must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", "/repo_targets", "repo_targets must not be empty.")
		} else {
			for i, target := range member.value.array {
				path := joinPointer("/repo_targets", strconv.Itoa(i))
				if !v.objectShape(target, path, []string{"repo_target", "branch", "planning_base_commit"}, []string{"repo_target", "branch", "planning_base_commit"}) {
					continue
				}
				if key, ok := v.stringMember(target, "repo_target", path+"/repo_target", stringRepositoryKey); ok {
					if _, duplicate := repositories[key]; duplicate {
						v.add("duplicate_repository_target", path+"/repo_target", fmt.Sprintf("Repository target %q is duplicated.", key))
					}
					repositories[key] = struct{}{}
				}
				v.stringMember(target, "branch", path+"/branch", stringBranch)
				v.stringMember(target, "planning_base_commit", path+"/planning_base_commit", stringCommit)
			}
		}
	}

	var dependencies [][]int
	passCount := 0
	if member, ok := root.objectMember("passes"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", "/passes", "passes must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", "/passes", "passes must not be empty.")
		} else {
			passCount = len(member.value.array)
			dependencies = make([][]int, passCount)
			for i, pass := range member.value.array {
				path := joinPointer("/passes", strconv.Itoa(i))
				dependencies[i] = v.validatePass(pass, path, i+1, passCount, repositories)
			}
		}
	}
	v.validateDependencyCycles(dependencies)
	if member, ok := root.objectMember("completion_criteria"); ok {
		v.validateStringArray(member.value, "/completion_criteria", false)
	}
	return v.diagnostics
}

func (v *validator) validatePass(node *jsonNode, path string, expectedNumber, passCount int, repositories map[string]struct{}) []int {
	if !v.objectShape(node, path, []string{"number", "name", "repo_target", "goal", "context", "scope", "depends_on", "outcomes", "source_targets", "validation_intent", "completion_criteria"}, []string{"number", "name", "repo_target", "goal", "context", "scope", "depends_on", "outcomes", "source_targets", "validation_intent", "completion_criteria"}) {
		return nil
	}
	if number, ok := v.integerMember(node, "number", path+"/number", 1); ok && number != expectedNumber {
		v.add("nonsequential_pass_number", path+"/number", fmt.Sprintf("Pass number must be %d.", expectedNumber))
	}
	v.stringMember(node, "name", path+"/name", stringSingleLine)
	if target, ok := v.stringMember(node, "repo_target", path+"/repo_target", stringRepositoryKey); ok {
		if _, exists := repositories[target]; !exists {
			v.add("unknown_repository_target", path+"/repo_target", fmt.Sprintf("Repository target %q is not declared.", target))
		}
	}
	v.stringMember(node, "goal", path+"/goal", stringSingleLine)
	v.stringMember(node, "context", path+"/context", stringMultiline)
	if member, ok := node.objectMember("scope"); ok {
		v.validateScope(member.value, path+"/scope")
	}

	var dependencies []int
	if member, ok := node.objectMember("depends_on"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/depends_on", "depends_on must be an array.")
		} else {
			seen := map[int]struct{}{}
			for i, dependency := range member.value.array {
				depPath := joinPointer(path+"/depends_on", strconv.Itoa(i))
				if dependency.kind != nodeNumber {
					v.add("invalid_value_type", depPath, "Dependency must be an integer.")
					continue
				}
				value64, err := dependency.number.Int64()
				if err != nil || value64 < 1 {
					v.add("invalid_value_type", depPath, "Dependency must be a positive integer.")
					continue
				}
				value := int(value64)
				dependencies = append(dependencies, value)
				if _, duplicate := seen[value]; duplicate {
					v.add("duplicate_dependency", depPath, fmt.Sprintf("Dependency %d is duplicated.", value))
				}
				seen[value] = struct{}{}
				switch {
				case value == expectedNumber:
					v.add("self_dependency", depPath, "A pass cannot depend on itself.")
				case value > passCount:
					v.add("unknown_dependency", depPath, fmt.Sprintf("Dependency %d does not exist.", value))
				case value > expectedNumber:
					v.add("forward_dependency", depPath, fmt.Sprintf("Dependency %d is a later pass.", value))
				}
			}
		}
	}
	for _, name := range []string{"outcomes", "validation_intent", "completion_criteria"} {
		if member, ok := node.objectMember(name); ok {
			v.validateStringArray(member.value, path+"/"+name, false)
		}
	}
	if member, ok := node.objectMember("source_targets"); ok {
		if member.value.kind != nodeArray {
			v.add("invalid_value_type", path+"/source_targets", "source_targets must be an array.")
		} else if len(member.value.array) == 0 {
			v.add("empty_required_value", path+"/source_targets", "source_targets must not be empty.")
		} else {
			for i, target := range member.value.array {
				targetPath := joinPointer(path+"/source_targets", strconv.Itoa(i))
				if !v.objectShape(target, targetPath, []string{"path", "purpose"}, []string{"path", "purpose"}) {
					continue
				}
				v.stringMember(target, "path", targetPath+"/path", stringRepositoryPath)
				v.stringMember(target, "purpose", targetPath+"/purpose", stringSingleLine)
			}
		}
	}
	return dependencies
}

func (v *validator) validateDependencyCycles(dependencies [][]int) {
	if len(dependencies) == 0 {
		return
	}
	state := make([]uint8, len(dependencies))
	var visit func(int) bool
	visit = func(index int) bool {
		if state[index] == 1 {
			return true
		}
		if state[index] == 2 {
			return false
		}
		state[index] = 1
		for _, dependency := range dependencies[index] {
			if dependency < 1 || dependency > len(dependencies) {
				continue
			}
			if visit(dependency - 1) {
				return true
			}
		}
		state[index] = 2
		return false
	}
	for i := range dependencies {
		if visit(i) {
			v.add("circular_dependency", "/passes", "Pass dependencies contain a cycle.")
			return
		}
	}
}

func (v *validator) validateScope(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"in_scope", "out_of_scope"}, []string{"in_scope", "out_of_scope"}) {
		return
	}
	if member, ok := node.objectMember("in_scope"); ok {
		v.validateStringArray(member.value, path+"/in_scope", false)
	}
	if member, ok := node.objectMember("out_of_scope"); ok {
		v.validateStringArray(member.value, path+"/out_of_scope", false)
	}
}

func (v *validator) objectShape(node *jsonNode, path string, order, required []string) bool {
	if node == nil || node.kind != nodeObject {
		v.add("invalid_value_type", path, "Value must be an object.")
		return false
	}
	allowed := make(map[string]int, len(order))
	for i, key := range order {
		allowed[key] = i
	}
	last := -1
	canonical := true
	for _, entry := range node.object {
		index, ok := allowed[entry.key]
		if !ok {
			v.add("unknown_property", joinPointer(path, entry.key), fmt.Sprintf("Unknown property %q.", entry.key))
			continue
		}
		if index < last {
			canonical = false
		}
		last = index
	}
	if !canonical {
		v.add("noncanonical_property_order", path, "Object properties are not in canonical order.")
	}
	for _, key := range required {
		if _, ok := node.objectMember(key); !ok {
			v.add("missing_required_property", joinPointer(path, key), fmt.Sprintf("Missing required property %q.", key))
		}
	}
	return true
}

type stringKind uint8

const (
	stringSingleLine stringKind = iota
	stringMultiline
	stringFeatureSlug
	stringRepositoryKey
	stringBranch
	stringCommit
	stringRepositoryPath
)

func (v *validator) stringMember(node *jsonNode, key, path string, kind stringKind) (string, bool) {
	member, ok := node.objectMember(key)
	if !ok {
		return "", false
	}
	return v.stringNode(member.value, path, kind)
}

func (v *validator) stringNode(node *jsonNode, path string, kind stringKind) (string, bool) {
	if node == nil || node.kind != nodeString {
		v.add("invalid_value_type", path, "Value must be a string.")
		return "", false
	}
	value := node.text
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		v.add("empty_required_value", path, "String value must not be empty.")
		return value, false
	}
	if kind != stringMultiline && strings.ContainsAny(value, "\r\n") {
		v.add("multiline_value_not_allowed", path, "Value must be single-line.")
	}
	switch kind {
	case stringFeatureSlug:
		if !validFeatureSlug(value) {
			v.add("invalid_feature_slug", path, "Value must be lowercase kebab-case.")
		}
	case stringRepositoryKey:
		if !validMachineString(value) {
			v.add("invalid_repository_key", path, "Repository key contains invalid whitespace or control characters.")
		}
	case stringBranch:
		if !validMachineString(value) {
			v.add("invalid_branch", path, "Branch contains invalid whitespace or control characters.")
		}
	case stringCommit:
		if !validCommit(value) {
			v.add("invalid_commit_sha", path, "Commit must be a lowercase full 40-character SHA.")
		}
	case stringRepositoryPath:
		if !validRepositoryPath(value) {
			v.add("unsafe_repository_path", path, "Path must be a safe repository-relative POSIX path.")
		}
	}
	return value, true
}

func (v *validator) integerMember(node *jsonNode, key, path string, minimum int) (int, bool) {
	return v.integerMemberWithCode(node, key, path, minimum, "invalid_value_type")
}

func (v *validator) integerMemberWithCode(node *jsonNode, key, path string, minimum int, code string) (int, bool) {
	member, ok := node.objectMember(key)
	if !ok {
		return 0, false
	}
	if member.value.kind != nodeNumber {
		v.add(code, path, "Value must be an integer.")
		return 0, false
	}
	value, err := member.value.number.Int64()
	if err != nil || value < int64(minimum) {
		v.add(code, path, fmt.Sprintf("Value must be an integer of at least %d.", minimum))
		return 0, false
	}
	return int(value), true
}

func (v *validator) validateStringArray(node *jsonNode, path string, allowEmpty bool) {
	if node == nil || node.kind != nodeArray {
		v.add("invalid_value_type", path, "Value must be an array.")
		return
	}
	if !allowEmpty && len(node.array) == 0 {
		v.add("empty_required_value", path, "Array must not be empty.")
		return
	}
	for i, item := range node.array {
		v.stringNode(item, joinPointer(path, strconv.Itoa(i)), stringSingleLine)
	}
}

func (v *validator) validateTargetContent(value, path string) {
	trimmed := strings.TrimSpace(value)
	rejected := map[string]struct{}{
		"...": {}, "TODO": {}, "TBD": {}, "FIXME": {},
		"implement appropriately": {}, "follow existing patterns": {}, "as needed": {}, "where applicable": {},
	}
	if _, ok := rejected[trimmed]; ok || (strings.HasPrefix(trimmed, "Provide verified current-source context") && strings.HasSuffix(trimmed, "or exact transformation guidance.")) {
		v.add("placeholder_implementation_content", path, "Implementation target content is only a placeholder or meta-instruction.")
	}
	if templateMarkerPattern.MatchString(value) {
		v.add("unresolved_template_marker", path, "Implementation target content contains an unresolved template marker.")
	}
}

func validFeatureSlug(value string) bool {
	return featureSlugPattern.MatchString(value)
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func validMachineString(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validRepositoryPath(value string) bool {
	if !validMachineString(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return false
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
