package sources

import (
	"os/exec"
	"strings"
	"testing"

	"relay/internal/store"
)

func TestBuildRGArgsUsesFixedStringsAndPatternExpression(t *testing.T) {
	args, err := buildRGArgs(store.ProjectRepository{
		AllowedRootsJson: `["src"]`,
		IgnoredGlobsJson: `["ignored/**"]`,
	}, "--glob *", false, 2)
	if err != nil {
		t.Fatalf("buildRGArgs error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !containsArg(args, "--fixed-strings") {
		t.Fatalf("expected fixed strings arg, got %v", args)
	}
	if !containsArg(args, "-e") {
		t.Fatalf("expected -e arg, got %v", args)
	}
	if !containsArg(args, "--glob") || !containsArg(args, "!ignored/**") {
		t.Fatalf("expected ignored glob args, got %v", args)
	}
	if !strings.Contains(joined, "--context=2") {
		t.Fatalf("expected context arg, got %v", args)
	}
	if args[len(args)-2] != "--glob *" || args[len(args)-1] != "src" {
		t.Fatalf("expected pattern to remain operand before root, got %v", args)
	}
}

func TestParseRGMatchesFiltersAndRedacts(t *testing.T) {
	resolved := &sourceSnapshotContext{
		project:  &store.Project{ProjectID: "relay"},
		snapshot: &store.SourceSnapshot{SourceSnapshotID: "srcsnap_1"},
	}
	included := map[string]store.SourceSnapshotFile{
		"src/token.txt": {Path: "src/token.txt", ContentHash: "hash"},
	}
	data := []byte(`{"type":"match","data":{"path":{"text":"src/token.txt"},"lines":{"text":"token: super-secret-token\n"},"line_number":7}}` + "\n" +
		`{"type":"match","data":{"path":{"text":"ignored/secret.txt"},"lines":{"text":"secret\n"},"line_number":1}}` + "\n")

	matches, blockers, err := parseRGMatches(data, resolved, "relay", included, 10, "now")
	if err != nil {
		t.Fatalf("parseRGMatches error: %v", err)
	}
	if len(blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", blockers)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one included match, got %+v", matches)
	}
	if matches[0].LineStart != 7 || matches[0].Path != "src/token.txt" || matches[0].ContentHash != "hash" {
		t.Fatalf("unexpected match provenance: %+v", matches[0])
	}
	if strings.Contains(matches[0].Snippet, "super-secret-token") || !strings.Contains(matches[0].Snippet, "[REDACTED_TOKEN]") {
		t.Fatalf("expected redacted snippet, got %q", matches[0].Snippet)
	}
}

func TestParseRGMatchesBlocksPrivateKeyStart(t *testing.T) {
	resolved := &sourceSnapshotContext{
		project:  &store.Project{ProjectID: "relay"},
		snapshot: &store.SourceSnapshot{SourceSnapshotID: "srcsnap_1"},
	}
	included := map[string]store.SourceSnapshotFile{
		"src/private.txt": {Path: "src/private.txt", ContentHash: "hash"},
	}
	data := []byte(`{"type":"match","data":{"path":{"text":"src/private.txt"},"lines":{"text":"-----BEGIN PRIVATE KEY-----\n"},"line_number":1}}` + "\n")

	matches, blockers, err := parseRGMatches(data, resolved, "relay", included, 10, "now")
	if err != nil {
		t.Fatalf("parseRGMatches error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no private-key matches, got %+v", matches)
	}
	if len(blockers) != 1 || blockers[0].Code != SourceBlockerRedactionBlocked {
		t.Fatalf("expected redaction blocker, got %+v", blockers)
	}
}

func TestSearchProjectFilesRipgrepUnavailableReturnsBlocker(t *testing.T) {
	if _, err := exec.LookPath("rg"); err == nil {
		t.Skip("rg is available; unavailable path is environment-dependent")
	}
	service, _ := setupSourceSnapshotFixture(t, sourceFixtureOptions{})
	result, err := service.SearchProjectFiles(t.Context(), SourceSearchInput{ProjectID: "relay", Pattern: "line"})
	if err != nil {
		t.Fatalf("SearchProjectFiles error: %v", err)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != SourceBlockerRipgrepMissing {
		t.Fatalf("expected ripgrep unavailable blocker, got %+v", result.Blockers)
	}
}

func TestSearchProjectFilesReturnsProvenanceAndRedaction(t *testing.T) {
	requireGit(t)
	requireRG(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{})

	result, err := service.SearchProjectFiles(t.Context(), SourceSearchInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		Pattern:          "super-secret-token",
	})
	if err != nil {
		t.Fatalf("SearchProjectFiles error: %v", err)
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("expected one match, got %+v", result.Matches)
	}
	match := result.Matches[0]
	if match.ProjectID != "relay" || match.RepoID != "relay" || match.SourceSnapshotID != snapshotID || match.Path != "src/token.txt" || match.LineStart == 0 || match.ContentHash == "" {
		t.Fatalf("unexpected provenance: %+v", match)
	}
	if strings.Contains(match.Snippet, "super-secret-token") || !strings.Contains(match.Snippet, "[REDACTED_TOKEN]") {
		t.Fatalf("expected redacted snippet, got %q", match.Snippet)
	}
}

func TestSearchProjectFilesBlocksPrivateKeyStart(t *testing.T) {
	requireGit(t)
	requireRG(t)
	service, snapshotID := setupSourceSnapshotFixture(t, sourceFixtureOptions{withPrivateKey: true})

	result, err := service.SearchProjectFiles(t.Context(), SourceSearchInput{
		ProjectID:        "relay",
		SourceSnapshotID: snapshotID,
		Pattern:          "BEGIN PRIVATE KEY",
	})
	if err != nil {
		t.Fatalf("SearchProjectFiles error: %v", err)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("expected private-key match to be blocked, got %+v", result.Matches)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != SourceBlockerRedactionBlocked {
		t.Fatalf("expected redaction blocker, got %+v", result.Blockers)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func requireRG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
}
