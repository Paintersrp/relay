package registry

import "testing"

func TestPublishedContractsArePinnedClosedAndDefensive(t *testing.T) {
	if err := ValidatePublishedContracts(); err != nil {
		t.Fatal(err)
	}
	operations, err := ListPublishedOperations()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := ListRouteDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 16 || len(routes) != 7 {
		t.Fatalf("operations=%d routes=%d", len(operations), len(routes))
	}
	// The pinned c166ca manifests publish the Planner Delivery Plan and the
	// Auditor Delivery Plan Review operations on their exact surfaces and
	// manifest domains, with their family tools mounted on the same routes.
	for _, wanted := range []struct {
		id       OperationID
		role     Role
		surface  SurfaceContractID
		domain   ManifestDomain
		userTool string
	}{
		{"planner.delivery_plan", "planner", "planner-authoring.v1", "delivery_plan", "open_delivery_plan"},
		{"auditor.delivery_plan_review", "auditor", "auditor-review.v1", "delivery_plan_review", "open_delivery_plan_review"},
	} {
		operation, ok := LookupPublishedOperation(wanted.id)
		if !ok || operation.Role != wanted.role || operation.SurfaceContract != wanted.surface || operation.ManifestDomain != wanted.domain || operation.SourcePolicy == "" || operation.HistoricalAuthority == "" {
			t.Fatalf("published %s=%#v", wanted.id, operation)
		}
		onRoute := false
		for _, route := range routes {
			if route.Surface != wanted.surface {
				continue
			}
			for _, member := range route.Operations {
				if member == wanted.id {
					onRoute = true
				}
			}
			for _, name := range route.Tools {
				if name == wanted.userTool {
					tool, ok := LookupPublishedToolContract(name)
					if !ok || tool.OperationID != wanted.id || tool.Adapter == "" || tool.DispatcherOwner == "" {
						t.Fatalf("family tool %q=%#v", name, tool)
					}
				}
			}
		}
		if !onRoute {
			t.Fatalf("%s is not a route member on %s", wanted.id, wanted.surface)
		}
	}
	seen := map[string]struct{}{}
	for _, route := range routes {
		for _, name := range route.Tools {
			seen[name] = struct{}{}
			tool, ok := LookupPublishedToolContract(name)
			if !ok || len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 || tool.Adapter == "" || tool.DispatcherOwner == "" {
				t.Fatalf("invalid tool %q", name)
			}
		}
	}
	if len(seen) != 39 {
		t.Fatalf("tools=%d", len(seen))
	}
	if _, ok := seen["create_run"]; ok {
		t.Fatal("retired create_run tool is published by a route")
	}
	if _, ok := LookupPublishedToolContract("create_run"); ok {
		t.Fatal("retired create_run tool is published")
	}
	for _, forbidden := range []OperationID{"planner.plan", "planner.one_shot_execution_spec", "auditor.plan_review", "auditor.remediation_execution_spec", "features.authority", "local_operator.ticket_workflow"} {
		if _, ok := LookupPublishedOperation(forbidden); ok {
			t.Fatalf("forbidden %q", forbidden)
		}
	}
	operations[0].RequiredInputs = append(operations[0].RequiredInputs, InputSlotDefinition{})
	again, _ := ListPublishedOperations()
	if len(operations[0].RequiredInputs) == len(again[0].RequiredInputs) {
		t.Fatal("operation clone is aliased")
	}
}
