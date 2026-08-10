package executor

import (
	"context"
	"strings"
	"testing"
)

// failedOutcomeInput builds the DeterministicOutcomeInput for a preflight
// failure with the requested operations coverage.
func failedOutcomeInput(coverage string) DeterministicOutcomeInput {
	return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightFailed, Coverage: coverage, Failure: &DeterministicPreflightFailure{Code: "source_missing", OperationIndex: 1, Path: "internal/example.go", Expected: "exists=true", Observed: "exists=false"}}}
}

// appliedOutcomeInput builds the DeterministicOutcomeInput for a successful
// deterministic application with the requested coverage.
func appliedOutcomeInput(coverage string) DeterministicOutcomeInput {
	plan := testDeterministicCreatePlan(coverage)
	model, err := validateDeterministicPlan(plan)
	if err != nil {
		panic(err)
	}
	application := applicationResult(model)
	return DeterministicOutcomeInput{Preflight: DeterministicPreflightResult{Status: DeterministicPreflightReady, Coverage: coverage, Plan: plan}, Application: &application}
}

// persistOutcome persists a deterministic outcome against the fixture Run.
func persistOutcome(t *testing.T, fixture *executionAssignmentFixture, input DeterministicOutcomeInput) DeterministicOutcomeResult {
	t.Helper()
	service, err := NewDeterministicOutcomeService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	input.RunID = fixture.run.RunID
	result, err := service.Persist(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// testfixturesTicket returns the canonical approved Delivery Ticket bytes used
// by the executor fixtures.
func testfixturesTicket(t *testing.T) string {
	t.Helper()
	return string(packageDeliveryTicketBytes(strings.Repeat("a", 40)))
}
