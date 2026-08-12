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
		if a.StandingAuthority.Repository != "Paintersrp/relay-specs" || a.StandingAuthority.Commit != "9ea40ac112d0683affc10ba6bad2d15efe9e59f4" {
			t.Fatalf("standing=%#v", a.StandingAuthority)
		}
		for _, tool := range a.Tools {
			if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 || tool.InputSchemaSHA256 == "" || tool.OutputSchemaSHA256 == "" || tool.Adapter == "" {
				t.Fatalf("tool=%#v", tool)
			}
		}
	}
}
