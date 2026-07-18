package mcp

import (
	"bytes"
	"testing"

	"relay/internal/app/idempotency"
	"relay/internal/mcp/semanticidentity"
)

func TestLifecycleEnvelopeReturnsExactStoredResultIdentity(t *testing.T) {
	raw := []byte(`{"packet":{"summary":{"packet_id":"opkt"}}}`)
	stored := idempotency.StoredResult{ResultKind: semanticidentity.ResultKindCreateOperationPacket, ResultIdentityJSON: raw, ResultSHA256: "sha", CommittedAt: "2026-07-18T00:00:00.000000000Z"}
	envelope := lifecycleEnvelope(stored, true)
	if !envelope.Replay || envelope.ResultKind != stored.ResultKind || envelope.ResultSHA256 != stored.ResultSHA256 || envelope.CommittedAt != stored.CommittedAt || !bytes.Equal(envelope.ResultIdentityJSON, raw) {
		t.Fatalf("envelope = %#v", envelope)
	}
	envelope.ResultIdentityJSON[0] = '['
	if raw[0] != '{' {
		t.Fatal("result identity bytes were aliased")
	}
}

func TestLifecycleHandlerRequiresService(t *testing.T) {
	if _, err := NewOperationPacketLifecycleHandler(nil); err == nil {
		t.Fatal("expected nil service rejection")
	}
}
