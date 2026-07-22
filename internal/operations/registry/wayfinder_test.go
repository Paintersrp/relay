package registry

import "testing"

func TestWayfinderOperationInventoryIsClosedAndStable(t *testing.T) {
	profiles := WayfinderRoleProfiles()
	if len(profiles) != 3 {
		t.Fatalf("profile count = %d", len(profiles))
	}
	for _, profile := range profiles {
		if profile.ManifestSHA256 == "" || len(profile.Operations) != 1 {
			t.Fatalf("invalid profile: %#v", profile)
		}
		operation, ok := Lookup(profile.Operations[0])
		if !ok || operation.Role != profile.Role || operation.SurfaceContract != profile.SurfaceContract {
			t.Fatalf("profile operation mismatch: %#v", profile)
		}
	}
	if _, ok := Lookup("wayfinder.unregistered"); ok {
		t.Fatal("unregistered operation was admitted")
	}
}
