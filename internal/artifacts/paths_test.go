package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextPathUsesNamingPolicy(t *testing.T) {
	oldBase := BaseDir
	SetBaseDir(t.TempDir())
	t.Cleanup(func() { SetBaseDir(oldBase) })

	path, err := ContextPath("2026-06-22", "context-packets-coverage-reports", "context_packet_json")
	if err != nil {
		t.Fatalf("ContextPath error: %v", err)
	}
	want := filepath.Join(BaseDir, "handoffs", "context", "2026-06-22_context-packets-coverage-reports.context-packet.json")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestWriteContextWritesBelowContextDir(t *testing.T) {
	oldBase := BaseDir
	SetBaseDir(t.TempDir())
	t.Cleanup(func() { SetBaseDir(oldBase) })

	path, err := WriteContext("2026-06-22", "packet", "context_coverage_report_json", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("WriteContext error: %v", err)
	}
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(ContextDir())+string(filepath.Separator)) {
		t.Fatalf("expected path below context dir, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected artifact content: %q", string(data))
	}
}

func TestContextPathRejectsUnsafeInputs(t *testing.T) {
	cases := []struct {
		name string
		date string
		slug string
		kind string
	}{
		{name: "bad date", date: "20260622", slug: "packet", kind: "context_packet_json"},
		{name: "traversal slug", date: "2026-06-22", slug: "../packet", kind: "context_packet_json"},
		{name: "absolute slug", date: "2026-06-22", slug: "/packet", kind: "context_packet_json"},
		{name: "unknown kind", date: "2026-06-22", slug: "packet", kind: "planner_handoff"},
		{name: "empty slug", date: "2026-06-22", slug: "", kind: "context_packet_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if path, err := ContextPath(tc.date, tc.slug, tc.kind); err == nil {
				t.Fatalf("expected error, got path %q", path)
			}
		})
	}
}
