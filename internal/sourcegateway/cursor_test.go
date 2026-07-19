package sourcegateway

import (
	"strings"
	"testing"
)

func TestHMACCursorCodecRejectsTamperingAndKeyChanges(t *testing.T) {
	codec, err := NewHMACCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	value := cursorPayload{Version: CursorVersion, Kind: "tree", PacketID: "opkt-test", PacketSHA256: strings.Repeat("a", 64), SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: "project-test", RepositoryKey: "relay", PublicationID: "publication-test", VaultRelationshipRowID: 7, CommitOID: strings.Repeat("b", 40), TreeOID: strings.Repeat("c", 40), RequestFingerprint: strings.Repeat("d", 64), AfterPath: PathReference{PathID: strings.Repeat("e", 64), InlineBase64: "YQ=="}}
	token, err := codec.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(token)
	if err != nil || decoded.PacketID != value.PacketID || decoded.AfterPath.PathID != value.AfterPath.PathID {
		t.Fatalf("decoded = %#v err=%v", decoded, err)
	}
	replacement := byte('A')
	if token[0] == replacement {
		replacement = 'B'
	}
	if _, err := codec.Decode(string(replacement) + token[1:]); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("tampered cursor error = %v", err)
	}
	other, err := NewHMACCursorCodec([]byte(strings.Repeat("z", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decode(token); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("wrong-key cursor error = %v", err)
	}
}

func TestHMACCursorCodecRejectsOversizedTokens(t *testing.T) {
	codec, err := NewHMACCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(strings.Repeat("a", MaxCursorTokenBytes+1)); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("oversized cursor error = %v", err)
	}
}
