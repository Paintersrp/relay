package transporttrace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStoreProtectsFilesRotatesAndPrunes(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store, err := newStore(root, "planner-frontier", RetentionPolicy{MaxAge: time.Hour, MaxBytes: MinimumMaxBytes}, storeOptions{segmentBytes: 900, now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := 0; index < 6; index++ {
		record := testRecord("planner-frontier", strings.Repeat(string(rune('a'+index)), 64))
		if _, err := store.Append(record); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	directory := filepath.Join(root, "planner-frontier")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("segments=%d", len(entries))
	}
	rootInfo, _ := os.Stat(root)
	directoryInfo, _ := os.Stat(directory)
	if runtime.GOOS != "windows" && (rootInfo.Mode().Perm() != 0o700 || directoryInfo.Mode().Perm() != 0o700) {
		t.Fatalf("directory modes root=%o mapping=%o", rootInfo.Mode().Perm(), directoryInfo.Mode().Perm())
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("segment mode=%o", info.Mode().Perm())
		}
	}
	old := filepath.Join(directory, "planner-frontier-20260722T090000.000000000Z-000000.jsonl")
	if err := os.WriteFile(old, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-2 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if removed == 0 {
		t.Fatal("expired segment was not pruned")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("expired segment remains: %v", err)
	}
}

func testRecord(mappingID, digest string) Record {
	return Record{
		SchemaVersion:       SchemaVersion,
		RequestID:           strings.Repeat("a", 32),
		StartedAt:           "2026-07-22T12:00:00Z",
		MappingID:           mappingID,
		RoutePath:           "/mcp/v1/planner/frontier",
		SurfaceContract:     "planner-ticket-frontier.v1",
		RouteManifestSHA256: strings.Repeat("b", 64),
		ResponseSHA256:      digest,
		CompletionState:     CompletionNotApplicable,
		OutcomeClass:        OutcomeSuccess,
		DownstreamWrite:     DownstreamWrite{Complete: true},
	}
}
