package registry

import "testing"

func TestTicketOperationInventoryContainsOnlyPlannerFrontier(t *testing.T) {
	operations := TicketOperations()
	if len(operations) != 1 {
		t.Fatalf("operation count = %d, want 1", len(operations))
	}
	if operations[0].OperationID != PlannerTicketFrontierOperationID || operations[0].Role != "planner" ||
		operations[0].SurfaceContract != PlannerTicketFrontierSurface ||
		len(operations[0].AllowedNonSourceActions) != 1 || operations[0].AllowedNonSourceActions[0] != TicketActionReadFrontier {
		t.Fatalf("planner frontier operation = %#v", operations[0])
	}
}

func TestTicketOperationForActionResolvesOnlyFrontierRead(t *testing.T) {
	op, ok := TicketOperationForAction(TicketActionReadFrontier)
	if !ok || op.OperationID != PlannerTicketFrontierOperationID {
		t.Fatalf("frontier read = %#v, %v", op, ok)
	}
	for _, action := range []AllowedAction{
		TicketActionPublish, TicketActionApprove, TicketActionUpdatePriority,
		TicketActionReplaceDependencies, TicketActionSelect,
		PackageActionPrepare, PackageActionApprove,
		MutationLeaseActionReconcile, FeatureCompletionActionComplete,
	} {
		if _, ok := TicketOperationForAction(action); ok {
			t.Fatalf("mutation action %q unexpectedly resolved through registry", action)
		}
	}
}

func TestTicketRegistryDoesNotReturnLocalOperatorTicketWorkflow(t *testing.T) {
	for _, op := range TicketOperations() {
		if op.OperationID == "local_operator.ticket_workflow" {
			t.Fatalf("local_operator.ticket_workflow found in TicketOperations()")
		}
	}
	if _, ok := TicketOperationForAction("prepare_execution_package"); ok {
		t.Fatal("package prepare action resolved through ticket registry")
	}
	if _, ok := TicketOperationForAction("reconcile_mutation_lease"); ok {
		t.Fatal("lease reconcile action resolved through ticket registry")
	}
	if _, ok := TicketOperationForAction("complete_feature_workspace"); ok {
		t.Fatal("completion action resolved through ticket registry")
	}
}

func TestTicketRoleProfilesExposeOnlyPlannerRead(t *testing.T) {
	profiles := TicketRoleProfiles()
	if len(profiles) != 1 {
		t.Fatalf("profile count = %d, want 1", len(profiles))
	}
	profile := profiles[0]
	if profile.Role != "planner" || len(profile.Operations) != 1 || profile.Operations[0] != PlannerTicketFrontierOperationID {
		t.Fatalf("profile = %#v", profile)
	}
	if profile.ManifestSHA256 == "" {
		t.Fatal("manifest SHA256 is empty")
	}
	operation, ok := Lookup(profile.Operations[0])
	if !ok || operation.Role != profile.Role || operation.SurfaceContract != profile.SurfaceContract {
		t.Fatalf("profile operation mismatch: %#v", profile)
	}
}
