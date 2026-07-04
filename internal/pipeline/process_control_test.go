package pipeline

import (
	"errors"
	"testing"
)

func TestProcessIdentityEncodeDecode(t *testing.T) {
	identity := ProcessIdentity{
		PID:       123,
		GroupID:   456,
		StartedAt: "fingerprint",
		Platform:  "test",
	}
	decoded, err := DecodeProcessIdentity(identity.Encode())
	if err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	if decoded.PID != identity.PID || decoded.GroupID != identity.GroupID || decoded.StartedAt != identity.StartedAt {
		t.Fatalf("decoded identity mismatch: got %+v want %+v", decoded, identity)
	}
}

func TestDecodeProcessIdentityRequiresFingerprint(t *testing.T) {
	_, err := DecodeProcessIdentity(`{"pid":123,"platform":"test"}`)
	if !errors.Is(err, ErrProcessUnverifiable) {
		t.Fatalf("expected ErrProcessUnverifiable, got %v", err)
	}
}
