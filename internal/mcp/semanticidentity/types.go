package semanticidentity

import (
	"sort"

	"relay/internal/mcp/fileacquisition"
	"relay/internal/operations/registry"
)

type RequestIdentity interface {
	requestIdentity()
	SurfaceContractID() registry.SurfaceContractID
	MutationTool() registry.MutationTool
	SemanticIdentityVersion() string
}

type Fingerprint struct {
	surfaceContractID       registry.SurfaceContractID
	tool                    registry.MutationTool
	semanticIdentityVersion string
	semanticRequestSHA256   string
}

func (f Fingerprint) SurfaceContractID() registry.SurfaceContractID {
	return f.surfaceContractID
}

func (f Fingerprint) Tool() registry.MutationTool {
	return f.tool
}

func (f Fingerprint) SemanticIdentityVersion() string {
	return f.semanticIdentityVersion
}

func (f Fingerprint) SemanticRequestSHA256() string {
	return f.semanticRequestSHA256
}

type DeclaredFile struct {
	FileIndex      int64  `json:"file_index"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

type InputBinding struct {
	InputName      string             `json:"input_name"`
	SourceKind     string             `json:"source_kind"`
	DisplayName    string             `json:"display_name"`
	MediaType      string             `json:"media_type"`
	ExpectedSHA256 string             `json:"expected_sha256"`
	Source         InputBindingSource `json:"source"`
}

type InputBindingSource struct {
	FileIndex       *int64                        `json:"file_index,omitempty"`
	ArtifactID      string                        `json:"artifact_id,omitempty"`
	Text            string                        `json:"text,omitempty"`
	WorkflowRecord  *WorkflowRecordInputReference `json:"workflow_record,omitempty"`
	RepositoryKey   string                        `json:"repository_key,omitempty"`
	Revision        string                        `json:"revision,omitempty"`
	Path            *SourcePathSelector           `json:"path,omitempty"`
	ExpectedBlobOID string                        `json:"expected_blob_oid,omitempty"`
}

type WorkflowRecordInputReference struct {
	Kind            string `json:"kind"`
	PlanID          string `json:"plan_id,omitempty"`
	PassID          string `json:"pass_id,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	ArtifactID      string `json:"artifact_id,omitempty"`
	AuditPacketID   string `json:"audit_packet_id,omitempty"`
	AuditDecisionID string `json:"audit_decision_id,omitempty"`
	ExpectedSHA256  string `json:"expected_sha256,omitempty"`
}

type SourcePathSelector struct {
	PathBytesBase64 string `json:"path_bytes_base64,omitempty"`
	PathID          string `json:"path_id,omitempty"`
}

type WorkflowReferenceRequest struct {
	Kind                      string `json:"kind"`
	PlanID                    string `json:"plan_id,omitempty"`
	PassID                    string `json:"pass_id,omitempty"`
	RunID                     string `json:"run_id,omitempty"`
	AuditPacketID             string `json:"audit_packet_id,omitempty"`
	ExpectedAuditPacketSHA256 string `json:"expected_audit_packet_sha256,omitempty"`
	AuditDecisionID           string `json:"audit_decision_id,omitempty"`
}

type AttestationRequest struct {
	Kind                    string                           `json:"kind"`
	InputName               string                           `json:"input_name"`
	SubjectSHA256           string                           `json:"subject_sha256,omitempty"`
	Confirmed               bool                             `json:"confirmed,omitempty"`
	Approved                bool                             `json:"approved,omitempty"`
	CompleteTransfer        bool                             `json:"complete_transfer,omitempty"`
	SelectedMode            string                           `json:"selected_mode,omitempty"`
	ReviewedCandidateSHA256 string                           `json:"reviewed_candidate_sha256,omitempty"`
	ReviewResult            string                           `json:"review_result,omitempty"`
	Complete                bool                             `json:"complete,omitempty"`
	Clearance               *registry.SensitiveDataClearance `json:"clearance,omitempty"`
}

type PrimaryRevisionRequest struct {
	RepositoryKey string `json:"repository_key"`
	CommitOID     string `json:"commit_oid"`
}

type ComparisonAnchorRequest struct {
	RepositoryKey   string `json:"repository_key"`
	AnchorName      string `json:"anchor_name"`
	Purpose         string `json:"purpose"`
	CommitOID       string `json:"commit_oid"`
	ExpectedTreeOID string `json:"expected_tree_oid"`
}

type CreateOperationPacket struct {
	SurfaceContract    registry.SurfaceContractID `json:"surface_contract"`
	OperationID        registry.OperationID       `json:"operation_id"`
	ProjectID          string                     `json:"project_id"`
	InputFileCount     int                        `json:"input_file_count"`
	DeclaredFiles      []DeclaredFile             `json:"declared_files"`
	Inputs             []InputBinding             `json:"inputs"`
	WorkflowReferences []WorkflowReferenceRequest `json:"workflow_references"`
	Attestations       []AttestationRequest       `json:"attestations"`
	PrimaryRevisions   []PrimaryRevisionRequest   `json:"primary_revisions,omitempty"`
	ComparisonAnchors  []ComparisonAnchorRequest  `json:"comparison_anchors,omitempty"`
	RelaySpecsRevision string                     `json:"relay_specs_revision,omitempty"`
}

func (CreateOperationPacket) requestIdentity() {}

func (v CreateOperationPacket) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (CreateOperationPacket) MutationTool() registry.MutationTool {
	return registry.MutationToolCreateOperationPacket
}

func (CreateOperationPacket) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolCreateOperationPacket)
}

type RefreshOperationPacket struct {
	SurfaceContract    registry.SurfaceContractID `json:"surface_contract"`
	ExpectedPacketID   string                     `json:"expected_packet_id"`
	InputFileCount     int                        `json:"input_file_count"`
	DeclaredFiles      []DeclaredFile             `json:"declared_files"`
	Inputs             []InputBinding             `json:"inputs"`
	WorkflowReferences []WorkflowReferenceRequest `json:"workflow_references"`
	Attestations       []AttestationRequest       `json:"attestations"`
	PrimaryRevisions   []PrimaryRevisionRequest   `json:"primary_revisions,omitempty"`
	ComparisonAnchors  []ComparisonAnchorRequest  `json:"comparison_anchors,omitempty"`
	RelaySpecsRevision string                     `json:"relay_specs_revision,omitempty"`
}

func (RefreshOperationPacket) requestIdentity() {}

func (v RefreshOperationPacket) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (RefreshOperationPacket) MutationTool() registry.MutationTool {
	return registry.MutationToolRefreshOperationPacket
}

func (RefreshOperationPacket) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolRefreshOperationPacket)
}

type CloseOperationPacket struct {
	SurfaceContract  registry.SurfaceContractID `json:"surface_contract"`
	ExpectedPacketID string                     `json:"expected_packet_id"`
}

func (CloseOperationPacket) requestIdentity() {}

func (v CloseOperationPacket) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (CloseOperationPacket) MutationTool() registry.MutationTool {
	return registry.MutationToolCloseOperationPacket
}

func (CloseOperationPacket) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolCloseOperationPacket)
}

type CanonicalArtifactMutation struct {
	SurfaceContract        registry.SurfaceContractID      `json:"surface_contract"`
	ExpectedPacketID       string                          `json:"expected_packet_id"`
	ArtifactName           string                          `json:"artifact_name"`
	MediaType              string                          `json:"media_type"`
	ExpectedSHA256         string                          `json:"expected_sha256"`
	SensitiveDataClearance registry.SensitiveDataClearance `json:"sensitive_data_clearance"`
}

type SubmitPlan struct{ CanonicalArtifactMutation }

func (SubmitPlan) requestIdentity() {}

func (v SubmitPlan) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (SubmitPlan) MutationTool() registry.MutationTool {
	return registry.MutationToolSubmitPlan
}

func (SubmitPlan) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolSubmitPlan)
}

type CreateRun struct{ CanonicalArtifactMutation }

func (CreateRun) requestIdentity() {}

func (v CreateRun) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (CreateRun) MutationTool() registry.MutationTool {
	return registry.MutationToolCreateRun
}

func (CreateRun) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolCreateRun)
}

type AuditWorkflowScope struct {
	Kind   string `json:"kind"`
	PlanID string `json:"plan_id,omitempty"`
	PassID string `json:"pass_id,omitempty"`
}

type AuditFinding struct {
	Source           string `json:"source"`
	Location         string `json:"location"`
	Summary          string `json:"summary"`
	Evidence         string `json:"evidence"`
	RequiredRevision string `json:"required_revision"`
}

type AuditObservation struct {
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
}

type RecordAuditDecision struct {
	SurfaceContract           registry.SurfaceContractID `json:"surface_contract"`
	ExpectedPacketID          string                     `json:"expected_packet_id"`
	RunID                     string                     `json:"run_id"`
	AuditPacketID             string                     `json:"audit_packet_id"`
	ExpectedAuditPacketSHA256 string                     `json:"expected_audit_packet_sha256"`
	AuditedCommitOID          string                     `json:"audited_commit_oid"`
	WorkflowScope             AuditWorkflowScope         `json:"workflow_scope"`
	Decision                  string                     `json:"decision"`
	Rationale                 string                     `json:"rationale"`
	MaterialFindings          []AuditFinding             `json:"material_findings"`
	NonBlockingObservations   []AuditObservation         `json:"non_blocking_observations"`
	OperatorConfirmed         bool                       `json:"operator_confirmed"`
}

func (RecordAuditDecision) requestIdentity() {}

func (v RecordAuditDecision) SurfaceContractID() registry.SurfaceContractID {
	return v.SurfaceContract
}

func (RecordAuditDecision) MutationTool() registry.MutationTool {
	return registry.MutationToolRecordAuditDecision
}

func (RecordAuditDecision) SemanticIdentityVersion() string {
	return semanticVersion(registry.MutationToolRecordAuditDecision)
}

func CanonicalDeclaredFiles(values []fileacquisition.DeclaredFile) []DeclaredFile {
	out := make([]DeclaredFile, len(values))
	for index, value := range values {
		out[index] = DeclaredFile{FileIndex: value.FileIndex, ExpectedSHA256: value.ExpectedSHA256}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FileIndex < out[j].FileIndex })
	return out
}

func semanticVersion(tool registry.MutationTool) string {
	version, _ := registry.SemanticProjectionVersion(string(tool))
	return version
}
