package views

import (
	"context"
	"strings"
	"testing"

	"relay/internal/store"
)

func TestDashboardDesktopTablePreserved(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run 1", Status: "draft", RepoName: "repo-a", RecommendedModel: "gpt-4", SelectedModel: "gpt-4", UpdatedAt: "2024-01-01"},
		{ID: 2, Title: "Run 2", Status: "ready", RepoName: "repo-b", RecommendedModel: "claude-3", SelectedModel: "", UpdatedAt: "2024-01-02"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	for _, h := range []string{"Title", "Repo", "Status", "Recommended", "Selected", "Updated"} {
		if !strings.Contains(html, h) {
			t.Errorf("expected table header %q in dashboard output", h)
		}
	}
	if !strings.Contains(html, `class="hidden md:block overflow-x-auto"`) {
		t.Errorf("expected desktop table wrapper class hidden md:block")
	}
}

func TestDashboardMobileCardContainerRendered(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run 1", Status: "draft", RepoName: "repo-a", UpdatedAt: "2024-01-01"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `data-dashboard-mobile-runs`) {
		t.Errorf("expected mobile runs container with data attribute")
	}
	if !strings.Contains(html, `class="md:hidden space-y-3"`) {
		t.Errorf("expected mobile wrapper with md:hidden class")
	}
}

func TestDashboardMobileCardContent(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Test Run Title", Status: "draft", RepoName: "my-repo", SelectedModel: "gpt-4", RecommendedModel: "claude-3", UpdatedAt: "2024-06-10"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "Test Run Title") {
		t.Errorf("expected run title in mobile card")
	}
	if !strings.Contains(html, "draft") {
		t.Errorf("expected status in mobile card")
	}
	if !strings.Contains(html, "my-repo") {
		t.Errorf("expected repo name in mobile card")
	}
	if !strings.Contains(html, "gpt-4") {
		t.Errorf("expected selected model in mobile card")
	}
	if !strings.Contains(html, "2024-06-10") {
		t.Errorf("expected updated timestamp in mobile card")
	}
}

func TestDashboardMobileCardLinksToRunDetail(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 42, Title: "Test Run", Status: "draft", RepoName: "repo", UpdatedAt: "2024-01-01"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `/runs/42`) {
		t.Errorf("expected mobile card link to /runs/42")
	}
}

func TestDashboardRunLinksNoHTMX(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run 1", Status: "draft", RepoName: "repo", UpdatedAt: "2024-01-01"},
		{ID: 2, Title: "Run 2", Status: "ready", RepoName: "repo", UpdatedAt: "2024-01-02"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if strings.Contains(html, `hx-get`) {
		t.Errorf("dashboard run links should not have hx-get")
	}
	if strings.Contains(html, `hx-post`) {
		t.Errorf("dashboard run links should not have hx-post")
	}
	if strings.Contains(html, `hx-target="#run-workbench-shell"`) {
		t.Errorf("dashboard run links should not have hx-target")
	}
}

func TestDashboardMobileCardHasRelayCardClass(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run", Status: "draft", RepoName: "repo", UpdatedAt: "2024-01-01"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "relay-dashboard-run-card") {
		t.Errorf("expected relay-dashboard-run-card class on mobile cards")
	}
	if !strings.Contains(html, `data-dashboard-run-card`) {
		t.Errorf("expected data-dashboard-run-card attribute on mobile cards")
	}
}

func TestDashboardDesktopTableHiddenOnMobile(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run", Status: "draft", RepoName: "repo", UpdatedAt: "2024-01-01"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `hidden md:block`) {
		t.Errorf("expected desktop table to have hidden md:block class")
	}
}

func TestDashboardMobileHiddenOnDesktop(t *testing.T) {
	runs := []store.DashboardRun{
		{ID: 1, Title: "Run", Status: "draft", RepoName: "repo", UpdatedAt: "2024-01-01"},
	}
	var buf strings.Builder
	err := Dashboard(runs).Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Dashboard: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `md:hidden`) {
		t.Errorf("expected mobile card list to have md:hidden class")
	}
}

func TestLayoutMobileSafeClasses(t *testing.T) {
	var buf strings.Builder
	err := Layout("Test").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `overflow-x-hidden`) {
		t.Errorf("expected body with overflow-x-hidden")
	}
	if !strings.Contains(html, `min-w-0`) {
		t.Errorf("expected main with min-w-0 class")
	}
	if !strings.Contains(html, `min-h-screen`) {
		t.Errorf("expected body with min-h-screen")
	}
}

func TestLayoutNavHasMobileSafeClasses(t *testing.T) {
	var buf strings.Builder
	err := Layout("Test").Render(context.Background(), &buf)
	if err != nil {
		t.Fatalf("render Layout: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `min-w-0`) {
		t.Errorf("expected nav or main to have min-w-0")
	}
}
