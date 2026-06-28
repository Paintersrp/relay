package contextpackets

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/projects"
	"relay/internal/artifacts"
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

func TestCreateContextPacketRequiredSeedFilePrecedesInventory(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "required-before-inventory",
		SourceSnapshotID: fixture.snapshotID,
		IncludeInventory: true,
		MaxSources:       1,
		SeedFiles: []ContextSeedFile{{
			RepoID:   "relay",
			Path:     "src/app.txt",
			Required: true,
			Reason:   "required source",
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusCreated || result.Truncated || result.Summary.MaxSources != 1 || !result.Summary.InventoryIncluded || !result.Summary.OptionalInventoryTruncated || result.LimitHit != LimitHitNone {
		t.Fatalf("expected created packet with optional inventory warning, got status=%q truncated=%t summary=%+v limit=%q", result.Status, result.Truncated, result.Summary, result.LimitHit)
	}
	if len(result.Coverage) < 2 || result.Coverage[0].SeedType != "file" || result.Coverage[1].SeedType != "inventory" {
		t.Fatalf("expected required file coverage before inventory, got %+v", result.Coverage)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if len(packet.Sources) != 1 || packet.Sources[0].SourceType != SourceTypeFileRead {
		t.Fatalf("expected required file to consume the single source slot, got %+v", packet.Sources)
	}
}

func TestCreateContextPacketRequiredSeedsRemainUsableWhenInventoryTruncates(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})
	requiredFiles := []ContextSeedFile{
		{RepoID: "relay", Path: "src/app.txt", Required: true, Reason: "required source 1"},
		{RepoID: "relay", Path: "src/file-1.txt", Required: true, Reason: "required source 2"},
		{RepoID: "relay", Path: "src/file-2.txt", Required: true, Reason: "required source 3"},
		{RepoID: "relay", Path: "src/file-3.txt", Required: true, Reason: "required source 4"},
		{RepoID: "relay", Path: "src/file-4.txt", Required: true, Reason: "required source 5"},
	}
	for i := 1; i <= 16; i++ {
		writeFile(t, filepath.Join(fixture.repoRoot, "src", fmt.Sprintf("file-%d.txt", i)), fmt.Sprintf("needle-%d\n", i))
	}
	runGit(t, fixture.repoRoot, "git", "add", ".")
	runGit(t, fixture.repoRoot, "git", "commit", "-m", "add inventory pressure")
	snapshot, err := sources.NewService(fixture.store).CreateSourceSnapshot(t.Context(), sources.SourceSnapshotInput{
		ProjectID:           "relay",
		RepoIDs:             []string{"relay"},
		IncludeFileMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateSourceSnapshot: %v", err)
	}

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "pass002-inventory-warning",
		SourceSnapshotID: snapshot.SourceSnapshotID,
		IncludeInventory: true,
		MaxSources:       12,
		MaxTotalBytes:    180000,
		SeedFiles:        requiredFiles,
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if result.Status != ContextPacketStatusCreated || result.Truncated || !result.Summary.OptionalInventoryTruncated {
		t.Fatalf("expected required-complete packet with inventory warning, got %+v", result)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if len(packet.Sources) < len(requiredFiles) {
		t.Fatalf("expected required file sources, got %+v", packet.Sources)
	}
	for i, src := range packet.Sources[:len(requiredFiles)] {
		if src.SourceType != SourceTypeFileRead {
			t.Fatalf("source %d should be required file before inventory, got %+v", i, src)
		}
	}
}

func TestCreateContextPacketRequiredSearchPrecedesInventory(t *testing.T) {
	requireRG(t)
	fixture := setupContextPacketFixture(t, fixtureOptions{})

	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		TaskSlug:         "required-search-before-inventory",
		SourceSnapshotID: fixture.snapshotID,
		IncludeInventory: true,
		MaxSources:       1,
		SeedSearches: []ContextSeedSearch{{
			RepoIDs:    []string{"relay"},
			Pattern:    "needle",
			Required:   true,
			Reason:     "required search",
			MaxResults: 5,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	if len(result.Coverage) < 2 || result.Coverage[0].SeedType != "search" || result.Coverage[1].SeedType != "inventory" {
		t.Fatalf("expected required search coverage before inventory, got %+v", result.Coverage)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if len(packet.Sources) != 1 || packet.Sources[0].SourceType != SourceTypeSearchMatch {
		t.Fatalf("expected required search to consume the single source slot, got %+v", packet.Sources)
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
	if required.Status != ContextPacketStatusPartial {
		t.Fatalf("expected partial required search with no matches, got %+v", required)
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

func TestCreateContextPacketRequiresAtLeastOneSourceRequest(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})
	_, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		SourceSnapshotID: fixture.snapshotID,
	})
	if err == nil || !strings.Contains(err.Error(), "at least one seed file, seed search, or inventory request is required") {
		t.Fatalf("expected error for empty request, got: %v", err)
	}
}

func TestCreateContextPacketArtifactsIncludeSchemaVersion(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})
	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:    "relay",
			Path:      "src/app.txt",
			LineStart: 1,
			LineEnd:   2,
			Required:  true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if packet.SchemaVersion != "1.0.0" {
		t.Fatalf("expected schema version 1.0.0 in packet JSON, got: %s", packet.SchemaVersion)
	}
	data, err := os.ReadFile(result.CoverageReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile coverage report error: %v", err)
	}
	var report ContextCoverageReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Unmarshal coverage report: %v", err)
	}
	if report.SchemaVersion != "1.0.0" {
		t.Fatalf("expected schema version 1.0.0 in coverage report, got: %s", report.SchemaVersion)
	}
}

func TestCreateContextPacketJSONIncludesCoverage(t *testing.T) {
	fixture := setupContextPacketFixture(t, fixtureOptions{})
	result, err := fixture.service.CreateContextPacket(t.Context(), ContextPacketInput{
		ProjectID:        "relay",
		SourceSnapshotID: fixture.snapshotID,
		SeedFiles: []ContextSeedFile{{
			RepoID:    "relay",
			Path:      "src/app.txt",
			LineStart: 1,
			LineEnd:   2,
			Required:  true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContextPacket error: %v", err)
	}
	packet := readPacketArtifact(t, result.PacketJSONPath)
	if len(packet.Coverage) != 1 || packet.Coverage[0].SeedID != "file:1" {
		t.Fatalf("expected embedded coverage with 1 entry, got: %+v", packet.Coverage)
	}
}

func TestFinalRedactionScanBlocksAndRedacts(t *testing.T) {
	srcs := []ContextSource{
		{
			SourceID:   "src_token",
			SourceType: SourceTypeFileRead,
			RepoID:     "relay",
			Path:       "src/token.txt",
			Content:    "Authorization: bearer abcdef123\n",
		},
		{
			SourceID:   "src_private",
			SourceType: SourceTypeFileRead,
			RepoID:     "relay",
			Path:       "src/private.txt",
			Content:    "-----BEGIN RSA PRIVATE KEY-----\nsecret_key_material\n",
		},
	}
	blockers := []sources.SourceBlocker{}
	coverage := []ContextCoverageEntry{
		{
			SeedID:    "file:1",
			SeedType:  "file",
			Required:  false,
			Status:    CoverageStatusCovered,
			SourceIDs: []string{"src_token"},
		},
		{
			SeedID:    "file:2",
			SeedType:  "file",
			Required:  true,
			Status:    CoverageStatusCovered,
			SourceIDs: []string{"src_private"},
		},
	}

	finalSources, finalBlockers, finalCoverage := runFinalRedactionScan(srcs, blockers, coverage)

	// src_private should be blocked and omitted, src_token should be redacted and included
	if len(finalSources) != 1 || finalSources[0].SourceID != "src_token" {
		t.Fatalf("expected final sources to contain only src_token, got: %+v", finalSources)
	}
	if !strings.Contains(finalSources[0].Content, "[REDACTED_AUTH_HEADER]") {
		t.Fatalf("expected token redaction, got: %s", finalSources[0].Content)
	}
	if finalSources[0].RedactionStatus != sources.RedactionStatusRedacted {
		t.Fatalf("expected redaction status redacted, got: %s", finalSources[0].RedactionStatus)
	}

	// 1 blocker added
	if len(finalBlockers) != 1 || finalBlockers[0].Code != sources.SourceBlockerRedactionBlocked {
		t.Fatalf("expected 1 blocked blocker, got: %+v", finalBlockers)
	}

	// Coverage: file:1 remains covered (redaction is allowed), file:2 becomes blocked (required seed file containing blocked key)
	if finalCoverage[0].Status != CoverageStatusCovered {
		t.Fatalf("expected optional redacted file coverage status to be covered, got: %s", finalCoverage[0].Status)
	}
	if finalCoverage[1].Status != CoverageStatusBlocked {
		t.Fatalf("expected required blocked file coverage status to be blocked, got: %s", finalCoverage[1].Status)
	}
}
