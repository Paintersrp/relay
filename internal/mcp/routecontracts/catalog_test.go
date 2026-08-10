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
		if a.StandingAuthority.Repository != "Paintersrp/relay-specs" || a.StandingAuthority.Commit != "590c4ba79e557e3957a80ad1fefe8698cef0219e" {
			t.Fatalf("standing=%#v", a.StandingAuthority)
		}
		for _, tool := range a.Tools {
			if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 || tool.InputSchemaSHA256 == "" || tool.OutputSchemaSHA256 == "" || tool.Adapter == "" {
				t.Fatalf("tool=%#v", tool)
			}
		}
	}
}
