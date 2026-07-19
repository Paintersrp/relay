package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	featureapp "relay/internal/app/features"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

var ErrFeatureCompletionAdmission = errors.New("invalid feature completion packet admission")

const featureCompletionDependencyClass = "feature_workspace_completion"

// FeatureCompletionOperationRequest is the packet-bound identity for an
// explicit Feature Workspace completion. The shared features owner remains the
// only authority for calculating gates and creating the completion record.
type FeatureCompletionOperationRequest struct {
	PacketID             string
	OperationID          registry.OperationID
	Action               registry.AllowedAction
	WorkspaceID          string
	ExpectedVersion      int64
	PayloadSHA256        string
	RequiredDependencies []DependencyRequirement
}

// FeatureCompletionWorkflowOwner is deliberately limited to the shared
// feature-completion contract. It cannot publish authority, create tickets, or
// make any remediation change.
type FeatureCompletionWorkflowOwner interface {
	EvaluateCompletion(context.Context, string) (featureapp.CompletionStatus, error)
	Complete(context.Context, featureapp.CompletionInput) (featureapp.CompletionResult, error)
}

// FeatureCompletionWorkspace is the API-safe workspace projection returned by
// the packet-admitted completion workflow. Persistence models stay behind this
// shared application boundary.
type FeatureCompletionWorkspace struct {
	WorkspaceID string
	FeatureSlug string
	State       string
	Version     int64
	CreatedAt   string
	UpdatedAt   string
}

type FeatureCompletionGate struct {
	Name  string
	Ready bool
}

type FeatureCompletionDecision struct {
	CompletionDecisionID   string
	AuthorityRevisionRowID int64
	SourceClosureRowID     int64
	Decision               string
	CreatedAt              string
}

type FeatureCompletionStatus struct {
	Workspace       FeatureCompletionWorkspace
	Gates           []FeatureCompletionGate
	CurrentDecision *FeatureCompletionDecision
}

type FeatureCompletionResult struct {
	Decision  FeatureCompletionDecision
	Workspace FeatureCompletionWorkspace
}

// FeatureCompletionWorkflowService admits the explicit local-operator action
// before delegating to the shared feature owner. Evaluation is read-only and
// intentionally does not require a packet.
type FeatureCompletionWorkflowService struct {
	packets PacketMutationAuthorizer
	owner   FeatureCompletionWorkflowOwner
}

func NewFeatureCompletionWorkflowService(packets PacketMutationAuthorizer, owner FeatureCompletionWorkflowOwner) (*FeatureCompletionWorkflowService, error) {
	if packets == nil || owner == nil {
		return nil, ErrFeatureCompletionAdmission
	}
	return &FeatureCompletionWorkflowService{packets: packets, owner: owner}, nil
}

func (s *FeatureCompletionWorkflowService) Evaluate(ctx context.Context, workspaceID string) (FeatureCompletionStatus, error) {
	if s == nil || s.owner == nil {
		return FeatureCompletionStatus{}, ErrFeatureCompletionAdmission
	}
	status, err := s.owner.EvaluateCompletion(ctx, workspaceID)
	if err != nil {
		return FeatureCompletionStatus{}, err
	}
	return featureCompletionStatusProjection(status), nil
}

type FeatureCompletionOperationInput struct {
	Admission FeatureCompletionOperationRequest
	Complete  featureapp.CompletionInput
}

func (s *FeatureCompletionWorkflowService) Complete(ctx context.Context, input FeatureCompletionOperationInput) (FeatureCompletionResult, error) {
	payload, err := FeatureCompletionPayloadSHA256(input.Complete)
	if err != nil || input.Admission.Action != registry.FeatureCompletionActionComplete ||
		input.Admission.WorkspaceID != input.Complete.WorkspaceID ||
		input.Admission.ExpectedVersion != input.Complete.ExpectedVersion ||
		input.Admission.PayloadSHA256 != payload ||
		!sameDependencies(input.Admission.RequiredDependencies, featureCompletionDependencies(input.Complete)) {
		return FeatureCompletionResult{}, ErrFeatureCompletionAdmission
	}
	if err := ValidateFeatureCompletionOperationRequest(input.Admission); err != nil {
		return FeatureCompletionResult{}, err
	}
	operation, _ := registry.TicketOperationForAction(input.Admission.Action)
	if _, err := s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: input.Admission.PacketID, SurfaceContract: operation.SurfaceContract,
		OperationID: operation.OperationID, Action: input.Admission.Action,
		RequiredDependencies: append([]DependencyRequirement(nil), input.Admission.RequiredDependencies...),
	}); err != nil {
		return FeatureCompletionResult{}, err
	}
	result, err := s.owner.Complete(ctx, input.Complete)
	if err != nil {
		return FeatureCompletionResult{}, err
	}
	return featureCompletionResultProjection(result), nil
}

func featureCompletionStatusProjection(value featureapp.CompletionStatus) FeatureCompletionStatus {
	gates := make([]FeatureCompletionGate, 0, len(value.Gates))
	for _, gate := range value.Gates {
		gates = append(gates, FeatureCompletionGate{Name: gate.Name, Ready: gate.Ready})
	}
	status := FeatureCompletionStatus{Workspace: featureCompletionWorkspaceProjection(value.Workspace), Gates: gates}
	if value.CurrentDecision != nil {
		decision := featureCompletionDecisionProjection(*value.CurrentDecision)
		status.CurrentDecision = &decision
	}
	return status
}

func featureCompletionResultProjection(value featureapp.CompletionResult) FeatureCompletionResult {
	return FeatureCompletionResult{
		Decision:  featureCompletionDecisionProjection(value.Decision),
		Workspace: featureCompletionWorkspaceProjection(value.Workspace),
	}
}

func featureCompletionWorkspaceProjection(value workflowstore.FeatureWorkspace) FeatureCompletionWorkspace {
	return FeatureCompletionWorkspace{
		WorkspaceID: value.WorkspaceID,
		FeatureSlug: value.FeatureSlug,
		State:       value.State,
		Version:     value.Version,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func featureCompletionDecisionProjection(value workflowstore.FeatureWorkspaceCompletionDecision) FeatureCompletionDecision {
	return FeatureCompletionDecision{
		CompletionDecisionID:   value.CompletionDecisionID,
		AuthorityRevisionRowID: value.AuthorityRevisionRowID,
		SourceClosureRowID:     value.SourceClosureRowID,
		Decision:               value.Decision,
		CreatedAt:              value.CreatedAt,
	}
}

func ValidateFeatureCompletionOperationRequest(request FeatureCompletionOperationRequest) error {
	if !exactNonBlank(request.PacketID) || !exactNonBlank(request.WorkspaceID) || request.ExpectedVersion < 1 ||
		!validTicketSHA256(request.PayloadSHA256) ||
		!sameDependencies(request.RequiredDependencies, featureCompletionDependencies(featureapp.CompletionInput{WorkspaceID: request.WorkspaceID, ExpectedVersion: request.ExpectedVersion})) {
		return ErrFeatureCompletionAdmission
	}
	operation, ok := registry.TicketOperationForAction(request.Action)
	if !ok || operation.OperationID != request.OperationID || request.Action != registry.FeatureCompletionActionComplete {
		return ErrFeatureCompletionAdmission
	}
	return nil
}

func FeatureCompletionPayloadSHA256(input featureapp.CompletionInput) (string, error) {
	raw, err := json.Marshal(struct {
		WorkspaceID       string `json:"workspace_id"`
		ExpectedVersion   int64  `json:"expected_version"`
		OperatorConfirmed bool   `json:"operator_confirmed"`
	}{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorConfirmed: input.OperatorConfirmed})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func featureCompletionDependencies(input featureapp.CompletionInput) []DependencyRequirement {
	return []DependencyRequirement{{
		Class: featureCompletionDependencyClass,
		Key:   "workspace:" + input.WorkspaceID + ":version:" + stringRevisionID(input.ExpectedVersion),
	}}
}
