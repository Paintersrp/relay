package audits

import (
	"encoding/json"
	"strings"
	"testing"

	"relay/internal/artifactschema"
	"relay/internal/executor"
)

func TestAuditPacketSchemaHasNoEffectiveExecutorBrief(t *testing.T) {
	definition, ok := artifactschema.Current(artifactschema.KindAuditPacket)
	if !ok {
		t.Fatal("audit packet schema is missing")
	}
	content := string(definition.Bytes)
	if strings.Contains(content, "effective_executor_brief") {
		t.Fatal("current audit packet schema still defines effective_executor_brief")
	}
	// The retired field must not appear in any other embedded schema either.
	for _, other := range artifactschema.Definitions() {
		if strings.Contains(string(other.Bytes), "effective_executor_brief") {
			t.Fatalf("embedded schema %s still defines effective_executor_brief", other.Kind)
		}
	}
}

func TestAuditPacketSchemaRequiresExecutionAssignmentAndNoBrief(t *testing.T) {
	definition, ok := artifactschema.Current(artifactschema.KindAuditPacket)
	if !ok {
		t.Fatal("audit packet schema is missing")
	}
	var document struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(definition.Bytes, &document); err != nil {
		t.Fatal(err)
	}
	var authority struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(document.Defs["authority"], &authority); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"delivery_ticket", "requirements", "shared_design", "execution_assignment"} {
		found := false
		for _, candidate := range authority.Required {
			if candidate == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("audit packet authority does not require %q", required)
		}
	}
	for _, field := range []string{"execution_assignment", "delivery_ticket", "requirements", "shared_design", "deterministic_operations"} {
		if _, exists := authority.Properties[field]; !exists {
			t.Fatalf("audit packet authority property %q is missing", field)
		}
	}
	if _, exists := authority.Properties["effective_executor_brief"]; exists {
		t.Fatal("audit packet authority property effective_executor_brief still exists")
	}
}

func TestGeneratedAuditPacketsValidateAgainstEmbeddedSchema(t *testing.T) {
	for _, mode := range []executor.ExecutionMode{
		executor.ExecutionModeAbsent,
		executor.ExecutionModePreflightFailed,
		executor.ExecutionModePartialApplied,
		executor.ExecutionModeCompleteApplied,
	} {
		t.Run(string(mode), func(t *testing.T) {
			input := testPackageAuditInput(t, mode, strings.Repeat("c", 40))
			if mode == executor.ExecutionModePreflightFailed {
				setPackageAuditPreflightCoverage(t, &input, "partial")
			}
			_, data, err := buildWorkflowPackageAuditPacket(input)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			valid, err := artifactschema.Validate(artifactschema.KindAuditPacket, data)
			if err != nil {
				t.Fatalf("validate against embedded schema: %v", err)
			}
			if !valid {
				t.Fatal("generated packet does not validate against the embedded schema")
			}
		})
	}
}

func TestAuditPacketConstructionRequiresNoBriefArtifact(t *testing.T) {
	// The construction input carries no Brief artifact and the built packet
	// authority binds exactly the approved Delivery Ticket, requirements,
	// shared design, and the runtime ExecutionAssignment reference.
	input := testPackageAuditInput(t, executor.ExecutionModeAbsent, strings.Repeat("c", 40))
	packet, data, err := buildWorkflowPackageAuditPacket(input)
	if err != nil {
		t.Fatalf("build without any Brief artifact: %v", err)
	}
	if strings.Contains(string(data), "effective_executor_brief") || strings.Contains(string(data), "executor_brief") {
		t.Fatal("generated packet references a Brief artifact")
	}
	var decoded struct {
		Authority struct {
			DeliveryTicket          json.RawMessage `json:"delivery_ticket"`
			Requirements            json.RawMessage `json:"requirements"`
			SharedDesign            json.RawMessage `json:"shared_design"`
			ExecutionAssignment     json.RawMessage `json:"execution_assignment"`
			EffectiveExecutorBrief  json.RawMessage `json:"effective_executor_brief"`
		} `json:"authority"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Authority.DeliveryTicket) == 0 || len(decoded.Authority.Requirements) == 0 || len(decoded.Authority.ExecutionAssignment) == 0 {
		t.Fatalf("authority = %#v", decoded.Authority)
	}
	if len(decoded.Authority.EffectiveExecutorBrief) != 0 {
		t.Fatal("authority carries an effective_executor_brief value")
	}
	if packet.Authority.ExecutionAssignment.ArtifactReference == "" || packet.Authority.ExecutionAssignment.SHA256 == "" {
		t.Fatalf("execution_assignment reference = %#v", packet.Authority.ExecutionAssignment)
	}
}
