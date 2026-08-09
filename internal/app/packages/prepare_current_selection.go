package packages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrApprovedBriefMissing = errors.New("current approved ticket design brief is missing")
	ErrBriefNotApproved     = errors.New("current ticket design brief is not approved")
)

// PrepareCurrentSelection is the packages-owner implementation of the guided
// prepare action. It carries only the workspace identity; the active selection
// and the current approved Ticket Design Brief are resolved server-side and no
// selection ID, brief ID, or digest is accepted from the guided boundary.
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
	brief, err := s.store.GetTicketDesignBriefBySelectionRowID(ctx, selection.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: %s", ErrApprovedBriefMissing, in.WorkspaceID)
	}
	if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	if _, err := s.store.GetTicketDesignBriefApprovalByBriefRowID(ctx, brief.ID); errors.Is(err, sql.ErrNoRows) {
		return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: %s", ErrBriefNotApproved, in.WorkspaceID)
	} else if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	bytes, err := s.store.ReadTicketDesignBriefBytes(ctx, brief.BriefID, 1<<20)
	if err != nil {
		return guidedapp.PreparePackageResult{}, fmt.Errorf("%w: %v", ErrPackageBasisChanged, err)
	}
	prepared, err := s.Prepare(ctx, PrepareInput{
		SelectionID: selection.SelectionID,
		TicketDesignBrief: ArtifactInput{
			DisplayName:    brief.Filename,
			ExpectedSHA256: brief.ArtifactSha256,
			Bytes:          bytes,
		},
	})
	if err != nil {
		return guidedapp.PreparePackageResult{}, err
	}
	return guidedapp.PreparePackageResult{PackageID: prepared.Package.PackageID, State: "prepared"}, nil
}
