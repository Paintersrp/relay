package mcp

import "testing"

func TestWayfinderMutationIdentityRejectsUnregisteredAndDuplicateDependencies(t *testing.T) {
	identity := WayfinderMutationIdentity{MutationID: "mutation-1", ExpectedPacketID: "packet-1", OperationID: "wayfinder.workspace", Action: "create_workspace"}
	first, err := identity.SemanticRequestSHA256()
	if err != nil || first == "" {
		t.Fatalf("fingerprint = %q, %v", first, err)
	}
	second, err := identity.SemanticRequestSHA256()
	if err != nil || first != second {
		t.Fatalf("fingerprint replay = %q, %v", second, err)
	}
	identity.OperationID = "wayfinder.unregistered"
	if _, err := identity.SemanticRequestSHA256(); err == nil {
		t.Fatal("unregistered operation identity was accepted")
	}
	identity.OperationID = "wayfinder.workspace"
	identity.RequiredDependencies = append(identity.RequiredDependencies, struct {
		Class string `json:"class"`
		Key   string `json:"key"`
	}{Class: "manifest_member", Key: "one"}, struct {
		Class string `json:"class"`
		Key   string `json:"key"`
	}{Class: "manifest_member", Key: "one"})
	if _, err := identity.SemanticRequestSHA256(); err == nil {
		t.Fatal("duplicate dependency identity was accepted")
	}
}

func TestWayfinderRoleSurfacesExposeNoSourceTooling(t *testing.T) {
	surfaces := WayfinderRoleSurfaces()
	if len(surfaces) != 4 {
		t.Fatalf("surface count = %d", len(surfaces))
	}
	for _, surface := range surfaces {
		if surface.Role == "" || surface.SurfaceContract == "" || surface.ManifestSHA256 == "" || len(surface.Operations) != 1 {
			t.Fatalf("invalid role surface: %#v", surface)
		}
	}
}
