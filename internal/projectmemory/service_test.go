package projectmemory

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/store"
)

func TestServiceCreateRecordForExistingProject(t *testing.T) {
	svc := newTestService(t)
	record, issues, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
		ProjectID:    "relay",
		Kind:         KindDecision,
		Title:        "Planner handoffs are source grounded",
		Body:         "Important durable context: implementation handoffs must be grounded in source-controlled evidence.",
		Importance:   ImportanceHigh,
		Tags:         []string{"Planner", "Source Evidence", "planner"},
		DedupeReason: "Searched active memory and project context before saving.",
	})
	if err != nil {
		t.Fatalf("CreateProjectContextRecord error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	if record.ContextRecordID == "" || record.Status != StatusActive || record.RedactionStatus != "not_needed" {
		t.Fatalf("unexpected record metadata: %+v", record)
	}
	if strings.Join(record.Tags, ",") != "planner,source-evidence" {
		t.Fatalf("expected normalized/deduped tags, got %+v", record.Tags)
	}
}

func TestServiceRejectsUnknownProject(t *testing.T) {
	svc := newTestService(t)
	_, _, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
		ProjectID:    "missing",
		Kind:         KindDecision,
		Title:        "Missing",
		Body:         "This should not be saved.",
		DedupeReason: "Checked existing context.",
	})
	if err == nil {
		t.Fatal("expected unknown project error")
	}
	code, _, ok := ErrorCode(err)
	if !ok || code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND error, got %v", err)
	}
}

func TestServiceBlocksPrivateKeyBody(t *testing.T) {
	svc := newTestService(t)
	_, issues, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
		ProjectID:    "relay",
		Kind:         KindRisk,
		Title:        "Blocked",
		Body:         "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		DedupeReason: "Checked existing context.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasIssue(issues, "redaction_blocked") {
		t.Fatalf("expected redaction_blocked issue, got %+v", issues)
	}
	list, err := svc.ListProjectContextRecords(t.Context(), ListInput{ProjectID: "relay"})
	if err != nil {
		t.Fatalf("ListProjectContextRecords error: %v", err)
	}
	if len(list.Records) != 0 {
		t.Fatalf("blocked content should not persist, got %+v", list.Records)
	}
}

func TestServiceRedactsTokenLikeBody(t *testing.T) {
	svc := newTestService(t)
	record, issues, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
		ProjectID:    "relay",
		Kind:         KindConstraint,
		Title:        "Token redaction",
		Body:         "Use Authorization: Bearer super-secret-value only in ephemeral config.",
		DedupeReason: "Checked existing context.",
	})
	if err != nil {
		t.Fatalf("CreateProjectContextRecord error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	if record.RedactionStatus != "redacted" || !strings.Contains(record.Body, "[REDACTED_AUTH_HEADER]") {
		t.Fatalf("expected redacted body, got %+v", record)
	}
}

func TestServiceRejectsDuplicateActiveBody(t *testing.T) {
	svc := newTestService(t)
	input := CreateInput{
		ProjectID:    "relay",
		Kind:         KindProjectPrinciple,
		Title:        "Durable principle",
		Body:         "This exact durable body should only appear once while active.",
		DedupeReason: "Checked existing context.",
	}
	if _, issues, err := svc.CreateProjectContextRecord(t.Context(), input); err != nil || len(issues) != 0 {
		t.Fatalf("first create err=%v issues=%+v", err, issues)
	}
	input.Title = "Different title, same body"
	_, issues, err := svc.CreateProjectContextRecord(t.Context(), input)
	if err != nil {
		t.Fatalf("duplicate create unexpected error: %v", err)
	}
	if !hasIssue(issues, "duplicate_context") {
		t.Fatalf("expected duplicate_context issue, got %+v", issues)
	}
}

func TestServiceListSearchBoundsAndExcerpts(t *testing.T) {
	svc := newTestService(t)
	longBody := strings.Repeat("important context ", 40)
	for i := 0; i < 55; i++ {
		if _, issues, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
			ProjectID:    "relay",
			Kind:         KindDecision,
			Title:        "Decision",
			Body:         longBody + string(rune('a'+i)),
			DedupeReason: "Checked existing context.",
		}); err != nil || len(issues) != 0 {
			t.Fatalf("create %d err=%v issues=%+v", i, err, issues)
		}
	}
	list, err := svc.ListProjectContextRecords(t.Context(), ListInput{ProjectID: "relay", Limit: 500})
	if err != nil {
		t.Fatalf("ListProjectContextRecords error: %v", err)
	}
	if len(list.Records) != MaxLimit || !list.Truncated || list.Records[0].Body != "" || len([]rune(list.Records[0].BodyExcerpt)) > MaxExcerptRunes {
		t.Fatalf("expected bounded excerpt list, got len=%d truncated=%v first=%+v", len(list.Records), list.Truncated, list.Records[0])
	}
	search, err := svc.SearchProjectContextMemory(t.Context(), SearchInput{ProjectID: "relay", Query: "important", Limit: 3})
	if err != nil {
		t.Fatalf("SearchProjectContextMemory error: %v", err)
	}
	if len(search.Records) != 3 || !search.Truncated || search.Records[0].Body != "" {
		t.Fatalf("expected bounded search excerpts, got %+v", search)
	}
}

func TestServiceSupersedeCreatesLinkedReplacement(t *testing.T) {
	svc := newTestService(t)
	old, issues, err := svc.CreateProjectContextRecord(t.Context(), CreateInput{
		ProjectID:    "relay",
		Kind:         KindArchitectureRationale,
		Title:        "Original rationale",
		Body:         "The original durable rationale.",
		Importance:   ImportanceCritical,
		DedupeReason: "Checked existing context.",
	})
	if err != nil || len(issues) != 0 {
		t.Fatalf("create err=%v issues=%+v", err, issues)
	}
	result, issues, err := svc.SupersedeProjectContextRecord(t.Context(), SupersedeInput{
		ProjectID:    "relay",
		RecordID:     old.ContextRecordID,
		Kind:         KindArchitectureRationale,
		Title:        "Updated rationale",
		Body:         "The updated durable rationale.",
		Importance:   ImportanceCritical,
		DedupeReason: "Searched active memory and found the original changed.",
	})
	if err != nil || len(issues) != 0 {
		t.Fatalf("supersede err=%v issues=%+v", err, issues)
	}
	if result.OldRecord.Status != StatusSuperseded || result.NewRecord.Status != StatusActive {
		t.Fatalf("unexpected statuses: %+v", result)
	}
	if result.OldRecord.SupersededByRecordID != result.NewRecord.ContextRecordID || result.NewRecord.SupersedesRecordID != old.ContextRecordID {
		t.Fatalf("expected supersession links, got %+v", result)
	}
	list, err := svc.ListProjectContextRecords(t.Context(), ListInput{ProjectID: "relay"})
	if err != nil {
		t.Fatalf("ListProjectContextRecords error: %v", err)
	}
	if len(list.Records) != 1 || list.Records[0].ContextRecordID != result.NewRecord.ContextRecordID {
		t.Fatalf("active list should only include replacement, got %+v", list.Records)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "relay.sqlite"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("store.Close: %v", err)
		}
	})
	if _, err := st.CreateProject("relay", "Relay", "", "active", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return NewService(st)
}

func hasIssue(issues []ValidationIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
