package sourcegateway

import (
	"bytes"
	"strings"
	"testing"
)

func TestPathIdentityPreservesExactBytesAndRejectsUnsafeComponents(t *testing.T) {
	values := [][]byte{{}, []byte("README.md"), []byte("dir/line\nname"), {0xff, 0xfe, 'x'}, []byte(strings.Repeat("a", MaxInlinePathBytes+1))}
	for _, value := range values {
		if !validatePath(value, true) {
			t.Fatalf("valid path rejected: %x", value)
		}
		decoded, ok := decodeCanonicalInline(canonicalInline(value))
		if !ok || !bytes.Equal(decoded, value) {
			t.Fatalf("inline round trip failed: %x", value)
		}
		if pathID(value) != pathID(append([]byte(nil), value...)) {
			t.Fatalf("path digest is not deterministic: %x", value)
		}
	}
	for _, value := range [][]byte{{'/'}, {'a', 0, 'b'}, []byte("a//b"), []byte("a/./b"), []byte("a/../b")} {
		if validatePath(value, true) {
			t.Fatalf("unsafe path accepted: %x", value)
		}
	}
	if pathID([]byte("a")) == pathID([]byte("A")) || pathID([]byte("a")) == pathID([]byte("a\n")) {
		t.Fatal("different exact path bytes share an identity")
	}
}
