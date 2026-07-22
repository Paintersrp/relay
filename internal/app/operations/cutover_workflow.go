package operations

import (
	"context"

	appcutover "relay/internal/app/cutover"
	"relay/internal/operations/registry"
)

type CutoverWorkflowService struct {
	owner *appcutover.Service
}

func NewCutoverWorkflowService(packets PacketMutationAuthorizer, owner *appcutover.Service) (*CutoverWorkflowService, error) {
	if packets == nil || owner == nil {
		return nil, ErrTicketAdmission
	}
	return &CutoverWorkflowService{owner: owner}, nil
}

func (s *CutoverWorkflowService) Prepare(ctx context.Context, request appcutover.PrepareRequest) (*appcutover.State, error) {
	return s.owner.Prepare(ctx, request)
}

func (s *CutoverWorkflowService) Activate(ctx context.Context, request appcutover.ActivationRequest) (*appcutover.State, error) {
	return s.owner.Activate(ctx, request)
}

func (s *CutoverWorkflowService) Rollback(ctx context.Context, request appcutover.RollbackRequest) (*appcutover.State, error) {
	return s.owner.Rollback(ctx, request)
}

func (s *CutoverWorkflowService) CrossExecutionBoundary(ctx context.Context, request appcutover.BoundaryRequest) error {
	return s.owner.CrossExecutionBoundary(ctx, request)
}

func (s *CutoverWorkflowService) RecordRollForwardEvidence(ctx context.Context, request appcutover.RollForwardEvidenceRequest) error {
	return s.owner.RecordRollForwardEvidence(ctx, request)
}

func (s *CutoverWorkflowService) State(ctx context.Context) (*appcutover.State, bool, error) {
	return s.owner.State(ctx)
}

func (s *CutoverWorkflowService) Readiness(ctx context.Context, activationID string) (*appcutover.Readiness, error) {
	return s.owner.Readiness(ctx, activationID)
}

func (s *CutoverWorkflowService) History(ctx context.Context) ([]appcutover.State, error) {
	return s.owner.History(ctx)
}

var _ registry.OperationID = ""
