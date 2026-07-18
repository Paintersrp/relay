package wayfinder

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestWorkspaceResumesAndProtectsVersionedDiscoveryMutations(t *testing.T) {
	ctx := context.Background()
	store, artifactID, databasePath, artifactRoot := openWayfinderServiceStore(t, ctx)
	ids := &wayfinderTestIDs{}
	service, err := NewServiceWithIDs(store, ids)
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := service.CreateWorkspace(ctx, CreateWorkspaceInput{ProjectID: "project-wayfinder-service", FeatureSlug: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AdmitInput(ctx, AdmitInputInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Sequence: 1, Name: "requirements", Role: "governing", SourceKind: "relay_artifact", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: sql.NullString{String: strings.Repeat("b", 64), Valid: true}}); err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AddDestination(ctx, AddDestinationInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Sequence: 1, Kind: "destination", Key: "internal/app/payments"}); err != nil {
		t.Fatal(err)
	}
	first, workspace, err := service.CreateDiscoveryTicket(ctx, CreateDiscoveryTicketInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, TicketKey: "source-map", Subject: "Map source authority"})
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.CreateDiscoveryTicket(ctx, CreateDiscoveryTicketInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, TicketKey: "implement", Subject: "Investigate implementation", DependsOnTicketIDs: []string{first.DiscoveryTicketID}, DependencyKind: "blocks"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AdmitInput(ctx, AdmitInputInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: 1, Sequence: 2, Name: "stale", Role: "evidence", SourceKind: "relay_artifact", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSHA256: sql.NullString{String: strings.Repeat("b", 64), Valid: true}}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale mutation error = %v", err)
	}

	if _, _, err := service.CreateDiscoveryTicket(ctx, CreateDiscoveryTicketInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, TicketKey: "rollback", Subject: "Must roll back", DependsOnTicketIDs: []string{first.DiscoveryTicketID, first.DiscoveryTicketID}, DependencyKind: "blocks"}); err == nil {
		t.Fatal("duplicate dependency accepted")
	}
	detail, err := service.ReadWorkspace(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Workspace.Version != workspace.Version || len(detail.Inputs) != 1 || len(detail.Tickets) != 2 || len(detail.Tickets[1].Dependencies) != 1 {
		t.Fatalf("detail after failed mutation = %+v", detail)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workflowstore.Open(databasePath, artifactRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumed, err := NewServiceWithIDs(reopened, &wayfinderTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	resumedDetail, err := resumed.ReadWorkspace(ctx, workspace.WorkspaceID)
	if err != nil || resumedDetail.Workspace.Version != workspace.Version || len(resumedDetail.Tickets) != 2 {
		t.Fatalf("resumed detail = %+v, %v", resumedDetail, err)
	}
}

func TestWorkspaceReopensDiscoveryWhenRetainedSourceIsStale(t *testing.T) {
	ctx := context.Background()
	store, _, _, _ := openWayfinderServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &wayfinderTestIDs{}, staleReader{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.CreateWorkspace(ctx, CreateWorkspaceInput{ProjectID: "project-wayfinder-service", FeatureSlug: "stale-source"})
	if err != nil {
		t.Fatal(err)
	}
	route, updated, err := service.ReopenStaleSource(ctx, ReopenStaleSourceInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Sequence: 1})
	if err != nil || route.State != "discovery" || updated.Version != workspace.Version+1 {
		t.Fatalf("reopen = %#v %#v %v", route, updated, err)
	}
}

type staleReader struct{}

func (staleReader) ReadInvestigationClosure(context.Context, RetainedClosureIdentity) (RetainedClosureIdentity, error) {
	return RetainedClosureIdentity{}, ErrStaleSourceBase
}

type wayfinderTestIDs struct{ next int }

func (ids *wayfinderTestIDs) id(prefix string) string {
	ids.next++
	return prefix + "test-" + string(rune('a'+ids.next))
}
func (ids *wayfinderTestIDs) WorkspaceID() string       { return ids.id("workspace-") }
func (ids *wayfinderTestIDs) InputID() string           { return ids.id("input-") }
func (ids *wayfinderTestIDs) DestinationID() string     { return ids.id("destination-") }
func (ids *wayfinderTestIDs) DiscoveryTicketID() string { return ids.id("discovery-") }
func (ids *wayfinderTestIDs) ResolutionID() string      { return ids.id("resolution-") }
func (ids *wayfinderTestIDs) RouteStateID() string      { return ids.id("route-") }
func (ids *wayfinderTestIDs) InvestigationID() string   { return ids.id("investigation-") }

func openWayfinderServiceStore(t *testing.T, ctx context.Context) (*workflowstore.Store, int64, string, string) {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var projectID, planID, artifactID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-wayfinder-service', 'Wayfinder') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO plans (project_row_id, plan_id, feature_slug, canonical_sha256) VALUES (?, 'plan-wayfinder-service', 'wayfinder', ?) RETURNING id`, projectID, strings.Repeat("a", 64)).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES ('artifact-wayfinder-service', 'plan', ?, 'requirements', 'plans/wayfinder/requirements.json', 'application/json', ?, 2) RETURNING id`, planID, strings.Repeat("b", 64)).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	return store, artifactID, filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts")
}
