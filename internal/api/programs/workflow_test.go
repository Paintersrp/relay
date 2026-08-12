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
	prepared []app.PreparedMember
	dispatch app.Dispatch
	result   app.DispatchResultInput
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
func (f *fakeService) ListPrepared(context.Context, string) ([]app.PreparedMember, error) {
	return f.prepared, nil
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
