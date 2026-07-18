package registry

import "testing"

func TestTicketOperationInventoryIsClosedAndOrdered(t *testing.T) {
	operations := TicketOperations()
	if len(operations) != 2 {
		t.Fatalf("operation count = %d", len(operations))
	}

	if operations[0].OperationID != PlannerTicketFrontierOperationID || operations[0].Role != "planner" ||
		operations[0].SurfaceContract != PlannerTicketFrontierSurface ||
		len(operations[0].AllowedNonSourceActions) != 1 || operations[0].AllowedNonSourceActions[0] != TicketActionReadFrontier {
		t.Fatalf("planner frontier operation = %#v", operations[0])
	}

	wantMutations := []AllowedAction{
		TicketActionPublish,
		TicketActionApprove,
		TicketActionUpdatePriority,
		TicketActionReplaceDependencies,
		TicketActionSelect,
	}
	if operations[1].OperationID != LocalOperatorTicketWorkflowOperationID || operations[1].Role != "local_operator" ||
		operations[1].SurfaceContract != LocalOperatorTicketWorkflowSurface ||
		len(operations[1].AllowedNonSourceActions) != len(wantMutations) {
		t.Fatalf("local-operator workflow operation = %#v", operations[1])
	}
	for index, want := range wantMutations {
		if got := operations[1].AllowedNonSourceActions[index]; got != want {
			t.Fatalf("mutation action %d = %q, want %q", index, got, want)
		}
	}

	for _, operation := range operations {
		for _, action := range operation.AllowedNonSourceActions {
			owner, ok := TicketOperationForAction(action)
			if !ok || owner.OperationID != operation.OperationID || owner.SurfaceContract != operation.SurfaceContract {
				t.Fatalf("action %q owner = %#v, %v", action, owner, ok)
			}
		}
	}
	if _, ok := TicketOperationForAction("create_package"); ok {
		t.Fatal("package action was admitted")
	}
}

func TestTicketRoleProfilesExposeOnlyPlannerReadAndLocalOperatorMutations(t *testing.T) {
	profiles := TicketRoleProfiles()
	if len(profiles) != 2 {
		t.Fatalf("profile count = %d", len(profiles))
	}
	for _, profile := range profiles {
		if profile.ManifestSHA256 == "" || len(profile.Operations) != 1 {
			t.Fatalf("invalid profile: %#v", profile)
		}
		operation, ok := Lookup(profile.Operations[0])
		if !ok || operation.Role != profile.Role || operation.SurfaceContract != profile.SurfaceContract {
			t.Fatalf("profile operation mismatch: %#v", profile)
		}
	}
	if profiles[0].Role != "planner" || profiles[0].Operations[0] != PlannerTicketFrontierOperationID ||
		profiles[1].Role != "local_operator" || profiles[1].Operations[0] != LocalOperatorTicketWorkflowOperationID {
		t.Fatalf("profiles = %#v", profiles)
	}
}
