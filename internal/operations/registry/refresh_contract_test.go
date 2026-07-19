package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCorrectedPublicContractIdentityAndRefreshSchemas(t *testing.T) {
	publicRaw := RawPublicContract()
	if len(publicRaw) != 301441 {
		t.Fatalf("public contract bytes = %d, want 301441", len(publicRaw))
	}
	publicSum := sha256.Sum256(publicRaw)
	if got := hex.EncodeToString(publicSum[:]); got != "662b4055b5ae188c52bd8d5114af84cfda9aa0d7e5621586217b3dc38a8c42a4" {
		t.Fatalf("public contract sha256 = %s", got)
	}
	if len(publicRaw) == 0 || publicRaw[len(publicRaw)-1] == '\n' {
		t.Fatal("public contract must be compact JSON without a final line feed")
	}
	if PublicContractVersion != "relay.mcp.public-contract.v1" {
		t.Fatalf("public contract version = %q", PublicContractVersion)
	}

	registryRaw := RawRegistryDocument()
	if len(registryRaw) != 24150 {
		t.Fatalf("operation registry bytes = %d, want 24150", len(registryRaw))
	}
	registrySum := sha256.Sum256(registryRaw)
	if got := hex.EncodeToString(registrySum[:]); got != "6cb73de11e0f8f7f7903b7de0105f9dd139e324f25d71d98059512ff42b4622a" {
		t.Fatalf("operation registry sha256 = %s", got)
	}

	expectedManifests := map[SurfaceContractID]string{
		"planner-authoring.v1":   "0618add5d538eec9b2300695157439d2bfdf4349ce4684dfc8232a50cdf21a58",
		"planner-plan.v1":        "9f23d3745a26ac2e60f62aa737532d637b4705b17cf042c7f74b40ad3653d5a2",
		"planner-execution.v1":   "cee346ea5c41c6407485075db7963d86916b471e702a148e3230ec00b2508951",
		"auditor-review.v1":      "9ecf7331e5324a97e35917c4817e4f13a231976075735915e32d3def4983dc95",
		"auditor-audit.v1":       "1c6beea3a467453c978e8cd0353a82be7d2ce12be35eba35df907d9b2a2ecfdb",
		"auditor-remediation.v1": "a4651845c6c725380f364faed57b648f5941ddda32c9f394c49fc8d1b51ffc03",
	}

	for surface, expectedManifest := range expectedManifests {
		t.Run(string(surface), func(t *testing.T) {
			if got, ok := SurfaceManifestSHA256(surface); !ok || got != expectedManifest {
				t.Fatalf("surface manifest = %q, %v; want %q, true", got, ok, expectedManifest)
			}

			validCases := []struct {
				name         string
				includeFiles bool
				files        []any
			}{
				{name: "omitted"},
				{name: "empty", includeFiles: true, files: []any{}},
				{name: "one", includeFiles: true, files: refreshFiles(1)},
				{name: "sixty_four", includeFiles: true, files: refreshFiles(64)},
			}
			for _, test := range validCases {
				t.Run(test.name, func(t *testing.T) {
					raw := refreshSchemaRequest(t, surface, test.includeFiles, test.files)
					if _, err := ValidateRequest(surface, "refresh_operation_packet", raw); err != nil {
						t.Fatalf("valid refresh request failed: %v", err)
					}
				})
			}

			tooMany := refreshSchemaRequest(t, surface, true, refreshFiles(65))
			if _, err := ValidateRequest(surface, "refresh_operation_packet", tooMany); err == nil || !strings.Contains(err.Error(), "request_array_too_long") {
				t.Fatalf("65-file refresh request error = %v", err)
			}

			explicitNull := refreshSchemaRequest(t, surface, true, nil)
			if _, err := ValidateRequest(surface, "refresh_operation_packet", explicitNull); err == nil {
				t.Fatal("explicit null input_files was accepted")
			}

			var extra map[string]any
			if err := json.Unmarshal(refreshSchemaRequest(t, surface, false, nil), &extra); err != nil {
				t.Fatal(err)
			}
			extra["input_filez"] = []any{}
			extraRaw, err := json.Marshal(extra)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateRequest(surface, "refresh_operation_packet", extraRaw); err == nil {
				t.Fatal("undeclared refresh property was accepted")
			}
		})
	}

}

func refreshSchemaRequest(t *testing.T, surface SurfaceContractID, includeFiles bool, files []any) []byte {
	t.Helper()
	request := map[string]any{
		"surface_contract":    string(surface),
		"mutation_id":         "mutation-1",
		"expected_packet_id":  "packet-1",
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
	}
	if includeFiles {
		request["input_files"] = files
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func refreshFiles(count int) []any {
	files := make([]any, count)
	for index := range files {
		files[index] = map[string]any{
			"download_url": "https://files.example/item",
			"file_id":      "file-" + decimalRefreshIndex(index),
			"mime_type":    "application/json",
			"file_name":    "item.json",
		}
	}
	return files
}

func decimalRefreshIndex(value int) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	var reversed [20]byte
	index := len(reversed)
	for value > 0 {
		index--
		reversed[index] = digits[value%10]
		value /= 10
	}
	return string(reversed[index:])
}
