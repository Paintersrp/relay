package registry

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSensitiveDataClearanceExactContract(t *testing.T) {
	value := SensitiveDataClearance{PolicyVersion: SensitiveDataClearancePolicyVersion, SubjectSHA256: strings.Repeat("a", 64), Confirmed: true}
	if err := ValidateSensitiveDataClearance(value); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"policy_version":"relay.canonical-artifact-sensitive-data.v1","subject_sha256":"` + strings.Repeat("a", 64) + `","declaration":{"password":false,"api_key_or_access_token":false,"refresh_token_or_session_material":false,"cookie_or_authorization_header":false,"private_or_ssh_key":false,"credential":false,"complete_secret_bearing_environment_file":false,"avoidable_signed_secret_bearing_url":false},"confirmed":true}`
	if string(raw) != want {
		t.Fatalf("clearance JSON = %s", raw)
	}
	typeOf := reflect.TypeOf(value)
	wantFields := []string{"PolicyVersion", "SubjectSHA256", "Declaration", "Confirmed"}
	for index, name := range wantFields {
		if typeOf.Field(index).Name != name {
			t.Fatalf("field %d = %s, want %s", index, typeOf.Field(index).Name, name)
		}
	}
}

func TestSensitiveDataClearanceFailures(t *testing.T) {
	valid := SensitiveDataClearance{PolicyVersion: SensitiveDataClearancePolicyVersion, SubjectSHA256: strings.Repeat("b", 64), Confirmed: true}
	cases := []struct {
		name string
		edit func(*SensitiveDataClearance)
		want error
	}{
		{name: "version", edit: func(v *SensitiveDataClearance) { v.PolicyVersion = "future" }, want: ErrSensitiveDataClearance},
		{name: "sha", edit: func(v *SensitiveDataClearance) { v.SubjectSHA256 = strings.Repeat("B", 64) }, want: ErrSensitiveDataClearance},
		{name: "confirmation", edit: func(v *SensitiveDataClearance) { v.Confirmed = false }, want: ErrSensitiveDataClearance},
		{name: "password", edit: func(v *SensitiveDataClearance) { v.Declaration.Password = true }, want: ErrSensitiveDataDeclaration},
		{name: "api key", edit: func(v *SensitiveDataClearance) { v.Declaration.APIKeyOrAccessToken = true }, want: ErrSensitiveDataDeclaration},
		{name: "refresh token", edit: func(v *SensitiveDataClearance) { v.Declaration.RefreshTokenOrSessionMaterial = true }, want: ErrSensitiveDataDeclaration},
		{name: "cookie", edit: func(v *SensitiveDataClearance) { v.Declaration.CookieOrAuthorizationHeader = true }, want: ErrSensitiveDataDeclaration},
		{name: "private key", edit: func(v *SensitiveDataClearance) { v.Declaration.PrivateOrSSHKey = true }, want: ErrSensitiveDataDeclaration},
		{name: "credential", edit: func(v *SensitiveDataClearance) { v.Declaration.Credential = true }, want: ErrSensitiveDataDeclaration},
		{name: "environment", edit: func(v *SensitiveDataClearance) { v.Declaration.CompleteSecretBearingEnvironmentFile = true }, want: ErrSensitiveDataDeclaration},
		{name: "signed URL", edit: func(v *SensitiveDataClearance) { v.Declaration.AvoidableSignedSecretBearingURL = true }, want: ErrSensitiveDataDeclaration},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			if err := ValidateSensitiveDataClearance(candidate); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMutationAuthority(t *testing.T) {
	want := []MutationTool{
		"create_operation_packet",
		"refresh_operation_packet",
		"close_operation_packet",
		"submit_plan",
		"create_run",
		"record_audit_decision",
	}
	if got := StateChangingTools(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %#v", got)
	}
	for _, tool := range want {
		if !IsStateChangingTool(string(tool)) {
			t.Fatalf("missing tool %q", tool)
		}
		if version, ok := SemanticProjectionVersion(string(tool)); !ok || version == "" {
			t.Fatalf("semantic projection version for %q = %q, %v", tool, version, ok)
		}
	}
	memberships := []struct {
		surface SurfaceContractID
		tool    string
	}{
		{surface: "planner-authoring.v1", tool: "create_operation_packet"},
		{surface: "planner-authoring.v1", tool: "refresh_operation_packet"},
		{surface: "planner-authoring.v1", tool: "close_operation_packet"},
		{surface: "planner-plan.v1", tool: "submit_plan"},
		{surface: "planner-execution.v1", tool: "create_run"},
		{surface: "auditor-audit.v1", tool: "record_audit_decision"},
		{surface: "auditor-remediation.v1", tool: "create_run"},
	}
	for _, membership := range memberships {
		if !IsStateChangingToolForSurface(membership.surface, membership.tool) {
			t.Fatalf("missing surface tool %s.%s", membership.surface, membership.tool)
		}
	}
	for _, invalid := range []struct {
		surface SurfaceContractID
		tool    string
	}{
		{surface: "planner-authoring.v1", tool: "submit_plan"},
		{surface: "auditor-review.v1", tool: "create_run"},
		{surface: "unknown.v1", tool: "close_operation_packet"},
		{surface: "planner-plan.v1", tool: "validate_artifact"},
	} {
		if IsStateChangingToolForSurface(invalid.surface, invalid.tool) {
			t.Fatalf("unexpected surface tool %s.%s", invalid.surface, invalid.tool)
		}
	}
	for _, value := range []string{"m", "mutation-1", "a.b:c_d-9", strings.Repeat("a", 128)} {
		if err := ValidateMutationID(value); err != nil {
			t.Fatalf("valid mutation id %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "-bad", "space value", strings.Repeat("a", 129)} {
		if err := ValidateMutationID(value); !errors.Is(err, ErrMutationID) {
			t.Fatalf("invalid mutation id %q: %v", value, err)
		}
	}
}

func TestCanonicalArtifactClearanceSchemaIsClosedAndComplete(t *testing.T) {
	for _, test := range []struct {
		surface SurfaceContractID
		tool    string
	}{
		{surface: "planner-plan.v1", tool: "submit_plan"},
		{surface: "planner-execution.v1", tool: "create_run"},
		{surface: "auditor-remediation.v1", tool: "create_run"},
	} {
		t.Run(string(test.surface)+"/"+test.tool, func(t *testing.T) {
			baseline := canonicalArtifactClearanceRequest(test.surface)
			assertClearanceRequestAccepted(t, test.surface, test.tool, baseline)

			missingDeclaration := cloneJSONValue(t, baseline)
			delete(missingDeclaration["sensitive_data_clearance"].(map[string]any), "declaration")
			assertClearanceRequestRejected(t, test.surface, test.tool, missingDeclaration)

			partialDeclaration := cloneJSONValue(t, baseline)
			delete(partialDeclaration["sensitive_data_clearance"].(map[string]any)["declaration"].(map[string]any), "password")
			assertClearanceRequestRejected(t, test.surface, test.tool, partialDeclaration)

			pluralDeclaration := cloneJSONValue(t, baseline)
			clearance := pluralDeclaration["sensitive_data_clearance"].(map[string]any)
			clearance["declarations"] = clearance["declaration"]
			delete(clearance, "declaration")
			assertClearanceRequestRejected(t, test.surface, test.tool, pluralDeclaration)

			extraDeclaration := cloneJSONValue(t, baseline)
			extraDeclaration["sensitive_data_clearance"].(map[string]any)["declaration"].(map[string]any)["other"] = false
			assertClearanceRequestRejected(t, test.surface, test.tool, extraDeclaration)

			trueDeclaration := cloneJSONValue(t, baseline)
			trueDeclaration["sensitive_data_clearance"].(map[string]any)["declaration"].(map[string]any)["credential"] = true
			assertClearanceRequestRejected(t, test.surface, test.tool, trueDeclaration)
		})
	}
}

func canonicalArtifactClearanceRequest(surface SurfaceContractID) map[string]any {
	return map[string]any{
		"surface_contract":   string(surface),
		"mutation_id":        "mutation-1",
		"expected_packet_id": "packet-1",
		"artifact_file": map[string]any{
			"download_url": "https://files.example/item",
			"file_id":      "file-1",
			"mime_type":    "application/json",
			"file_name":    "feature.plan.json",
		},
		"artifact_name":   "feature.plan.json",
		"media_type":      "application/json",
		"expected_sha256": strings.Repeat("a", 64),
		"sensitive_data_clearance": map[string]any{
			"policy_version": SensitiveDataClearancePolicyVersion,
			"subject_sha256": strings.Repeat("a", 64),
			"declaration": map[string]any{
				"password":                                 false,
				"api_key_or_access_token":                  false,
				"refresh_token_or_session_material":        false,
				"cookie_or_authorization_header":           false,
				"private_or_ssh_key":                       false,
				"credential":                               false,
				"complete_secret_bearing_environment_file": false,
				"avoidable_signed_secret_bearing_url":      false,
			},
			"confirmed": true,
		},
	}
}

func cloneJSONValue(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func assertClearanceRequestAccepted(t *testing.T, surface SurfaceContractID, tool string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRequest(surface, tool, raw); err != nil {
		t.Fatalf("valid request failed: %v", err)
	}
}

func assertClearanceRequestRejected(t *testing.T, surface SurfaceContractID, tool string, value map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRequest(surface, tool, raw); err == nil {
		t.Fatal("invalid clearance request was accepted")
	}
}
