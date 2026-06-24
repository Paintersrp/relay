package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"relay/internal/app/projects"
)

func TestHandleProjectContextMemoryHappyPath(t *testing.T) {
	fixture := setupMemoryFixture(t)

	createResult := callTool(t, fixture.server, ToolCreateProjectContextRecord.Name, json.RawMessage(`{
		"project_id":"relay",
		"kind":"decision",
		"title":"Durable decision",
		"body":"This is important durable project context for future planning.",
		"importance":"high",
		"tags":["Planning","planning"],
		"dedupe_reason":"Searched active project context memory before saving."
	}`))
	if createResult.IsError {
		t.Fatalf("unexpected create error: %s", createResult.Content[0].Text)
	}
	createSuccess := decodeBrokerSuccess(t, createResult)
	var created struct {
		ContextRecordID string   `json:"context_record_id"`
		Body            string   `json:"body"`
		BodyExcerpt     string   `json:"body_excerpt"`
		Tags            []string `json:"tags"`
	}
	if err := json.Unmarshal(createSuccess.Result, &created); err != nil {
		t.Fatalf("unmarshal created record: %v", err)
	}
	if created.ContextRecordID == "" || created.Body == "" || created.BodyExcerpt != "" || len(created.Tags) != 1 || created.Tags[0] != "planning" {
		t.Fatalf("unexpected created payload: %+v", created)
	}

	searchResult := callTool(t, fixture.server, ToolSearchProjectContextMemory.Name, json.RawMessage(`{
		"project_id":"relay",
		"query":"future",
		"limit":5
	}`))
	if searchResult.IsError {
		t.Fatalf("unexpected search error: %s", searchResult.Content[0].Text)
	}
	searchSuccess := decodeBrokerSuccess(t, searchResult)
	var searchPayload struct {
		Records []struct {
			ContextRecordID string `json:"context_record_id"`
			Body            string `json:"body"`
			BodyExcerpt     string `json:"body_excerpt"`
		} `json:"records"`
	}
	if err := json.Unmarshal(searchSuccess.Result, &searchPayload); err != nil {
		t.Fatalf("unmarshal search payload: %v", err)
	}
	if len(searchPayload.Records) != 1 || searchPayload.Records[0].ContextRecordID != created.ContextRecordID || searchPayload.Records[0].Body != "" || searchPayload.Records[0].BodyExcerpt == "" {
		t.Fatalf("unexpected search payload: %+v", searchPayload)
	}

	getResult := callTool(t, fixture.server, ToolGetProjectContextRecord.Name, json.RawMessage(`{
		"project_id":"relay",
		"record_id":"`+created.ContextRecordID+`"
	}`))
	if getResult.IsError {
		t.Fatalf("unexpected get error: %s", getResult.Content[0].Text)
	}
	getSuccess := decodeBrokerSuccess(t, getResult)
	var got struct {
		ContextRecordID string `json:"context_record_id"`
		Body            string `json:"body"`
	}
	if err := json.Unmarshal(getSuccess.Result, &got); err != nil {
		t.Fatalf("unmarshal get payload: %v", err)
	}
	if got.ContextRecordID != created.ContextRecordID || got.Body == "" {
		t.Fatalf("expected full body get, got %+v", got)
	}

	supersedeResult := callTool(t, fixture.server, ToolSupersedeProjectContextRecord.Name, json.RawMessage(`{
		"project_id":"relay",
		"record_id":"`+created.ContextRecordID+`",
		"kind":"decision",
		"title":"Updated durable decision",
		"body":"This updated durable context replaces the original planning decision.",
		"importance":"high",
		"dedupe_reason":"Searched active memory and found the original materially changed."
	}`))
	if supersedeResult.IsError {
		t.Fatalf("unexpected supersede error: %s", supersedeResult.Content[0].Text)
	}
	supersedeSuccess := decodeBrokerSuccess(t, supersedeResult)
	var superseded struct {
		OldRecord struct {
			ContextRecordID      string `json:"context_record_id"`
			Status               string `json:"status"`
			SupersededByRecordID string `json:"superseded_by_record_id"`
		} `json:"old_record"`
		NewRecord struct {
			ContextRecordID    string `json:"context_record_id"`
			Status             string `json:"status"`
			SupersedesRecordID string `json:"supersedes_record_id"`
		} `json:"new_record"`
	}
	if err := json.Unmarshal(supersedeSuccess.Result, &superseded); err != nil {
		t.Fatalf("unmarshal supersede payload: %v", err)
	}
	if superseded.OldRecord.Status != "superseded" || superseded.NewRecord.Status != "active" || superseded.OldRecord.SupersededByRecordID != superseded.NewRecord.ContextRecordID || superseded.NewRecord.SupersedesRecordID != created.ContextRecordID {
		t.Fatalf("unexpected supersede payload: %+v", superseded)
	}
}

func TestHandleProjectContextMemoryCreateRedactionBlocked(t *testing.T) {
	fixture := setupMemoryFixture(t)
	result := callTool(t, fixture.server, ToolCreateProjectContextRecord.Name, json.RawMessage(`{
		"project_id":"relay",
		"kind":"risk",
		"title":"Blocked",
		"body":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"dedupe_reason":"Checked existing context first."
	}`))
	if !result.IsError {
		t.Fatal("expected redaction-blocked create to return validation error")
	}
	errEnvelope := decodeBrokerError(t, result)
	if errEnvelope.Error.Code != "VALIDATION_ERROR" || !strings.Contains(errEnvelope.Error.Message, "redaction_blocked") {
		t.Fatalf("expected redaction_blocked validation error, got %+v", errEnvelope)
	}
}

func setupMemoryFixture(t *testing.T) brokerFixture {
	t.Helper()
	deps := setupTestDeps(t)
	deps.ToolProfile = ToolProfileLocalOperator
	deps.ContextBrokerEnabled = true
	srv := NewServer(discardLogger(), deps)
	projectService := projects.NewService(deps.Store)
	project, err := projectService.GetProjectByProjectID(t.Context(), "relay")
	if err != nil {
		var issues []projects.ProjectValidationIssue
		project, issues, err = projectService.CreateProject(t.Context(), projects.ProjectInput{
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
	}
	return brokerFixture{deps: deps, server: srv, projectID: project.ProjectID}
}
