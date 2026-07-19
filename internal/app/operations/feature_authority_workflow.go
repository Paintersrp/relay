package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	featureapp "relay/internal/app/features"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

var ErrFeatureAuthorityAdmission = errors.New("invalid feature authority packet admission")

const featureAuthorityDependencyClass = "feature_workspace_authority"

// FeatureAuthorityWorkflowOwner is the shared authority surface. Approval
// recording and approved publication remain the only two authority mutations.
type FeatureAuthorityWorkflowOwner interface {
	RecordAuthorityApproval(context.Context, featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error)
	PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error)
}

// FeatureAuthorityWorkflowService admits explicit packet-gated authority
// recording and approved publication before delegating to the shared feature
// owner. Packet authorization is required but is never accepted as approval
// evidence.
type FeatureAuthorityWorkflowService struct {
	packets PacketMutationAuthorizer
	owner   FeatureAuthorityWorkflowOwner
}

func NewFeatureAuthorityWorkflowService(packets PacketMutationAuthorizer, owner FeatureAuthorityWorkflowOwner) (*FeatureAuthorityWorkflowService, error) {
	if packets == nil || owner == nil {
		return nil, ErrFeatureAuthorityAdmission
	}
	return &FeatureAuthorityWorkflowService{packets: packets, owner: owner}, nil
}

// FeatureAuthorityRecordApprovalRequest is the packet-bound identity for an
// explicit governing-artifact approval recording.
type FeatureAuthorityRecordApprovalRequest struct {
	PacketID             string
	OperationID          registry.OperationID
	Action               registry.AllowedAction
	WorkspaceID          string
	RequiredDependencies []DependencyRequirement
}

// RecordApproval admits the packet-gated recording of an immutable
// governing-artifact approval. The approval input is delivered alongside the
// packet admission so the owner can persist it.
func (s *FeatureAuthorityWorkflowService) RecordApproval(ctx context.Context, admission FeatureAuthorityRecordApprovalRequest, input featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error) {
	if err := s.admitAuthorityMutation(ctx, admission, registry.FeatureAuthorityActionRecordApproval); err != nil {
		return featureapp.RecordAuthorityApprovalResult{}, err
	}
	return s.owner.RecordAuthorityApproval(ctx, input)
}

// FeatureAuthorityPublishApprovedRequest is the packet-bound identity for an
// explicit approved authority publication.
type FeatureAuthorityPublishApprovedRequest struct {
	PacketID             string
	OperationID          registry.OperationID
	Action               registry.AllowedAction
	WorkspaceID          string
	RequiredDependencies []DependencyRequirement
}

// PublishApproved admits the packet-gated publication of approved authority
// layers. Every layer must carry a valid approval reference; this workflow
// cannot create or imply approval.
func (s *FeatureAuthorityWorkflowService) PublishApproved(ctx context.Context, admission FeatureAuthorityPublishApprovedRequest, input featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error) {
	if err := s.admitAuthorityMutation(ctx, FeatureAuthorityRecordApprovalRequest{
		PacketID: admission.PacketID, OperationID: admission.OperationID, Action: admission.Action,
		WorkspaceID: admission.WorkspaceID, RequiredDependencies: admission.RequiredDependencies,
	}, registry.FeatureAuthorityActionPublishApproved); err != nil {
		return featureapp.AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, err
	}
	return s.owner.PublishAuthority(ctx, input)
}

func (s *FeatureAuthorityWorkflowService) admitAuthorityMutation(ctx context.Context, request FeatureAuthorityRecordApprovalRequest, expectedAction registry.AllowedAction) error {
	if s == nil || s.packets == nil || strings.TrimSpace(request.PacketID) != request.PacketID || request.PacketID == "" {
		return ErrFeatureAuthorityAdmission
	}
	operation, ok := registry.Lookup(request.OperationID)
	if !ok || operation.Role != "features" || !containsAction(operation.AllowedNonSourceActions, expectedAction) {
		return ErrFeatureAuthorityAdmission
	}
	if request.Action != expectedAction || request.WorkspaceID == "" {
		return ErrFeatureAuthorityAdmission
	}
	for _, dependency := range request.RequiredDependencies {
		if strings.TrimSpace(dependency.Class) != dependency.Class || dependency.Class == "" ||
			strings.TrimSpace(dependency.Key) != dependency.Key || dependency.Key == "" {
			return ErrFeatureAuthorityAdmission
		}
	}
	if _, err := s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: request.PacketID, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Action: expectedAction, RequiredDependencies: append([]DependencyRequirement(nil), request.RequiredDependencies...),
	}); err != nil {
		return err
	}
	return nil
}

// FeatureAuthorityRecordApprovalPayloadSHA256 produces a deterministic hash
// of the approval input for packet admission binding.
func FeatureAuthorityRecordApprovalPayloadSHA256(input featureapp.RecordAuthorityApprovalInput) (string, error) {
	artifactRowID := nullInt64ForPayload(input.ArtifactRowID)
	retainedArtifactRowID := nullInt64ForPayload(input.RetainedArtifact)
	raw, err := json.Marshal(struct {
		WorkspaceID                  string          `json:"workspace_id"`
		Family                       string          `json:"family"`
		ArtifactRowID                json.RawMessage `json:"artifact_row_id"`
		RetainedArtifact             json.RawMessage `json:"retained_artifact"`
		ArtifactSHA256               string          `json:"artifact_sha256"`
		OperatorConfirmationEvidence string          `json:"operator_confirmation_evidence"`
	}{
		WorkspaceID:                  input.WorkspaceID,
		Family:                       input.Family,
		ArtifactRowID:                artifactRowID,
		RetainedArtifact:             retainedArtifactRowID,
		ArtifactSHA256:               input.ArtifactSHA256,
		OperatorConfirmationEvidence: input.OperatorConfirmationEvidence,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// FeatureAuthorityPublishApprovedPayloadSHA256 produces a deterministic hash
// of the publication input for packet admission binding.
func FeatureAuthorityPublishApprovedPayloadSHA256(input featureapp.PublishAuthorityInput) (string, error) {
	type layerPayload struct {
		Kind             string          `json:"kind"`
		ArtifactRowID    json.RawMessage `json:"artifact_row_id"`
		RetainedArtifact json.RawMessage `json:"retained_artifact"`
		ArtifactSHA256   string          `json:"artifact_sha256"`
		ApprovalRowID    json.RawMessage `json:"approval_row_id"`
	}
	layers := make([]layerPayload, len(input.Layers))
	for i, layer := range input.Layers {
		layers[i] = layerPayload{
			Kind:             layer.Kind,
			ArtifactRowID:    nullInt64ForPayload(layer.ArtifactRowID),
			RetainedArtifact: nullInt64ForPayload(layer.RetainedArtifact),
			ArtifactSHA256:   layer.ArtifactSHA256,
			ApprovalRowID:    nullInt64ForPayload(layer.ApprovalRowID),
		}
	}
	sourceClosure := nullInt64ForPayload(input.SourceClosureID)
	raw, err := json.Marshal(struct {
		WorkspaceID      string          `json:"workspace_id"`
		ExpectedVersion  int64           `json:"expected_version"`
		SourceClosureRowID json.RawMessage `json:"source_closure_row_id"`
		Layers           []layerPayload  `json:"layers"`
	}{
		WorkspaceID:        input.WorkspaceID,
		ExpectedVersion:    input.ExpectedVersion,
		SourceClosureRowID: sourceClosure,
		Layers:             layers,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func nullInt64ForPayload(value sql.NullInt64) json.RawMessage {
	if !value.Valid {
		return json.RawMessage("null")
	}
	data, _ := json.Marshal(value.Int64)
	return json.RawMessage(data)
}

// ValidateFeatureAuthorityRecordApprovalRequest validates the packet-bound
// approval recording request shape.
func ValidateFeatureAuthorityRecordApprovalRequest(request FeatureAuthorityRecordApprovalRequest, input featureapp.RecordAuthorityApprovalInput) error {
	if !exactNonBlank(request.PacketID) || !exactNonBlank(request.WorkspaceID) || request.WorkspaceID != input.WorkspaceID {
		return ErrFeatureAuthorityAdmission
	}
	payload, err := FeatureAuthorityRecordApprovalPayloadSHA256(input)
	if err != nil || !validTicketSHA256(payload) {
		return ErrFeatureAuthorityAdmission
	}
	operation, ok := registry.Lookup(request.OperationID)
	if !ok || operation.Role != "features" || request.Action != registry.FeatureAuthorityActionRecordApproval {
		return ErrFeatureAuthorityAdmission
	}
	return nil
}

// ValidateFeatureAuthorityPublishApprovedRequest validates the packet-bound
// publication request shape.
func ValidateFeatureAuthorityPublishApprovedRequest(request FeatureAuthorityPublishApprovedRequest, input featureapp.PublishAuthorityInput) error {
	if !exactNonBlank(request.PacketID) || !exactNonBlank(request.WorkspaceID) || request.WorkspaceID != input.WorkspaceID {
		return ErrFeatureAuthorityAdmission
	}
	payload, err := FeatureAuthorityPublishApprovedPayloadSHA256(input)
	if err != nil || !validTicketSHA256(payload) {
		return ErrFeatureAuthorityAdmission
	}
	operation, ok := registry.Lookup(request.OperationID)
	if !ok || operation.Role != "features" || request.Action != registry.FeatureAuthorityActionPublishApproved {
		return ErrFeatureAuthorityAdmission
	}
	return nil
}

func featureAuthorityDependencies(input featureapp.PublishAuthorityInput) []DependencyRequirement {
	deps := make([]DependencyRequirement, 0, len(input.Layers))
	for _, layer := range input.Layers {
		key := layer.Kind + ":" + layer.ArtifactSHA256
		if layer.ApprovalRowID.Valid {
			key = layer.Kind + ":" + layer.ArtifactSHA256 + ":approval:" + stringRevisionID(layer.ApprovalRowID.Int64)
		}
		deps = append(deps, DependencyRequirement{Class: featureAuthorityDependencyClass, Key: key})
	}
	return deps
}
