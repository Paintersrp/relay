package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizePlanPayload_ObjectInput(t *testing.T) {
	planObj := json.RawMessage(`{"plan_meta":{"plan_id":"p1"},"passes":[]}`)
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlan: planObj,
	}

	raw, err := normalizePlanPayload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != string(planObj) {
		t.Fatalf("expected normalized bytes to match input object, got %s", string(raw))
	}
}

func TestNormalizePlanPayload_LegacyStringInput(t *testing.T) {
	inner := `{"plan_meta":{"plan_id":"p1"},"passes":[]}`
	// planner_pass_plan_json arrives as a JSON string value (with surrounding quotes)
	jsonStr, _ := json.Marshal(inner)
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlanJSON: jsonStr,
	}

	raw, err := normalizePlanPayload(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != inner {
		t.Fatalf("expected normalized bytes to equal inner JSON, got %s", string(raw))
	}
}

func TestNormalizePlanPayload_BothPresent(t *testing.T) {
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlan:     json.RawMessage(`{"a":1}`),
		PlannerPassPlanJSON: json.RawMessage(`"{}"`),
	}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error when both payload fields are present")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected error about exactly one, got: %v", err)
	}
}

func TestNormalizePlanPayload_NeitherPresent(t *testing.T) {
	args := &submitPlannerPassPlanArgs{}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error when neither payload field is present")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected error about exactly one, got: %v", err)
	}
}

func TestNormalizePlanPayload_ObjectWrongType(t *testing.T) {
	cases := []struct {
		name    string
		raw     json.RawMessage
		wantErr string
	}{
		{"array", json.RawMessage(`[1,2,3]`), "must be a JSON object"},
		{"string", json.RawMessage(`"hello"`), "must be a JSON object"},
		{"number", json.RawMessage(`42`), "must be a JSON object"},
		{"boolean", json.RawMessage(`true`), "must be a JSON object"},
		// null is treated as absent per spec; neither present triggers exactly-one error.
		{"null", json.RawMessage(`null`), "exactly one"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := &submitPlannerPassPlanArgs{
				PlannerPassPlan: tc.raw,
			}
			_, err := normalizePlanPayload(args)
			if err == nil {
				t.Fatal("expected error for non-object planner_pass_plan")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestNormalizePlanPayload_LegacyNotAString(t *testing.T) {
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlanJSON: json.RawMessage(`{"a":1}`),
	}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error when planner_pass_plan_json is not a string")
	}
	if !strings.Contains(err.Error(), "valid JSON string") {
		t.Fatalf("expected string-type error, got: %v", err)
	}
}

func TestNormalizePlanPayload_LegacyEmptyString(t *testing.T) {
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlanJSON: json.RawMessage(`""`),
	}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error for empty planner_pass_plan_json string")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected empty-string error, got: %v", err)
	}
}

func TestNormalizePlanPayload_LegacyInvalidJSON(t *testing.T) {
	jsonStr, _ := json.Marshal("this is not json")
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlanJSON: jsonStr,
	}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error for invalid JSON in planner_pass_plan_json")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected valid-JSON error, got: %v", err)
	}
}

func TestNormalizePlanPayload_NullFieldsTreatedAsAbsent(t *testing.T) {
	args := &submitPlannerPassPlanArgs{
		PlannerPassPlan:     json.RawMessage(`null`),
		PlannerPassPlanJSON: json.RawMessage(`null`),
	}

	_, err := normalizePlanPayload(args)
	if err == nil {
		t.Fatal("expected error when both fields are null (treated as absent)")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected error about exactly one, got: %v", err)
	}
}

func TestSubmitPlannerPassPlanSchema_HasBothPayloadFields(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal([]byte(submitPlannerPassPlanSchema), &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties object in schema")
	}

	if _, ok := props["planner_pass_plan"]; !ok {
		t.Fatal("expected planner_pass_plan in schema properties")
	}
	if _, ok := props["planner_pass_plan_json"]; !ok {
		t.Fatal("expected planner_pass_plan_json in schema properties")
	}

	planProp, ok := props["planner_pass_plan"].(map[string]any)
	if !ok {
		t.Fatal("expected planner_pass_plan to be an object property")
	}
	if planProp["type"] != "object" {
		t.Fatalf("expected planner_pass_plan type object, got %v", planProp["type"])
	}

	required, ok := s["required"].([]any)
	if !ok {
		t.Fatal("expected required array in schema")
	}
	hasUnmanaged := false
	for _, r := range required {
		if r == "unmanaged_acknowledged" {
			hasUnmanaged = true
			break
		}
	}
	if !hasUnmanaged {
		t.Fatal("expected unmanaged_acknowledged in schema required")
	}
}

func TestSubmitPlannerPassPlanSchema_HasOneOf(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal([]byte(submitPlannerPassPlanSchema), &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	oneOf, ok := s["oneOf"].([]any)
	if !ok {
		t.Fatal("expected oneOf array in schema")
	}
	if len(oneOf) != 2 {
		t.Fatalf("expected oneOf with 2 entries, got %d", len(oneOf))
	}
}

func TestSubmitPlannerPassPlanSchema_AdditionalPropertiesFalse(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal([]byte(submitPlannerPassPlanSchema), &s); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	if s["additionalProperties"] != false {
		t.Fatal("expected additionalProperties false in schema")
	}
}
