package views

import (
	"context"
	"strings"
	"testing"

	"relay/internal/repos"
	"relay/internal/store"
)

func TestRepoSettingsRendersSettingsShell(t *testing.T) {
	var buf strings.Builder
	err := RepoSettings(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettings: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="repo-settings-shell"`) {
		t.Errorf("expected repo-settings-shell id in rendered page")
	}
}

func TestRepoSettingsShellHasDataRelaySettings(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `data-relay-settings`) {
		t.Errorf("expected data-relay-settings attribute on shell")
	}
}

func TestRepoSettingsFormsTargetSettingsShell(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-target="#repo-settings-shell"`) {
		t.Errorf("expected hx-target on settings forms")
	}
	if !strings.Contains(html, `hx-select="#repo-settings-shell"`) {
		t.Errorf("expected hx-select on settings forms")
	}
	if !strings.Contains(html, `hx-swap="outerHTML settle:120ms"`) {
		t.Errorf("expected hx-swap with settle:120ms on settings forms")
	}
	if !strings.Contains(html, `hx-indicator="#repo-settings-loading"`) {
		t.Errorf("expected hx-indicator on settings forms")
	}
	if !strings.Contains(html, `data-relay-settings-action="true"`) {
		t.Errorf("expected data-relay-settings-action on settings forms")
	}
}

func TestRepoSettingsLoadingIndicatorRenders(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `id="repo-settings-loading"`) {
		t.Errorf("expected repo-settings-loading id")
	}
	if !strings.Contains(html, `relay-settings-loading`) {
		t.Errorf("expected relay-settings-loading class")
	}
	if !strings.Contains(html, `htmx-indicator`) {
		t.Errorf("expected htmx-indicator class")
	}
	if !strings.Contains(html, "Updating repository settings...") {
		t.Errorf("expected loading indicator text")
	}
}

func TestRepoSettingsDeleteUsesHXConfirmWithoutAlpine(t *testing.T) {
	roots := []store.RepoRoot{
		{ID: 1, Path: "D:/test", Enabled: 1},
	}
	var buf strings.Builder
	err := RepoSettingsShell(roots, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-confirm="Delete this scan root?"`) {
		t.Errorf("expected hx-confirm on delete form, got: %s", html)
	}
	if strings.Contains(html, `x-data`) {
		t.Errorf("delete form should not contain x-data Alpine attribute")
	}
	if strings.Contains(html, `@submit.prevent`) {
		t.Errorf("delete form should not contain Alpine submit.prevent")
	}
	if strings.Contains(html, `x-text`) {
		t.Errorf("delete form should not contain Alpine x-text")
	}
	if strings.Contains(html, "Confirm delete?") {
		t.Errorf("delete form should not have Confirm delete? text from Alpine")
	}
}

func TestRepoSettingsDeleteFormButtonTextIsStatic(t *testing.T) {
	roots := []store.RepoRoot{
		{ID: 1, Path: "D:/test", Enabled: 1},
	}
	var buf strings.Builder
	err := RepoSettingsShell(roots, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	// The button text should be static "Delete"
	if !strings.Contains(html, ">Delete</button>") {
		t.Errorf("expected static Delete button text, got: %s", html)
	}
}

func TestRepoSettingsScanSummaryRendersInsideShell(t *testing.T) {
	summary := &repos.ScanSummary{
		RootsScanned: 2,
		ReposFound:   5,
		ReposSaved:   3,
		Warnings:     []string{"permission denied"},
	}
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, summary).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Scan Summary") {
		t.Errorf("expected Scan Summary heading")
	}
	if !strings.Contains(html, "Roots scanned: 2") {
		t.Errorf("expected roots scanned count")
	}
	if !strings.Contains(html, "Repos found: 5") {
		t.Errorf("expected repos found count")
	}
	if !strings.Contains(html, "Repos saved: 3") {
		t.Errorf("expected repos saved count")
	}
	if !strings.Contains(html, "permission denied") {
		t.Errorf("expected scan warning text")
	}
}

func TestRepoSettingsScanSummaryOmitsWhenNil(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if strings.Contains(html, "Scan Summary") {
		t.Errorf("should not render Scan Summary when scanSummary is nil")
	}
}

func TestRepoSettingsAddRootFormHasHTMXPost(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-post="/settings/repos/roots"`) {
		t.Errorf("expected hx-post on add root form")
	}
}

func TestRepoSettingsScanFormHasHTMXPost(t *testing.T) {
	var buf strings.Builder
	err := RepoSettingsShell(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-post="/settings/repos/scan"`) {
		t.Errorf("expected hx-post on scan form")
	}
}

func TestRepoSettingsToggleFormHasHTMXPost(t *testing.T) {
	roots := []store.RepoRoot{
		{ID: 42, Path: "D:/test", Enabled: 1},
	}
	var buf strings.Builder
	err := RepoSettingsShell(roots, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-post="/settings/repos/roots/42/toggle"`) {
		t.Errorf("expected hx-post on toggle form for root 42")
	}
}

func TestRepoSettingsDeleteFormHasHTMXPost(t *testing.T) {
	roots := []store.RepoRoot{
		{ID: 42, Path: "D:/test", Enabled: 1},
	}
	var buf strings.Builder
	err := RepoSettingsShell(roots, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettingsShell: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-post="/settings/repos/roots/42/delete"`) {
		t.Errorf("expected hx-post on delete form for root 42")
	}
}

func TestRepoSettingsShellDoesNotHaveDuplicatedMaxW3xl(t *testing.T) {
	var buf strings.Builder
	err := RepoSettings(nil, nil, nil).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render RepoSettings: %v", err)
	}
	html := buf.String()
	// Should only have one max-w-3xl (on the shell itself, not on a wrapper)
	count := strings.Count(html, "max-w-3xl")
	if count != 1 {
		t.Errorf("expected exactly one max-w-3xl, got %d", count)
	}
}
