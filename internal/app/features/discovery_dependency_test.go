package features

import (
	"context"
	"errors"
	"reflect"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestDiscoveryBlockingDependencyEligibilityAndSatisfaction(t *testing.T) {
	cases := []struct {
		name           string
		setup          func(*discoveryIntegrationFixture)
		wantEligible   bool
		wantBlockerKey string
	}{
		{name: "open dependency", setup: func(f *discoveryIntegrationFixture) { setDiscoveryTicketState(t, f, "discovery-target", "open") }, wantEligible: false, wantBlockerKey: "discovery-target"},
		{name: "blocked dependency", setup: func(f *discoveryIntegrationFixture) { setDiscoveryTicketState(t, f, "discovery-target", "blocked") }, wantEligible: false, wantBlockerKey: "discovery-target"},
		{name: "resolved without consequence", setup: func(_ *discoveryIntegrationFixture) {}, wantEligible: false, wantBlockerKey: "discovery-target"},
		{name: "cancelled without consequence", setup: func(f *discoveryIntegrationFixture) { setDiscoveryTicketState(t, f, "discovery-target", "cancelled") }, wantEligible: false, wantBlockerKey: "discovery-target"},
		{name: "integrated consequence", setup: func(f *discoveryIntegrationFixture) { integrateDependencyTarget(t, f, "integrated") }, wantEligible: true},
		{name: "no material change consequence", setup: func(f *discoveryIntegrationFixture) { integrateDependencyTarget(t, f, "no_material_change") }, wantEligible: true},
		{name: "superseded to integrated", setup: func(f *discoveryIntegrationFixture) {
			supersedeDependencyTarget(t, f)
			integrateDependencyReplacement(t, f, "integrated")
		}, wantEligible: true},
		{name: "superseded to no material change", setup: func(f *discoveryIntegrationFixture) {
			supersedeDependencyTarget(t, f)
			integrateDependencyReplacement(t, f, "no_material_change")
		}, wantEligible: true},
		{name: "superseded to open", setup: func(f *discoveryIntegrationFixture) {
			supersedeDependencyTarget(t, f)
			setDiscoveryTicketState(t, f, "discovery-replacement", "open")
		}, wantEligible: false, wantBlockerKey: "discovery-replacement"},
		{name: "superseded to terminal without consequence", setup: func(f *discoveryIntegrationFixture) { supersedeDependencyTarget(t, f) }, wantEligible: false, wantBlockerKey: "discovery-replacement"},
		{name: "cyclic chain", setup: func(f *discoveryIntegrationFixture) {
			f.insertSupersession(t, "discovery-target", "discovery-replacement")
			f.insertSupersession(t, "discovery-replacement", "discovery-target")
		}, wantEligible: false, wantBlockerKey: "discovery-target"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-dependent", "discovery-target", "discovery-replacement")
			setDiscoveryTicketState(t, fixture, "discovery-dependent", "open")
			test.setup(fixture)
			addDiscoveryDependency(t, fixture, "discovery-dependent", "discovery-target", "blocks")
			frontier, err := fixture.service.ReadIntegratedDiscoveryFrontier(fixture.ctx, fixture.workspace.WorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			summary := discoverySummary(t, frontier, "discovery-dependent")
			if summary.Eligible != test.wantEligible || (test.wantBlockerKey != "" && summary.BlockingTicketID != test.wantBlockerKey) {
				t.Fatalf("dependent summary = %#v, want eligible=%t blocker=%q", summary, test.wantEligible, test.wantBlockerKey)
			}
		})
	}
}

func TestDiscoveryInformingDependenciesNeverBlockEligibility(t *testing.T) {
	for _, name := range []string{"open", "terminal", "unintegrated terminal"} {
		t.Run(name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-informing-dependent", "discovery-informing-target")
			setDiscoveryTicketState(t, fixture, "discovery-informing-dependent", "open")
			if name == "open" {
				setDiscoveryTicketState(t, fixture, "discovery-informing-target", "open")
			}
			addDiscoveryDependency(t, fixture, "discovery-informing-dependent", "discovery-informing-target", "informs")
			frontier, err := fixture.service.ReadIntegratedDiscoveryFrontier(fixture.ctx, fixture.workspace.WorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if summary := discoverySummary(t, frontier, "discovery-informing-dependent"); !summary.Eligible {
				t.Fatalf("informing dependency blocked eligibility: %#v", summary)
			}
		})
	}
}

func TestDiscoveryInvalidDependencyChainIsUnsatisfied(t *testing.T) {
	fixture := newDiscoveryIntegrationFixture(t, "discovery-invalid-chain")
	if satisfied, blocker := discoveryDependencySatisfied(fixture.ctx, fixture.store, map[int64]workflowstore.FeatureWorkspaceDiscoveryTicket{}, map[int64]workflowstore.DiscoveryIntegrationConsequence{}, 999, map[int64]bool{}); satisfied || blocker != "unknown" {
		t.Fatalf("invalid dependency chain result = satisfied %t blocker %q", satisfied, blocker)
	}
}

func TestDiscoveryDependencyMutationValidationAndDuplicateContract(t *testing.T) {
	ctx := context.Background()
	fixture := newDiscoveryIntegrationFixture(t, "discovery-dependency-source", "discovery-dependency-target")
	setDiscoveryTicketState(t, fixture, "discovery-dependency-source", "open")
	source := fixture.tickets["discovery-dependency-source"]

	cases := []struct {
		name  string
		input DiscoveryWorkItemInput
	}{
		{name: "self dependency", input: DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: source.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: source.DiscoveryTicketID, Kind: "blocks"}}}},
		{name: "unsupported kind", input: DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: source.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: "discovery-dependency-target", Kind: "requires"}}}},
		{name: "stale version", input: DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: source.Version - 1, Dependencies: []DiscoveryDependencyInput{{TicketID: "discovery-dependency-target", Kind: "blocks"}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := fixture.state(t)
			if _, err := fixture.service.UpdateDiscoveryWorkItem(ctx, test.input); err == nil {
				t.Fatal("invalid dependency mutation succeeded")
			}
			if after := fixture.state(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid dependency mutation changed state: before=%#v after=%#v", before, after)
			}
		})
	}

	otherWorkspace, err := createFeatureWorkspace(ctx, fixture.store, "workspace-discovery-dependency-other", "dependency-other")
	if err != nil {
		t.Fatal(err)
	}
	var otherTicket workflowstore.FeatureWorkspaceDiscoveryTicket
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		otherTicket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-dependency-cross", WorkspaceRowID: otherWorkspace.ID, TicketKey: "cross", Subject: "cross workspace"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := fixture.state(t)
	if _, err := fixture.service.UpdateDiscoveryWorkItem(ctx, DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: source.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: otherTicket.DiscoveryTicketID, Kind: "blocks"}}}); !errors.Is(err, ErrDiscoveryCrossWorkspace) {
		t.Fatalf("cross-workspace dependency error = %v", err)
	}
	if after := fixture.state(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("cross-workspace dependency changed state: before=%#v after=%#v", before, after)
	}

	updated, err := fixture.service.UpdateDiscoveryWorkItem(ctx, DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: source.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: "discovery-dependency-target", Kind: "blocks"}}})
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := fixture.store.ListFeatureWorkspaceTicketDependencies(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 || updated.Version != source.Version+1 {
		t.Fatalf("first dependency insertion = %#v, version %d", dependencies, updated.Version)
	}
	if _, err := fixture.service.UpdateDiscoveryWorkItem(ctx, DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: updated.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: "discovery-dependency-target", Kind: "blocks"}}}); err == nil {
		t.Fatal("duplicate dependency insertion unexpectedly succeeded")
	}
	dependencies, err = fixture.store.ListFeatureWorkspaceTicketDependencies(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 {
		t.Fatalf("duplicate dependency multiplied state: %#v", dependencies)
	}
}

func addDiscoveryDependency(t *testing.T, fixture *discoveryIntegrationFixture, source, target, kind string) {
	t.Helper()
	ticket, err := fixture.ticket(source)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := fixture.service.UpdateDiscoveryWorkItem(fixture.ctx, DiscoveryWorkItemInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source, Kind: "investigation", ExpectedVersion: ticket.Version, Dependencies: []DiscoveryDependencyInput{{TicketID: target, Kind: kind}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.tickets[source] = updated
}

func integrateDependencyTarget(t *testing.T, fixture *discoveryIntegrationFixture, consequence string) {
	t.Helper()
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := fixture.ticket("discovery-target")
	if err != nil {
		t.Fatal(err)
	}
	input := IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: "resolution-discovery-target", Consequence: consequence, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "dependency evidence"}
	if consequence == "integrated" {
		input.Markdown = []byte("# dependency\n")
		input.ExpectedSHA256 = discoveryTestDigest(input.Markdown)
		input.CreatedIdentity = "operator"
	}
	if _, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); err != nil {
		t.Fatal(err)
	} else {
		fixture.workspace = updated
	}
}

func integrateDependencyReplacement(t *testing.T, fixture *discoveryIntegrationFixture, consequence string) {
	t.Helper()
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := fixture.ticket("discovery-replacement")
	if err != nil {
		t.Fatal(err)
	}
	input := IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: "resolution-discovery-replacement", Consequence: consequence, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "replacement evidence"}
	if consequence == "integrated" {
		input.Markdown = []byte("# replacement\n")
		input.ExpectedSHA256 = discoveryTestDigest(input.Markdown)
		input.CreatedIdentity = "operator"
	}
	if _, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); err != nil {
		t.Fatal(err)
	} else {
		fixture.workspace = updated
	}
}

func supersedeDependencyTarget(t *testing.T, fixture *discoveryIntegrationFixture) {
	t.Helper()
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket := fixture.tickets["discovery-target"]
	_, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: "resolution-discovery-target", Consequence: "superseded", ReplacementTicketID: "discovery-replacement", ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "superseded dependency"})
	if err != nil {
		t.Fatal(err)
	}
	fixture.workspace = updated
}

func discoverySummary(t *testing.T, frontier DiscoveryFrontier, ticketID string) DiscoveryWorkItemSummary {
	t.Helper()
	for _, summary := range frontier.WorkItems {
		if summary.Ticket.DiscoveryTicketID == ticketID {
			return summary
		}
	}
	t.Fatalf("ticket %q not in frontier", ticketID)
	return DiscoveryWorkItemSummary{}
}
