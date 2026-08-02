package features

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
