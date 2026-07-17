package packet

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"relay/internal/operations/registry"
)

const (
	maxPacketInputs             = 64
	maxPacketWorkflowReferences = 32
	maxPacketAttestations       = 128
	maxPacketRepositories       = 64
	maxPacketAnchors            = 128
	maxUploadIndex              = 63
)

var mediaTypePattern = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+(?:;[ -~]+)?$`)

type ValidationError struct {
	Code string
}

func (e *ValidationError) Error() string {
	if e == nil || e.Code == "" {
		return "invalid packet document"
	}
	return "invalid packet document: " + e.Code
}

func invalid(code string) error {
	return &ValidationError{Code: code}
}

func validateAndCanonicalize(input Document) (Document, registry.OperationDefinition, error) {
	if err := registry.Validate(); err != nil {
		return Document{}, registry.OperationDefinition{}, invalid("registry_unavailable")
	}
	if err := validateDocumentUTF8(input); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	operation, ok := registry.Lookup(input.OperationID)
	if !ok {
		return Document{}, registry.OperationDefinition{}, invalid("operation_id")
	}
	if input.SchemaVersion != SchemaVersion {
		return Document{}, registry.OperationDefinition{}, invalid("schema_version")
	}
	if err := validateTimestamp(input.CreatedAt); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	if input.Role != operation.Role {
		return Document{}, registry.OperationDefinition{}, invalid("role")
	}
	if input.SurfaceContract != operation.SurfaceContract {
		return Document{}, registry.OperationDefinition{}, invalid("surface_contract")
	}
	manifestSHA, ok := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	if !ok || input.SurfaceManifestSHA256 != manifestSHA {
		return Document{}, registry.OperationDefinition{}, invalid("surface_manifest_sha256")
	}
	if input.Output.OutputKind != operation.OutputKind || input.Output.OutputPersistence != operation.OutputPersistence {
		return Document{}, registry.OperationDefinition{}, invalid("output_contract")
	}
	if input.ManifestDomain.Domain != operation.ManifestDomain {
		return Document{}, registry.OperationDefinition{}, invalid("manifest_domain")
	}
	if input.SourcePolicy != operation.SourcePolicy {
		return Document{}, registry.OperationDefinition{}, invalid("source_policy")
	}
	if input.HistoricalAuthority != operation.HistoricalAuthority {
		return Document{}, registry.OperationDefinition{}, invalid("historical_authority")
	}
	if operation.PacketSemanticProjection == "" {
		return Document{}, registry.OperationDefinition{}, invalid("semantic_projection")
	}
	if input.ReadinessState != ReadinessReady {
		return Document{}, registry.OperationDefinition{}, invalid("readiness_state")
	}
	if err := validateOpaqueID(input.Project.ProjectID); err != nil {
		return Document{}, registry.OperationDefinition{}, invalid("project_id")
	}
	refreshing := input.PriorPacket != nil
	if refreshing {
		if err := validateOpaqueID(input.PriorPacket.PacketID); err != nil {
			return Document{}, registry.OperationDefinition{}, invalid("prior_packet_id")
		}
		if !validSHA256(input.PriorPacket.PacketSHA256) {
			return Document{}, registry.OperationDefinition{}, invalid("prior_packet_sha256")
		}
	}

	result := cloneDocument(input)
	var err error
	result.WorkflowReferences, err = canonicalWorkflowReferences(input.WorkflowReferences, operation)
	if err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	result.Inputs, err = canonicalInputs(input.Inputs, operation, refreshing)
	if err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	if err := validateWorkflowInputReferences(result.Inputs, result.WorkflowReferences); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	result.Attestations, err = canonicalAttestations(input.Attestations, result.Inputs, operation, refreshing)
	if err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	result.Repositories, err = canonicalRepositories(input.Repositories, operation)
	if err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	if err := validateGovernance(input.RelaySpecs); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	if err := validateManifestDomain(input.ManifestDomain); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	result.ManifestDomain.Members = append([]ManifestMember(nil), input.ManifestDomain.Members...)
	sort.Slice(result.ManifestDomain.Members, func(i, j int) bool {
		return result.ManifestDomain.Members[i].MemberOrder < result.ManifestDomain.Members[j].MemberOrder
	})
	if err := validateAllowedActions(input.AllowedActions, operation); err != nil {
		return Document{}, registry.OperationDefinition{}, err
	}
	result.AllowedActions = append([]registry.AllowedAction(nil), operation.AllowedNonSourceActions...)
	return result, operation, nil
}

func canonicalWorkflowReferences(values []WorkflowReference, operation registry.OperationDefinition) ([]WorkflowReference, error) {
	if len(values) > maxPacketWorkflowReferences {
		return nil, invalid("workflow_reference_count")
	}
	allowed := make(map[registry.WorkflowReferenceKind]struct{}, len(operation.WorkflowReferenceKinds))
	for _, kind := range operation.WorkflowReferenceKinds {
		allowed[kind] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	presentKinds := make(map[registry.WorkflowReferenceKind]bool, len(allowed))
	out := append([]WorkflowReference(nil), values...)
	for _, value := range out {
		if _, ok := allowed[value.Kind]; !ok {
			return nil, invalid("workflow_reference_kind")
		}
		if err := validateWorkflowReference(value); err != nil {
			return nil, err
		}
		key := string(value.Kind) + "\x00" + workflowReferenceKey(value)
		if _, duplicate := seen[key]; duplicate {
			return nil, invalid("workflow_reference_duplicate")
		}
		seen[key] = struct{}{}
		presentKinds[value.Kind] = true
	}
	for _, kind := range operation.WorkflowReferenceKinds {
		if !presentKinds[kind] {
			return nil, invalid("workflow_reference_missing")
		}
	}
	if err := validateWorkflowReferenceRelationships(out); err != nil {
		return nil, err
	}
	rank := make(map[registry.WorkflowReferenceKind]int)
	for index, kind := range registry.WorkflowReferenceRank() {
		rank[kind] = index
	}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Kind] != rank[out[j].Kind] {
			return rank[out[i].Kind] < rank[out[j].Kind]
		}
		return workflowReferenceKey(out[i]) < workflowReferenceKey(out[j])
	})
	return out, nil
}

func validateWorkflowReferenceRelationships(values []WorkflowReference) error {
	planIDs := make(map[string]struct{})
	runIDs := make(map[string]struct{})
	for _, value := range values {
		switch value.Kind {
		case "plan":
			planIDs[value.PlanID] = struct{}{}
		case "run":
			runIDs[value.RunID] = struct{}{}
		}
	}
	for _, value := range values {
		switch value.Kind {
		case "pass":
			if _, ok := planIDs[value.PlanID]; !ok {
				return invalid("workflow_reference_relationship")
			}
		case "audit_packet", "audit_decision":
			if _, ok := runIDs[value.RunID]; !ok {
				return invalid("workflow_reference_relationship")
			}
		}
	}
	return nil
}

func validateWorkflowInputReferences(inputs []InputBinding, references []WorkflowReference) error {
	for _, input := range inputs {
		if input.SourceKind != InputSourceWorkflowRecord {
			continue
		}
		if !workflowInputReferencePresent(input.Source.WorkflowReference, references) {
			return invalid("workflow_record_reference")
		}
	}
	return nil
}

func workflowInputReferencePresent(record WorkflowReference, references []WorkflowReference) bool {
	for _, reference := range references {
		if reference.Kind == record.Kind && reference == record {
			return true
		}
	}
	return false
}

func validateWorkflowReference(value WorkflowReference) error {
	var expected WorkflowReference
	switch value.Kind {
	case "plan":
		if validateOpaqueID(value.PlanID) != nil || validateOpaqueID(value.CanonicalArtifactID) != nil || !validSHA256(value.CanonicalArtifactSHA256) {
			return invalid("workflow_reference_plan")
		}
		expected = WorkflowReference{Kind: value.Kind, PlanID: value.PlanID, CanonicalArtifactID: value.CanonicalArtifactID, CanonicalArtifactSHA256: value.CanonicalArtifactSHA256}
	case "pass":
		if validateOpaqueID(value.PlanID) != nil || validateOpaqueID(value.PassID) != nil || value.PassNumber < 1 {
			return invalid("workflow_reference_pass")
		}
		expected = WorkflowReference{Kind: value.Kind, PlanID: value.PlanID, PassID: value.PassID, PassNumber: value.PassNumber}
	case "run":
		if validateOpaqueID(value.RunID) != nil || validateOpaqueID(value.ExecutionSpecArtifactID) != nil || !validSHA256(value.ExecutionSpecSHA256) {
			return invalid("workflow_reference_run")
		}
		expected = WorkflowReference{Kind: value.Kind, RunID: value.RunID, ExecutionSpecArtifactID: value.ExecutionSpecArtifactID, ExecutionSpecSHA256: value.ExecutionSpecSHA256}
	case "audit_packet":
		if validateOpaqueID(value.RunID) != nil || validateOpaqueID(value.AuditPacketID) != nil || !validSHA256(value.AuditPacketSHA256) {
			return invalid("workflow_reference_audit_packet")
		}
		expected = WorkflowReference{Kind: value.Kind, RunID: value.RunID, AuditPacketID: value.AuditPacketID, AuditPacketSHA256: value.AuditPacketSHA256}
	case "audit_decision":
		if validateOpaqueID(value.RunID) != nil || validateOpaqueID(value.AuditDecisionID) != nil || (value.Decision != "accepted" && value.Decision != "needs_revision") || validateTimestamp(value.RecordedAt) != nil {
			return invalid("workflow_reference_audit_decision")
		}
		expected = WorkflowReference{Kind: value.Kind, RunID: value.RunID, AuditDecisionID: value.AuditDecisionID, Decision: value.Decision, RecordedAt: value.RecordedAt}
	default:
		return invalid("workflow_reference_kind")
	}
	if !reflect.DeepEqual(value, expected) {
		return invalid("workflow_reference_closed")
	}
	return nil
}

func workflowReferenceKey(value WorkflowReference) string {
	switch value.Kind {
	case "plan":
		return value.PlanID
	case "pass":
		return value.PlanID + "\x00" + value.PassID
	case "run":
		return value.RunID
	case "audit_packet":
		return value.RunID + "\x00" + value.AuditPacketID
	case "audit_decision":
		return value.RunID + "\x00" + value.AuditDecisionID
	default:
		return ""
	}
}

func canonicalInputs(values []InputBinding, operation registry.OperationDefinition, refreshing bool) ([]InputBinding, error) {
	if len(values) > maxPacketInputs {
		return nil, invalid("input_count")
	}
	type slotInfo struct {
		definition registry.InputSlotDefinition
		order      int
		derived    bool
		required   bool
	}
	slots := make(map[string]slotInfo)
	order := 0
	for _, slot := range operation.RequiredInputs {
		slots[slot.InputName] = slotInfo{definition: slot, order: order, required: true}
		order++
	}
	if refreshing {
		if operation.Role != "planner" && len(operation.ConditionalRefreshInputs) != 0 {
			return nil, invalid("refresh_operation")
		}
		for _, slot := range operation.ConditionalRefreshInputs {
			slots[slot.InputName] = slotInfo{definition: slot, order: order, required: true}
			order++
		}
	}
	for _, slot := range operation.DerivedInputs {
		slots[slot.InputName] = slotInfo{definition: slot, order: order, derived: true, required: true}
		order++
	}
	seen := make(map[string]struct{}, len(values))
	seenFileIndexes := make(map[int64]struct{})
	out := append([]InputBinding(nil), values...)
	for index := range out {
		value := &out[index]
		info, ok := slots[value.InputName]
		if !ok {
			return nil, invalid("input_name")
		}
		if _, duplicate := seen[value.InputName]; duplicate {
			return nil, invalid("input_duplicate")
		}
		seen[value.InputName] = struct{}{}
		if value.InputRole != info.definition.InputRole || value.AttestationKind != info.definition.AttestationKind {
			return nil, invalid("input_authority")
		}
		if validateDisplayName(value.DisplayName) != nil || validateMediaType(value.MediaType) != nil || !validSHA256(value.SHA256) || value.SizeBytes < 0 {
			return nil, invalid("input_identity")
		}
		if value.Source.Kind != value.SourceKind || !knownInputSource(value.SourceKind) {
			return nil, invalid("input_source_kind")
		}
		if !info.derived && !containsSourceKind(info.definition.AllowedSourceKinds, value.SourceKind) {
			return nil, invalid("input_source_not_allowed")
		}
		if err := validateInputSource(value.Source); err != nil {
			return nil, err
		}
		if value.SourceKind == InputSourceUploadedFile {
			if _, duplicate := seenFileIndexes[value.Source.FileIndex]; duplicate {
				return nil, invalid("input_file_index_duplicate")
			}
			seenFileIndexes[value.Source.FileIndex] = struct{}{}
		}
	}
	for name, info := range slots {
		if info.required {
			if _, ok := seen[name]; !ok {
				return nil, invalid("input_missing")
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return slots[out[i].InputName].order < slots[out[j].InputName].order
	})
	return out, nil
}

func canonicalAttestations(values []Attestation, inputs []InputBinding, operation registry.OperationDefinition, refreshing bool) ([]Attestation, error) {
	if len(values) > maxPacketAttestations {
		return nil, invalid("attestation_count")
	}
	slotOrder := make(map[string]int)
	slotKind := make(map[string]registry.AttestationKind)
	derived := make(map[string]bool)
	order := 0
	for _, slot := range operation.RequiredInputs {
		slotOrder[slot.InputName] = order
		slotKind[slot.InputName] = slot.AttestationKind
		order++
	}
	if refreshing {
		for _, slot := range operation.ConditionalRefreshInputs {
			slotOrder[slot.InputName] = order
			slotKind[slot.InputName] = slot.AttestationKind
			order++
		}
	}
	for _, slot := range operation.DerivedInputs {
		slotOrder[slot.InputName] = order
		slotKind[slot.InputName] = slot.AttestationKind
		derived[slot.InputName] = true
		order++
	}
	inputByName := make(map[string]InputBinding, len(inputs))
	for _, input := range inputs {
		inputByName[input.InputName] = input
	}
	seen := make(map[string]struct{}, len(values))
	matched := make(map[string]bool)
	clearanceMatched := make(map[string]bool)
	attestationByInputAndKind := make(map[string]Attestation, len(values))
	out := append([]Attestation(nil), values...)
	for _, value := range out {
		input, ok := inputByName[value.InputName]
		if !ok {
			return nil, invalid("attestation_input")
		}
		if derived[value.InputName] {
			return nil, invalid("attestation_derived_input")
		}
		if value.Kind != slotKind[value.InputName] && value.Kind != "sensitive_data_clearance" {
			return nil, invalid("attestation_kind")
		}
		key := value.InputName + "\x00" + string(value.Kind)
		if _, duplicate := seen[key]; duplicate {
			return nil, invalid("attestation_duplicate")
		}
		seen[key] = struct{}{}
		if err := validateAttestation(value); err != nil {
			return nil, err
		}
		attestationByInputAndKind[key] = value
		if value.Kind == slotKind[value.InputName] {
			if attestationHasSubject(value.Kind) && value.SubjectSHA256 != input.SHA256 {
				return nil, invalid("attestation_subject_sha256")
			}
			matched[value.InputName] = true
		}
		if value.Kind == "sensitive_data_clearance" {
			if !requiresSensitiveClearance(input.SourceKind) {
				return nil, invalid("attestation_sensitive_data_source")
			}
			if value.Clearance.SubjectSHA256 != input.SHA256 {
				return nil, invalid("attestation_sensitive_data_subject")
			}
			clearanceMatched[value.InputName] = true
		}
	}
	for _, input := range inputs {
		if derived[input.InputName] {
			continue
		}
		if !matched[input.InputName] {
			return nil, invalid("attestation_missing")
		}
		if requiresSensitiveClearance(input.SourceKind) && !clearanceMatched[input.InputName] {
			return nil, invalid("attestation_sensitive_data_missing")
		}
		if input.SourceKind == InputSourceCommittedSource && clearanceMatched[input.InputName] {
			return nil, invalid("attestation_sensitive_data_source")
		}
	}
	if refreshing && operation.Role == "planner" && len(operation.ConditionalRefreshInputs) != 0 {
		reviewed, reviewedOK := inputByName["reviewed_candidate"]
		reviewResultInput, resultOK := inputByName["auditor_review_result"]
		if !reviewedOK || !resultOK {
			return nil, invalid("refresh_inputs")
		}
		reviewAttestation, ok := attestationByInputAndKind["auditor_review_result\x00complete_review_result"]
		if !ok || reviewAttestation.ReviewResult != "needs_revision" || !reviewAttestation.Complete {
			return nil, invalid("refresh_review_result")
		}
		if reviewAttestation.SubjectSHA256 != reviewResultInput.SHA256 || reviewAttestation.ReviewedCandidateSHA256 != reviewed.SHA256 {
			return nil, invalid("refresh_review_identity")
		}
	}
	rank := make(map[registry.AttestationKind]int)
	for index, kind := range registry.AttestationRank() {
		rank[kind] = index
	}
	sort.Slice(out, func(i, j int) bool {
		if slotOrder[out[i].InputName] != slotOrder[out[j].InputName] {
			return slotOrder[out[i].InputName] < slotOrder[out[j].InputName]
		}
		return rank[out[i].Kind] < rank[out[j].Kind]
	})
	return out, nil
}

func validateAttestation(value Attestation) error {
	if validateSlotName(value.InputName) != nil {
		return invalid("attestation_input_name")
	}
	var expected Attestation
	switch value.Kind {
	case "confirmed_intent":
		if !validSHA256(value.SubjectSHA256) || !value.Confirmed {
			return invalid("attestation_confirmed_intent")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Confirmed: true}
	case "approved_artifact":
		if !validSHA256(value.SubjectSHA256) || !value.Approved {
			return invalid("attestation_approved_artifact")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Approved: true}
	case "candidate_for_review":
		if !validSHA256(value.SubjectSHA256) || !value.CompleteTransfer {
			return invalid("attestation_candidate_for_review")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, CompleteTransfer: true}
	case "execution_mode_selection":
		if value.SelectedMode != "plan" && value.SelectedMode != "one_shot" {
			return invalid("attestation_execution_mode")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SelectedMode: value.SelectedMode}
	case "complete_review_result":
		if !validSHA256(value.SubjectSHA256) || !validSHA256(value.ReviewedCandidateSHA256) || (value.ReviewResult != "ready_for_approval" && value.ReviewResult != "needs_revision") || !value.Complete {
			return invalid("attestation_complete_review")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, ReviewedCandidateSHA256: value.ReviewedCandidateSHA256, ReviewResult: value.ReviewResult, Complete: true}
	case "completed_dependency_outcomes", "exact_evidence":
		if !validSHA256(value.SubjectSHA256) || !value.Complete {
			return invalid("attestation_exact_subject")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Complete: true}
	case "operator_confirmation", "separate_session_authorship":
		if !value.Confirmed {
			return invalid("attestation_confirmation")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, Confirmed: true}
	case "sensitive_data_clearance":
		if value.Clearance == nil {
			return invalid("attestation_sensitive_data_clearance")
		}
		if err := registry.ValidateSensitiveDataClearance(*value.Clearance); err != nil {
			if err == registry.ErrSensitiveDataDeclaration {
				return invalid("attestation_sensitive_data_declaration")
			}
			return invalid("attestation_sensitive_data_clearance")
		}
		expected = Attestation{Kind: value.Kind, InputName: value.InputName, Clearance: value.Clearance}
	default:
		return invalid("attestation_kind")
	}
	if !reflect.DeepEqual(value, expected) {
		return invalid("attestation_closed")
	}
	return nil
}

func attestationHasSubject(kind registry.AttestationKind) bool {
	switch kind {
	case "confirmed_intent", "approved_artifact", "candidate_for_review", "complete_review_result", "completed_dependency_outcomes", "exact_evidence":
		return true
	default:
		return false
	}
}

func requiresSensitiveClearance(kind registry.InputSourceKind) bool {
	switch kind {
	case InputSourceUploadedFile, InputSourceRelayArtifact, InputSourceInlineText, InputSourceWorkflowRecord:
		return true
	default:
		return false
	}
}

func canonicalRepositories(values []RepositoryBinding, operation registry.OperationDefinition) ([]RepositoryBinding, error) {
	if len(values) == 0 || len(values) > maxPacketRepositories {
		return nil, invalid("repository_count")
	}
	out := cloneRepositories(values)
	seenKeys := make(map[string]struct{}, len(out))
	allowedPurposes := make(map[registry.AnchorPurpose]struct{}, len(operation.ComparisonAnchorPurposes)+4)
	for _, purpose := range operation.ComparisonAnchorPurposes {
		allowedPurposes[purpose] = struct{}{}
	}
	requiredPurposes, derivedAllowed, err := historicalAnchorPolicy(operation.HistoricalAuthority)
	if err != nil {
		return nil, err
	}
	for _, purpose := range derivedAllowed {
		allowedPurposes[purpose] = struct{}{}
	}
	presentPurposes := make(map[registry.AnchorPurpose]int)
	totalAnchors := 0
	for index := range out {
		value := &out[index]
		if validateRepositoryKey(value.RepositoryKey) != nil || validateOpaqueID(value.RepositoryTarget) != nil || value.BindingOrder < 1 || value.RepositoryTargetConfigurationVersion < 1 || !validGitOID(value.CommitOID) || !validGitOID(value.TreeOID) {
			return nil, invalid("repository_binding")
		}
		if _, duplicate := seenKeys[value.RepositoryKey]; duplicate {
			return nil, invalid("repository_duplicate")
		}
		seenKeys[value.RepositoryKey] = struct{}{}
		if err := validateRevision(value.RevisionSource, value.ConfiguredWorkingBranchRef); err != nil {
			return nil, err
		}
		seenAnchors := make(map[string]struct{}, len(value.Anchors))
		totalAnchors += len(value.Anchors)
		if totalAnchors > maxPacketAnchors {
			return nil, invalid("repository_anchor_count")
		}
		for _, anchor := range value.Anchors {
			if validateAnchorName(anchor.AnchorName) != nil || !validGitOID(anchor.CommitOID) || !validGitOID(anchor.TreeOID) {
				return nil, invalid("repository_anchor")
			}
			if _, ok := allowedPurposes[anchor.Purpose]; !ok {
				return nil, invalid("repository_anchor_purpose")
			}
			if _, duplicate := seenAnchors[anchor.AnchorName]; duplicate {
				return nil, invalid("repository_anchor_duplicate")
			}
			seenAnchors[anchor.AnchorName] = struct{}{}
			presentPurposes[anchor.Purpose]++
		}
		sort.Slice(value.Anchors, func(i, j int) bool { return value.Anchors[i].AnchorName < value.Anchors[j].AnchorName })
	}
	for _, purpose := range requiredPurposes {
		if presentPurposes[purpose] == 0 {
			return nil, invalid("repository_anchor_missing")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepositoryKey < out[j].RepositoryKey })
	return out, nil
}

func historicalAnchorPolicy(policy registry.HistoricalAuthorityPolicy) ([]registry.AnchorPurpose, []registry.AnchorPurpose, error) {
	switch policy {
	case "none", "explicit_comparison_anchors":
		return nil, nil, nil
	case "plan_and_completed_dependency_anchors":
		return []registry.AnchorPurpose{"plan_base"}, []registry.AnchorPurpose{"plan_base", "completed_dependency"}, nil
	case "reviewed_commits", "reviewed_source_basis", "candidate_base_anchor":
		return []registry.AnchorPurpose{"reviewed_source_basis"}, []registry.AnchorPurpose{"reviewed_source_basis"}, nil
	case "candidate_plan_and_dependency_anchors":
		return []registry.AnchorPurpose{"reviewed_source_basis", "plan_base"}, []registry.AnchorPurpose{"reviewed_source_basis", "plan_base", "completed_dependency"}, nil
	case "run_base_and_audited_commit", "audited_and_run_base_anchors":
		return []registry.AnchorPurpose{"run_base", "audited_commit"}, []registry.AnchorPurpose{"run_base", "audited_commit"}, nil
	case "candidate_audited_and_run_base_anchors":
		return []registry.AnchorPurpose{"reviewed_source_basis", "run_base", "audited_commit"}, []registry.AnchorPurpose{"reviewed_source_basis", "run_base", "audited_commit"}, nil
	default:
		return nil, nil, invalid("historical_authority")
	}
}

func validateGovernance(value GovernanceBinding) error {
	if validateRepositoryKey(value.RepositoryKey) != nil || validateOpaqueID(value.RepositoryTarget) != nil || !value.Reserved || value.RepositoryTargetConfigurationVersion < 1 || !validGitOID(value.CommitOID) || !validGitOID(value.TreeOID) {
		return invalid("governance_binding")
	}
	return validateRevision(value.RevisionSource, value.ConfiguredWorkingBranchRef)
}

func validateManifestDomain(value ManifestDomainBinding) error {
	if validatePathIdentity(value.ManifestPath) != nil || !validGitOID(value.ManifestBlobOID) || !validSHA256(value.ManifestSHA256) || value.Domain == "" || len(value.Members) == 0 {
		return invalid("manifest_domain_binding")
	}
	seenPath := make(map[string]struct{}, len(value.Members))
	seenOrder := make(map[int64]struct{}, len(value.Members))
	for _, member := range value.Members {
		if member.MemberOrder < 1 || member.ByteSize < 0 || validatePathIdentity(member.Path) != nil || !validGitOID(member.BlobOID) || !validSHA256(member.SHA256) {
			return invalid("manifest_member")
		}
		if _, duplicate := seenPath[member.Path.PathID]; duplicate {
			return invalid("manifest_member_duplicate")
		}
		if _, duplicate := seenOrder[member.MemberOrder]; duplicate {
			return invalid("manifest_member_order_duplicate")
		}
		seenPath[member.Path.PathID] = struct{}{}
		seenOrder[member.MemberOrder] = struct{}{}
	}
	for order := int64(1); order <= int64(len(value.Members)); order++ {
		if _, ok := seenOrder[order]; !ok {
			return invalid("manifest_member_order")
		}
	}
	return nil
}

func validateAllowedActions(values []registry.AllowedAction, operation registry.OperationDefinition) error {
	if len(values) != len(operation.AllowedNonSourceActions) {
		return invalid("allowed_actions")
	}
	seen := make(map[registry.AllowedAction]struct{}, len(values))
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			return invalid("allowed_actions_duplicate")
		}
		seen[value] = struct{}{}
	}
	for _, value := range operation.AllowedNonSourceActions {
		if _, ok := seen[value]; !ok {
			return invalid("allowed_actions")
		}
	}
	return nil
}

func validateInputSource(value InputSource) error {
	var expected InputSource
	switch value.Kind {
	case InputSourceUploadedFile:
		if value.FileIndex < 0 || value.FileIndex > maxUploadIndex || validateOpaqueID(value.ArtifactID) != nil {
			return invalid("input_source_uploaded_file")
		}
		expected = InputSource{Kind: value.Kind, FileIndex: value.FileIndex, ArtifactID: value.ArtifactID}
	case InputSourceRelayArtifact, InputSourceInlineText:
		if validateOpaqueID(value.ArtifactID) != nil {
			return invalid("input_source_artifact")
		}
		expected = InputSource{Kind: value.Kind, ArtifactID: value.ArtifactID}
	case InputSourceWorkflowRecord:
		if validateWorkflowReference(value.WorkflowReference) != nil || validateOpaqueID(value.SnapshotArtifactID) != nil || !validSHA256(value.SnapshotSHA256) {
			return invalid("input_source_workflow_record")
		}
		expected = InputSource{Kind: value.Kind, WorkflowReference: value.WorkflowReference, SnapshotArtifactID: value.SnapshotArtifactID, SnapshotSHA256: value.SnapshotSHA256}
	case InputSourceCommittedSource:
		if validateOpaqueID(value.RepositoryBindingID) != nil || !validGitOID(value.CommitOID) || !validGitOID(value.TreeOID) || validatePathIdentity(value.Path) != nil || !validGitOID(value.BlobOID) {
			return invalid("input_source_committed_source")
		}
		expected = InputSource{Kind: value.Kind, RepositoryBindingID: value.RepositoryBindingID, CommitOID: value.CommitOID, TreeOID: value.TreeOID, Path: value.Path, BlobOID: value.BlobOID}
	default:
		return invalid("input_source_kind")
	}
	if !reflect.DeepEqual(value, expected) {
		return invalid("input_source_closed")
	}
	return nil
}

func validatePathIdentity(value PathIdentity) error {
	if !validSHA256(value.PathID) || value.ByteLength < 0 {
		return invalid("path_identity")
	}
	if value.ByteLength <= 8192 {
		if value.ByteLength > 0 && value.PathBytesBase64 == "" {
			return invalid("path_bytes_missing")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(value.PathBytesBase64)
		if err != nil || int64(len(decoded)) != value.ByteLength || base64.StdEncoding.EncodeToString(decoded) != value.PathBytesBase64 {
			return invalid("path_bytes_base64")
		}
		for _, value := range decoded {
			if value == 0 {
				return invalid("path_bytes_nul")
			}
		}
		digest := sha256.New()
		_, _ = digest.Write([]byte("relay.git-path.v1"))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(decoded)
		if hex.EncodeToString(digest.Sum(nil)) != value.PathID {
			return invalid("path_id_mismatch")
		}
	} else if value.PathBytesBase64 != "" {
		return invalid("path_bytes_oversize")
	}
	return nil
}

func validateRevision(source, ref string) error {
	if !utf8.ValidString(source) || !utf8.ValidString(ref) {
		return invalid("utf8")
	}
	switch source {
	case RevisionSourceExplicitCommit:
		if ref != "" {
			return invalid("configured_working_branch_ref")
		}
	case RevisionSourceConfiguredWorkingBranch:
		if !strings.HasPrefix(ref, "refs/heads/") || len(ref) < 12 || len(ref) > 4096 {
			return invalid("configured_working_branch_ref")
		}
	default:
		return invalid("revision_source")
	}
	return nil
}

func validateTimestamp(value string) error {
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format("2006-01-02T15:04:05.000000000Z") != value {
		return invalid("timestamp")
	}
	return nil
}

func validateOpaqueID(value string) error {
	if value == "" || utf8.RuneCountInString(value) > 255 || !utf8.ValidString(value) {
		return invalid("opaque_id")
	}
	return nil
}

func validateDisplayName(value string) error {
	if value == "" || utf8.RuneCountInString(value) > 1024 || !utf8.ValidString(value) {
		return invalid("display_name")
	}
	return nil
}

func validateMediaType(value string) error {
	if value == "" || len(value) > 255 || !utf8.ValidString(value) || !mediaTypePattern.MatchString(value) {
		return invalid("media_type")
	}
	return nil
}

func validateSlotName(value string) error {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) || value[0] < 'a' || value[0] > 'z' {
		return invalid("slot_name")
	}
	for _, char := range value[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return invalid("slot_name")
		}
	}
	return nil
}

func validateRepositoryKey(value string) error {
	if value == "" || len(value) > 255 || !utf8.ValidString(value) || strings.ContainsAny(value, "/\\") {
		return invalid("repository_key")
	}
	for _, char := range value {
		if char <= 0x20 || char == 0x7f {
			return invalid("repository_key")
		}
	}
	return nil
}

func validateAnchorName(value string) error {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return invalid("anchor_name")
	}
	for index, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || (index > 0 && (char == '.' || char == '_' || char == '-')) {
			continue
		}
		return invalid("anchor_name")
	}
	return nil
}

func validSHA256(value string) bool {
	return len(value) == 64 && validLowerHex(value)
}

func validGitOID(value string) bool {
	return len(value) >= 40 && len(value) <= 64 && validLowerHex(value)
}

func validLowerHex(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func knownInputSource(value registry.InputSourceKind) bool {
	switch value {
	case InputSourceUploadedFile, InputSourceRelayArtifact, InputSourceInlineText, InputSourceWorkflowRecord, InputSourceCommittedSource:
		return true
	default:
		return false
	}
}

func containsSourceKind(values []registry.InputSourceKind, target registry.InputSourceKind) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateDocumentUTF8(value Document) error {
	stringsToCheck := []string{
		value.SchemaVersion, value.CreatedAt, string(value.Role), string(value.OperationID), string(value.SurfaceContract), value.SurfaceManifestSHA256,
		value.Output.OutputKind, value.Output.OutputPersistence, value.Project.ProjectID, string(value.ManifestDomain.Domain), string(value.SourcePolicy), string(value.HistoricalAuthority), value.ReadinessState,
		value.RelaySpecs.RepositoryKey, value.RelaySpecs.RepositoryTarget, value.RelaySpecs.RevisionSource, value.RelaySpecs.ConfiguredWorkingBranchRef, value.RelaySpecs.CommitOID, value.RelaySpecs.TreeOID,
		value.ManifestDomain.ManifestPath.PathID, value.ManifestDomain.ManifestPath.PathBytesBase64, value.ManifestDomain.ManifestBlobOID, value.ManifestDomain.ManifestSHA256,
	}
	if value.PriorPacket != nil {
		stringsToCheck = append(stringsToCheck, value.PriorPacket.PacketID, value.PriorPacket.PacketSHA256)
	}
	for _, reference := range value.WorkflowReferences {
		stringsToCheck = append(stringsToCheck, string(reference.Kind), reference.PlanID, reference.CanonicalArtifactID, reference.CanonicalArtifactSHA256, reference.PassID, reference.RunID, reference.ExecutionSpecArtifactID, reference.ExecutionSpecSHA256, reference.AuditPacketID, reference.AuditPacketSHA256, reference.AuditDecisionID, reference.Decision, reference.RecordedAt)
	}
	for _, attestation := range value.Attestations {
		stringsToCheck = append(stringsToCheck, string(attestation.Kind), attestation.InputName, attestation.SubjectSHA256, attestation.SelectedMode, attestation.ReviewedCandidateSHA256, attestation.ReviewResult)
		if attestation.Clearance != nil {
			stringsToCheck = append(stringsToCheck, attestation.Clearance.PolicyVersion, attestation.Clearance.SubjectSHA256)
		}
	}
	for _, input := range value.Inputs {
		stringsToCheck = append(stringsToCheck, input.InputName, string(input.InputRole), string(input.SourceKind), input.DisplayName, input.MediaType, input.SHA256, string(input.AttestationKind), string(input.Source.Kind), input.Source.ArtifactID, input.Source.SnapshotArtifactID, input.Source.SnapshotSHA256, input.Source.RepositoryBindingID, input.Source.CommitOID, input.Source.TreeOID, input.Source.Path.PathID, input.Source.Path.PathBytesBase64, input.Source.BlobOID)
		reference := input.Source.WorkflowReference
		stringsToCheck = append(stringsToCheck, string(reference.Kind), reference.PlanID, reference.CanonicalArtifactID, reference.CanonicalArtifactSHA256, reference.PassID, reference.RunID, reference.ExecutionSpecArtifactID, reference.ExecutionSpecSHA256, reference.AuditPacketID, reference.AuditPacketSHA256, reference.AuditDecisionID, reference.Decision, reference.RecordedAt)
	}
	for _, repository := range value.Repositories {
		stringsToCheck = append(stringsToCheck, repository.RepositoryKey, repository.RepositoryTarget, repository.RevisionSource, repository.ConfiguredWorkingBranchRef, repository.CommitOID, repository.TreeOID)
		for _, anchor := range repository.Anchors {
			stringsToCheck = append(stringsToCheck, anchor.AnchorName, string(anchor.Purpose), anchor.CommitOID, anchor.TreeOID)
		}
	}
	for _, member := range value.ManifestDomain.Members {
		stringsToCheck = append(stringsToCheck, member.Path.PathID, member.Path.PathBytesBase64, member.BlobOID, member.SHA256)
	}
	for _, action := range value.AllowedActions {
		stringsToCheck = append(stringsToCheck, string(action))
	}
	for _, item := range stringsToCheck {
		if !utf8.ValidString(item) {
			return invalid("utf8")
		}
	}
	return nil
}

func cloneDocument(value Document) Document {
	out := value
	if value.PriorPacket != nil {
		prior := *value.PriorPacket
		out.PriorPacket = &prior
	}
	out.WorkflowReferences = append([]WorkflowReference(nil), value.WorkflowReferences...)
	out.Attestations = append([]Attestation(nil), value.Attestations...)
	for index := range out.Attestations {
		if value.Attestations[index].Clearance != nil {
			clearance := *value.Attestations[index].Clearance
			out.Attestations[index].Clearance = &clearance
		}
	}
	out.Inputs = append([]InputBinding(nil), value.Inputs...)
	out.Repositories = cloneRepositories(value.Repositories)
	out.ManifestDomain.Members = append([]ManifestMember(nil), value.ManifestDomain.Members...)
	out.AllowedActions = append([]registry.AllowedAction(nil), value.AllowedActions...)
	return out
}

func cloneRepositories(values []RepositoryBinding) []RepositoryBinding {
	out := append([]RepositoryBinding(nil), values...)
	for index := range out {
		out[index].Anchors = append([]Anchor(nil), values[index].Anchors...)
	}
	return out
}

func formatValidationError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
