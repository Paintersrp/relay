package contextpackets

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/artifacts"
	"relay/internal/projects"
	"relay/internal/sources"
	"relay/internal/store"
)

func TestCreateContextPacketFromSeedFile(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		PlanID:           "plan-1",
		PassID:           "PASS-005",
		TaskSlug:         "context-packets-coverage-reports",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:    "relay",
			Path:      "src/app.txt",
			LineStart: 1,
			LineEnd:   2,
			Required:  true,
			Reason:    "primary source",
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusCreated || result.SourceCount != 1 || result.CoveredSeedCount != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertReadableFile(t, result.PacketJSONPath)
	assertReadableFile(t, result.PacketMarkdownPath)
	assertReadableFile(t, result.CoverageReportPath)

	packet := readPacketArtifact(t, result.PacketJSONPath)
	if packet.Sources[0].Content != "alpha\nbeta\n" {
		t.Fatalf("unexpected packet content: %+v", packet.Sources[0])
	}
	rows, err := fixture.store.ListContextPacketSources(mustPacketRow(t, fixture.store, result.ContextPacketID).ID)
	if err != nil {
		t.Fatalf("ListContextPacketSources error: %v", err)
	}
	if len(rows) != 1 || rows[0].SnippetHash == "" || rows[0].ContentHash == "" {
		t.Fatalf("expected metadata source row, got %+v", rows)
	}
}

func TestCreateContextPacketFromSeedSearch(t *testing.T) {
	requireRG(t)
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "search-packet",
		SourceSnapshotID: fixture.snapshotID,
		SeedSearches: []ContextSeedSearch{{
			RepoIDs:    []string{"relay"},
			Pattern:    "needle",
			Required:   true,
			MaxResults: 5,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusCreated || result.SourceCount != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if packet.Sources[0].SourceType != SourceTypeSearchMatch || !strings.Contains(packet.Sources[0].Snippet, "needle") {
		t.Fatalf("expected search match source, got %+v", packet.Sources[0])
	}
}

func TestCreateContextPacketInventoryOnly(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "inventory",
		SourceSnapshotID: fixture.snapshotID,
		IncludeInventory: true,
		MaxSources:       1,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusPartial || result.SourceCount != 1 || !result.Truncated {
		t.Fatalf("expected capped partial inventory packet, got %+v", result)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if packet.Sources[0].Content != "" || packet.Sources[0].Snippet != "" {
		t.Fatalf("inventory source should not include content/snippet, got %+v", packet.Sources[0])
	}
}

func TestCreateContextPacketRequiredMissingFileBlocked(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "missing-file",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:   "relay",
			Path:     "src/missing.txt",
			Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusBlocked || result.BlockedSeedCount != 1 || result.SourceCount != 0 {
		t.Fatalf("expected blocked missing file, got %+v", result)
	}
}

func TestCreateContextPacketRequiredSearchNoMatchesBlockedAndOptionalPartial(t *testing.T) {
	requireRG(t)
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	required, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "required-search",
		SourceSnapshotID: fixture.snapshotID,
		SeedSearches: []ContextSeedSearch{{
			RepoIDs:  []string{"relay"},
			Pattern:  "does-not-exist",
			Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket required error: %v", err)
	}
	if required.Status != ContextPacketStatusBlocked || required.MissingSeedCount != 1 {
		t.Fatalf("expected blocked required search, got %+v", required)
	}

	optional, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "optional-search",
		SourceSnapshotID: fixture.snapshotID,
		SeedSearches: []ContextSeedSearch{{
			RepoIDs:  []string{"relay"},
			Pattern:  "does-not-exist",
			Required: false,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket optional error: %v", err)
	}
	if optional.Status != ContextPacketStatusPartial || optional.MissingSeedCount != 0 {
		t.Fatalf("expected partial optional search, got %+v", optional)
	}
}

func TestCreateContextPacketBlocksSnapshotFileChanged(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})
	writeFile(t, filepath.Join(fixture.repoRoot, "src", "app.txt"), "mutated\n")

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "drift",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:   "relay",
			Path:     "src/app.txt",
			Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusBlocked || len(result.Blockers) != 1 || result.Blockers[0].Code != sources.SourceBlockerSnapshotFileChanged {
		t.Fatalf("expected drift blocker, got %+v", result)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if len(packet.Sources) != 0 {
		t.Fatalf("expected no stale source content, got %+v", packet.Sources)
	}
}

func TestCreateContextPacketRedactsAndBlocksPrivateKey(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{withPrivateKey: true})

	redacted, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "redacted-token",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:   "relay",
			Path:     "src/token.txt",
			Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket redacted error: %v", err)
	}
	packet := readPacketArtifact(t, redacted.PacketJSONPath)
	if strings.Contains(packet.Sources[0].Content, "super-secret-token") || !strings.Contains(packet.Sources[0].Content, "[REDACTED_AUTH_HEADER]") {
		t.Fatalf("expected redacted token content, got %+v", packet.Sources[0])
	}
	md, err := os.ReadFile(redacted.PacketMarkdownPath)
	if err != nil {
		t.Fatalf("ReadFile markdown error: %v", err)
	}
	if strings.Contains(string(md), "super-secret-token") {
		t.Fatalf("markdown persisted unredacted token: %s", string(md))
	}

	blocked, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "private-key",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:   "relay",
			Path:     "src/private.txt",
			Required: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket private key error: %v", err)
	}
	if blocked.Status != ContextPacketStatusBlocked || blocked.SourceCount != 0 {
		t.Fatalf("expected private-key blocked packet, got %+v", blocked)
	}
}

type fixtureOptions struct {
	withPrivateKey bool
}

type contextFixture struct {
	service    *Service
	store      *store.Store
	repoRoot   string
	snapshotID string
}

func setupContextPacketFixture(t *testing.T, opts fixtureOptions) contextFixture {
	t.Helper()
	requireGit(t)

	oldBase := artifacts.BaseDir
	artifacts.SetBaseDir(filepath.Join(t.TempDir(), "artifacts"))
	t.Cleanup(func() { artifacts.SetBaseDir(oldBase) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(t.TempDir(), "relay.sqlite"), logger)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})
	projectService := projects.NewService(st)
	sourceService := sources.NewService(st)

	repoRoot := setupGitRepo(t)
	mkdirAll(t, filepath.Join(repoRoot, "src"))
	writeFile(t, filepath.Join(repoRoot, "src", "app.txt"), "alpha\nbeta\nneedle\n")
	writeFile(t, filepath.Join(repoRoot, "src", "token.txt"), "Authorization: Bearer super-secret-token\n")
	if opts.withPrivateKey {
		writeFile(t, filepath.Join(repoRoot, "src", "private.txt"), "-----BEGIN PRIVATE KEY-----\nabc\n")
	}
	runGit(t, repoRoot, "git", "add", ".")
	runGit(t, repoRoot, "git", "commit", "-m", "fixture")

	project, issues, err := projectService.CreateProject(t.Context(), projects.ProjectInput{
		ProjectID: "relay",
		Name:      "Relay",
		Status:    projects.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected project issues: %+v", issues)
	}
	_, issues, err = projectService.UpsertProjectRepository(t.Context(), project.ProjectID, projects.ProjectRepositoryInput{
		RepoID:           "relay",
		Role:             projects.RepositoryRolePrimary,
		LocalPath:        repoRoot,
		DefaultBranch:    "main",
		AllowedRoots:     []string{"src"},
		MaxFileSizeBytes: projects.MinMaxFileSizeBytes,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("UpsertProjectRepository error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected repository issues: %+v", issues)
	}
	snapshot, err := sourceService.CreateSourceSnapshot(t.Context(), sources.SourceSnapshotInput{
		ProjectID:           project.ProjectID,
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot error: %v", err)
	}
	return contextFixture{
		service:    NewServiceWithSources(st, sourceService),
		store:      st,
		repoRoot:   repoRoot,
		snapshotID: snapshot.SourceSnapshotID,
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "git", "init", "-b", "main")
	runGit(t, dir, "git", "config", "user.name", "Relay Test")
	runGit(t, dir, "git", "config", "user.email", "relay@example.test")
	return dir
}

func runGit(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
}

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not available")
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func writeFile(t *testing.T, filePath string, content string) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", filePath, err)
	}
}

func assertReadableFile(t *testing.T, path string) {
	t.Helper()
	if data, err := os.ReadFile(path); err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	} else if len(data) == 0 {
		t.Fatalf("expected %s to contain data", path)
	}
}

func readPacketArtifact(t *testing.T, path string) ContextPacket {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	var packet ContextPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("Unmarshal packet: %v\n%s", err, string(data))
	}
	return packet
}

func mustPacketRow(t *testing.T, st *store.Store, contextPacketID string) *store.ContextPacket {
	t.Helper()
	row, err := st.GetContextPacketByID(contextPacketID)
	if err != nil {
		t.Fatalf("GetContextPacketByID: %v", err)
	}
	return row
}
