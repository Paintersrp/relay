package artifactschema

import (
	"bytes"
	"strings"
	"testing"
)

func TestPinnedCurrentAuthorityCatalog(t *testing.T) {
	if AuthorityRepository != "Paintersrp/relay-specs" || AuthorityCommit != "0b1ab1705157d0b0fcc184b35037008fce64c469" {
		t.Fatalf("authority = %s@%s", AuthorityRepository, AuthorityCommit)
	}
	want := []struct {
		kind               Kind
		version, path, sha string
	}{
		{KindPlan, "1.0", "schemas/plan.schema.json", "03a75ab1352d27193ec27b5aec9f449e65daf69de66d6897ab74672bdc705cf8"},
		{KindDeterministicOperations, "1.0", "schemas/deterministic-operations.schema.json", "630d5e028c243deae76038e3e44b6c4ff9a3f482f32d1aece69573ba04e9df28"},
		{KindAuditPacket, "3.0", "schemas/audit-packet.schema.json", "cf66e0775593079a2e288ef2634e12fb40a19d02b2e7637fdaeffee49c4ac95b"},
		{KindDeliveryTicket, "1.0", "schemas/delivery-ticket.schema.json", "663845dfe1191d397102e689fd09f1ff1d26823ae6dc6798a2ec9cd623a02ee7"},
		{KindTransitionPlan, "1.0", "schemas/transition-plan.schema.json", "73b552bac0201d9aa6ad907b8faad1fe6b5b88367fff18840306c8da82e5e9ec"},
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
