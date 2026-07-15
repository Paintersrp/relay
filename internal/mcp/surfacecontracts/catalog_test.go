package surfacecontracts

import (
	"bytes"
	"sync"
	"testing"

	"relay/internal/operations/registry"
)

func TestCatalogReconstructsRegisteredDraft202012SchemasAndManifests(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
	if DefinitionCount() != 115 {
		t.Fatalf("definition count = %d, want 115", DefinitionCount())
	}
	surfaces, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 6 {
		t.Fatalf("surface count = %d, want 6", len(surfaces))
	}

	schemaCount := 0
	for _, surface := range surfaces {
		if surface.ManifestBasisSize != len(surface.ManifestBasis) {
			t.Fatalf("%s manifest size = %d, bytes = %d", surface.SurfaceContract, surface.ManifestBasisSize, len(surface.ManifestBasis))
		}
		if len(surface.ManifestSHA256) != 64 {
			t.Fatalf("%s manifest sha256 = %q", surface.SurfaceContract, surface.ManifestSHA256)
		}
		for _, tool := range surface.Tools {
			schemaCount += 2
			if err := registry.ValidateSchemaDocument(tool.InputSchema); err != nil {
				t.Fatalf("%s/%s input schema: %v", surface.SurfaceContract, tool.Name, err)
			}
			if err := registry.ValidateSchemaDocument(tool.OutputSchema); err != nil {
				t.Fatalf("%s/%s output schema: %v", surface.SurfaceContract, tool.Name, err)
			}
			if len(tool.InputSchema) != tool.InputSizeBytes {
				t.Fatalf("%s/%s input size mismatch", surface.SurfaceContract, tool.Name)
			}
			if len(tool.OutputSchema) != tool.OutputSizeBytes {
				t.Fatalf("%s/%s output size mismatch", surface.SurfaceContract, tool.Name)
			}
			if tool.Annotations.OpenWorldHint {
				t.Fatalf("%s/%s openWorldHint is true", surface.SurfaceContract, tool.Name)
			}
		}
	}
	if schemaCount != 210 {
		t.Fatalf("schema count = %d, want 210", schemaCount)
	}
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	first, ok := Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	originalBasis := append([]byte(nil), first.ManifestBasis...)
	originalInput := append([]byte(nil), first.Tools[0].InputSchema...)
	first.ManifestBasis[0] ^= 0xff
	first.Tools[0].InputSchema[0] ^= 0xff
	first.Tools[0].FileParams = append(first.Tools[0].FileParams, "mutated")
	first.Operations[0] = "mutated"

	second, ok := Get("planner-authoring.v1")
	if !ok {
		t.Fatal("planner-authoring.v1 missing")
	}
	if !bytes.Equal(second.ManifestBasis, originalBasis) {
		t.Fatal("manifest basis was mutated")
	}
	if !bytes.Equal(second.Tools[0].InputSchema, originalInput) {
		t.Fatal("input schema was mutated")
	}
	if second.Operations[0] != "planner.requirements" {
		t.Fatalf("operation was mutated: %q", second.Operations[0])
	}
}

func TestFileParameterPolicy(t *testing.T) {
	surfaces, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range surfaces {
		for _, tool := range surface.Tools {
			switch tool.Name {
			case "create_operation_packet", "refresh_operation_packet":
				assertFileParams(t, surface.SurfaceContract, tool.Name, tool.FileParams, []string{"input_files"})
			case "validate_artifact", "submit_plan", "create_run":
				assertFileParams(t, surface.SurfaceContract, tool.Name, tool.FileParams, []string{"artifact_file"})
			default:
				assertFileParams(t, surface.SurfaceContract, tool.Name, tool.FileParams, nil)
			}
		}
	}
}

func TestAuditSurfaceHasNoIngestionOnlyMutation(t *testing.T) {
	manifest, ok := Get("auditor-audit.v1")
	if !ok {
		t.Fatal("auditor-audit.v1 missing")
	}
	for _, tool := range manifest.Tools {
		if tool.Name == "stage_operation_input" {
			t.Fatal("audit surface exposes stage_operation_input")
		}
		if len(tool.FileParams) != 0 && tool.Name != "create_operation_packet" && tool.Name != "refresh_operation_packet" {
			t.Fatalf("audit tool %s unexpectedly declares files", tool.Name)
		}
	}
}

func TestConcurrentCatalogSchemaAndManifestReadsAreDeterministic(t *testing.T) {
	expected, ok := Get("planner-plan.v1")
	if !ok {
		t.Fatal("planner-plan.v1 missing")
	}
	const workers = 32
	const iterations = 20
	errorsOut := make(chan string, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				manifest, ok := Get("planner-plan.v1")
				if !ok {
					errorsOut <- "manifest missing"
					return
				}
				if !bytes.Equal(manifest.ManifestBasis, expected.ManifestBasis) || manifest.ManifestSHA256 != expected.ManifestSHA256 {
					errorsOut <- "manifest is nondeterministic"
					return
				}
				if len(manifest.Tools) != len(expected.Tools) {
					errorsOut <- "tool count changed"
					return
				}
				for index := range manifest.Tools {
					if !bytes.Equal(manifest.Tools[index].InputSchema, expected.Tools[index].InputSchema) ||
						!bytes.Equal(manifest.Tools[index].OutputSchema, expected.Tools[index].OutputSchema) {
						errorsOut <- "schema is nondeterministic"
						return
					}
				}
				manifest.ManifestBasis[0] ^= 0xff
				manifest.Tools[0].InputSchema[0] ^= 0xff
				manifest.Tools[0].FileParams = append(manifest.Tools[0].FileParams, "caller-mutation")
				fresh, ok := Get("planner-plan.v1")
				if !ok || !bytes.Equal(fresh.ManifestBasis, expected.ManifestBasis) || !bytes.Equal(fresh.Tools[0].InputSchema, expected.Tools[0].InputSchema) {
					errorsOut <- "defensive copy isolation failed"
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsOut)
	for message := range errorsOut {
		t.Fatal(message)
	}
}

func assertFileParams(t *testing.T, surface interface{}, tool string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%v/%s file params = %v, want %v", surface, tool, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%v/%s file params = %v, want %v", surface, tool, got, want)
		}
	}
}
