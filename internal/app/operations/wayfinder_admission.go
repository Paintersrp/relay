package operations

import (
	"context"
	"errors"
	"strings"

	"relay/internal/operations/registry"
)

var ErrWayfinderAdmission = errors.New("invalid wayfinder packet admission")

// PacketMutationAuthorizer is deliberately the smallest packet boundary used
// by Wayfinder. Durable workspace owners are never called from this package.
type PacketMutationAuthorizer interface {
	AuthorizeMutation(context.Context, MutationRequest) (MutationAuthorization, error)
}

type WayfinderAdmissionService struct{ packets PacketMutationAuthorizer }

func NewWayfinderAdmissionService(packets PacketMutationAuthorizer) (*WayfinderAdmissionService, error) {
	if packets == nil {
		return nil, ErrWayfinderAdmission
	}
	return &WayfinderAdmissionService{packets: packets}, nil
}

type WayfinderMutationRequest struct {
	PacketID             string
	OperationID          registry.OperationID
	Action               registry.AllowedAction
	RequiredDependencies []DependencyRequirement
}

// AdmitWayfinderMutation verifies the exact role operation, action, active
// packet, packet document, and declared retained dependencies before a caller
// may invoke a P3-T3 durable owner.
func (s *WayfinderAdmissionService) AdmitWayfinderMutation(ctx context.Context, request WayfinderMutationRequest) (MutationAuthorization, error) {
	if s == nil || s.packets == nil || strings.TrimSpace(request.PacketID) != request.PacketID || request.PacketID == "" {
		return MutationAuthorization{}, ErrWayfinderAdmission
	}
	operation, ok := registry.Lookup(request.OperationID)
	if !ok || (operation.Role != "wayfinder" && operation.Role != "features") || !containsAction(operation.AllowedNonSourceActions, request.Action) {
		return MutationAuthorization{}, ErrWayfinderAdmission
	}
	for _, dependency := range request.RequiredDependencies {
		if strings.TrimSpace(dependency.Class) != dependency.Class || dependency.Class == "" || strings.TrimSpace(dependency.Key) != dependency.Key || dependency.Key == "" {
			return MutationAuthorization{}, ErrWayfinderAdmission
		}
	}
	return s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: request.PacketID, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Action: request.Action, RequiredDependencies: append([]DependencyRequirement(nil), request.RequiredDependencies...),
	})
}
