package features

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestDiscoveryArtifactAndDatabaseFailuresLeaveNoCommittedState(t *testing.T) {
	ctx := context.Background()

	t.Run("staging failure", func(t *testing.T) {
		fixture := newDiscoveryIntegrationFixture(t, "discovery-stage-failure")
		stagingRoot := filepath.Join(fixture.store.ArtifactStore().Root(), ".staging")
		if err := os.RemoveAll(stagingRoot); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stagingRoot, []byte("staging failure"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := fixture.state(t)
		content := []byte("# Staging failure\n")
		_, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{
			WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-stage-failure", ResolutionID: "resolution-discovery-stage-failure", Consequence: "integrated", ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-stage-failure"].Version, ExpectedSHA256: discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "staging failure",
		})
		if err == nil {
			t.Fatal("expected artifact staging failure")
		}
		if after := fixture.state(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("staging failure mutated state: before=%#v after=%#v", before, after)
		}
	})

	t.Run("promotion failure", func(t *testing.T) {
		fixture := newDiscoveryIntegrationFixture(t, "discovery-promotion")
		before := fixture.state(t)
		destination := filepath.Join(fixture.store.ArtifactStore().Root(), "feature-discovery", fixture.workspace.WorkspaceID, "discovery-artifact-feature-3", "discovery.md")
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("promotion failure"), 0o600); err != nil {
			t.Fatal(err)
		}
		content := []byte("# Promotion failure\n")
		_, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, IntegrateDiscoveryResultInput{
			WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-promotion", ResolutionID: "resolution-discovery-promotion",
			Consequence: "integrated", ExpectedWorkspaceVersion: before.workspace.Version,
			ExpectedWorkItemVersion: fixture.tickets["discovery-promotion"].Version,
			ExpectedSHA256:          discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "promotion failure",
		})
		if err == nil {
			t.Fatal("expected artifact promotion failure")
		}
		if after := fixture.state(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("promotion failure mutated state: before=%#v after=%#v", before, after)
		}
	})

	t.Run("database failure after promotion", func(t *testing.T) {
		fixture := newDiscoveryIntegrationFixture(t, "discovery-database-failure")
		before := fixture.state(t)
		if _, err := fixture.store.DB().Exec(`CREATE TRIGGER fail_discovery_consequence BEFORE INSERT ON feature_workspace_discovery_integration_consequences BEGIN SELECT RAISE(ABORT, 'injected discovery database failure'); END`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = fixture.store.DB().Exec(`DROP TRIGGER fail_discovery_consequence`) })
		content := []byte("# Database failure\n")
		_, _, err := fixture.service.IntegrateDiscoveryResult(fixture.ctx, IntegrateDiscoveryResultInput{
			WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-database-failure", ResolutionID: "resolution-discovery-database-failure",
			Consequence: "integrated", ExpectedWorkspaceVersion: before.workspace.Version,
			ExpectedWorkItemVersion: fixture.tickets["discovery-database-failure"].Version,
			ExpectedSHA256:          discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "database failure",
		})
		if err == nil {
			t.Fatal("expected database failure")
		}
		if after := fixture.state(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("database failure mutated state: before=%#v after=%#v", before, after)
		}
		entries, err := os.ReadDir(filepath.Join(fixture.store.ArtifactStore().Root(), "feature-discovery", fixture.workspace.WorkspaceID))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("retained artifacts after database failure = %d, want initial artifact only", len(entries))
		}
		staging, err := os.ReadDir(filepath.Join(fixture.store.ArtifactStore().Root(), ".staging"))
		if err != nil {
			t.Fatal(err)
		}
		if len(staging) != 0 {
			t.Fatalf("staged artifacts after database failure = %v", staging)
		}
	})
}

func TestConcurrentDiscoveryOperationsHaveOneDeterministicWinner(t *testing.T) {
	ctx := context.Background()

	t.Run("initial revision", func(t *testing.T) {
		store, _, _ := openFeatureServiceStore(t, ctx)
		service, err := NewServiceWithIDs(store, &featureTestIDs{})
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-concurrent-initial", "concurrent-initial")
		if err != nil {
			t.Fatal(err)
		}
		workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
		if err != nil {
			t.Fatal(err)
		}
		type result struct{ err error }
		start := make(chan struct{})
		ready := make(chan struct{}, 2)
		results := make(chan result, 2)
		var wg sync.WaitGroup
		for _, content := range [][]byte{[]byte("# A\n"), []byte("# B\n")} {
			content := content
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready <- struct{}{}
				<-start
				_, _, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator"})
				results <- result{err: err}
			}()
		}
		<-ready
		<-ready
		close(start)
		wg.Wait()
		close(results)
		var successes int
		for result := range results {
			if result.err == nil {
				successes++
			} else if !errors.Is(result.err, ErrDiscoveryStaleState) && !errors.Is(result.err, ErrInvalidDiscoveryConsequence) {
				t.Fatalf("unexpected concurrent initial error: %v", result.err)
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent initial successes = %d, want 1", successes)
		}
		current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if current.CurrentDiscoveryRevisionRowID.Int64 == 0 || current.Version != workspace.Version+1 {
			t.Fatalf("current initial workspace = %#v", current)
		}
		assertDiscoveryCounts(t, store, current.ID, 1, 1, 0)
	})

	t.Run("material results", func(t *testing.T) {
		fixture := newDiscoveryIntegrationFixture(t, "discovery-concurrent-a", "discovery-concurrent-b")
		workspace := fixture.workspace
		start := make(chan struct{})
		ready := make(chan struct{}, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, id := range []string{"discovery-concurrent-a", "discovery-concurrent-b"} {
			id := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready <- struct{}{}
				<-start
				content := []byte("# " + id + "\n")
				_, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: workspace.WorkspaceID, TicketID: id, ResolutionID: "resolution-" + id, Consequence: "integrated", ExpectedWorkspaceVersion: workspace.Version, ExpectedWorkItemVersion: fixture.tickets[id].Version, ExpectedSHA256: discoveryTestDigest(content), Markdown: content, CreatedIdentity: "operator", EvidenceBasis: "concurrent result"})
				errs <- err
			}()
		}
		<-ready
		<-ready
		close(start)
		wg.Wait()
		close(errs)
		var successes int
		for err := range errs {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrDiscoveryStaleState) {
				t.Fatalf("unexpected concurrent material error: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent material successes = %d, want 1", successes)
		}
		current, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		assertDiscoveryCounts(t, fixture.store, current.ID, 2, 2, 1)
		if current.CurrentDiscoveryRevisionRowID.Int64 == 0 {
			t.Fatal("no current revision after concurrent material integration")
		}
	})

	t.Run("same work item result", func(t *testing.T) {
		fixture := newDiscoveryIntegrationFixture(t, "discovery-concurrent-replay")
		before := fixture.state(t)
		start := make(chan struct{})
		ready := make(chan struct{}, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready <- struct{}{}
				<-start
				_, _, err := fixture.service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: fixture.workspace.WorkspaceID, TicketID: "discovery-concurrent-replay", ResolutionID: "resolution-discovery-concurrent-replay", Consequence: "no_material_change", ExpectedWorkspaceVersion: before.workspace.Version, ExpectedWorkItemVersion: fixture.tickets["discovery-concurrent-replay"].Version, EvidenceBasis: "concurrent replay"})
				errs <- err
			}()
		}
		<-ready
		<-ready
		close(start)
		wg.Wait()
		close(errs)
		var successes int
		for err := range errs {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrDiscoveryStaleState) {
				t.Fatalf("unexpected concurrent replay error: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("concurrent replay successes = %d, want 1", successes)
		}
		after := fixture.state(t)
		if len(after.consequences) != len(before.consequences)+1 || after.artifactCount != before.artifactCount || after.current.ID != before.current.ID || after.workspace.Version != before.workspace.Version+1 {
			t.Fatalf("concurrent replay state = %#v, before %#v", after, before)
		}
	})
}

func TestDiscoveryClosurePersistenceFailuresRollbackAuthoritativeState(t *testing.T) {
	cases := []struct {
		name string
		table string
	}{
		{name: "packet member", table: "feature_workspace_discovery_closure_packet_members"},
		{name: "route history", table: "feature_workspace_route_states"},
		{name: "currentness", table: "feature_workspaces"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			trigger := "fail_closure_" + strings.ReplaceAll(test.name, " ", "_")
			if _, err := store.DB().Exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON ` + test.table + ` BEGIN SELECT RAISE(ABORT, 'injected closure failure'); END`); err != nil {
				t.Fatal(err)
			}
			if test.table == "feature_workspaces" {
				if _, err := store.DB().Exec(`DROP TRIGGER ` + trigger); err != nil { t.Fatal(err) }
				if _, err := store.DB().Exec(`CREATE TRIGGER ` + trigger + ` BEFORE UPDATE ON feature_workspaces BEGIN SELECT RAISE(ABORT, 'injected closure failure'); END`); err != nil { t.Fatal(err) }
			}
			t.Cleanup(func() { _, _ = store.DB().Exec(`DROP TRIGGER ` + trigger) })
			beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
			if _, _, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err == nil {
				t.Fatal("expected injected closure failure")
			}
			assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
		})
	}
}

func TestDiscoveryReopenPersistenceFailuresPreserveCurrentPacketAndRevision(t *testing.T) {
	cases := []struct { name, table, event string }{
		{name: "replacement revision", table: "feature_workspace_integrated_discovery_revisions", event: "INSERT"},
		{name: "reopen event", table: "feature_workspace_discovery_reopen_events", event: "INSERT"},
		{name: "route history", table: "feature_workspace_route_states", event: "INSERT"},
		{name: "currentness", table: "feature_workspaces", event: "UPDATE"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			if err != nil { t.Fatal(err) }
			var revisionsBefore int
			if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_integrated_discovery_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&revisionsBefore); err != nil { t.Fatal(err) }
			trigger := "fail_reopen_" + strings.ReplaceAll(test.name, " ", "_")
			if _, err = store.DB().Exec(`CREATE TRIGGER ` + trigger + ` BEFORE ` + test.event + ` ON ` + test.table + ` BEGIN SELECT RAISE(ABORT, 'injected reopen failure'); END`); err != nil { t.Fatal(err) }
			t.Cleanup(func() { _, _ = store.DB().Exec(`DROP TRIGGER ` + trigger) })
			replacement := []byte("# Replacement\n")
			if _, _, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "new evidence", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements}); err == nil { t.Fatal("expected injected reopen failure") }
			current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
			if err != nil || !current.CurrentDiscoveryClosurePacketRowID.Valid || current.CurrentDiscoveryClosurePacketRowID.Int64 != closed.Packet.ID || current.CurrentDiscoveryRevisionRowID.Int64 != revision.ID || current.Version != workspace.Version { t.Fatalf("current state = %#v, %v", current, err) }
			var revisionsAfter int
			if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_integrated_discovery_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&revisionsAfter); err != nil || revisionsAfter != revisionsBefore { t.Fatalf("revision count = %d, %v", revisionsAfter, err) }
		})
	}
}

func assertDiscoveryCounts(t *testing.T, store *workflowstore.Store, workspaceRowID int64, artifacts, revisions, consequences int) {
	t.Helper()
	for table, want := range map[string]int{"feature_workspace_discovery_artifacts": artifacts, "feature_workspace_integrated_discovery_revisions": revisions, "feature_workspace_discovery_integration_consequences": consequences} {
		var got int
		if err := store.DB().QueryRow(`SELECT count(*) FROM `+table+` WHERE workspace_row_id = ?`, workspaceRowID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}
