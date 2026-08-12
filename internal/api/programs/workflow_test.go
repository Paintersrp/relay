package programs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	app "relay/internal/app/programs"

	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	prepared     []app.PreparedMember
	dispatch     app.Dispatch
	handoff      app.Handoff
	result       app.DispatchResultInput
	assignment   app.IntegrationAssignmentResult
	merge        app.IntegrationMergeResult
	verification app.IntegrationVerification
	failure      app.IntegrationFailure
	admitted     app.IntegrationMergeResultInput
	verified     app.IntegrationVerification
}

func (f *fakeService) Prepare(context.Context, string, string, int64) (app.PreparedMember, error) {
	return app.PreparedMember{}, nil
}
func (f *fakeService) Cancel(context.Context, string, string, int64) error { return nil }
func (f *fakeService) CreateDispatch(context.Context, string, int64, []string) (app.Dispatch, error) {
	return f.dispatch, nil
}
func (f *fakeService) RecordDispatchResult(_ context.Context, _, _ string, _ int64, in app.DispatchResultInput) error {
	f.result = in
	return nil
}
func (f *fakeService) Read(context.Context, string, string) (app.Dispatch, error) {
	return f.dispatch, nil
}
func (f *fakeService) ReadHandoff(context.Context, string, string) (app.Handoff, error) {
	return f.handoff, nil
}
func (f *fakeService) ListPrepared(context.Context, string) ([]app.PreparedMember, error) {
	return f.prepared, nil
}
func (f *fakeService) GenerateIntegrationAssignment(context.Context, string, string, int64, []string) (app.IntegrationAssignmentResult, error) {
	return f.assignment, nil
}
func (f *fakeService) ReadIntegrationAssignment(context.Context, string, string, string) (app.IntegrationAssignmentResult, error) {
	return f.assignment, nil
}
func (f *fakeService) AdmitIntegrationMergeResult(_ context.Context, _, _, _ string, _ int64, in app.IntegrationMergeResultInput) (app.IntegrationMergeResult, error) {
	f.admitted = in
	return f.merge, nil
}
func (f *fakeService) ReadIntegrationMergeResult(context.Context, string, string, string) (app.IntegrationMergeResult, error) {
	return f.merge, nil
}
func (f *fakeService) VerifyIntegration(context.Context, string, string, string, int64) (app.IntegrationVerification, error) {
	return f.verified, nil
}
func (f *fakeService) ReadIntegrationVerification(context.Context, string, string, string) (app.IntegrationVerification, error) {
	return f.verification, nil
}
func (f *fakeService) ReadIntegrationFailure(context.Context, string, string, string) (app.IntegrationFailure, error) {
	return f.failure, nil
}

func programRouter(service Service) http.Handler {
	r := chi.NewRouter()
	MountRoutes(r, NewHandler(service))
	return r
}

func TestProgramRoutesStrictDecodeAndExactReadListResultProjection(t *testing.T) {
	service := &fakeService{
		prepared: []app.PreparedMember{{ID: "program-member-1", State: "prepared", Branch: "main", BaseCommit: strings.Repeat("a", 40)}},
		dispatch: app.Dispatch{ID: "dispatch-1", WorkspaceID: "workspace-1", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), Status: "reported", LaterIntegrationRisks: "manual merge", Members: []app.PreparedMember{{ID: "program-member-1", Outcome: "done", ResultBranch: "feature/p1", BranchHeadSHA: strings.Repeat("b", 40)}, {ID: "program-member-2", Outcome: "blocked", Blocker: "external dependency"}}},
	}
	router := programRouter(service)
	for _, body := range []string{`{"expectedVersion":1,"memberIds":["program-member-1","program-member-2"],"unknown":true}`, `{"expectedVersion":1,"memberIds":["program-member-1"]}{}`} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("strict decode status=%d body=%s", response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-members", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ID":"program-member-1"`) || !strings.Contains(response.Body.String(), `"State":"prepared"`) {
		t.Fatalf("list response=%d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"LaterIntegrationRisks":"manual merge"`) || !strings.Contains(response.Body.String(), `"ResultBranch":"feature/p1"`) || !strings.Contains(response.Body.String(), `"BranchHeadSHA":"`+strings.Repeat("b", 40)+`"`) || !strings.Contains(response.Body.String(), `"Blocker":"external dependency"`) {
		t.Fatalf("read response=%d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	body := `{"expectedVersion":1,"members":[{"memberId":"program-member-1","outcome":"done","branch":"feature/p1","branchHeadSha":"` + strings.Repeat("b", 40) + `","blocker":""},{"memberId":"program-member-2","outcome":"blocked","branch":"","branchHeadSha":"","blocker":"external dependency"}],"laterIntegrationRisks":"manual merge"}`
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/result", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("result status=%d body=%s", response.Code, response.Body.String())
	}
	if len(service.result.Members) != 2 || service.result.Members[0].BranchHeadSHA != strings.Repeat("b", 40) || service.result.Members[1].Blocker != "external dependency" || service.result.LaterIntegrationRisks != "manual merge" {
		t.Fatalf("result input=%#v", service.result)
	}
	var reply map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || !reply["recorded"] {
		t.Fatalf("result reply=%s err=%v", response.Body.String(), err)
	}
}

func TestProgramHandoffReturnsCanonicalTicketIdentityAndEmbeddedAssignment(t *testing.T) {
	assignment := []byte(`{"schema_version":"1.0","run":{"run_id":"run-1"},"ticket":{"ticket_id":"T-ONE","revision_number":1},"repository":{"target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
	service := &fakeService{handoff: app.Handoff{
		DispatchID:  "dispatch-1",
		WorkspaceID: "workspace-1",
		RepoTarget:  "relay",
		Branch:      "main",
		BaseCommit:  strings.Repeat("a", 40),
		Members: []app.HandoffMember{
			{Sequence: 1, MemberID: "program-member-1", TicketID: "T-ONE", TicketRevision: 1, PackageID: "package-1", RunID: "run-1", AssignmentArtifactID: "artifact-1", AssignmentSHA256: strings.Repeat("2", 64), Assignment: json.RawMessage(assignment), RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40)},
			{Sequence: 2, MemberID: "program-member-2", TicketID: "T-TWO", TicketRevision: 2, PackageID: "package-2", RunID: "run-2", AssignmentArtifactID: "artifact-2", AssignmentSHA256: strings.Repeat("2", 64), Assignment: json.RawMessage([]byte(`{"schema_version":"1.0","run":{"run_id":"run-2"}}`)), RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40)},
		},
	}}
	router := programRouter(service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-1/program-dispatches/dispatch-1/handoff", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("handoff status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"DispatchID":"dispatch-1"`, `"WorkspaceID":"workspace-1"`, `"TicketID":"T-ONE"`, `"TicketRevision":1`, `"TicketID":"T-TWO"`, `"TicketRevision":2`, `"Assignment":{"schema_version":"1.0","run":{"run_id":"run-1"}`, `"BaseCommit":"` + strings.Repeat("a", 40) + `"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("handoff body %s missing %q", body, want)
		}
	}
	if strings.Contains(body, "TicketRevisionRowID") {
		t.Fatalf("handoff body exposes an internal ticket revision row ID: %s", body)
	}
	var decoded struct {
		DispatchID string
		Members    []struct {
			Sequence   int
			Assignment json.RawMessage
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DispatchID != "dispatch-1" || len(decoded.Members) != 2 || decoded.Members[0].Sequence != 1 || decoded.Members[1].Sequence != 2 {
		t.Fatalf("handoff order = %#v", decoded)
	}
	if string(decoded.Members[0].Assignment) != string(assignment) {
		t.Fatalf("embedded assignment = %s, want %s", decoded.Members[0].Assignment, assignment)
	}
}
