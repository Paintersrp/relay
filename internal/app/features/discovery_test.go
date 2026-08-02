package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestIntegratedDiscoveryRetainsExactBytesAndSeparatesTerminalIntegration(t *testing.T) {
	ctx := context.Background()
	store, artifactRowID, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-foundation", "discovery-foundation")
	if err != nil {
		t.Fatal(err)
	}
	initial := []byte("# Discovery\n\nExact bytes.\n")
	if _, _, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: initial, SHA256: discoveryTestDigest(initial), CreatedIdentity: "operator"}); !errors.Is(err, ErrDiscoveryCapabilityDisabled) {
		t.Fatalf("disabled start error = %v", err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: initial, SHA256: discoveryTestDigest(initial), CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Revision.RevisionNumber != 1 || string(started.Markdown) != string(initial) || !strings.HasPrefix(started.Artifact.RelativePath, "feature-discovery/"+workspace.WorkspaceID+"/") {
		t.Fatalf("start = %#v", started)
	}

	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-foundation-item", WorkspaceRowID: workspace.ID, TicketKey: "item", Subject: "terminal item"})
		if err != nil {
			return err
		}
		if _, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-foundation-item", TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sqlNullInt64(artifactRowID), ArtifactSha256: strings.Repeat("b", 64)}); err != nil {
			return err
		}
		ticket, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "resolved", ticket.Version)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	frontier, err := service.ReadIntegratedDiscoveryFrontier(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.WorkItems) != 1 || !frontier.WorkItems[0].PendingIntegration || string(frontier.Current.Markdown) != string(initial) {
		t.Fatalf("frontier before integration = %#v", frontier)
	}
	consequence, updated, err := service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: "resolution-foundation-item", Consequence: "no_material_change", ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "exact terminal evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if consequence.ConsequenceKind != "no_material_change" || updated.CurrentDiscoveryRevisionRowID.Int64 != started.Revision.ID {
		t.Fatalf("integration = %#v %#v", consequence, updated)
	}
}

func discoveryTestDigest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}
func sqlNullInt64(value int64) sql.NullInt64 { return sql.NullInt64{Int64: value, Valid: true} }

func TestIntegrateDiscoveryResultRejectsInvalidSupersessionTopologyWithoutMutation(t *testing.T) {
	tests := []struct {
		name                string
		setup               func(t *testing.T, fixture *discoveryIntegrationFixture)
		source, replacement string
	}{
		{
			name:   "direct self reference",
			setup:  func(t *testing.T, fixture *discoveryIntegrationFixture) {},
			source: "discovery-a", replacement: "discovery-a",
		},
		{
			name: "direct two item cycle",
			setup: func(t *testing.T, fixture *discoveryIntegrationFixture) {
				fixture.integrateSuperseded(t, "discovery-a", "discovery-b")
			},
			source: "discovery-b", replacement: "discovery-a",
		},
		{
			name: "multi hop cycle",
			setup: func(t *testing.T, fixture *discoveryIntegrationFixture) {
				fixture.integrateSuperseded(t, "discovery-a", "discovery-b")
				fixture.integrateSuperseded(t, "discovery-b", "discovery-c")
			},
			source: "discovery-c", replacement: "discovery-a",
		},
		{
			name: "existing replacement cycle",
			setup: func(t *testing.T, fixture *discoveryIntegrationFixture) {
				fixture.insertSupersession(t, "discovery-b", "discovery-c")
				fixture.insertSupersession(t, "discovery-c", "discovery-b")
			},
			source: "discovery-a", replacement: "discovery-b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDiscoveryIntegrationFixture(t, "discovery-a", "discovery-b", "discovery-c")
			test.setup(t, fixture)
			before := fixture.state(t)
			err := fixture.tryIntegrateSuperseded(test.source, test.replacement)
			if !errors.Is(err, ErrInvalidDiscoverySupersessionTopology) {
				t.Fatalf("integration error = %v", err)
			}
			if after := fixture.state(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected integration mutated state:\n before: %#v\n after: %#v", before, after)
			}
		})
	}
}

func TestIntegrateDiscoveryResultAllowsAcyclicSupersessionChain(t *testing.T) {
	fixture := newDiscoveryIntegrationFixture(t, "discovery-a", "discovery-b", "discovery-c", "discovery-d")
	fixture.integrateSuperseded(t, "discovery-b", "discovery-c")
	fixture.integrateSuperseded(t, "discovery-c", "discovery-d")
	fixture.integrateSuperseded(t, "discovery-a", "discovery-b")
	consequences, err := fixture.store.ListDiscoveryIntegrationConsequences(context.Background(), fixture.workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(consequences) != 3 || consequences[2].TicketRowID != fixture.tickets["discovery-a"].ID || consequences[2].ReplacementTicketRowID.Int64 != fixture.tickets["discovery-b"].ID {
		t.Fatalf("consequences = %#v", consequences)
	}
}

type discoveryIntegrationFixture struct {
	ctx         context.Context
	store       *workflowstore.Store
	service     *Service
	workspace   workflowstore.FeatureWorkspace
	initial     []byte
	tickets     map[string]workflowstore.FeatureWorkspaceDiscoveryTicket
	resolutions map[string]workflowstore.FeatureWorkspaceTicketResolution
}

type discoveryIntegrationState struct {
	workspace     workflowstore.FeatureWorkspace
	tickets       []workflowstore.FeatureWorkspaceDiscoveryTicket
	consequences  []workflowstore.DiscoveryIntegrationConsequence
	artifactCount int
	current       workflowstore.IntegratedDiscoveryRevision
	markdown      []byte
}

func newDiscoveryIntegrationFixture(t *testing.T, ids ...string) *discoveryIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	store, artifactRowID, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-topology", "discovery-topology")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	initial := []byte("# Initial discovery\n")
	_, workspace, err = service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: initial, SHA256: discoveryTestDigest(initial), CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &discoveryIntegrationFixture{ctx: ctx, store: store, service: service, workspace: workspace, initial: initial, tickets: map[string]workflowstore.FeatureWorkspaceDiscoveryTicket{}, resolutions: map[string]workflowstore.FeatureWorkspaceTicketResolution{}}
	err = store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		for _, id := range ids {
			ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: id, WorkspaceRowID: workspace.ID, TicketKey: id, Subject: id})
			if err != nil {
				return err
			}
			resolution, err := tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-" + id, TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sqlNullInt64(artifactRowID), ArtifactSha256: strings.Repeat("b", 64)})
			if err != nil {
				return err
			}
			ticket, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, id, "open", "resolved", ticket.Version)
			if err != nil {
				return err
			}
			fixture.tickets[id], fixture.resolutions[id] = ticket, resolution
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *discoveryIntegrationFixture) tryIntegrateSuperseded(source, replacement string) error {
	workspace, err := f.store.GetFeatureWorkspaceByWorkspaceID(f.ctx, f.workspace.WorkspaceID)
	if err != nil {
		return err
	}
	ticket, err := f.ticket(source)
	if err != nil {
		return err
	}
	_, _, err = f.service.IntegrateDiscoveryResult(f.ctx, IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: source, ResolutionID: "resolution-" + source, Consequence: "superseded", ReplacementTicketID: replacement, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "terminal discovery superseded"})
	return err
}

func (f *discoveryIntegrationFixture) integrateSuperseded(t *testing.T, source, replacement string) {
	t.Helper()
	if err := f.tryIntegrateSuperseded(source, replacement); err != nil {
		t.Fatal(err)
	}
}

func (f *discoveryIntegrationFixture) insertSupersession(t *testing.T, source, replacement string) {
	t.Helper()
	err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDiscoveryIntegrationConsequence(f.ctx, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-existing-" + source, WorkspaceRowID: f.workspace.ID, TicketRowID: f.tickets[source].ID, ResolutionRowID: f.resolutions[source].ID, ConsequenceKind: "superseded", ReplacementTicketRowID: sqlNullInt64(f.tickets[replacement].ID), EvidenceBasis: "existing legacy supersession"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *discoveryIntegrationFixture) ticket(id string) (workflowstore.FeatureWorkspaceDiscoveryTicket, error) {
	var result workflowstore.FeatureWorkspaceDiscoveryTicket
	err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		var err error
		result, err = tx.GetFeatureWorkspaceDiscoveryTicketByID(f.ctx, id)
		return err
	})
	return result, err
}

func (f *discoveryIntegrationFixture) state(t *testing.T) discoveryIntegrationState {
	t.Helper()
	workspace, err := f.store.GetFeatureWorkspaceByWorkspaceID(f.ctx, f.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	tickets, err := f.store.ListFeatureWorkspaceDiscoveryTickets(f.ctx, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	consequences, err := f.store.ListDiscoveryIntegrationConsequences(f.ctx, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.store.GetCurrentIntegratedDiscoveryRevision(f.ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	frontier, err := f.service.ReadIntegratedDiscoveryFrontier(f.ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if string(frontier.Current.Markdown) != string(f.initial) {
		t.Fatalf("initial artifact changed = %q", frontier.Current.Markdown)
	}
	var artifactCount int
	if err := f.store.DB().QueryRowContext(f.ctx, `SELECT count(*) FROM feature_workspace_discovery_artifacts WHERE workspace_row_id = ?`, workspace.ID).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	return discoveryIntegrationState{workspace: workspace, tickets: tickets, consequences: consequences, artifactCount: artifactCount, current: current, markdown: frontier.Current.Markdown}
}
