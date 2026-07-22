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
	if len(operations) != 17 || len(routes) != 7 {
		t.Fatalf("operations=%d routes=%d", len(operations), len(routes))
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
	if len(seen) != 40 {
		t.Fatalf("tools=%d", len(seen))
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
