package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestDiscoveryLifecycleAdoptionIsExplicitAndOneWay(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-lifecycle", "discovery-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID); err != nil || assessment.Currentness != DiscoveryNotClosed || assessment.State != "" {
		t.Fatalf("unadopted assessment = %#v, %v", assessment, err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("capability enabled adoption error = %v", err)
	}
	adoption, workspace, err := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
	if err != nil || adoption.WorkspaceRowID != workspace.ID {
		t.Fatalf("adoption = %#v, %#v, %v", adoption, workspace, err)
	}
	if _, _, err := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); !errors.Is(err, ErrDiscoveryAlreadyAdopted) {
		t.Fatalf("duplicate adoption error = %v", err)
	}
}

func TestDiscoveryLifecycleOptimisticConcurrencyHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-lifecycle-race", "lifecycle-race")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	run := func(operation func() error) int {
		var wait sync.WaitGroup
		wait.Add(2)
		results := make(chan error, 2)
		for range 2 {
			go func() {
				defer wait.Done()
				results <- operation()
			}()
		}
		wait.Wait()
		close(results)
		successes := 0
		for result := range results {
			if result == nil {
				successes++
			}
		}
		return successes
	}
	expectedAdoptionVersion := workspace.Version
	if winners := run(func() error {
		_, _, operationErr := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: expectedAdoptionVersion, OperatorIdentity: "operator"})
		return operationErr
	}); winners != 1 {
		t.Fatalf("adoption winners = %d, want 1", winners)
	}
	workspace, err = store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# Concurrent closure\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	expectedClosureVersion := workspace.Version
	if winners := run(func() error {
		_, _, operationErr := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: expectedClosureVersion, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
		return operationErr
	}); winners != 1 {
		t.Fatalf("closure winners = %d, want 1", winners)
	}
	var packets int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?`, workspace.ID).Scan(&packets); err != nil || packets != 1 {
		t.Fatalf("closure packets = %d, err = %v", packets, err)
	}
}

func TestDiscoveryLifecycleClosesAllDestinationsAndReopensHistoricalPacket(t *testing.T) {
	destinations := []DiscoveryDestination{
		DiscoveryDestinationNoDeliveryWork,
		DiscoveryDestinationDirectDeliveryTicket,
		DiscoveryDestinationRequirements,
		DiscoveryDestinationSharedDesign,
		DiscoveryDestinationRequirementsThenSharedDesign,
		DiscoveryDestinationExistingRouteContinuation,
	}
	for index, destination := range destinations {
		t.Run(string(destination), func(t *testing.T) {
			ctx := context.Background()
			store, _, _ := openFeatureServiceStore(t, ctx)
			service, err := NewServiceWithIDs(store, &featureTestIDs{})
			if err != nil {
				t.Fatal(err)
			}
			workspace, err := createFeatureWorkspace(ctx, store, fmt.Sprintf("workspace-lifecycle-%d", index), fmt.Sprintf("lifecycle-%d", index))
			if err != nil {
				t.Fatal(err)
			}
			workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
			if err != nil {
				t.Fatal(err)
			}
			_, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("# Settled discovery\n")
			started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: destination, Continuation: "continue"})
			if err != nil {
				t.Fatal(err)
			}
			assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
			if err != nil || assessment.Destination != destination || assessment.State != DiscoveryStateActive {
				t.Fatalf("assessment = %#v, %v", assessment, err)
			}
			closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"})
			if err != nil || len(closed.Members) != 1 || closed.Currentness != DiscoveryCurrent {
				t.Fatalf("closure = %#v, %v", closed, err)
			}
			verified, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
			if err != nil || verified.Currentness != DiscoveryCurrent {
				t.Fatalf("verified current packet = %#v, %v", verified, err)
			}
			replacement := []byte("# Reopened discovery\n")
			revision, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "new exact evidence", CreatedIdentity: "operator", SHA256: discoveryTestDigest(replacement), Markdown: replacement, Destination: destination})
			if err != nil || revision.PredecessorRevisionRowID.Int64 != started.Revision.ID || workspace.CurrentDiscoveryClosurePacketRowID.Valid {
				t.Fatalf("reopen = %#v, %#v, %v", revision, workspace, err)
			}
			historical, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
			if err != nil || historical.Currentness != DiscoveryHistorical {
				t.Fatalf("historical packet = %#v, %v", historical, err)
			}
		})
	}
}

func TestCanonicalDiscoveryManifestBindsMemberOrder(t *testing.T) {
	workspace := workflowstore.FeatureWorkspace{WorkspaceID: "workspace-canonical", FeatureSlug: "canonical"}
	revision := workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-canonical", CreatedAt: "2026-08-04T00:00:00.000Z"}
	first := discoveryPacketMemberBasis{Artifact: workflowstore.DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-a", SHA256: string(make([]byte, 64)), MediaType: "text/plain", SizeBytes: 1}, OwnerFamily: "input", SourceIdentity: "input-a", SemanticRole: "input:001"}
	second := first
	second.Artifact.DiscoveryArtifactID = "discovery-artifact-b"
	second.SourceIdentity = "input-b"
	second.SemanticRole = "input:002"
	a := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{first, second})
	b := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{first, second})
	c := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{second, first})
	if string(a) != string(b) || string(a) == string(c) || len(a) == 0 || a[len(a)-1] != '\n' {
		t.Fatalf("canonical manifests are not stable and order-sensitive")
	}
}

func TestDiscoveryClosureRejectsStaleAndMismatchedRequestsWithoutPublication(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput
		want   error
	}{
		{name: "stale workspace", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.ExpectedVersion--
			return input
		}, want: ErrDiscoveryStaleState},
		{name: "stale revision", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.ExpectedRevisionID = "discovery-revision-stale"
			return input
		}, want: ErrDiscoveryStaleState},
		{name: "wrong destination", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.Destination = DiscoveryDestinationSharedDesign
			return input
		}, want: ErrDiscoveryInvalidDestination},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
			input := test.mutate(CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			if _, _, err := service.CloseFeatureDiscovery(ctx, input); !errors.Is(err, test.want) {
				t.Fatalf("close error = %v, want %v", err, test.want)
			}
			assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
		})
	}
}

func TestDiscoveryClosureRejectsActiveAndBlockedWorkWithoutPublication(t *testing.T) {
	for _, state := range []string{"open", "blocked"} {
		t.Run(state, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-" + state, WorkspaceRowID: workspace.ID, TicketKey: state, Subject: state})
				if err != nil {
					return err
				}
				if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "investigation", false); err != nil {
					return err
				}
				if state == "blocked" {
					_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "blocked", ticket.Version)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
			if err != nil || assessment.State != map[string]DiscoveryState{"open": DiscoveryStateActive, "blocked": DiscoveryStateBlocked}[state] {
				t.Fatalf("assessment = %#v, %v", assessment, err)
			}
			beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
			_, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			want := ErrDiscoveryActiveOperation
			if state == "blocked" {
				want = ErrDiscoveryBlocked
			}
			if !errors.Is(err, want) {
				t.Fatalf("close error = %v, want %v", err, want)
			}
			assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
		})
	}
}

func TestDiscoveryPacketRetrievalFailsClosedForCorruptManifestAndWrongWorkspace(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if verified, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID); err != nil || verified.Currentness != DiscoveryCurrent {
		t.Fatalf("valid current packet = %#v, %v", verified, err)
	}
	other, err := createFeatureWorkspace(ctx, store, "workspace-discovery-other", "discovery-other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadDiscoveryClosurePacket(ctx, other.WorkspaceID, closed.Packet.ClosurePacketID); !errors.Is(err, ErrDiscoveryCrossWorkspace) {
		t.Fatalf("wrong workspace error = %v", err)
	}
	manifest, err := store.GetDiscoveryArtifactByRowID(ctx, closed.Packet.ManifestArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(manifest.RelativePath)), []byte("corrupt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if packet, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID); !errors.Is(err, ErrDiscoveryManifestIntegrity) || packet.Packet.ID != 0 {
		t.Fatalf("corrupt manifest packet = %#v, error = %v", packet, err)
	}
}

func TestDiscoveryReopenPreservesHistoricalPacketAndReclosureUsesNewPacket(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Reopened discovery\n")
	if _, _, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, Cause: "new evidence", CreatedIdentity: "operator", OperatorConfirmed: false, SHA256: discoveryTestDigest(replacement), Markdown: replacement}); !errors.Is(err, ErrDiscoveryReopenConfirmation) {
		t.Fatalf("unconfirmed reopen error = %v", err)
	}
	newRevision, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, Cause: "new evidence", CreatedIdentity: "operator", OperatorConfirmed: true, SHA256: discoveryTestDigest(replacement), Markdown: replacement, Destination: DiscoveryDestinationRequirements})
	if err != nil || newRevision.PredecessorRevisionRowID.Int64 != revision.ID || workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		t.Fatalf("reopen = %#v, %#v, %v", newRevision, workspace, err)
	}
	historical, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
	if err != nil || historical.Currentness != DiscoveryHistorical {
		t.Fatalf("historical packet = %#v, %v", historical, err)
	}
	reclosed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: newRevision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil || reclosed.Packet.ID == closed.Packet.ID || workspace.CurrentDiscoveryClosurePacketRowID.Int64 != reclosed.Packet.ID || len(reclosed.Members) != 2 {
		t.Fatalf("reclosure = %#v, %#v, %v", reclosed, workspace, err)
	}
	for index, member := range reclosed.Members {
		if member.Sequence != int64(index+1) || member.OwnerFamily == "" || member.SourceIdentity == "" || member.SemanticRole == "" || member.Sha256 == "" || member.SizeBytes < 0 || member.MediaType == "" {
			t.Fatalf("reclosure member %d = %#v", index, member)
		}
	}
	if reclosed.Members[0].OwnerFamily != "integrated_discovery" || reclosed.Members[0].SourceIdentity != newRevision.DiscoveryRevisionID || reclosed.Members[1].OwnerFamily != "discovery_reopen" {
		t.Fatalf("reclosure member basis = %#v", reclosed.Members)
	}
	historical, err = service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
	if err != nil || historical.Currentness != DiscoveryHistorical {
		t.Fatalf("historical packet after reclosure = %#v, %v", historical, err)
	}
}

func adoptedDiscoveryLifecycle(t *testing.T, destination DiscoveryDestination) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.IntegratedDiscoveryRevision) {
	t.Helper()
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-proof", "discovery-proof")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Settled discovery\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, workspace, started.Revision
}

func assertNoDiscoveryClosurePublication(t *testing.T, ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace, routes int) {
	t.Helper()
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.CurrentDiscoveryClosurePacketRowID.Valid || current.Version != workspace.Version {
		t.Fatalf("workspace after rejected closure = %#v, %v", current, err)
	}
	var packets, members int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?`, workspace.ID).Scan(&packets); err != nil || packets != 0 {
		t.Fatalf("closure packets = %d, %v", packets, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packet_members WHERE closure_packet_row_id IN (SELECT id FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?)`, workspace.ID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("closure members = %d, %v", members, err)
	}
	if got := discoveryRouteCount(t, store, workspace.ID); got != routes {
		t.Fatalf("route transitions = %d, want %d", got, routes)
	}
}

func discoveryRouteCount(t *testing.T, store *workflowstore.Store, workspaceRowID int64) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_route_states WHERE workspace_row_id = ?`, workspaceRowID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
