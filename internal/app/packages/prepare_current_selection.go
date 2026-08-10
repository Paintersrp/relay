package packages

import (
	"context"
	"fmt"
	"strings"

	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

// PrepareCurrentSelection is the packages-owner implementation of the guided
// prepare action. It carries only the workspace identity; the active selection
// and the selected approved Delivery Ticket are resolved server-side and no
// selection ID, Ticket digest, or Brief identity is accepted from the guided
// boundary. The selected approved Delivery Ticket is the sole ticket semantic
// authority: the server resolves its exact source-vault bytes and
// deterministic projection.
func (s *Service) PrepareCurrentSelection(ctx context.Context, in guidedapp.PreparePackageInput) (guidedapp.PreparePackageResult, error) {
	if in.WorkspaceID == "" || strings.TrimSpace(in.WorkspaceID) != in.WorkspaceID {
		return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: workspace ID must be nonblank without outer whitespace", ErrInvalidPackageInput)
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, in.WorkspaceID)
	if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	var selection workflowstore.DeliveryTicketSelection
	found := false
	for _, candidate := range selections {
		if candidate.State != "active" {
			continue
		}
		if found {
			return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: workspace has more than one active selection", ErrSelectionInvalid)
		}
		selection, found = candidate, true
	}
	if !found {
		return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: %s", ErrSelectionNotActive, in.WorkspaceID)
	}
	prepared, err := s.Prepare(ctx, PrepareInput{
		SelectionID: selection.SelectionID,
	})
	if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	return guidedapp.PreparePackageResult{PackageID: prepared.Package.PackageID, State: "prepared"}, nil
}
