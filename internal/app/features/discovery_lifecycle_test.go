package features

import (
	"context"
	"database/sql"
	"errors"
	"testing"
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
