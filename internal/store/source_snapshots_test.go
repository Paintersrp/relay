package store

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestSourceSnapshotStoreRoundTrip(t *testing.T) {
	t.Parallel()

	st := newSourceSnapshotTestStore(t)

	project, err := st.CreateProject("relay", "Relay", "", "active", "")
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	repo, err := st.UpsertProjectRepository(UpsertProjectRepositoryParams{
		ProjectRowID:     project.ID,
		RepoID:           "relay",
		Role:             "primary",
		LocalPath:        filepath.Join(`D:\Code`, "relay"),
		DefaultBranch:    "main",
		AllowedRootsJSON: `["internal"]`,
		IgnoredGlobsJSON: `["node_modules/**"]`,
		MaxFileSizeBytes: 262144,
		Enabled:          1,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}

	snapshot, err := st.CreateSourceSnapshot(CreateSourceSnapshotParams{
		SourceSnapshotID: "snapshot-1",
		ProjectRowID:     project.ID,
		ProjectID:        project.ProjectID,
		SnapshotKind:     "clean_commit",
		Status:           "created",
		CompletedAt:      "",
		SummaryJSON:      `{"repository_count":1}`,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}

	if _, err := st.CreateSourceSnapshotRepository(CreateSourceSnapshotRepositoryParams{
		SourceSnapshotRowID:    snapshot.ID,
		ProjectRepositoryRowID: repo.ID,
		RepoID:                 repo.RepoID,
		Role:                   repo.Role,
		LocalPath:              repo.LocalPath,
		DefaultBranch:          repo.DefaultBranch,
		CurrentBranch:          "main",
		HeadSHA:                "abc123",
		GitStatusAvailable:     1,
		StatusPorcelainHash:    "hash",
	}); err != nil {
		t.Fatalf("CreateSourceSnapshotRepository error: %v", err)
	}

	rows, err := st.ListSourceSnapshotRepositories(snapshot.ID)
	if err != nil {
		t.Fatalf("ListSourceSnapshotRepositories error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 repository row, got %d", len(rows))
	}

	if _, err := st.CreateSourceSnapshotFile(CreateSourceSnapshotFileParams{
		SourceSnapshotRepositoryRowID: rows[0].ID,
		Path:                          "internal/app.go",
		SizeBytes:                     42,
		ContentHash:                   "content-hash",
		HashAlgorithm:                 "sha256",
		Tracked:                       1,
		Included:                      1,
		RedactionStatus:               "not_scanned",
	}); err != nil {
		t.Fatalf("CreateSourceSnapshotFile error: %v", err)
	}

	files, err := st.ListSourceSnapshotFiles(rows[0].ID)
	if err != nil {
		t.Fatalf("ListSourceSnapshotFiles error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file row, got %d", len(files))
	}

	stored, err := st.GetSourceSnapshotByID("snapshot-1")
	if err != nil {
		t.Fatalf("GetSourceSnapshotByID error: %v", err)
	}
	if stored.ProjectID != "relay" {
		t.Fatalf("expected project_id relay, got %q", stored.ProjectID)
	}

	listed, err := st.ListSourceSnapshotsByProject(project.ID)
	if err != nil {
		t.Fatalf("ListSourceSnapshotsByProject error: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 source snapshot, got %d", len(listed))
	}
}

func newSourceSnapshotTestStore(t *testing.T) *Store {
	t.Helper()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := Open(filepath.Join(dir, "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})
	return st
}
