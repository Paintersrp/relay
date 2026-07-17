package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	workflowapp "relay/internal/app/workflow"
	workflowrepos "relay/internal/repos/workflow"

	"github.com/go-chi/chi/v5"
)

func TestRepositoryAPIListAndGetExposeClosedConfigurationState(t *testing.T) {
	service := &branchAPIFakeService{
		repositories: []workflowapp.RepositoryTarget{
			{
				RepoTarget:           "configured",
				LocalPath:            "/repos/configured",
				ConfiguredBranchRef:  sql.NullString{String: "refs/heads/main", Valid: true},
				ConfigurationVersion: 3,
				CreatedAt:            "created",
				UpdatedAt:            "updated",
			},
			{
				RepoTarget:           "unconfigured",
				LocalPath:            "/repos/unconfigured",
				ConfigurationVersion: 1,
				CreatedAt:            "created",
				UpdatedAt:            "updated",
			},
		},
	}
	router := branchAPIRouter(service)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repositories", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", response.Code, response.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	items := list["items"].([]any)
	configured := items[0].(map[string]any)
	unconfigured := items[1].(map[string]any)
	assertJSONKeys(t, configured, []string{
		"configuredBranchRef", "configurationVersion", "createdAt", "localPath", "repoTarget", "updatedAt",
	})
	if configured["configuredBranchRef"] != "refs/heads/main" ||
		configured["configurationVersion"] != float64(3) {
		t.Fatalf("configured DTO = %#v", configured)
	}
	if value, ok := unconfigured["configuredBranchRef"]; !ok || value != nil {
		t.Fatalf("unconfigured branch JSON = %#v", unconfigured)
	}
	if unconfigured["configurationVersion"] != float64(1) {
		t.Fatalf("unconfigured DTO = %#v", unconfigured)
	}

	service.get = service.repositories[0]
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/repositories/configured", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", response.Code, response.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, got, []string{
		"configuredBranchRef", "configurationVersion", "createdAt", "localPath", "repoTarget", "updatedAt",
	})
}

func TestRepositoryAPIInspectAndConfirmTransferCompleteBranchAuthority(t *testing.T) {
	ref := sql.NullString{String: "refs/heads/main", Valid: true}
	service := &branchAPIFakeService{
		inspection: workflowapp.RepositoryInspection{
			State:                        workflowrepos.InspectionStateReady,
			SelectedPath:                 "/selected",
			ResolvedLocalPath:            "/repo",
			Remotes:                      []workflowapp.RepositoryRemoteCandidate{},
			RepoTarget:                   "relay",
			RepoTargetSource:             workflowrepos.RepoTargetSourceOperatorOverride,
			RegistrationDisposition:      workflowrepos.RegistrationDispositionReuse,
			CurrentConfiguredBranchRef:   ref,
			ExpectedConfigurationVersion: 2,
			ProposedConfiguredBranchRef:  ref,
			ProposedConfigurationVersion: 2,
			ProposedBranchCommitOID:      strings.Repeat("a", 40),
			ProposedBranchTreeOID:        strings.Repeat("b", 40),
			ConfigurationDisposition:     workflowrepos.ConfigurationDispositionPreserve,
			ConfirmationHash:             strings.Repeat("c", 64),
			Notices:                      []string{},
		},
		confirmation: workflowapp.RepositoryRegistrationResult{
			Outcome:                  workflowrepos.RegistrationOutcomeReused,
			ConfigurationDisposition: workflowrepos.ConfigurationDispositionPreserve,
			Repository: workflowapp.RepositoryTarget{
				RepoTarget:           "relay",
				LocalPath:            "/repo",
				ConfiguredBranchRef:  ref,
				ConfigurationVersion: 2,
				CreatedAt:            "created",
				UpdatedAt:            "updated",
			},
		},
	}
	router := branchAPIRouter(service)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/repositories/inspect",
		strings.NewReader(`{"localPath":"/repo","repoTargetOverride":"relay","proposedConfiguredBranchRef":"refs/heads/main"}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("inspect status = %d body=%s", response.Code, response.Body.String())
	}
	if service.inspectInput.ProposedConfiguredBranchRef != "refs/heads/main" {
		t.Fatalf("inspect input = %#v", service.inspectInput)
	}
	var inspection map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &inspection); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"currentConfiguredBranchRef":   "refs/heads/main",
		"expectedConfigurationVersion": float64(2),
		"proposedConfiguredBranchRef":  "refs/heads/main",
		"proposedConfigurationVersion": float64(2),
		"proposedBranchCommitOid":      strings.Repeat("a", 40),
		"proposedBranchTreeOid":        strings.Repeat("b", 40),
		"configurationDisposition":     workflowrepos.ConfigurationDispositionPreserve,
	} {
		if inspection[key] != want {
			t.Fatalf("inspection %s = %#v, want %#v", key, inspection[key], want)
		}
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/repositories",
		strings.NewReader(`{"localPath":"/repo","repoTargetOverride":"relay","proposedConfiguredBranchRef":"refs/heads/main","expectedConfirmationHash":"`+strings.Repeat("c", 64)+`"}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("confirm status = %d body=%s", response.Code, response.Body.String())
	}
	if service.confirmInput.ProposedConfiguredBranchRef != "refs/heads/main" {
		t.Fatalf("confirm input = %#v", service.confirmInput)
	}
	var confirmed map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed["outcome"] != workflowrepos.RegistrationOutcomeReused ||
		confirmed["configurationDisposition"] != workflowrepos.ConfigurationDispositionPreserve {
		t.Fatalf("confirm response = %#v", confirmed)
	}
	repository := confirmed["repository"].(map[string]any)
	if repository["configuredBranchRef"] != "refs/heads/main" ||
		repository["configurationVersion"] != float64(2) {
		t.Fatalf("confirmed repository = %#v", repository)
	}
}

func TestRepositoryAPIConfirmationOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		outcome     string
		disposition string
		version     int64
	}{
		{
			name:        "created configured target",
			status:      http.StatusCreated,
			outcome:     workflowrepos.RegistrationOutcomeCreated,
			disposition: workflowrepos.ConfigurationDispositionConfigure,
			version:     1,
		},
		{
			name:        "reused unchanged target",
			status:      http.StatusOK,
			outcome:     workflowrepos.RegistrationOutcomeReused,
			disposition: workflowrepos.ConfigurationDispositionPreserve,
			version:     2,
		},
		{
			name:        "reused changed target",
			status:      http.StatusOK,
			outcome:     workflowrepos.RegistrationOutcomeReused,
			disposition: workflowrepos.ConfigurationDispositionChange,
			version:     3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &branchAPIFakeService{
				confirmation: workflowapp.RepositoryRegistrationResult{
					Outcome:                  tc.outcome,
					ConfigurationDisposition: tc.disposition,
					Repository: workflowapp.RepositoryTarget{
						RepoTarget:           "relay",
						LocalPath:            "/repo",
						ConfiguredBranchRef:  sql.NullString{String: "refs/heads/main", Valid: true},
						ConfigurationVersion: tc.version,
						CreatedAt:            "created",
						UpdatedAt:            "updated",
					},
				},
			}
			response := httptest.NewRecorder()
			branchAPIRouter(service).ServeHTTP(response, httptest.NewRequest(
				http.MethodPost,
				"/repositories",
				strings.NewReader(`{"localPath":"/repo","proposedConfiguredBranchRef":"refs/heads/main","expectedConfirmationHash":"hash"}`),
			))
			if response.Code != tc.status {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["outcome"] != tc.outcome ||
				body["configurationDisposition"] != tc.disposition {
				t.Fatalf("confirmation body = %#v", body)
			}
			repository := body["repository"].(map[string]any)
			if repository["configurationVersion"] != float64(tc.version) {
				t.Fatalf("repository body = %#v", repository)
			}
		})
	}
}

func TestRepositoryAPIStrictBranchRequestClosure(t *testing.T) {
	router := branchAPIRouter(&branchAPIFakeService{
		inspection: workflowapp.RepositoryInspection{
			State:   workflowrepos.InspectionStateNeedsTargetOverride,
			Remotes: []workflowapp.RepositoryRemoteCandidate{},
			Notices: []string{},
		},
	})
	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "unknown inspect field", path: "/repositories/inspect", body: `{"localPath":"/repo","unknown":true}`},
		{name: "null inspect branch", path: "/repositories/inspect", body: `{"localPath":"/repo","proposedConfiguredBranchRef":null}`},
		{name: "non string inspect branch", path: "/repositories/inspect", body: `{"localPath":"/repo","proposedConfiguredBranchRef":7}`},
		{name: "second inspect object", path: "/repositories/inspect", body: `{"localPath":"/repo"} {"localPath":"/other"}`},
		{name: "unknown confirm field", path: "/repositories", body: `{"localPath":"/repo","expectedConfirmationHash":"x","unknown":true}`},
		{name: "null confirm branch", path: "/repositories", body: `{"localPath":"/repo","proposedConfiguredBranchRef":null,"expectedConfirmationHash":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/repositories/inspect",
		strings.NewReader(`{"localPath":"/repo"}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("omitted branch status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestRepositoryAPIMapsStaleConfigurationToConfirmationRequired(t *testing.T) {
	service := &branchAPIFakeService{
		confirmErr: &workflowrepos.ConfirmationError{
			Reason: "stale",
			Inspection: workflowrepos.Inspection{
				State:                        workflowrepos.InspectionStateReady,
				RepoTarget:                   "relay",
				ConfigurationDisposition:     workflowrepos.ConfigurationDispositionChange,
				ExpectedConfigurationVersion: 4,
				ProposedConfigurationVersion: 5,
				Remotes:                      []workflowrepos.RemoteCandidate{},
				Notices:                      []string{},
			},
		},
	}
	router := branchAPIRouter(service)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/repositories",
		strings.NewReader(`{"localPath":"/repo","expectedConfirmationHash":"hash"}`),
	))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "CONFIRMATION_REQUIRED" {
		t.Fatalf("error response = %#v", body)
	}
	details := body["details"].(map[string]any)
	inspection := details["inspection"].(map[string]any)
	if inspection["expectedConfigurationVersion"] != float64(4) ||
		inspection["proposedConfigurationVersion"] != float64(5) {
		t.Fatalf("stale inspection = %#v", inspection)
	}
}

func TestRepositoryAPIRouteIdentityIsUnchanged(t *testing.T) {
	router := branchAPIRouter(&branchAPIFakeService{})
	var got []string
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{
		"GET /repositories",
		"GET /repositories/{repoTarget}",
		"POST /repositories",
		"POST /repositories/inspect",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %v, want %v", got, want)
	}
}

type branchAPIFakeService struct {
	repositories []workflowapp.RepositoryTarget
	get          workflowapp.RepositoryTarget
	inspection   workflowapp.RepositoryInspection
	confirmation workflowapp.RepositoryRegistrationResult
	confirmErr   error
	inspectInput workflowapp.RepositoryInspectionInput
	confirmInput workflowapp.RepositoryConfirmationInput
}

func (s *branchAPIFakeService) ListRepositories(context.Context) ([]workflowapp.RepositoryTarget, error) {
	return append([]workflowapp.RepositoryTarget{}, s.repositories...), nil
}

func (s *branchAPIFakeService) GetRepository(context.Context, string) (workflowapp.RepositoryTarget, error) {
	if s.get.RepoTarget == "" {
		return workflowapp.RepositoryTarget{}, sql.ErrNoRows
	}
	return s.get, nil
}

func (s *branchAPIFakeService) InspectRepository(
	_ context.Context,
	input workflowapp.RepositoryInspectionInput,
) (workflowapp.RepositoryInspection, error) {
	s.inspectInput = input
	return s.inspection, nil
}

func (s *branchAPIFakeService) ConfirmRepository(
	_ context.Context,
	input workflowapp.RepositoryConfirmationInput,
) (workflowapp.RepositoryRegistrationResult, error) {
	s.confirmInput = input
	if s.confirmErr != nil {
		return workflowapp.RepositoryRegistrationResult{}, s.confirmErr
	}
	return s.confirmation, nil
}

func branchAPIRouter(service WorkflowRepositoryService) chi.Router {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(
		service,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	))
	return router
}

func assertJSONKeys(t *testing.T, value map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

var _ = errors.New
