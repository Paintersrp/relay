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

// TestPlannerTicketFrontierPublishesCanonicalV2Contract asserts the published
// planner.ticket_frontier operation exposes the canonical v2 workspace
// frontier surface, semantic projection, and Planner ticket_frontier manifest
// domain, with no separate v2 operation ID and no retained v1 identity.
func TestPlannerTicketFrontierPublishesCanonicalV2Contract(t *testing.T) {
	operation, ok := LookupPublishedOperation(PlannerTicketFrontierOperationID)
	if !ok {
		t.Fatal("published planner.ticket_frontier operation is missing")
	}
	if operation.OperationID != "planner.ticket_frontier" {
		t.Fatalf("operation ID = %q, want planner.ticket_frontier", operation.OperationID)
	}
	if operation.SurfaceContract != "planner-ticket-frontier.v2" {
		t.Fatalf("surface = %q, want planner-ticket-frontier.v2", operation.SurfaceContract)
	}
	if operation.PacketSemanticProjection != "relay.semantic.ticket-frontier-read.v2" {
		t.Fatalf("packet semantic projection = %q, want relay.semantic.ticket-frontier-read.v2", operation.PacketSemanticProjection)
	}
	if operation.ManifestDomain != "ticket_frontier" {
		t.Fatalf("manifest domain = %q, want ticket_frontier", operation.ManifestDomain)
	}
	operations, err := ListPublishedOperations()
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range operations {
		if candidate.OperationID == "planner.ticket_frontier_v2" {
			t.Fatal("planner.ticket_frontier_v2 is separately registered")
		}
		if candidate.SurfaceContract == "planner-ticket-frontier.v1" || candidate.PacketSemanticProjection == "relay.semantic.ticket-frontier-read.v1" {
			t.Fatalf("active v1 frontier identity remains published on %q", candidate.OperationID)
		}
	}
}

// TestPlannerTicketFrontierRoutePublishesV2Surface asserts the published
// frontier route binds planner.ticket_frontier to the v2 surface and no route
// publishes the former v1 frontier surface.
func TestPlannerTicketFrontierRoutePublishesV2Surface(t *testing.T) {
	routes, err := ListRouteDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, route := range routes {
		if route.Surface == "planner-ticket-frontier.v1" {
			t.Fatal("frontier v1 route surface remains published")
		}
		for _, operation := range route.Operations {
			if operation == PlannerTicketFrontierOperationID {
				if route.Surface != "planner-ticket-frontier.v2" {
					t.Fatalf("frontier route surface = %q, want planner-ticket-frontier.v2", route.Surface)
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("planner.ticket_frontier is not a published route member")
	}
}
