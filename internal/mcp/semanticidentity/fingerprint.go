package semanticidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"

	"relay/internal/operations/registry"
)

const MaxSemanticIdentityBytes = 32 * 1024 * 1024

var (
	ErrInvalidRequestIdentity = errors.New("invalid semantic request identity")
	slotNamePattern           = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
	anchorNamePattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

func BuildFingerprint(identity RequestIdentity) (Fingerprint, error) {
	if identity == nil {
		return Fingerprint{}, ErrInvalidRequestIdentity
	}
	surface := identity.SurfaceContractID()
	tool := identity.MutationTool()
	if !registry.IsStateChangingToolForSurface(surface, string(tool)) {
		return Fingerprint{}, ErrInvalidRequestIdentity
	}
	version, ok := registry.SemanticProjectionVersion(string(tool))
	if !ok || version == "" || identity.SemanticIdentityVersion() != version {
		return Fingerprint{}, ErrInvalidRequestIdentity
	}
	if err := validateRequestIdentity(identity); err != nil {
		return Fingerprint{}, ErrInvalidRequestIdentity
	}
	raw, err := json.Marshal(identity)
	if err != nil || len(raw) == 0 || len(raw) > MaxSemanticIdentityBytes || !json.Valid(raw) {
		return Fingerprint{}, ErrInvalidRequestIdentity
	}
	sum := sha256.Sum256(raw)
	return Fingerprint{
		surfaceContractID:       surface,
		tool:                    tool,
		semanticIdentityVersion: version,
		semanticRequestSHA256:   hex.EncodeToString(sum[:]),
	}, nil
}

func validateRequestIdentity(identity RequestIdentity) error {
	switch value := identity.(type) {
	case CreateOperationPacket:
		return validateCreateOperationPacket(value)
	case RefreshOperationPacket:
		return validateRefreshOperationPacket(value)
	case CloseOperationPacket:
		return validateCloseOperationPacket(value)
	case SubmitPlan:
		if value.MutationTool() != registry.MutationToolSubmitPlan {
			return ErrInvalidRequestIdentity
		}
		return validateCanonicalArtifactMutation(value.CanonicalArtifactMutation)
	case CreateRun:
		if value.MutationTool() != registry.MutationToolCreateRun {
			return ErrInvalidRequestIdentity
		}
		return validateCanonicalArtifactMutation(value.CanonicalArtifactMutation)
	case RecordAuditDecision:
		return validateRecordAuditDecision(value)
	default:
		return ErrInvalidRequestIdentity
	}
}

func validateCreateOperationPacket(value CreateOperationPacket) error {
	operation, ok := registry.Lookup(value.OperationID)
	if !ok || operation.SurfaceContract != value.SurfaceContract || !validOpaque(value.ProjectID) {
		return ErrInvalidRequestIdentity
	}
	return validatePacketRequest(
		value.SurfaceContract,
		value.InputFileCount,
		value.DeclaredFiles,
		value.Inputs,
		value.WorkflowReferences,
		value.Attestations,
		value.PrimaryRevisions,
		value.ComparisonAnchors,
		value.RelaySpecsRevision,
	)
}

func validateRefreshOperationPacket(value RefreshOperationPacket) error {
	if !validOpaque(value.ExpectedPacketID) {
		return ErrInvalidRequestIdentity
	}
	return validatePacketRequest(
		value.SurfaceContract,
		value.InputFileCount,
		value.DeclaredFiles,
		value.Inputs,
		value.WorkflowReferences,
		value.Attestations,
		value.PrimaryRevisions,
		value.ComparisonAnchors,
		value.RelaySpecsRevision,
	)
}

func validatePacketRequest(
	surface registry.SurfaceContractID,
	inputFileCount int,
	declaredFiles []DeclaredFile,
	inputs []InputBinding,
	workflowReferences []WorkflowReferenceRequest,
	attestations []AttestationRequest,
	primaryRevisions []PrimaryRevisionRequest,
	comparisonAnchors []ComparisonAnchorRequest,
	relaySpecsRevision string,
) error {
	if surface == "" || inputFileCount < 0 || inputFileCount > 64 || len(declaredFiles) != inputFileCount {
		return ErrInvalidRequestIdentity
	}
	if len(inputs) > 64 || len(workflowReferences) > 32 || len(attestations) > 128 || len(primaryRevisions) > 64 || len(comparisonAnchors) > 128 {
		return ErrInvalidRequestIdentity
	}
	if relaySpecsRevision != "" && !validGitOID(relaySpecsRevision) {
		return ErrInvalidRequestIdentity
	}
	seenFiles := make(map[int64]struct{}, len(declaredFiles))
	for _, value := range declaredFiles {
		if value.FileIndex < 0 || value.FileIndex > 63 || !validSHA256(value.ExpectedSHA256) {
			return ErrInvalidRequestIdentity
		}
		if _, duplicate := seenFiles[value.FileIndex]; duplicate {
			return ErrInvalidRequestIdentity
		}
		seenFiles[value.FileIndex] = struct{}{}
	}
	for _, value := range inputs {
		if err := validateInputBinding(value); err != nil {
			return err
		}
	}
	for _, value := range workflowReferences {
		if err := validateWorkflowReference(value); err != nil {
			return err
		}
	}
	for _, value := range attestations {
		if err := validateAttestation(value); err != nil {
			return err
		}
	}
	for _, value := range primaryRevisions {
		if !validRepositoryKey(value.RepositoryKey) || !validGitOID(value.CommitOID) {
			return ErrInvalidRequestIdentity
		}
	}
	for _, value := range comparisonAnchors {
		if !validRepositoryKey(value.RepositoryKey) || !anchorNamePattern.MatchString(value.AnchorName) || !validAnchorPurpose(value.Purpose) || !validGitOID(value.CommitOID) || !validGitOID(value.ExpectedTreeOID) {
			return ErrInvalidRequestIdentity
		}
	}
	return nil
}

func validateCloseOperationPacket(value CloseOperationPacket) error {
	if !validOpaque(value.ExpectedPacketID) {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validateCanonicalArtifactMutation(value CanonicalArtifactMutation) error {
	if !validOpaque(value.ExpectedPacketID) || !validBounded(value.ArtifactName, 1024) || !validBounded(value.MediaType, 255) || !validSHA256(value.ExpectedSHA256) {
		return ErrInvalidRequestIdentity
	}
	if err := registry.ValidateSensitiveDataClearance(value.SensitiveDataClearance); err != nil || value.SensitiveDataClearance.SubjectSHA256 != value.ExpectedSHA256 {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validateRecordAuditDecision(value RecordAuditDecision) error {
	if !validOpaque(value.ExpectedPacketID) || !validOpaque(value.RunID) || !validOpaque(value.AuditPacketID) || !validSHA256(value.ExpectedAuditPacketSHA256) || !validGitOID(value.AuditedCommitOID) {
		return ErrInvalidRequestIdentity
	}
	switch value.WorkflowScope.Kind {
	case "one_shot":
		if value.WorkflowScope.PlanID != "" || value.WorkflowScope.PassID != "" {
			return ErrInvalidRequestIdentity
		}
	case "selected_pass":
		if !validOpaque(value.WorkflowScope.PlanID) || !validOpaque(value.WorkflowScope.PassID) {
			return ErrInvalidRequestIdentity
		}
	default:
		return ErrInvalidRequestIdentity
	}
	if value.Decision != "accepted" && value.Decision != "needs_revision" {
		return ErrInvalidRequestIdentity
	}
	if !validBounded(value.Rationale, 262144) || len(value.MaterialFindings) > 128 || len(value.NonBlockingObservations) > 128 || !value.OperatorConfirmed {
		return ErrInvalidRequestIdentity
	}
	for _, finding := range value.MaterialFindings {
		if !validBounded(finding.Source, 4096) || !validBounded(finding.Location, 4096) || !validBounded(finding.Summary, 4096) || !validBounded(finding.Evidence, 262144) || !validBounded(finding.RequiredRevision, 262144) {
			return ErrInvalidRequestIdentity
		}
	}
	for _, observation := range value.NonBlockingObservations {
		if !validBounded(observation.Summary, 4096) || !validBounded(observation.Evidence, 262144) {
			return ErrInvalidRequestIdentity
		}
	}
	return nil
}

func validateInputBinding(value InputBinding) error {
	if !slotNamePattern.MatchString(value.InputName) || !validBounded(value.DisplayName, 1024) || !validBounded(value.MediaType, 255) || !validSHA256(value.ExpectedSHA256) {
		return ErrInvalidRequestIdentity
	}
	source := value.Source
	var expected InputBindingSource
	switch value.SourceKind {
	case "uploaded_file":
		if source.FileIndex == nil || *source.FileIndex < 0 || *source.FileIndex > 63 {
			return ErrInvalidRequestIdentity
		}
		index := *source.FileIndex
		expected.FileIndex = &index
	case "relay_artifact":
		if !validOpaque(source.ArtifactID) {
			return ErrInvalidRequestIdentity
		}
		expected.ArtifactID = source.ArtifactID
	case "inline_text":
		if !validBounded(source.Text, 262144) {
			return ErrInvalidRequestIdentity
		}
		expected.Text = source.Text
	case "workflow_record":
		if source.WorkflowRecord == nil || validateWorkflowRecord(*source.WorkflowRecord) != nil {
			return ErrInvalidRequestIdentity
		}
		copy := *source.WorkflowRecord
		expected.WorkflowRecord = &copy
	case "committed_source":
		if !validRepositoryKey(source.RepositoryKey) || !validRevision(source.Revision) || source.Path == nil || validateSourcePath(*source.Path) != nil || !validGitOID(source.ExpectedBlobOID) {
			return ErrInvalidRequestIdentity
		}
		copy := *source.Path
		expected.RepositoryKey = source.RepositoryKey
		expected.Revision = source.Revision
		expected.Path = &copy
		expected.ExpectedBlobOID = source.ExpectedBlobOID
	default:
		return ErrInvalidRequestIdentity
	}
	if !reflect.DeepEqual(source, expected) {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validateWorkflowRecord(value WorkflowRecordInputReference) error {
	var expected WorkflowRecordInputReference
	switch value.Kind {
	case "plan_artifact":
		if !validOpaque(value.PlanID) || !validOpaque(value.ArtifactID) || !validSHA256(value.ExpectedSHA256) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowRecordInputReference{Kind: value.Kind, PlanID: value.PlanID, ArtifactID: value.ArtifactID, ExpectedSHA256: value.ExpectedSHA256}
	case "pass_record":
		if !validOpaque(value.PlanID) || !validOpaque(value.PassID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowRecordInputReference{Kind: value.Kind, PlanID: value.PlanID, PassID: value.PassID}
	case "run_execution_spec":
		if !validOpaque(value.RunID) || !validOpaque(value.ArtifactID) || !validSHA256(value.ExpectedSHA256) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowRecordInputReference{Kind: value.Kind, RunID: value.RunID, ArtifactID: value.ArtifactID, ExpectedSHA256: value.ExpectedSHA256}
	case "audit_packet":
		if !validOpaque(value.RunID) || !validOpaque(value.AuditPacketID) || !validSHA256(value.ExpectedSHA256) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowRecordInputReference{Kind: value.Kind, RunID: value.RunID, AuditPacketID: value.AuditPacketID, ExpectedSHA256: value.ExpectedSHA256}
	case "audit_decision":
		if !validOpaque(value.RunID) || !validOpaque(value.AuditDecisionID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowRecordInputReference{Kind: value.Kind, RunID: value.RunID, AuditDecisionID: value.AuditDecisionID}
	default:
		return ErrInvalidRequestIdentity
	}
	if !reflect.DeepEqual(value, expected) {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validateSourcePath(value SourcePathSelector) error {
	if value.PathBytesBase64 != "" && value.PathID == "" && len(value.PathBytesBase64) <= 10924 {
		return nil
	}
	if value.PathID != "" && value.PathBytesBase64 == "" && validSHA256(value.PathID) {
		return nil
	}
	return ErrInvalidRequestIdentity
}

func validateWorkflowReference(value WorkflowReferenceRequest) error {
	var expected WorkflowReferenceRequest
	switch value.Kind {
	case "plan":
		if !validOpaque(value.PlanID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowReferenceRequest{Kind: value.Kind, PlanID: value.PlanID}
	case "pass":
		if !validOpaque(value.PlanID) || !validOpaque(value.PassID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowReferenceRequest{Kind: value.Kind, PlanID: value.PlanID, PassID: value.PassID}
	case "run":
		if !validOpaque(value.RunID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowReferenceRequest{Kind: value.Kind, RunID: value.RunID}
	case "audit_packet":
		if !validOpaque(value.RunID) || !validOpaque(value.AuditPacketID) || !validSHA256(value.ExpectedAuditPacketSHA256) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowReferenceRequest{Kind: value.Kind, RunID: value.RunID, AuditPacketID: value.AuditPacketID, ExpectedAuditPacketSHA256: value.ExpectedAuditPacketSHA256}
	case "audit_decision":
		if !validOpaque(value.RunID) || !validOpaque(value.AuditDecisionID) {
			return ErrInvalidRequestIdentity
		}
		expected = WorkflowReferenceRequest{Kind: value.Kind, RunID: value.RunID, AuditDecisionID: value.AuditDecisionID}
	default:
		return ErrInvalidRequestIdentity
	}
	if !reflect.DeepEqual(value, expected) {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validateAttestation(value AttestationRequest) error {
	if !slotNamePattern.MatchString(value.InputName) {
		return ErrInvalidRequestIdentity
	}
	var expected AttestationRequest
	switch value.Kind {
	case "confirmed_intent":
		if !validSHA256(value.SubjectSHA256) || !value.Confirmed {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Confirmed: true}
	case "approved_artifact":
		if !validSHA256(value.SubjectSHA256) || !value.Approved {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Approved: true}
	case "candidate_for_review":
		if !validSHA256(value.SubjectSHA256) || !value.CompleteTransfer {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, CompleteTransfer: true}
	case "execution_mode_selection":
		if value.SelectedMode != "plan" && value.SelectedMode != "one_shot" {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SelectedMode: value.SelectedMode}
	case "complete_review_result":
		if !validSHA256(value.SubjectSHA256) || !validSHA256(value.ReviewedCandidateSHA256) || (value.ReviewResult != "ready_for_approval" && value.ReviewResult != "needs_revision") || !value.Complete {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, ReviewedCandidateSHA256: value.ReviewedCandidateSHA256, ReviewResult: value.ReviewResult, Complete: true}
	case "completed_dependency_outcomes", "exact_evidence":
		if !validSHA256(value.SubjectSHA256) || !value.Complete {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, SubjectSHA256: value.SubjectSHA256, Complete: true}
	case "operator_confirmation", "separate_session_authorship":
		if !value.Confirmed {
			return ErrInvalidRequestIdentity
		}
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, Confirmed: true}
	case "sensitive_data_clearance":
		if value.Clearance == nil || registry.ValidateSensitiveDataClearance(*value.Clearance) != nil {
			return ErrInvalidRequestIdentity
		}
		copy := *value.Clearance
		expected = AttestationRequest{Kind: value.Kind, InputName: value.InputName, Clearance: &copy}
	default:
		return ErrInvalidRequestIdentity
	}
	if !reflect.DeepEqual(value, expected) {
		return ErrInvalidRequestIdentity
	}
	return nil
}

func validOpaque(value string) bool {
	return validBounded(value, 255)
}

func validBounded(value string, max int) bool {
	return value != "" && len(value) <= max
}

func validSHA256(value string) bool {
	return validLowerHex(value, 64, 64)
}

func validGitOID(value string) bool {
	return validLowerHex(value, 40, 64)
}

func validLowerHex(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validRepositoryKey(value string) bool {
	if !validBounded(value, 255) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\\x00\r\n\t ") {
		return false
	}
	return true
}

func validRevision(value string) bool {
	if value == "primary" {
		return true
	}
	if strings.HasPrefix(value, "anchor:") {
		return anchorNamePattern.MatchString(strings.TrimPrefix(value, "anchor:"))
	}
	if strings.HasPrefix(value, "commit:") {
		return validGitOID(strings.TrimPrefix(value, "commit:"))
	}
	return false
}

func validAnchorPurpose(value string) bool {
	switch value {
	case "plan_base", "completed_dependency", "run_base", "audited_commit", "reviewed_source_basis", "operator_supplied_comparison":
		return true
	default:
		return false
	}
}
