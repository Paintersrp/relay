package features

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func deliveryPlanCandidateBytes(featureSlug string) []byte {
	return []byte(`{"schema_version":"1.0","feature_slug":"` + featureSlug + `","goal":"Deliver the approved bounded planning outcome.","context":"Planning context for the planned delivery outcome.","scope":{"in_scope":["Plan the approved bounded delivery outcome."],"out_of_scope":["Do not plan execution, integration, lifecycle, or roadmap content."]},"units":[{"unit_id":"P3-T1","goal":"Deliver the first planned bounded outcome.","depends_on":[]},{"unit_id":"P3-T2","goal":"Deliver the second planned bounded outcome.","depends_on":["P3-T1"]}]}`)
}

func TestDeliveryPlanCandidateLifecyclePromotesToCurrentness(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationSharedDesign)
	bytes := deliveryPlanCandidateBytes(workspace.FeatureSlug)
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: workspace.FeatureSlug + ".delivery-plan.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	})
	if err != nil || candidate.Candidate.Family != CandidateFamilyDeliveryPlan || candidate.AuthorizedNextAction != "review_candidate" {
		t.Fatalf("delivery plan admission = %#v, %v", candidate, err)
	}
	if candidate.Candidate.Destination != string(DiscoveryDestinationSharedDesign) {
		t.Fatalf("delivery plan destination = %q", candidate.Candidate.Destination)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, candidate.Candidate.CandidateID, "approved exact delivery plan candidate", bytes)
	promoted, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.DeliveryPlan.PlanID == "" || promoted.DeliveryPlan.WorkspaceRowID != workspace.ID || promoted.DeliveryPlan.ArtifactSha256 != digestForPlanningTest(bytes) {
		t.Fatalf("promoted plan = %#v", promoted.DeliveryPlan)
	}
	if !promoted.Workspace.CurrentDeliveryPlanRowID.Valid || promoted.Workspace.CurrentDeliveryPlanRowID.Int64 != promoted.DeliveryPlan.ID || promoted.Workspace.Version != workspace.Version+1 {
		t.Fatalf("promoted workspace = %#v", promoted.Workspace)
	}
	units, err := store.ListDeliveryPlanUnitsByPlan(ctx, promoted.DeliveryPlan.ID)
	if err != nil || len(units) != 2 || units[0].UnitID != "P3-T1" || units[1].UnitID != "P3-T2" {
		t.Fatalf("plan units = %#v, %v", units, err)
	}
	dependencies, err := store.ListDeliveryPlanUnitDependenciesByUnit(ctx, units[1].ID)
	if err != nil || len(dependencies) != 1 || dependencies[0].DependsOnUnitRowID != units[0].ID {
		t.Fatalf("plan dependencies = %#v, %v", dependencies, err)
	}
	if _, err := store.GetDeliveryPlanByCandidateRowID(ctx, candidate.Candidate.ID); err != nil {
		t.Fatalf("plan candidate binding = %v", err)
	}
	// A promoted delivery_plan candidate is never promotable again.
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: promoted.Workspace.Version, CreatedIdentity: "operator"}); !errors.Is(err, ErrCandidateApprovalInvalid) {
		t.Fatalf("re-promotion = %v", err)
	}
}

func TestDeliveryPlanCandidateRejectsInvalidDestinationAndFilename(t *testing.T) {
	ctx, _, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationDirectDeliveryTicket)
	bytes := deliveryPlanCandidateBytes(workspace.FeatureSlug)
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: workspace.FeatureSlug + ".delivery-plan.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationDirectDeliveryTicket, CreatedIdentity: "planner",
	}); !errors.Is(err, ErrInvalidCandidateDestination) {
		t.Fatalf("direct destination admission = %v", err)
	}
	ctx, _, service, workspace, _ = deliveryTicketCandidateFixture(t, DiscoveryDestinationSharedDesign)
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: workspace.FeatureSlug + ".design.md", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	}); !errors.Is(err, ErrInvalidCandidateInput) {
		t.Fatalf("noncanonical filename admission = %v", err)
	}
}

func TestDeliveryPlanPromotionBindsExactCandidateAndCurrentness(t *testing.T) {
	ctx, store, service, workspace, _ := deliveryTicketCandidateFixture(t, DiscoveryDestinationSharedDesign)
	bytes := deliveryPlanCandidateBytes(workspace.FeatureSlug)
	candidate, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: workspace.FeatureSlug + ".delivery-plan.json", Bytes: bytes, SHA256: digestForPlanningTest(bytes),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	})
	if err != nil {
		t.Fatal(err)
	}
	approval := approveCurrentPlanningCandidate(t, ctx, service, workspace, candidate.Candidate.CandidateID, "approved exact delivery plan candidate", bytes)
	if _, err := service.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{CandidateID: candidate.Candidate.CandidateID, ApprovalID: approval.Approval.ApprovalID, ExpectedVersion: workspace.Version, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	// A newer immutable delivery_plan candidate on the same basis cannot
	// produce a second current Plan.
	promotedWorkspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	other := deliveryPlanCandidateBytes("other")
	if _, err := service.AdmitPlanningCandidate(ctx, CandidateAdmissionInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: promotedWorkspace.Version, Family: CandidateFamilyDeliveryPlan,
		Filename: "other.delivery-plan.json", Bytes: other, SHA256: digestForPlanningTest(other),
		RepoTarget: "candidate-production", Branch: "main", BaseCommit: strings.Repeat("a", 40),
		Destination: DiscoveryDestinationSharedDesign, CreatedIdentity: "planner",
	}); err != nil {
		t.Fatal(err)
	}
	var planCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_plans WHERE workspace_row_id = ?`, workspace.ID).Scan(&planCount); err != nil || planCount != 1 {
		t.Fatalf("plan rows = %d, %v", planCount, err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || !current.CurrentDeliveryPlanRowID.Valid {
		t.Fatalf("current plan pointer = %#v, %v", current.CurrentDeliveryPlanRowID, err)
	}
	// The immutable plan rows cannot be mutated or deleted.
	if _, err := store.DB().Exec(`UPDATE delivery_plans SET goal = 'mutated'`); err == nil {
		t.Fatal("delivery plan was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM delivery_plans`); err == nil {
		t.Fatal("delivery plan was deletable")
	}
	// No authority layer or package membership exists for the Plan.
	var layerCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM feature_workspace_authority_layers WHERE candidate_artifact_row_id = ?`, candidate.Candidate.ArtifactRowID).Scan(&layerCount); err != nil || layerCount != 0 {
		t.Fatalf("plan authority layers = %d, %v", layerCount, err)
	}
	if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("plan promotion created a ticket: %v", err)
	}
	_ = workflowstore.FeatureWorkspace{}
}
