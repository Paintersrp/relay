package store

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestContextPacketStoreCreateGetList(t *testing.T) {
	st := newTestStore(t)

	project, err := st.CreateProject("relay", "Relay", "", "active", "")
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	snapshot, err := st.CreateSourceSnapshot(CreateSourceSnapshotParams{
		SourceSnapshotID: "srcsnap_test",
		ProjectRowID:     project.ID,
		ProjectID:        "relay",
		SnapshotKind:     "clean_commit",
		Status:           "created",
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}

	packet, err := st.CreateContextPacket(CreateContextPacketParams{
		ContextPacketID:     "ctxpkt_test",
		ProjectRowID:        project.ID,
		ProjectID:           "relay",
		PlanID:              "plan-1",
		PassID:              "PASS-005",
		TaskSlug:            "packet",
		SourceSnapshotRowID: snapshot.ID,
		SourceSnapshotID:    "srcsnap_test",
		Status:              "created",
		PacketJSONPath:      "handoffs/context/packet.context-packet.json",
		PacketMarkdownPath:  "handoffs/context/packet.context-packet.md",
		CoverageReportPath:  "handoffs/context/packet.context-coverage-report.json",
		SourceCount:         1,
		CoveredSeedCount:    1,
		BlockersJSON:        "[]",
		SummaryJSON:         "{}",
		CompletedAt:         "2026-06-22 00:00:00",
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}

	if _, err := st.CreateContextPacketSource(CreateContextPacketSourceParams{
		ContextPacketRowID: packet.ID,
		SourceID:           "src_test",
		SourceType:         "file_read",
		ProjectID:          "relay",
		RepoID:             "relay",
		SourceSnapshotID:   "srcsnap_test",
		Path:               "src/app.txt",
		LineStart:          1,
		LineEnd:            2,
		ContentHash:        "content-hash",
		SnippetHash:        "snippet-hash",
		RedactionStatus:    "not_needed",
		GeneratedAt:        "2026-06-22 00:00:00",
	}); err != nil {
		t.Fatalf("CreateContextPacketSource error: %v", err)
	}

	got, err := st.GetContextPacketByID("ctxpkt_test")
	if err != nil {
		t.Fatalf("GetContextPacketByID error: %v", err)
	}
	if got.ProjectID != "relay" || got.SourceCount != 1 {
		t.Fatalf("unexpected packet row: %+v", got)
	}
	list, err := st.ListContextPacketsByProject("relay")
	if err != nil {
		t.Fatalf("ListContextPacketsByProject error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one packet row, got %+v", list)
	}
	sources, err := st.ListContextPacketSources(packet.ID)
	if err != nil {
		t.Fatalf("ListContextPacketSources error: %v", err)
	}
	if len(sources) != 1 || sources[0].SnippetHash == "" || sources[0].Path != "src/app.txt" {
		t.Fatalf("unexpected source metadata rows: %+v", sources)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "relay.sqlite"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("Close store: %v", err)
		}
	})
	return st
}
