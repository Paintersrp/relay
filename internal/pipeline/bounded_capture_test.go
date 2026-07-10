package pipeline

import (
	"strings"
	"testing"
)

func TestBoundedCaptureKeepsTailAndCountsFullStream(t *testing.T) {
	capture := newBoundedCapture(8)
	for _, chunk := range []string{"abcd", "efgh", "ijkl"} {
		if _, err := capture.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got := capture.String(); got != "efghijkl" {
		t.Fatalf("capture = %q", got)
	}
	if capture.TotalBytes() != 12 || !capture.Truncated() {
		t.Fatalf("total=%d truncated=%v", capture.TotalBytes(), capture.Truncated())
	}
}

func TestBoundedCaptureUnlimitedPreservesContent(t *testing.T) {
	capture := newBoundedCapture(0)
	input := strings.Repeat("x", 1024)
	if _, err := capture.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if capture.String() != input || capture.Truncated() || capture.TotalBytes() != int64(len(input)) {
		t.Fatalf("unexpected unlimited capture state")
	}
}
