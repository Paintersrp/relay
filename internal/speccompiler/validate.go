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

func validateDeliveryTicket(root *jsonNode, filename FilenameInfo) []Diagnostic {
	v := &validator{}
	order := []string{
		"schema_version", "feature_slug", "ticket_id", "revision", "replaces_revision",
		"repo_target", "branch", "base_commit", "goal", "context", "scope", "depends_on",
		"required_invariants", "forbidden_behaviors", "implementation_obligations",
		"proof_obligations", "validation_commands", "transition_applicability",
		"explicit_deferrals", "cancellation", "completion_criteria",
	}
	required := []string{
		"feature_slug", "ticket_id", "revision", "replaces_revision", "repo_target", "branch",
		"base_commit", "goal", "context", "scope", "depends_on", "required_invariants",
		"forbidden_behaviors", "implementation_obligations", "proof_obligations",
		"validation_commands", "transition_applicability", "explicit_deferrals",
		"completion_criteria",
	}
	if !v.objectShape(root, "", order, required) {
		return v.diagnostics
	}

	if slug, ok := v.stringMember(root, "feature_slug", "/feature_slug", stringFeatureSlug); ok && slug != filename.FeatureSlug {
		v.add("filename_slug_mismatch", "/feature_slug", fmt.Sprintf("feature_slug %q does not match filename slug %q.", slug, filename.FeatureSlug))
	}
	if ticketID, ok := v.stringMember(root, "ticket_id", "/ticket_id", stringTicketID); ok && ticketID != filename.TicketID {
		v.add("ticket_id_mismatch", "/ticket_id", fmt.Sprintf("ticket_id %q does not match filename ticket ID %q.", ticketID, filename.TicketID))
	}
	revision, revisionOK := v.integerMemberWithCode(root, "revision", "/revision", 1, "invalid_ticket_revision")
	if revisionOK && int64(revision) != filename.Revision {
		v.add("revision_mismatch", "/revision", fmt.Sprintf("revision %d does not match filename revision %d.", revision, filename.Revision))
	}
	v.validateReplacesRevision(root, "/replaces_revision", revision, revisionOK)
	v.stringMember(root, "repo_target", "/repo_target", stringRepositoryKey)
	v.stringMember(root, "branch", "/branch", stringBranch)
	v.stringMember(root, "base_commit", "/base_commit", stringCommit)
	v.stringMember(root, "goal", "/goal", stringSingleLine)
	v.stringMember(root, "context", "/context", stringMultiline)
	if member, ok := root.objectMember("scope"); ok {
		v.validateScope(member.value, "/scope")
	}
	cancelled := false
	if member, ok := root.objectMember("cancellation"); ok {
		cancelled = member.value.kind == nodeObject
		v.validateCancellation(member.value, "/cancellation")
	}
	v.validateDeliveryTicketDependencies(root, filename.TicketID, &cancelled)
	v.validateDeliveryTicketStringArrays(root, cancelled)
	v.validateDeliveryTicketObligations(root, cancelled)
	v.validateDeliveryTicketValidationCommands(root, cancelled)
	if member, ok := root.objectMember("transition_applicability"); ok {
		if value, ok := v.stringNode(member.value, "/transition_applicability", stringSingleLine); ok && value != "not_required" && value != "required" {
			v.add("invalid_transition_applicability", "/transition_applicability", "Value must be either not_required or required.")
		} else if cancelled && value == "required" {
			v.add("cancellation_requires_no_transition", "/transition_applicability", "Cancelled tickets must use not_required transition applicability.")
		}
	}
	if member, ok := root.objectMember("completion_criteria"); ok {
		v.validateStringArray(member.value, "/completion_criteria", false)
	}
	return v.diagnostics
}

func validateTransitionPlan(root *jsonNode, filename FilenameInfo) []Diagnostic {
	v := &validator{}
	order := []string{
		"schema_version", "feature_slug", "ticket_id", "ticket_revision", "cutover_prerequisites",
		"activation_obligations", "rollback_eligibility", "rollback_obligations", "completion_criteria",
	}
	required := []string{
		"feature_slug", "ticket_id", "ticket_revision", "cutover_prerequisites", "activation_obligations",
		"rollback_eligibility", "rollback_obligations", "completion_criteria",
	}
	if !v.objectShape(root, "", order, required) {
		return v.diagnostics
	}

	if slug, ok := v.stringMember(root, "feature_slug", "/feature_slug", stringFeatureSlug); ok && slug != filename.FeatureSlug {
		v.add("filename_slug_mismatch", "/feature_slug", fmt.Sprintf("feature_slug %q does not match filename slug %q.", slug, filename.FeatureSlug))
	}
	if ticketID, ok := v.stringMember(root, "ticket_id", "/ticket_id", stringTicketID); ok && ticketID != filename.TicketID {
		v.add("ticket_id_mismatch", "/ticket_id", fmt.Sprintf("ticket_id %q does not match filename ticket ID %q.", ticketID, filename.TicketID))
	}
	if revision, ok := v.integerMemberWithCode(root, "ticket_revision", "/ticket_revision", 1, "invalid_ticket_revision"); ok && int64(revision) != filename.Revision {
		v.add("revision_mismatch", "/ticket_revision", fmt.Sprintf("ticket_revision %d does not match filename revision %d.", revision, filename.Revision))
	}

	for _, field := range []string{"cutover_prerequisites", "activation_obligations", "rollback_obligations", "completion_criteria"} {
		if member, ok := root.objectMember(field); ok {
			path := "/" + field
			v.validateStringArray(member.value, path, field == "rollback_obligations")
			if member.value.kind == nodeArray {
				for index, item := range member.value.array {
					if item.kind == nodeString {
						v.validateSafeRollForwardContent(item.text, joinPointer(path, strconv.Itoa(index)))
					}
				}
			}
		}
	}

	rollbackEligibility, eligibilityOK := v.stringMember(root, "rollback_eligibility", "/rollback_eligibility", stringSingleLine)
	if eligibilityOK && rollbackEligibility != "eligible" && rollbackEligibility != "not_eligible" {
		v.add("invalid_rollback_eligibility", "/rollback_eligibility", "Value must be either eligible or not_eligible.")
	}
	if member, ok := root.objectMember("rollback_obligations"); ok && member.value.kind == nodeArray && eligibilityOK {
		switch rollbackEligibility {
		case "eligible":
			if len(member.value.array) == 0 {
				v.add("rollback_requires_obligations", "/rollback_obligations", "Eligible rollback must declare at least one rollback obligation before the boundary.")
			}
		case "not_eligible":
			if len(member.value.array) != 0 {
				v.add("one_way_boundary_has_rollback", "/rollback_obligations", "A one-way transition boundary must not declare rollback obligations.")
			}
		}
	}
	return v.diagnostics
}

func (v *validator) validateSafeRollForwardContent(value, path string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	if templateMarkerPattern.MatchString(value) {
		v.add("unsafe_roll_forward_content", path, "Transition content must not contain an unresolved template marker.")
		return
	}
	switch strings.ToUpper(trimmed) {
	case "TODO", "TBD", "FIXME", "...":
		v.add("unsafe_roll_forward_content", path, "Transition content must state a concrete safe roll-forward condition.")
	}
}

func (v *validator) validateReplacesRevision(node *jsonNode, path string, revision int, revisionOK bool) {
	member, ok := node.objectMember("replaces_revision")
	if !ok || member.value.kind == nodeNull {
		return
	}
	if member.value.kind != nodeNumber {
		v.add("invalid_ticket_revision", path, "Value must be an integer of at least 1 or null.")
		return
	}
	value, err := member.value.number.Int64()
	if err != nil || value < 1 {
		v.add("invalid_ticket_revision", path, "Value must be an integer of at least 1 or null.")
		return
	}
	if revisionOK && value >= int64(revision) {
		v.add("invalid_revision_replacement", path, "replaces_revision must be lower than revision.")
	}
}

func (v *validator) validateDeliveryTicketDependencies(node *jsonNode, ticketID string, cancelled *bool) {
	member, ok := node.objectMember("depends_on")
	if !ok {
		return
	}
	if member.value.kind != nodeArray {
		v.add("invalid_value_type", "/depends_on", "Value must be an array.")
		return
	}
	if *cancelled && len(member.value.array) != 0 {
		v.add("cancellation_has_dependencies", "/depends_on", "Cancelled tickets must not declare dependencies.")
	}
	seen := map[string]struct{}{}
	for index, dependency := range member.value.array {
		path := joinPointer("/depends_on", strconv.Itoa(index))
		if !v.objectShape(dependency, path, []string{"ticket_id", "revision"}, []string{"ticket_id", "revision"}) {
			continue
		}
		dependencyID, idOK := v.stringMember(dependency, "ticket_id", path+"/ticket_id", stringTicketID)
		dependencyRevision, revisionOK := v.integerMemberWithCode(dependency, "revision", path+"/revision", 1, "invalid_ticket_revision")
		if idOK && revisionOK {
			key := dependencyID + "\x00" + strconv.Itoa(dependencyRevision)
			if _, duplicate := seen[key]; duplicate {
				v.add("duplicate_dependency", path, "Dependency appears more than once.")
			}
			seen[key] = struct{}{}
		}
	}
}

func (v *validator) validateDeliveryTicketStringArrays(node *jsonNode, cancelled bool) {
	for _, field := range []string{"required_invariants", "forbidden_behaviors", "proof_obligations", "explicit_deferrals"} {
		member, ok := node.objectMember(field)
		if !ok {
			continue
		}
		path := "/" + field
		v.validateStringArray(member.value, path, true)
		if cancelled && member.value.kind == nodeArray && len(member.value.array) != 0 {
			v.add("cancellation_has_"+field, path, fmt.Sprintf("Cancelled tickets must use an empty %s array.", field))
		}
		if field == "proof_obligations" && !cancelled && member.value.kind == nodeArray && len(member.value.array) == 0 {
			v.add("empty_required_value", "/proof_obligations", "Array must not be empty.")
		}
	}
}

func (v *validator) validateDeliveryTicketObligations(node *jsonNode, cancelled bool) {
	member, ok := node.objectMember("implementation_obligations")
	if !ok {
		return
	}
	if member.value.kind != nodeArray {
		v.add("invalid_value_type", "/implementation_obligations", "Value must be an array.")
		return
	}
	if len(member.value.array) == 0 && !cancelled {
		v.add("empty_required_value", "/implementation_obligations", "Array must not be empty.")
	}
	if cancelled && len(member.value.array) != 0 {
		v.add("cancellation_has_obligations", "/implementation_obligations", "Cancelled tickets must not declare implementation obligations.")
	}
	for index, obligation := range member.value.array {
		path := joinPointer("/implementation_obligations", strconv.Itoa(index))
		if !v.objectShape(obligation, path, []string{"source_area", "obligation", "prerequisites"}, []string{"source_area", "obligation", "prerequisites"}) {
			continue
		}
		v.validateObligationSourceArea(obligation, path+"/source_area")
		v.stringMember(obligation, "obligation", path+"/obligation", stringSingleLine)
		if member, ok := obligation.objectMember("prerequisites"); ok {
			v.validateStringArray(member.value, path+"/prerequisites", true)
		}
	}
}

func (v *validator) validateObligationSourceArea(node *jsonNode, path string) {
	member, ok := node.objectMember("source_area")
	if !ok {
		return
	}
	if member.value.kind == nodeNull {
		return
	}
	v.stringNode(member.value, path, stringRepositoryPath)
}

func (v *validator) validateDeliveryTicketValidationCommands(node *jsonNode, cancelled bool) {
	member, ok := node.objectMember("validation_commands")
	if !ok {
		return
	}
	if member.value.kind != nodeArray {
		v.add("invalid_value_type", "/validation_commands", "Value must be an array.")
		return
	}
	if len(member.value.array) == 0 && !cancelled {
		v.add("empty_required_value", "/validation_commands", "Array must not be empty.")
	}
	if cancelled && len(member.value.array) != 0 {
		v.add("cancellation_has_validation", "/validation_commands", "Cancelled tickets must not declare validation commands.")
	}
	for index, command := range member.value.array {
		path := joinPointer("/validation_commands", strconv.Itoa(index))
		if !v.objectShape(command, path, []string{"working_directory", "command", "expected"}, []string{"working_directory", "command", "expected"}) {
			continue
		}
		v.stringMember(command, "working_directory", path+"/working_directory", stringWorkingDirectory)
		v.stringMember(command, "command", path+"/command", stringSingleLine)
		v.stringMember(command, "expected", path+"/expected", stringSingleLine)
	}
}

func (v *validator) validateCancellation(node *jsonNode, path string) {
	if !v.objectShape(node, path, []string{"reason"}, []string{"reason"}) {
		return
	}
	v.stringMember(node, "reason", path+"/reason", stringSingleLine)
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
	stringTicketID
	stringRepositoryKey
	stringBranch
	stringCommit
	stringRepositoryPath
	stringWorkingDirectory
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
	if kind == stringWorkingDirectory && value == "" {
		return value, true
	}
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
	case stringTicketID:
		if !ticketIDPattern.MatchString(value) {
			v.add("invalid_ticket_id", path, "Value must use uppercase canonical ticket syntax.")
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
	case stringWorkingDirectory:
		if !validWorkingDirectory(value) {
			v.add("unsafe_working_directory", path, "Working directory must be empty for the repository root or a safe repository-relative POSIX directory.")
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

func validWorkingDirectory(value string) bool {
	if value == "" {
		return true
	}
	return validRepositoryPath(value)
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
