package packet

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

func TestPacketClearanceUsesExactSharedAuthority(t *testing.T) {
	if reflect.TypeOf(SensitiveDataClearance{}) != reflect.TypeOf(registry.SensitiveDataClearance{}) {
		t.Fatal("packet clearance is not the shared exact type")
	}
	value := SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: strings.Repeat("a", 64), Confirmed: true}
	attestation := Attestation{Kind: "sensitive_data_clearance", InputName: "approved_plan", Clearance: &value}
	if err := validateAttestation(attestation); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := `{"policy_version":"relay.canonical-artifact-sensitive-data.v1","subject_sha256":"`
	if !strings.HasPrefix(string(raw), wantPrefix) || !strings.Contains(string(raw), `,"declaration":{`) || !strings.HasSuffix(string(raw), `},"confirmed":true}`) {
		t.Fatalf("clearance JSON order changed: %s", raw)
	}
}
