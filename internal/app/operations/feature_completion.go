package operations

import (
	"context"

	featureapp "relay/internal/app/features"
	workflowstore "relay/internal/store/workflow"
)

// FeatureCompletionWorkflowOwner is deliberately limited to the existing
// completion owner. It cannot publish authority, create Tickets, or mutate
// package state.
type FeatureCompletionWorkflowOwner interface {
	EvaluateCompletion(context.Context, string) (featureapp.CompletionStatus, error)
	Complete(context.Context, featureapp.CompletionInput) (featureapp.CompletionResult, error)
}

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

// FeatureCompletionWorkflowService is a direct projection over the existing
// feature-completion owner. Completion evaluation and mutation retain the
// owner's authority, source-state, version, and transactional gates.
type FeatureCompletionWorkflowService struct {
	owner FeatureCompletionWorkflowOwner
}

func NewFeatureCompletionWorkflowService(owner FeatureCompletionWorkflowOwner) (*FeatureCompletionWorkflowService, error) {
	if owner == nil {
		return nil, ErrFeatureCompletionAdmission
	}
	return &FeatureCompletionWorkflowService{owner: owner}, nil
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

func (s *FeatureCompletionWorkflowService) Complete(ctx context.Context, input featureapp.CompletionInput) (FeatureCompletionResult, error) {
	if s == nil || s.owner == nil {
		return FeatureCompletionResult{}, ErrFeatureCompletionAdmission
	}
	result, err := s.owner.Complete(ctx, input)
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
