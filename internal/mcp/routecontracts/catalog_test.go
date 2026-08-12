package routecontracts

import (
	"bytes"
	"testing"
)

func TestRouteManifestIdentityIsCanonicalAndComplete(t *testing.T) {
	first, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Manifests) != 7 || len(second.Manifests) != 7 {
		t.Fatalf("routes=%d/%d", len(first.Manifests), len(second.Manifests))
	}
	for i := range first.Manifests {
		a, b := first.Manifests[i], second.Manifests[i]
		if !bytes.Equal(a.ManifestBasis, b.ManifestBasis) || a.ManifestSHA256 != b.ManifestSHA256 || a.ManifestBasisSizeBytes != len(a.ManifestBasis) {
			t.Fatalf("manifest %d is unstable", i)
		}
		if a.StandingAuthority.Repository != "Paintersrp/relay-specs" || a.StandingAuthority.Commit != "c166ca6020e86dad50e68962d733c7c7eb996b5e" {
			t.Fatalf("standing=%#v", a.StandingAuthority)
		}
		for _, tool := range a.Tools {
			if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 || tool.InputSchemaSHA256 == "" || tool.OutputSchemaSHA256 == "" || tool.Adapter == "" {
				t.Fatalf("tool=%#v", tool)
			}
		}
	}
}

// TestPlannerFrontierAuthorityResolvesPinnedTicketFrontierDomain asserts the
// published planner frontier route binds planner.ticket_frontier to the
// pinned Planner ticket_frontier manifest domain and loads its exact pinned
// manifest members.
func TestPlannerFrontierAuthorityResolvesPinnedTicketFrontierDomain(t *testing.T) {
	set, err := BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range set.Manifests {
		if manifest.SurfaceContract != "planner-ticket-frontier.v2" {
			continue
		}
		if manifest.Role != "planner" || len(manifest.Operations) != 1 || manifest.Operations[0].OperationID != "planner.ticket_frontier" || manifest.Operations[0].ManifestDomain != "ticket_frontier" {
			t.Fatalf("frontier route manifest = %#v", manifest)
		}
		if manifest.Operations[0].PacketSemanticProjection != "relay.semantic.ticket-frontier-read.v2" {
			t.Fatalf("frontier semantic projection = %q, want relay.semantic.ticket-frontier-read.v2", manifest.Operations[0].PacketSemanticProjection)
		}
		if len(manifest.DomainAuthority) != 1 || manifest.DomainAuthority[0].Domain != "ticket_frontier" {
			t.Fatalf("frontier domain authority = %#v", manifest.DomainAuthority)
		}
		wantMembers := []string{"contracts/cross-cutting.md", "contracts/delivery-plan.md", "contracts/delivery-ticket.md", "contracts/ticket-frontier.md"}
		if len(manifest.DomainAuthority[0].Members) != len(wantMembers) {
			t.Fatalf("frontier domain members = %d, want %d", len(manifest.DomainAuthority[0].Members), len(wantMembers))
		}
		for index, want := range wantMembers {
			if manifest.DomainAuthority[0].Members[index].Path != want {
				t.Fatalf("frontier domain member %d = %q, want %q", index, manifest.DomainAuthority[0].Members[index].Path, want)
			}
		}
		return
	}
	t.Fatal("Planner frontier route is missing")
}
