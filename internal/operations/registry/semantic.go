package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

type orderedMember struct {
	Name  string
	Value *orderedValue
}

type orderedValue struct {
	kind    byte
	object  []orderedMember
	array   []*orderedValue
	text    string
	boolean bool
}

type semanticCatalogData struct {
	root *orderedValue
	defs map[string]*orderedValue
}

var (
	semanticCatalogOnce sync.Once
	semanticCatalog     semanticCatalogData
	semanticCatalogErr  error
)

func ValidateOperationRequest(surface SurfaceContractID, tool string, raw []byte) error {
	_, _, _, err := validateOperationRequest(surface, tool, raw)
	return err
}

func validateOperationRequest(surface SurfaceContractID, tool string, raw []byte) (map[string]any, OperationDefinition, bool, error) {
	request, err := ValidateRequest(surface, tool, raw)
	if err != nil {
		return nil, OperationDefinition{}, false, err
	}
	operation, hasOperation, err := validateRequestAuthority(surface, tool, request)
	if err != nil {
		return nil, OperationDefinition{}, false, requestError("request_authority_invalid", "$")
	}
	if err := validateSurfaceAction(surface, tool); err != nil {
		return nil, OperationDefinition{}, false, requestError("request_authority_invalid", "$")
	}

	switch tool {
	case "create_operation_packet", "refresh_operation_packet":
		if !hasOperation {
			return nil, OperationDefinition{}, false, requestError("request_authority_invalid", "$.operation_id")
		}
		if err := normalizePacketRequest(tool, operation, request); err != nil {
			return nil, OperationDefinition{}, false, requestError("request_semantic_invalid", "$")
		}
	case "validate_artifact", "submit_plan", "create_run":
		if err := validateTopLevelClearance(request); err != nil {
			return nil, OperationDefinition{}, false, requestError("request_semantic_invalid", "$.sensitive_data_clearance")
		}
	case "close_operation_packet", "record_audit_decision":
	}
	return request, operation, hasOperation, nil
}

func validateSurfaceAction(surface SurfaceContractID, tool string) error {
	var action AllowedAction
	switch tool {
	case "validate_artifact":
		action = "validate_artifact"
	case "submit_plan":
		action = "submit_plan"
	case "create_run":
		action = "create_run"
	case "get_run_artifact":
		action = "get_run_artifact"
	case "record_audit_decision":
		action = "record_audit_decision"
	default:
		return nil
	}
	operations, err := OperationsForSurface(surface)
	if err != nil || len(operations) == 0 {
		return errors.New("surface action authority is unavailable")
	}
	for _, operation := range operations {
		if !containsAction(operation.AllowedNonSourceActions, action) {
			return errors.New("surface action is not allowed")
		}
	}
	return nil
}

func SemanticRequestBasis(surface SurfaceContractID, tool string, raw []byte) ([]byte, error) {
	if err := Validate(); err != nil {
		return nil, requestError("request_contract_unavailable", "$")
	}
	version, ok := SemanticProjectionVersion(tool)
	if !ok {
		return nil, requestError("request_projection_unavailable", "$")
	}

	request, operation, hasOperation, err := validateOperationRequest(surface, tool, raw)
	if err != nil {
		return nil, err
	}

	delete(request, "input_files")
	delete(request, "artifact_file")
	removeTransportValues(request)

	schema, definitions, err := inputSchema(surface, tool)
	if err != nil {
		return nil, requestError("request_contract_unavailable", "$")
	}
	var requestBasis bytes.Buffer
	if err := encodeBySchema(&requestBasis, request, schema, definitions); err != nil {
		return nil, requestError("request_semantic_invalid", "$")
	}

	var output bytes.Buffer
	output.WriteString(`{"semantic_projection":`)
	appendJSONString(&output, version)
	if hasOperation {
		output.WriteString(`,"operation_projection":`)
		appendJSONString(&output, operation.PacketSemanticProjection)
	}
	output.WriteString(`,"request":`)
	output.Write(requestBasis.Bytes())
	output.WriteByte('}')
	return output.Bytes(), nil
}

func SemanticRequestSHA256(surface SurfaceContractID, tool string, raw []byte) (string, error) {
	basis, err := SemanticRequestBasis(surface, tool, raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(basis)
	return hex.EncodeToString(sum[:]), nil
}

func decodeRequest(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode semantic request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("semantic request contains trailing JSON")
	}
	request, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("semantic request must be an object")
	}
	return request, nil
}

func validateRequestAuthority(surface SurfaceContractID, tool string, request map[string]any) (OperationDefinition, bool, error) {
	surfaceValue, ok := request["surface_contract"].(string)
	if !ok || surfaceValue == "" {
		return OperationDefinition{}, false, errors.New("surface_contract is required")
	}
	if surfaceValue != string(surface) {
		return OperationDefinition{}, false, fmt.Errorf("surface_contract %q does not equal mounted surface %q", surfaceValue, surface)
	}

	rawOperation, exists := request["operation_id"]
	if !exists {
		return OperationDefinition{}, false, nil
	}
	operationID, ok := rawOperation.(string)
	if !ok || operationID == "" {
		return OperationDefinition{}, false, errors.New("operation_id must be a non-empty string")
	}
	operation, ok := Lookup(OperationID(operationID))
	if !ok {
		return OperationDefinition{}, false, fmt.Errorf("unknown operation_id %q", operationID)
	}
	if operation.SurfaceContract != surface {
		return OperationDefinition{}, false, fmt.Errorf("operation %q does not belong to surface %q", operationID, surface)
	}
	if tool == "record_audit_decision" && operation.OperationID != "auditor.audit" {
		return OperationDefinition{}, false, fmt.Errorf("record_audit_decision is not allowed for operation %q", operation.OperationID)
	}
	return operation, true, nil
}

func normalizePacketRequest(tool string, operation OperationDefinition, request map[string]any) error {
	inputFiles, err := objectArray(request, "input_files", false)
	if err != nil {
		return err
	}
	inputs, err := objectArray(request, "inputs", true)
	if err != nil {
		return err
	}
	references, err := objectArray(request, "workflow_references", true)
	if err != nil {
		return err
	}
	attestations, err := objectArray(request, "attestations", true)
	if err != nil {
		return err
	}
	primaryRevisions, err := objectArray(request, "primary_revisions", false)
	if err != nil {
		return err
	}
	comparisonAnchors, err := objectArray(request, "comparison_anchors", false)
	if err != nil {
		return err
	}
	if _, exists := request["primary_revisions"]; !exists {
		request["primary_revisions"] = []any{}
		primaryRevisions = nil
	}
	if _, exists := request["comparison_anchors"]; !exists {
		request["comparison_anchors"] = []any{}
		comparisonAnchors = nil
	}

	slots := append([]InputSlotDefinition(nil), operation.RequiredInputs...)
	conditionalPresent := false
	for _, candidate := range operation.ConditionalRefreshInputs {
		if objectArrayContains(inputs, "input_name", candidate.InputName) {
			conditionalPresent = true
			break
		}
	}
	if conditionalPresent {
		if tool != "refresh_operation_packet" {
			return errors.New("conditional review inputs are permitted only on refresh")
		}
		slots = append(slots, operation.ConditionalRefreshInputs...)
	}
	if operation.OperationID == "auditor.audit" {
		if len(inputs) != 0 || len(attestations) != 0 {
			return errors.New("auditor.audit requires empty caller inputs and attestations")
		}
	}

	slotOrder := make(map[string]int, len(slots))
	slotByName := make(map[string]InputSlotDefinition, len(slots))
	for index, slot := range slots {
		slotOrder[slot.InputName] = index
		slotByName[slot.InputName] = slot
	}
	if len(inputs) != len(slots) {
		return fmt.Errorf("operation %q requires %d inputs, got %d", operation.OperationID, len(slots), len(inputs))
	}

	seenInputs := make(map[string]struct{}, len(inputs))
	usedFileIndexes := make(map[int]struct{})
	allInputSHA := make(map[string]string, len(inputs))
	externalInputs := make(map[string]string)
	for _, input := range inputs {
		name, ok := input["input_name"].(string)
		if !ok || name == "" {
			return errors.New("input_name is required")
		}
		if _, duplicate := seenInputs[name]; duplicate {
			return fmt.Errorf("input_name %q is duplicated", name)
		}
		seenInputs[name] = struct{}{}
		slot, allowed := slotByName[name]
		if !allowed {
			return fmt.Errorf("input_name %q is not allowed for operation %q", name, operation.OperationID)
		}
		sourceKind, ok := input["source_kind"].(string)
		if !ok || !containsSourceKind(slot.AllowedSourceKinds, InputSourceKind(sourceKind)) {
			return fmt.Errorf("input %q source_kind %q is not allowed", name, sourceKind)
		}
		if err := validateInputSourceKind(sourceKind, input); err != nil {
			return fmt.Errorf("input %q: %w", name, err)
		}
		sha := expectedSHA(input)
		if sha == "" {
			return fmt.Errorf("input %q expected_sha256 is required", name)
		}
		allInputSHA[name] = sha
		if sourceKind != "committed_source" {
			externalInputs[name] = sha
		}
		if sourceKind == "workflow_record" {
			if err := validateWorkflowRecordPolicy(slot.WorkflowRecordPolicy, input); err != nil {
				return fmt.Errorf("input %q: %w", name, err)
			}
		}
		if sourceKind == "uploaded_file" {
			index, err := uploadedFileIndex(input)
			if err != nil {
				return fmt.Errorf("input %q: %w", name, err)
			}
			if index < 0 || index >= len(inputFiles) {
				return fmt.Errorf("input %q file_index %d is outside input_files", name, index)
			}
			if _, duplicate := usedFileIndexes[index]; duplicate {
				return fmt.Errorf("input_file index %d is duplicated", index)
			}
			usedFileIndexes[index] = struct{}{}
		}
	}
	if len(usedFileIndexes) != len(inputFiles) {
		return errors.New("every input_files member must be referenced exactly once")
	}
	sort.Slice(inputs, func(left, right int) bool {
		return slotOrder[inputs[left]["input_name"].(string)] < slotOrder[inputs[right]["input_name"].(string)]
	})
	request["inputs"] = mapsToAny(inputs)

	if err := validateAndSortAttestations(attestations, slots, allInputSHA, externalInputs, conditionalPresent); err != nil {
		return err
	}
	request["attestations"] = mapsToAny(attestations)

	if err := validateAndSortReferences(references, operation.WorkflowReferenceKinds); err != nil {
		return err
	}
	request["workflow_references"] = mapsToAny(references)

	if err := sortUniqueObjects(primaryRevisions, []string{"repository_key"}); err != nil {
		return fmt.Errorf("primary_revisions: %w", err)
	}
	request["primary_revisions"] = mapsToAny(primaryRevisions)

	if err := validateAndSortAnchors(comparisonAnchors, operation.ComparisonAnchorPurposes); err != nil {
		return err
	}
	request["comparison_anchors"] = mapsToAny(comparisonAnchors)

	return nil
}

func objectArray(request map[string]any, key string, required bool) ([]map[string]any, error) {
	raw, exists := request[key]
	if !exists {
		if required {
			return nil, fmt.Errorf("%s is required", key)
		}
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	out := make([]map[string]any, len(values))
	for index, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", key, index)
		}
		out[index] = object
	}
	return out, nil
}

func objectArrayContains(values []map[string]any, key, expected string) bool {
	for _, value := range values {
		if value[key] == expected {
			return true
		}
	}
	return false
}

func expectedSHA(input map[string]any) string {
	value, _ := input["expected_sha256"].(string)
	return value
}

func validateInputSourceKind(sourceKind string, input map[string]any) error {
	source, ok := input["source"].(map[string]any)
	if !ok {
		return errors.New("source object is required")
	}
	var requiredKeys []string
	switch sourceKind {
	case "uploaded_file":
		requiredKeys = []string{"file_index"}
	case "relay_artifact":
		requiredKeys = []string{"artifact_id"}
	case "inline_text":
		requiredKeys = []string{"text"}
	case "workflow_record":
		requiredKeys = []string{"workflow_record"}
	case "committed_source":
		requiredKeys = []string{"repository_key", "revision", "path", "expected_blob_oid"}
	default:
		return errors.New("source_kind is unsupported")
	}
	if len(source) != len(requiredKeys) {
		return errors.New("source object does not match source_kind")
	}
	for _, key := range requiredKeys {
		if _, exists := source[key]; !exists {
			return errors.New("source object does not match source_kind")
		}
	}
	return nil
}

func uploadedFileIndex(input map[string]any) (int, error) {
	source, ok := input["source"].(map[string]any)
	if !ok {
		return 0, errors.New("source object is required")
	}
	number, ok := source["file_index"].(json.Number)
	if !ok {
		return 0, errors.New("uploaded_file source requires integer file_index")
	}
	value, err := strconv.Atoi(number.String())
	if err != nil {
		return 0, errors.New("uploaded_file source requires integer file_index")
	}
	return value, nil
}

func validateWorkflowRecordPolicy(policy string, input map[string]any) error {
	source, ok := input["source"].(map[string]any)
	if !ok {
		return errors.New("workflow_record source object is required")
	}
	record, ok := source["workflow_record"].(map[string]any)
	if !ok {
		return errors.New("workflow_record reference is required")
	}
	kind, ok := record["kind"].(string)
	if !ok || kind == "" {
		return errors.New("workflow_record kind is required")
	}
	allowed := false
	switch policy {
	case "artifact":
		allowed = kind == "plan_artifact" || kind == "run_execution_spec"
	case "plan_artifact":
		allowed = kind == "plan_artifact"
	case "pass_or_artifact":
		allowed = kind == "pass_record" || kind == "plan_artifact"
	case "audit_packet":
		allowed = kind == "audit_packet"
	case "audit_decision":
		allowed = kind == "audit_decision"
	case "run_execution_spec":
		allowed = kind == "run_execution_spec"
	case "none", "derived":
		allowed = false
	default:
		return fmt.Errorf("unknown workflow_record policy %q", policy)
	}
	if !allowed {
		return fmt.Errorf("workflow_record kind %q is not allowed by policy %q", kind, policy)
	}
	if nestedExpected, exists := record["expected_sha256"]; exists {
		value, ok := nestedExpected.(string)
		if !ok || value != expectedSHA(input) {
			return errors.New("workflow_record expected_sha256 does not equal input expected_sha256")
		}
	}
	return nil
}

func validateTopLevelClearance(request map[string]any) error {
	expected, _ := request["expected_sha256"].(string)
	if expected == "" {
		return errors.New("expected_sha256 is required")
	}
	clearance, ok := request["sensitive_data_clearance"].(map[string]any)
	if !ok {
		return errors.New("sensitive_data_clearance is required")
	}
	if clearance["policy_version"] != "relay.canonical-artifact-sensitive-data.v1" || clearance["confirmed"] != true {
		return errors.New("sensitive_data_clearance policy is invalid")
	}
	if clearance["subject_sha256"] != expected {
		return errors.New("sensitive_data_clearance subject_sha256 does not equal expected_sha256")
	}
	declaration, ok := clearance["declaration"].(map[string]any)
	if !ok || !allClearanceValuesFalse(declaration) {
		return errors.New("sensitive_data_clearance declaration is invalid")
	}
	return nil
}

func validateAndSortAttestations(attestations []map[string]any, slots []InputSlotDefinition, allInputSHA, externalInputs map[string]string, conditional bool) error {
	slotOrder := make(map[string]int, len(slots))
	requiredKind := make(map[string]string, len(slots))
	for index, slot := range slots {
		slotOrder[slot.InputName] = index
		requiredKind[slot.InputName] = string(slot.AttestationKind)
	}
	kindRank := make(map[string]int)
	for index, kind := range AttestationRank() {
		kindRank[string(kind)] = index
	}

	seen := make(map[string]struct{}, len(attestations))
	ordinarySeen := make(map[string]bool, len(slots))
	clearanceSeen := make(map[string]bool, len(externalInputs))
	var reviewedCandidateSHA string

	for _, attestation := range attestations {
		name, ok := attestation["input_name"].(string)
		if !ok || name == "" {
			return errors.New("attestation input_name is required")
		}
		kind, ok := attestation["kind"].(string)
		if !ok || kind == "" {
			return errors.New("attestation kind is required")
		}
		if _, exists := slotOrder[name]; !exists {
			return fmt.Errorf("attestation input_name %q is not an operation slot", name)
		}
		key := name + "\x00" + kind
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("attestation %q for %q is duplicated", kind, name)
		}
		seen[key] = struct{}{}

		if kind == "sensitive_data_clearance" {
			expected, external := externalInputs[name]
			if !external {
				return fmt.Errorf("input %q does not accept sensitive_data_clearance", name)
			}
			clearance, ok := attestation["clearance"].(map[string]any)
			if !ok {
				return fmt.Errorf("input %q clearance object is required", name)
			}
			subject, _ := clearance["subject_sha256"].(string)
			if subject == "" || subject != expected {
				return fmt.Errorf("input %q clearance subject_sha256 does not equal expected_sha256", name)
			}
			if clearance["policy_version"] != "relay.canonical-artifact-sensitive-data.v1" || clearance["confirmed"] != true {
				return fmt.Errorf("input %q clearance policy is invalid", name)
			}
			declaration, ok := clearance["declaration"].(map[string]any)
			if !ok || !allClearanceValuesFalse(declaration) {
				return fmt.Errorf("input %q clearance declaration is invalid", name)
			}
			clearanceSeen[name] = true
			continue
		}

		if requiredKind[name] != kind {
			return fmt.Errorf("input %q requires attestation %q, got %q", name, requiredKind[name], kind)
		}
		if subject, exists := attestation["subject_sha256"].(string); exists && subject != allInputSHA[name] {
			return fmt.Errorf("input %q attestation subject_sha256 does not equal expected_sha256", name)
		}
		ordinarySeen[name] = true
		if name == "reviewed_candidate" {
			reviewedCandidateSHA, _ = attestation["subject_sha256"].(string)
		}
	}

	for _, slot := range slots {
		if !ordinarySeen[slot.InputName] {
			return fmt.Errorf("input %q is missing attestation %q", slot.InputName, slot.AttestationKind)
		}
	}
	for name := range externalInputs {
		if !clearanceSeen[name] {
			return fmt.Errorf("input %q is missing sensitive_data_clearance", name)
		}
	}

	if conditional {
		var reviewResult map[string]any
		for _, attestation := range attestations {
			if attestation["input_name"] == "auditor_review_result" && attestation["kind"] == "complete_review_result" {
				reviewResult = attestation
				break
			}
		}
		if reviewResult == nil || reviewResult["review_result"] != "needs_revision" {
			return errors.New("conditional refresh requires complete_review_result(needs_revision)")
		}
		candidateSHA, _ := reviewResult["reviewed_candidate_sha256"].(string)
		if candidateSHA == "" || candidateSHA != reviewedCandidateSHA {
			return errors.New("reviewed_candidate_sha256 does not equal reviewed candidate input sha256")
		}
	}

	sort.Slice(attestations, func(left, right int) bool {
		leftName := attestations[left]["input_name"].(string)
		rightName := attestations[right]["input_name"].(string)
		if slotOrder[leftName] != slotOrder[rightName] {
			return slotOrder[leftName] < slotOrder[rightName]
		}
		leftKind, _ := attestations[left]["kind"].(string)
		rightKind, _ := attestations[right]["kind"].(string)
		return kindRank[leftKind] < kindRank[rightKind]
	})
	return nil
}

func allClearanceValuesFalse(declaration map[string]any) bool {
	required := []string{
		"password",
		"api_key_or_access_token",
		"refresh_token_or_session_material",
		"cookie_or_authorization_header",
		"private_or_ssh_key",
		"credential",
		"complete_secret_bearing_environment_file",
		"avoidable_signed_secret_bearing_url",
	}
	if len(declaration) != len(required) {
		return false
	}
	for _, key := range required {
		if declaration[key] != false {
			return false
		}
	}
	return true
}

func validateAndSortReferences(references []map[string]any, allowed []WorkflowReferenceKind) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, kind := range allowed {
		allowedSet[string(kind)] = struct{}{}
	}
	if len(references) != len(allowedSet) {
		return fmt.Errorf("workflow_references requires %d entries, got %d", len(allowedSet), len(references))
	}
	rank := make(map[string]int)
	for index, kind := range WorkflowReferenceRank() {
		rank[string(kind)] = index
	}
	seen := make(map[string]struct{}, len(references))
	seenKinds := make(map[string]struct{}, len(references))
	for _, reference := range references {
		kind, ok := reference["kind"].(string)
		if !ok {
			return errors.New("workflow reference kind is required")
		}
		if _, ok := allowedSet[kind]; !ok {
			return fmt.Errorf("workflow reference kind %q is not allowed", kind)
		}
		if _, duplicate := seenKinds[kind]; duplicate {
			return fmt.Errorf("workflow reference kind %q is duplicated", kind)
		}
		seenKinds[kind] = struct{}{}
		identity, err := compactSortedJSON(reference)
		if err != nil {
			return err
		}
		key := kind + "\x00" + string(identity)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("workflow reference %q is duplicated", kind)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(references, func(left, right int) bool {
		leftKind := references[left]["kind"].(string)
		rightKind := references[right]["kind"].(string)
		if rank[leftKind] != rank[rightKind] {
			return rank[leftKind] < rank[rightKind]
		}
		leftJSON, _ := compactSortedJSON(references[left])
		rightJSON, _ := compactSortedJSON(references[right])
		return bytes.Compare(leftJSON, rightJSON) < 0
	})
	return nil
}

func validateAndSortAnchors(anchors []map[string]any, allowed []AnchorPurpose) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, purpose := range allowed {
		allowedSet[string(purpose)] = struct{}{}
	}
	for _, anchor := range anchors {
		purpose, ok := anchor["purpose"].(string)
		if !ok || purpose == "" {
			return errors.New("comparison anchor purpose is required")
		}
		if _, ok := allowedSet[purpose]; !ok {
			return fmt.Errorf("comparison anchor purpose %q is not allowed", purpose)
		}
	}
	if len(allowedSet) == 0 && len(anchors) != 0 {
		return errors.New("comparison_anchors are not allowed for this operation")
	}
	return sortUniqueObjects(anchors, []string{"repository_key", "anchor_name"})
}

func sortUniqueObjects(values []map[string]any, keys []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		var identity strings.Builder
		for _, key := range keys {
			text, ok := value[key].(string)
			if !ok || text == "" {
				return fmt.Errorf("%s is required", key)
			}
			identity.WriteString(text)
			identity.WriteByte(0)
		}
		key := identity.String()
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("identifier %q is duplicated", key)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(values, func(left, right int) bool {
		for _, key := range keys {
			leftValue := values[left][key].(string)
			rightValue := values[right][key].(string)
			if leftValue == rightValue {
				continue
			}
			return leftValue < rightValue
		}
		return false
	})
	return nil
}

func mapsToAny(values []map[string]any) []any {
	out := make([]any, len(values))
	for index, value := range values {
		out[index] = value
	}
	return out
}

func containsSourceKind(values []InputSourceKind, expected InputSourceKind) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsAction(values []AllowedAction, expected AllowedAction) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func removeTransportValues(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range TransportExcludedKeys() {
			delete(typed, key)
		}
		for _, child := range typed {
			removeTransportValues(child)
		}
	case []any:
		for _, child := range typed {
			removeTransportValues(child)
		}
	}
}

func compactSortedJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := encodeSorted(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodeSorted(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		appendJSONString(output, typed)
	case json.Number:
		number, err := normalizeNumber(typed)
		if err != nil {
			return err
		}
		output.WriteString(number)
	case []any:
		output.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := encodeSorted(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := sortedKeys(typed)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendJSONString(output, key)
			output.WriteByte(':')
			if err := encodeSorted(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func inputSchema(surface SurfaceContractID, tool string) (*orderedValue, map[string]*orderedValue, error) {
	semanticCatalogOnce.Do(func() {
		root, err := parseOrderedJSON(publicContractJSON)
		if err != nil {
			semanticCatalogErr = err
			return
		}
		definitionsNode, ok := objectValue(root, "definitions")
		if !ok {
			semanticCatalogErr = errors.New("public contract definitions are missing")
			return
		}
		definitions := make(map[string]*orderedValue, len(definitionsNode.object))
		for _, member := range definitionsNode.object {
			definitions[member.Name] = member.Value
		}
		semanticCatalog = semanticCatalogData{root: root, defs: definitions}
	})
	if semanticCatalogErr != nil {
		return nil, nil, semanticCatalogErr
	}
	surfaces, ok := objectValue(semanticCatalog.root, "surfaces")
	if !ok {
		return nil, nil, errors.New("public contract surfaces are missing")
	}
	surfaceNode, ok := objectValue(surfaces, string(surface))
	if !ok {
		return nil, nil, fmt.Errorf("surface %q is not registered", surface)
	}
	tools, ok := objectValue(surfaceNode, "tools")
	if !ok {
		return nil, nil, fmt.Errorf("surface %q tools are missing", surface)
	}
	toolNode, ok := objectValue(tools, tool)
	if !ok {
		return nil, nil, fmt.Errorf("tool %q is not registered on surface %q", tool, surface)
	}
	schema, ok := objectValue(toolNode, "input_root")
	if !ok {
		return nil, nil, fmt.Errorf("tool %q input_root is missing", tool)
	}
	return schema, semanticCatalog.defs, nil
}

func parseOrderedJSON(raw []byte) (*orderedValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeOrderedValue(decoder)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing token %v: %w", token, err)
	}
	return value, nil
}

func decodeOrderedValue(decoder *json.Decoder) (*orderedValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			value := &orderedValue{kind: 'o'}
			for decoder.More() {
				nameToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := nameToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				child, err := decodeOrderedValue(decoder)
				if err != nil {
					return nil, err
				}
				value.object = append(value.object, orderedMember{Name: name, Value: child})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return value, nil
		case '[':
			value := &orderedValue{kind: 'a'}
			for decoder.More() {
				child, err := decodeOrderedValue(decoder)
				if err != nil {
					return nil, err
				}
				value.array = append(value.array, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return value, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", typed)
		}
	case string:
		return &orderedValue{kind: 's', text: typed}, nil
	case json.Number:
		return &orderedValue{kind: 'n', text: typed.String()}, nil
	case bool:
		return &orderedValue{kind: 'b', boolean: typed}, nil
	case nil:
		return &orderedValue{kind: '0'}, nil
	default:
		return nil, fmt.Errorf("unsupported token %T", token)
	}
}

func objectValue(object *orderedValue, name string) (*orderedValue, bool) {
	if object == nil || object.kind != 'o' {
		return nil, false
	}
	for _, member := range object.object {
		if member.Name == name {
			return member.Value, true
		}
	}
	return nil, false
}

func resolveSchema(schema *orderedValue, definitions map[string]*orderedValue) (*orderedValue, error) {
	if reference, ok := objectValue(schema, "$ref"); ok {
		if reference.kind != 's' || !strings.HasPrefix(reference.text, "#/$defs/") {
			return nil, fmt.Errorf("unsupported schema reference %q", reference.text)
		}
		name := strings.TrimPrefix(reference.text, "#/$defs/")
		resolved, ok := definitions[name]
		if !ok {
			return nil, fmt.Errorf("schema definition %q is missing", name)
		}
		return resolveSchema(resolved, definitions)
	}
	return schema, nil
}

func encodeBySchema(output *bytes.Buffer, value any, schema *orderedValue, definitions map[string]*orderedValue) error {
	resolved, err := resolveSchema(schema, definitions)
	if err != nil {
		return err
	}
	if branches, ok := objectValue(resolved, "oneOf"); ok {
		selected, err := selectSchemaBranch(branches, value, definitions)
		if err != nil {
			return err
		}
		return encodeBySchema(output, value, selected, definitions)
	}

	switch typed := value.(type) {
	case map[string]any:
		properties, ok := objectValue(resolved, "properties")
		if !ok {
			return errors.New("object schema has no properties")
		}
		propertySchemas := make(map[string]*orderedValue, len(properties.object))
		for _, member := range properties.object {
			propertySchemas[member.Name] = member.Value
		}
		for key := range typed {
			if _, ok := propertySchemas[key]; !ok {
				return fmt.Errorf("unknown semantic request property %q", key)
			}
		}
		output.WriteByte('{')
		written := 0
		for _, member := range properties.object {
			child, exists := typed[member.Name]
			if !exists {
				continue
			}
			if written > 0 {
				output.WriteByte(',')
			}
			appendJSONString(output, member.Name)
			output.WriteByte(':')
			if err := encodeBySchema(output, child, member.Value, definitions); err != nil {
				return fmt.Errorf("%s: %w", member.Name, err)
			}
			written++
		}
		output.WriteByte('}')
		return nil
	case []any:
		items, ok := objectValue(resolved, "items")
		if !ok {
			return errors.New("array schema has no items")
		}
		output.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := encodeBySchema(output, child, items, definitions); err != nil {
				return fmt.Errorf("array item %d: %w", index, err)
			}
		}
		output.WriteByte(']')
		return nil
	case string:
		appendJSONString(output, typed)
		return nil
	case json.Number:
		number, err := normalizeNumber(typed)
		if err != nil {
			return err
		}
		output.WriteString(number)
		return nil
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
		return nil
	case nil:
		output.WriteString("null")
		return nil
	default:
		return fmt.Errorf("unsupported semantic request value %T", value)
	}
}

func selectSchemaBranch(branches *orderedValue, value any, definitions map[string]*orderedValue) (*orderedValue, error) {
	if branches.kind != 'a' {
		return nil, errors.New("oneOf is not an array")
	}
	object, objectValueOK := value.(map[string]any)
	for _, branch := range branches.array {
		resolved, err := resolveSchema(branch, definitions)
		if err != nil {
			return nil, err
		}
		if !objectValueOK {
			if schemaAcceptsPrimitive(resolved, value) {
				return branch, nil
			}
			continue
		}
		properties, ok := objectValue(resolved, "properties")
		if !ok {
			continue
		}
		if discriminatorMatches(properties, object) && objectKeysCovered(properties, object) {
			return branch, nil
		}
	}
	return nil, errors.New("no oneOf branch matches semantic request value")
}

func schemaAcceptsPrimitive(schema *orderedValue, value any) bool {
	typeNode, ok := objectValue(schema, "type")
	if !ok || typeNode.kind != 's' {
		return false
	}
	switch typeNode.text {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer", "number":
		_, ok := value.(json.Number)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func discriminatorMatches(properties *orderedValue, value map[string]any) bool {
	for _, member := range properties.object {
		constant, ok := objectValue(member.Value, "const")
		if !ok {
			continue
		}
		actual, exists := value[member.Name]
		if !exists {
			return false
		}
		switch constant.kind {
		case 's':
			if actual != constant.text {
				return false
			}
		case 'b':
			if actual != constant.boolean {
				return false
			}
		}
	}
	return true
}

func objectKeysCovered(properties *orderedValue, value map[string]any) bool {
	allowed := make(map[string]struct{}, len(properties.object))
	for _, member := range properties.object {
		allowed[member.Name] = struct{}{}
	}
	for key := range value {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func normalizeNumber(number json.Number) (string, error) {
	text := number.String()
	if !strings.ContainsAny(text, ".eE") {
		if _, err := strconv.ParseInt(text, 10, 64); err != nil {
			return "", fmt.Errorf("invalid integer %q", text)
		}
		trimmed := strings.TrimLeft(text, "0")
		if strings.HasPrefix(text, "-") {
			trimmed = "-" + strings.TrimLeft(strings.TrimPrefix(text, "-"), "0")
		}
		if trimmed == "" || trimmed == "-" {
			return "0", nil
		}
		return trimmed, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return "", fmt.Errorf("invalid number %q", text)
	}
	return strconv.FormatFloat(value, 'g', -1, 64), nil
}

func appendJSONString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, runeValue := range value {
		switch runeValue {
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			if runeValue < 0x20 {
				fmt.Fprintf(output, `\u%04x`, runeValue)
				continue
			}
			if runeValue == utf8.RuneError {
				output.WriteRune(runeValue)
				continue
			}
			output.WriteRune(runeValue)
		}
	}
	output.WriteByte('"')
}
