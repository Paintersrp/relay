package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
