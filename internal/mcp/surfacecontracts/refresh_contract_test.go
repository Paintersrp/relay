package surfacecontracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCorrectedRefreshSchemaAndManifestIdentities(t *testing.T) {
	expected := map[string]struct {
		inputSize    int
		inputSHA256  string
		manifestSize int
		manifestSHA  string
	}{
		"planner-authoring.v1":   {12549, "56f445deed49a1766eef7b20cc8651bea5133e3bf5b394f2ccb61b90f6507172", 204675, "0618add5d538eec9b2300695157439d2bfdf4349ce4684dfc8232a50cdf21a58"},
		"planner-plan.v1":        {12539, "708c72aa50854eef57c4f5d6159a7da721a7d75024f755f8adadc3abb94f96dd", 227639, "9f23d3745a26ac2e60f62aa737532d637b4705b17cf042c7f74b40ad3653d5a2"},
		"planner-execution.v1":   {12549, "c83bc91320f51816ca815be58f9276e3e47acb672892ffec8de9cf07f5f994b3", 228738, "cee346ea5c41c6407485075db7963d86916b471e702a148e3230ec00b2508951"},
		"auditor-review.v1":      {12543, "e2ca46ba30a8d5b6365d16f8ebcb7df7c7479009714b77fbdb155b143323b2a0", 204732, "9ecf7331e5324a97e35917c4817e4f13a231976075735915e32d3def4983dc95"},
		"auditor-audit.v1":       {12541, "2a2fe32c205b3109afd8a1e6667fdc506207cf1dec9e113423404b885a4f59fc", 225324, "1c6beea3a467453c978e8cd0353a82be7d2ce12be35eba35df907d9b2a2ecfdb"},
		"auditor-remediation.v1": {12553, "e1e8c38c0067722469f42396e0ef6be554299253f85893d74cb4b5d4bfee2094", 238445, "a4651845c6c725380f364faed57b648f5941ddda32c9f394c49fc8d1b51ffc03"},
	}

	surfaces, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != len(expected) {
		t.Fatalf("surface count = %d, want %d", len(surfaces), len(expected))
	}

	inputFilesFragment := []byte(`"input_files":{"type":"array","minItems":0,"maxItems":64,"items":{"$ref":"#/$defs/OpenAIFileParameter"}}`)
	for _, manifest := range surfaces {
		t.Run(string(manifest.SurfaceContract), func(t *testing.T) {
			want, ok := expected[string(manifest.SurfaceContract)]
			if !ok {
				t.Fatalf("unexpected surface %q", manifest.SurfaceContract)
			}
			if manifest.ManifestBasisSize != want.manifestSize || len(manifest.ManifestBasis) != want.manifestSize {
				t.Fatalf("manifest size = %d/%d, want %d", manifest.ManifestBasisSize, len(manifest.ManifestBasis), want.manifestSize)
			}
			manifestSum := sha256.Sum256(manifest.ManifestBasis)
			if got := hex.EncodeToString(manifestSum[:]); got != want.manifestSHA || manifest.ManifestSHA256 != want.manifestSHA {
				t.Fatalf("manifest sha256 = %s/%s, want %s", got, manifest.ManifestSHA256, want.manifestSHA)
			}

			var refresh ToolContract
			for _, tool := range manifest.Tools {
				if tool.Name == "refresh_operation_packet" {
					refresh = tool
					break
				}
			}
			if refresh.Name == "" {
				t.Fatal("refresh_operation_packet is missing")
			}
			if refresh.InputSizeBytes != want.inputSize || len(refresh.InputSchema) != want.inputSize {
				t.Fatalf("refresh input size = %d/%d, want %d", refresh.InputSizeBytes, len(refresh.InputSchema), want.inputSize)
			}
			inputSum := sha256.Sum256(refresh.InputSchema)
			if got := hex.EncodeToString(inputSum[:]); got != want.inputSHA256 || refresh.InputSHA256 != want.inputSHA256 {
				t.Fatalf("refresh input sha256 = %s/%s, want %s", got, refresh.InputSHA256, want.inputSHA256)
			}
			if !bytes.Contains(refresh.InputSchema, inputFilesFragment) {
				t.Fatalf("refresh input schema lacks exact input_files fragment: %s", refresh.InputSchema)
			}
			expectedPacketIndex := bytes.Index(refresh.InputSchema, []byte(`"expected_packet_id"`))
			inputFilesIndex := bytes.Index(refresh.InputSchema, []byte(`"input_files"`))
			inputsIndex := bytes.Index(refresh.InputSchema, []byte(`"inputs":{"type"`))
			if expectedPacketIndex < 0 || inputFilesIndex <= expectedPacketIndex || inputsIndex <= inputFilesIndex {
				t.Fatalf("refresh property order is not canonical: %s", refresh.InputSchema)
			}
			if len(refresh.FileParams) != 1 || refresh.FileParams[0] != "input_files" {
				t.Fatalf("refresh file params = %v", refresh.FileParams)
			}
			if refresh.ManifestSHA256 != manifest.ManifestSHA256 {
				t.Fatalf("tool manifest sha256 = %s, surface = %s", refresh.ManifestSHA256, manifest.ManifestSHA256)
			}
		})
	}
}
