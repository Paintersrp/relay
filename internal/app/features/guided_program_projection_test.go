package features

import (
	"context"
	"strings"
	"testing"

	workflowtickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
)

type stubProgramOwner struct {
	state guidedapp.ProgramState
	err   error
}

func (s stubProgramOwner) ReadWorkspaceProgramState(context.Context, string) (guidedapp.ProgramState, error) {
	return s.state, s.err
}

// TestGuidedProjectionCarriesProgramIntegrationBackendState proves the guided
// Feature Workspace projection carries the program-owner integration backend
// state: eligible constituents not yet bound by a current Assignment and every
// Integration Assignment with its recorded verification disposition. The
// projection never re-derives or mutates the integration surface.
func TestGuidedProjectionCarriesProgramIntegrationBackendState(t *testing.T) {
	ctx, store, service, workspace := guidedAbandonFixture(t)
	service.SetGuidedAuditOwnerForTest(&guidedFakeAuditOwner{})
	ticketOwner, err := workflowtickets.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	service.SetGuidedTicketOwnerForTest(ticketOwner)
	service.SetGuidedProgramOwnerForTest(stubProgramOwner{state: guidedapp.ProgramState{
		Eligible: []guidedapp.ProgramEligibleMember{
			{MemberID: "program-member-two", TicketID: "T-TWO", TicketRevision: 1, AcceptedCommit: strings.Repeat("c", 40), PushedBranch: "feature/two"},
		},
		Integration: []guidedapp.ProgramIntegrationAssignment{
			{AssignmentID: "integration-assignment-1", DispatchID: "dispatch-1", Status: "verified", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), Verification: "passed", Members: []guidedapp.ProgramIntegrationMember{{MemberID: "program-member-one", TicketID: "T-ONE", TicketRevision: 1}}},
		},
	}})
	projection, err := service.ReadGuidedProjection(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Program.Available {
		t.Fatal("program section is not available")
	}
	if len(projection.Program.Eligible) != 1 || projection.Program.Eligible[0].MemberID != "program-member-two" || projection.Program.Eligible[0].AcceptedCommit != strings.Repeat("c", 40) {
		t.Fatalf("eligible = %#v", projection.Program.Eligible)
	}
	if len(projection.Program.Integration) != 1 || projection.Program.Integration[0].AssignmentID != "integration-assignment-1" || projection.Program.Integration[0].Verification != "passed" || len(projection.Program.Integration[0].Members) != 1 || projection.Program.Integration[0].Members[0].TicketID != "T-ONE" {
		t.Fatalf("integration = %#v", projection.Program.Integration)
	}
}
