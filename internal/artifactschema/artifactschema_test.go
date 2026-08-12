package artifactschema

import (
	"bytes"
	"strings"
	"testing"
)

func TestPinnedCurrentAuthorityCatalog(t *testing.T) {
	if AuthorityRepository != "Paintersrp/relay-specs" || AuthorityCommit != "9ea40ac112d0683affc10ba6bad2d15efe9e59f4" {
		t.Fatalf("authority = %s@%s", AuthorityRepository, AuthorityCommit)
	}
	want := []struct {
		kind               Kind
		version, path, sha string
	}{
		{KindPlan, "1.0", "schemas/plan.schema.json", "03a75ab1352d27193ec27b5aec9f449e65daf69de66d6897ab74672bdc705cf8"},
		{KindDeterministicOperations, "1.0", "schemas/deterministic-operations.schema.json", "630d5e028c243deae76038e3e44b6c4ff9a3f482f32d1aece69573ba04e9df28"},
		{KindAuditPacket, "4.0", "schemas/audit-packet.schema.json", "2fdf6769701c937c83869a2ec656146ce6b3cc75f3918ae38c42f01336071dcf"},
		{KindDeliveryTicket, "2.0", "schemas/delivery-ticket.schema.json", "681f0b9fa9ce60fb6d6f325426fc1d935385de63ea74324606fc7858dd5a9b0b"},
		{KindTransitionPlan, "1.0", "schemas/transition-plan.schema.json", "73b552bac0201d9aa6ad907b8faad1fe6b5b88367fff18840306c8da82e5e9ec"},
		{KindDeliveryPlan, "1.0", "schemas/delivery-plan.schema.json", "bafc8d30d67cd6c15181a65e6e69d0c384d1372aa13c2ad873298d3bfba0f18e"},
	}
	got := Definitions()
	if len(got) != len(want) {
		t.Fatalf("definitions=%d want=%d", len(got), len(want))
	}
	for i, expected := range want {
		if got[i].Kind != expected.kind || got[i].ProducerVersion != expected.version || got[i].AuthorityPath != expected.path || got[i].SHA256 != expected.sha {
			t.Fatalf("definition %d=%+v want=%+v", i, got[i], expected)
		}
	}
}

func TestPinnedAuthorityDeliveryTicketSchemaIsExactV20(t *testing.T) {
	definition, ok := Current(KindDeliveryTicket)
	if !ok {
		t.Fatal("delivery ticket schema missing")
	}
	if definition.ProducerVersion != "2.0" || definition.SHA256 != "681f0b9fa9ce60fb6d6f325426fc1d935385de63ea74324606fc7858dd5a9b0b" {
		t.Fatalf("delivery ticket definition = %+v", definition)
	}
	// The embedded bytes are exactly the pinned authority schema bytes: the
	// v2.0 title and the v2 fields must be present while the retired v1
	// validation_intent field must be absent.
	content := string(definition.Bytes)
	for _, required := range []string{`"Delivery Ticket v2.0"`, `"source_area"`, `"working_directory"`, `"prerequisites"`, `"required_invariants"`, `"forbidden_behaviors"`, `"proof_obligations"`, `"validation_commands"`, `"explicit_deferrals"`} {
		if !strings.Contains(content, required) {
			t.Fatalf("pinned v2.0 schema is missing %s", required)
		}
	}
	if strings.Contains(content, `"validation_intent"`) {
		t.Fatal("pinned v2.0 schema retains retired validation_intent")
	}
	active := []byte(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P1-T1","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Deliver the outcome.","context":"Carried context.","scope":{"in_scope":["Deliver."],"out_of_scope":["Other."]},"depends_on":[],"required_invariants":["Invariant."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":null,"obligation":"Implement it.","prerequisites":[]}],"proof_obligations":["Prove it."],"validation_commands":[{"working_directory":"","command":"go test ./internal/example","expected":"Tests pass."}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["Complete."]}`)
	valid, err := Validate(KindDeliveryTicket, active)
	if err != nil || !valid {
		t.Fatalf("active v2.0 delivery ticket valid=%v err=%v", valid, err)
	}
	cancellation := []byte(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P1-T1","revision":2,"replaces_revision":1,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Cancel the outcome.","context":"Cancellation context.","scope":{"in_scope":["Record the cancellation."],"out_of_scope":["No execution."]},"depends_on":[],"required_invariants":[],"forbidden_behaviors":[],"implementation_obligations":[],"proof_obligations":[],"validation_commands":[],"transition_applicability":"not_required","explicit_deferrals":[],"cancellation":{"reason":"Superseded."},"completion_criteria":["Cancellation is recorded."]}`)
	valid, err = Validate(KindDeliveryTicket, cancellation)
	if err != nil || !valid {
		t.Fatalf("cancellation v2.0 delivery ticket valid=%v err=%v", valid, err)
	}
	unsafe := []byte(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P1-T1","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Deliver the outcome.","context":"Carried context.","scope":{"in_scope":["Deliver."],"out_of_scope":["Other."]},"depends_on":[],"required_invariants":["Invariant."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"../escape","obligation":"Implement it.","prerequisites":[]}],"proof_obligations":["Prove it."],"validation_commands":[{"working_directory":"C:\\Windows","command":"go test ./internal/example","expected":"Tests pass."}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["Complete."]}`)
	valid, err = Validate(KindDeliveryTicket, unsafe)
	if err != nil || valid {
		t.Fatalf("unsafe v2.0 delivery ticket valid=%v err=%v", valid, err)
	}
}

func TestDeterministicOperationsAndAuditSchemasValidate(t *testing.T) {
	raw := []byte("{\"schema_version\":\"1.0\",\"feature_slug\":\"schema-test\",\"repo_target\":\"relay\",\"branch\":\"main\",\"base_commit\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"coverage\":\"complete\",\"operations\":[{\"path\":\"internal/example.go\",\"operation\":\"create\",\"implementation\":{\"content\":\"package example\\n\"}}]}")
	valid, err := Validate(KindDeterministicOperations, raw)
	if err != nil || !valid {
		t.Fatalf("deterministic valid=%v err=%v", valid, err)
	}
	audit, ok := Current(KindAuditPacket)
	if !ok {
		t.Fatal("audit packet schema missing")
	}
	if _, err := prepareSchema(audit); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentReturnsIndependentSchemaBytesAndUnknownPatternsFailClosed(t *testing.T) {
	first, ok := Current(KindDeterministicOperations)
	if !ok {
		t.Fatal("deterministic operations schema missing")
	}
	original := append([]byte(nil), first.Bytes...)
	first.Bytes[0] ^= 0xff
	second, _ := Current(KindDeterministicOperations)
	if !bytes.Equal(second.Bytes, original) {
		t.Fatal("Current exposed mutable schema bytes")
	}
	if _, err := portablePatternConstraints("^(?=unregistered)"); err == nil || !strings.Contains(err.Error(), "unsupported authoritative schema pattern") {
		t.Fatalf("error=%v", err)
	}
}
