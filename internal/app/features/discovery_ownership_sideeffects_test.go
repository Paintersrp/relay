package features

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestDiscoveryServiceRejectsCrossWorkspaceReferencesWithoutMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newDiscoveryIntegrationFixture(t, "discovery-owner-source", "discovery-owner-other")
	otherWorkspace, err := createFeatureWorkspace(ctx, fixture.store, "workspace-discovery-owner-other", "owner-other")
	if err != nil {
		t.Fatal(err)
	}
	var otherTicket workflowstore.FeatureWorkspaceDiscoveryTicket
	var otherResolution workflowstore.FeatureWorkspaceTicketResolution
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		otherTicket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-owner-cross", WorkspaceRowID: otherWorkspace.ID, TicketKey: "cross", Subject: "cross workspace"})
		if err != nil {
			return err
		}
		otherResolution, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-owner-cross", TicketRowID: otherTicket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sqlNullInt64(1), ArtifactSha256: strings.Repeat("b", 64)})
		if err != nil {
			return err
		}
		_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, otherTicket.DiscoveryTicketID, "open", "resolved", otherTicket.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	before := fixture.state(t)
	if _, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-owner-source", ResolutionID: otherResolution.ResolutionID, Consequence: "no_material_change", ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-owner-source"].Version, EvidenceBasis: "ownership"}); err == nil {
		t.Fatal("cross-workspace resolution succeeded")
	}
	if _, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: otherTicket.DiscoveryTicketID, ResolutionID: otherResolution.ResolutionID, Consequence: "no_material_change", ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: otherTicket.Version, EvidenceBasis: "ownership"}); !errors.Is(err, ErrDiscoveryCrossWorkspace) {
		t.Fatalf("cross-workspace source error = %v", err)
	}
	if _, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-owner-source", ResolutionID: "resolution-discovery-owner-source", Consequence: "superseded", ReplacementTicketID: otherTicket.DiscoveryTicketID, ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-owner-source"].Version, EvidenceBasis: "ownership"}); !errors.Is(err, ErrDiscoveryCrossWorkspace) {
		t.Fatalf("cross-workspace replacement error = %v", err)
	}
	if after := fixture.state(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("cross-workspace service mutation: before=%#v after=%#v", before, after)
	}
}

func TestDiscoveryPersistenceRejectsCrossWorkspaceRelationships(t *testing.T) {
	ctx := context.Background()
	fixture := newDiscoveryIntegrationFixture(t, "discovery-persistence-a")
	workspaceB, err := createFeatureWorkspace(ctx, fixture.store, "workspace-discovery-persistence-b", "persistence-b")
	if err != nil {
		t.Fatal(err)
	}
	var ticketB workflowstore.FeatureWorkspaceTicketResolution
	var ticketBValue workflowstore.FeatureWorkspaceDiscoveryTicket
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticketBValue, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-persistence-b", WorkspaceRowID: workspaceB.ID, TicketKey: "b", Subject: "workspace b"})
		if err != nil {
			return err
		}
		ticketB, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-discovery-persistence-b", TicketRowID: ticketBValue.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sqlNullInt64(1), ArtifactSha256: strings.Repeat("b", 64)})
		if err != nil {
			return err
		}
		if _, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticketBValue.DiscoveryTicketID, "open", "resolved", ticketBValue.Version); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var artifactB workflowstore.DiscoveryArtifact
	var revisionB workflowstore.IntegratedDiscoveryRevision
	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		artifactB, err = tx.CreateDiscoveryArtifact(ctx, workflowstore.DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-persistence-b", WorkspaceRowID: workspaceB.ID, RelativePath: "feature-discovery/" + workspaceB.WorkspaceID + "/artifact-b/discovery.md", SHA256: strings.Repeat("c", 64), MediaType: "text/markdown", SizeBytes: 1})
		if err != nil {
			return err
		}
		revisionB, err = tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-persistence-b", WorkspaceRowID: workspaceB.ID, RevisionNumber: 1, ArtifactRowID: artifactB.ID, CreatedIdentity: "operator"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateIntegratedDiscoveryRevision(ctx, workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-cross-artifact", WorkspaceRowID: fixture.workspace.ID, RevisionNumber: 2, ArtifactRowID: artifactB.ID, CreatedIdentity: "operator"})
		return err
	}); err == nil {
		t.Fatal("cross-workspace revision artifact was accepted")
	}
	if _, err := fixture.store.DB().Exec(`UPDATE feature_workspaces SET current_discovery_revision_row_id = ? WHERE id = ?`, revisionB.ID, fixture.workspace.ID); err == nil {
		t.Fatal("cross-workspace current revision was accepted")
	}

	ticketA := fixture.tickets["discovery-persistence-a"]
	resolutionA := fixture.resolutions["discovery-persistence-a"]
	assertCrossWorkspaceConsequenceRejected(t, fixture.store, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-cross-ticket", WorkspaceRowID: fixture.workspace.ID, TicketRowID: ticketBValue.ID, ResolutionRowID: ticketB.ID, ConsequenceKind: "no_material_change", EvidenceBasis: "ownership"})
	assertCrossWorkspaceConsequenceRejected(t, fixture.store, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-cross-resolution", WorkspaceRowID: fixture.workspace.ID, TicketRowID: ticketA.ID, ResolutionRowID: ticketB.ID, ConsequenceKind: "no_material_change", EvidenceBasis: "ownership"})
	assertCrossWorkspaceConsequenceRejected(t, fixture.store, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-cross-produced", WorkspaceRowID: fixture.workspace.ID, TicketRowID: ticketA.ID, ResolutionRowID: resolutionA.ID, ConsequenceKind: "integrated", ProducedRevisionRowID: sqlNullInt64(revisionB.ID), EvidenceBasis: "ownership"})
	assertCrossWorkspaceConsequenceRejected(t, fixture.store, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-cross-replacement", WorkspaceRowID: fixture.workspace.ID, TicketRowID: ticketA.ID, ResolutionRowID: resolutionA.ID, ConsequenceKind: "superseded", ReplacementTicketRowID: sqlNullInt64(ticketBValue.ID), EvidenceBasis: "ownership"})
}

func assertCrossWorkspaceConsequenceRejected(t *testing.T, store *workflowstore.Store, value workflowstore.DiscoveryIntegrationConsequence) {
	t.Helper()
	err := store.WithTx(context.Background(), func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDiscoveryIntegrationConsequence(context.Background(), value)
		return err
	})
	if err == nil {
		t.Fatalf("cross-workspace consequence was accepted: %#v", value)
	}
}

func TestDiscoveryFoundationOperationsDoNotCreateDownstreamLifecycleState(t *testing.T) {
	ctx := context.Background()
	tables := []string{"source_vault_closures", "feature_workspace_completion_reopenings", "governing_artifact_approvals", "delivery_ticket_revision_approvals", "delivery_ticket_selections", "execution_packages", "execution_package_approvals", "runs", "execution_attempts", "audit_packets", "audit_decisions", "feature_workspace_completion_decisions"}

	assertOperation := func(t *testing.T, run func(*discoveryIntegrationFixture)) {
		t.Helper()
		fixture := newDiscoveryIntegrationFixture(t, "discovery-side-effect-a", "discovery-side-effect-b", "discovery-side-effect-c")
		before := downstreamCounts(t, fixture.store, tables)
		run(fixture)
		after := downstreamCounts(t, fixture.store, tables)
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("foundation operation changed downstream state: before=%v after=%v", before, after)
		}
	}

	{
		store, _, _ := openFeatureServiceStore(t, ctx)
		service, err := NewServiceWithIDs(store, &featureTestIDs{})
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-side-effect-enable", "side-effect-enable")
		if err != nil {
			t.Fatal(err)
		}
		before := downstreamCounts(t, store, tables)
		if _, err := service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true); err != nil {
			t.Fatal(err)
		}
		if after := downstreamCounts(t, store, tables); !reflect.DeepEqual(after, before) {
			t.Fatalf("capability enable changed downstream state: before=%v after=%v", before, after)
		}
	}
	{
		store, _, _ := openFeatureServiceStore(t, ctx)
		service, err := NewServiceWithIDs(store, &featureTestIDs{})
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-side-effect-initial", "side-effect-initial")
		if err != nil {
			t.Fatal(err)
		}
		workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
		if err != nil {
			t.Fatal(err)
		}
		before := downstreamCounts(t, store, tables)
		content := []byte("# initial side effect\n")
		if _, _, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator"}); err != nil {
			t.Fatal(err)
		}
		if after := downstreamCounts(t, store, tables); !reflect.DeepEqual(after, before) {
			t.Fatalf("initial revision changed downstream state: before=%v after=%v", before, after)
		}
	}
	assertOperation(t, func(f *discoveryIntegrationFixture) {
		ticket := f.tickets["discovery-side-effect-a"]
		updated, err := f.service.UpdateDiscoveryWorkItem(ctx, DiscoveryWorkItemInput{WorkspaceID: f.workspace.WorkspaceID, TicketID: ticket.DiscoveryTicketID, Kind: "investigation", ExpectedVersion: ticket.Version})
		if err != nil {
			t.Fatal(err)
		}
		f.tickets[ticket.DiscoveryTicketID] = updated
	})
	assertOperation(t, func(f *discoveryIntegrationFixture) {
		addDiscoveryDependency(t, f, "discovery-side-effect-a", "discovery-side-effect-b", "blocks")
	})
	assertOperation(t, func(f *discoveryIntegrationFixture) {
		integrateSideEffectResult(t, f, "discovery-side-effect-a", "integrated")
	})
	assertOperation(t, func(f *discoveryIntegrationFixture) {
		integrateSideEffectResult(t, f, "discovery-side-effect-a", "no_material_change")
	})
	assertOperation(t, func(f *discoveryIntegrationFixture) {
		integrateSideEffectResult(t, f, "discovery-side-effect-a", "superseded")
	})
}

func integrateSideEffectResult(t *testing.T, fixture *discoveryIntegrationFixture, ticketID, consequence string) {
	t.Helper()
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket := fixture.tickets[ticketID]
	input := IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: ticketID, ResolutionID: "resolution-" + ticketID, Consequence: consequence, ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "side effect test"}
	if consequence == "integrated" {
		input.Markdown = []byte("# side effect\n")
		input.ExpectedSHA256 = discoveryTestDigest(input.Markdown)
		input.CreatedIdentity = "operator"
	} else if consequence == "superseded" {
		input.ReplacementTicketID = "discovery-side-effect-b"
	}
	_, updated, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	fixture.workspace = updated
}

func downstreamCounts(t *testing.T, store *workflowstore.Store, tables []string) map[string]int {
	t.Helper()
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		var count int
		if err := store.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}
