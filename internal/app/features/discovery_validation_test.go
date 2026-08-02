package features

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestIntegratedDiscoveryConsequenceInputValidationIsAtomic(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*discoveryIntegrationFixture, *IntegrateDiscoveryResultInput)
	}{
		{name: "missing markdown", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.Markdown = nil
			input.ExpectedSHA256 = ""
		}},
		{name: "invalid lowercase sha", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedSHA256 = strings.Repeat("A", 64)
		}},
		{name: "digest mismatch", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedSHA256 = strings.Repeat("0", 64)
		}},
		{name: "missing creator", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.CreatedIdentity = " "
		}},
		{name: "missing evidence", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) { input.EvidenceBasis = "\t" }},
		{name: "nonterminal work item", mutate: func(fixture *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			setDiscoveryTicketState(t, fixture, input.TicketID, "open")
			input.ExpectedWorkItemVersion = fixture.tickets[input.TicketID].Version
		}},
		{name: "resolution belongs to another work item", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ResolutionID = "resolution-discovery-integrated-other"
		}},
		{name: "stale workspace version", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedWorkspaceVersion--
		}},
		{name: "stale work item version", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedWorkItemVersion--
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-integrated", "discovery-integrated-other")
			before := discoveryIntegrationState{}
			content := []byte("# integrated\n")
			workspace := fixture.workspace
			input := IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-integrated", ResolutionID: "resolution-discovery-integrated", Consequence: "integrated", ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-integrated"].Version, ExpectedSHA256: discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "valid evidence"}
			test.mutate(fixture, &input)
			before = fixture.state(t)
			if _, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); err == nil {
				t.Fatal("invalid integrated consequence succeeded")
			}
			if after := fixture.state(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid integrated consequence mutated state: before=%#v after=%#v", before, after)
			}
		})
	}

	fixture := newDiscoveryIntegrationFixture(t, "discovery-integrated-valid")
	before := fixture.state(t)
	content := []byte("# valid integrated\n")
	consequence, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-integrated-valid", ResolutionID: "resolution-discovery-integrated-valid", Consequence: "integrated", ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-integrated-valid"].Version, ExpectedSHA256: discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "valid evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if consequence.ConsequenceKind != "integrated" || !consequence.ProducedRevisionRowID.Valid || updated.CurrentDiscoveryRevisionRowID.Int64 != consequence.ProducedRevisionRowID.Int64 {
		t.Fatalf("valid integrated result = %#v, %#v", consequence, updated)
	}
	assertDiscoveryCounts(t, fixture.store, updated.ID, beforeStateCount(before, "artifacts")+1, beforeStateCount(before, "revisions")+1, beforeStateCount(before, "consequences")+1)
}

func TestNoMaterialChangeValidationAndSuccess(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*discoveryIntegrationFixture, *IntegrateDiscoveryResultInput)
	}{
		{name: "markdown supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.Markdown = []byte("not allowed")
		}},
		{name: "produced digest supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedSHA256 = strings.Repeat("a", 64)
		}},
		{name: "produced identity supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.CreatedIdentity = "operator"
		}},
		{name: "replacement supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ReplacementTicketID = "discovery-no-material-other"
		}},
		{name: "nonterminal work item", mutate: func(fixture *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			setDiscoveryTicketState(t, fixture, input.TicketID, "open")
			input.ExpectedWorkItemVersion = fixture.tickets[input.TicketID].Version
		}},
		{name: "resolution belongs to another work item", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ResolutionID = "resolution-no-material-other"
		}},
		{name: "stale workspace version", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedWorkspaceVersion--
		}},
		{name: "stale work item version", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedWorkItemVersion--
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-no-material", "discovery-no-material-other")
			input := noMaterialInput(fixture, "discovery-no-material")
			test.mutate(fixture, &input)
			before := fixture.state(t)
			if _, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); err == nil {
				t.Fatal("invalid no-material-change consequence succeeded")
			}
			if after := fixture.state(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid no-material-change mutated state: before=%#v after=%#v", before, after)
			}
		})
	}

	fixture := newDiscoveryIntegrationFixture(t, "discovery-no-material-valid")
	before := fixture.state(t)
	consequence, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, noMaterialInput(fixture, "discovery-no-material-valid"))
	if err != nil {
		t.Fatal(err)
	}
	if consequence.ConsequenceKind != "no_material_change" || consequence.ProducedRevisionRowID.Valid || updated.CurrentDiscoveryRevisionRowID.Int64 != before.current.ID {
		t.Fatalf("valid no-material-change = %#v, %#v", consequence, updated)
	}
	after := fixture.state(t)
	if after.artifactCount != before.artifactCount || after.current.ID != before.current.ID || len(after.consequences) != len(before.consequences)+1 {
		t.Fatalf("no-material-change created revision/artifact: before=%#v after=%#v", before, after)
	}

	workspace := updated
	ticket, err := fixture.ticket("discovery-no-material-valid")
	if err != nil {
		t.Fatal(err)
	}
	input := noMaterialInput(fixture, ticket.DiscoveryTicketID)
	input.ExpectedWorkspaceVersion = workspace.Version
	input.ExpectedWorkItemVersion = ticket.Version
	if _, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); !errors.Is(err, ErrDuplicateDiscoveryIntegration) {
		t.Fatalf("already integrated no-material-change error = %v", err)
	}
}

func TestSupersededValidationAndAcyclicSuccess(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*discoveryIntegrationFixture, *IntegrateDiscoveryResultInput)
	}{
		{name: "missing replacement", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ReplacementTicketID = ""
		}},
		{name: "unknown replacement", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ReplacementTicketID = "discovery-does-not-exist"
		}},
		{name: "markdown supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.Markdown = []byte("not allowed")
		}},
		{name: "produced digest supplied", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ExpectedSHA256 = strings.Repeat("a", 64)
		}},
		{name: "nonterminal source", mutate: func(fixture *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			setDiscoveryTicketState(t, fixture, input.TicketID, "open")
			input.ExpectedWorkItemVersion = fixture.tickets[input.TicketID].Version
		}},
		{name: "resolution belongs to source other", mutate: func(_ *discoveryIntegrationFixture, input *IntegrateDiscoveryResultInput) {
			input.ResolutionID = "resolution-discovery-superseded-other"
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-superseded", "discovery-superseded-other")
			input := supersededInput(fixture, "discovery-superseded", "discovery-superseded-other")
			test.mutate(fixture, &input)
			before := fixture.state(t)
			if _, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input); err == nil {
				t.Fatal("invalid superseded consequence succeeded")
			}
			if after := fixture.state(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("invalid superseded consequence mutated state: before=%#v after=%#v", before, after)
			}
		})
	}

	fixture := newDiscoveryIntegrationFixture(t, "discovery-superseded-valid", "discovery-superseded-replacement")
	before := fixture.state(t)
	consequence, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, supersededInput(fixture, "discovery-superseded-valid", "discovery-superseded-replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if consequence.ConsequenceKind != "superseded" || !consequence.ReplacementTicketRowID.Valid || updated.CurrentDiscoveryRevisionRowID.Int64 != before.current.ID || len(fixture.state(t).consequences) != len(before.consequences)+1 {
		t.Fatalf("valid superseded result = %#v, %#v", consequence, updated)
	}
}

func noMaterialInput(fixture *discoveryIntegrationFixture, ticketID string) IntegrateDiscoveryResultInput {
	ticket := fixture.tickets[ticketID]
	return IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: ticketID, ResolutionID: "resolution-" + ticketID, Consequence: "no_material_change", ExpectedWorkspaceVersion: fixture.workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "no material change evidence"}
}

func supersededInput(fixture *discoveryIntegrationFixture, source, replacement string) IntegrateDiscoveryResultInput {
	ticket := fixture.tickets[source]
	return IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: source, ResolutionID: "resolution-" + source, Consequence: "superseded", ReplacementTicketID: replacement, ExpectedWorkspaceVersion: fixture.workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "supersession evidence"}
}

func setDiscoveryTicketState(t *testing.T, fixture *discoveryIntegrationFixture, ticketID, state string) {
	t.Helper()
	err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		ticket, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(fixture.ctx, ticketID)
		if err != nil {
			return err
		}
		updated, err := tx.TransitionFeatureWorkspaceDiscoveryTicket(fixture.ctx, ticketID, ticket.State, state, ticket.Version)
		if err == nil {
			fixture.tickets[ticketID] = updated
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func beforeStateCount(state discoveryIntegrationState, kind string) int {
	switch kind {
	case "artifacts":
		return state.artifactCount
	case "revisions":
		return 1
	case "consequences":
		return len(state.consequences)
	default:
		return 0
	}
}
