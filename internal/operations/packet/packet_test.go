package packet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifyBytesRequiresExactSizeAndDigest(t *testing.T) {
	data := []byte("packet")
	digest := sha256.Sum256(data)
	want := hex.EncodeToString(digest[:])
	if err := VerifyBytes(data, want, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBytes(data, want, int64(len(data)+1)); err == nil {
		t.Fatal("size mismatch was accepted")
	}
	if err := VerifyBytes(append([]byte(nil), data...), strings.Repeat("a", 64), int64(len(data))); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}

func TestInputSourceClosedUnionRejectsInactiveFields(t *testing.T) {
	value := InputSource{Kind: InputSourceInlineText, ArtifactID: "artifact-text", FileIndex: 1}
	if err := validateInputSource(value); err == nil {
		t.Fatal("inactive input-source branch field was accepted")
	}
}

func TestCanonicalStringEscapesControlCharactersWithoutHTMLEscaping(t *testing.T) {
	var out strings.Builder
	for _, value := range []string{"\"", "\\", "\x00", "<"} {
		var buffer bytes.Buffer
		writeString(&buffer, value)
		out.WriteString(buffer.String())
	}
	got := out.String()
	if !strings.Contains(got, `\u0000`) || !strings.Contains(got, `\\`) || !strings.Contains(got, `<`) {
		t.Fatalf("canonical escapes = %q", got)
	}
}
