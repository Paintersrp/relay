package repositories

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workflowapp "relay/internal/app/workflow"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowRepositoryService struct {
	values             []workflowstore.RepositoryTarget
	value              workflowstore.RepositoryTarget
	inspection         workflowapp.RepositoryInspection
	registrationResult workflowapp.RepositoryRegistrationResult
	err                error
	inspectionInput    workflowapp.RepositoryInspectionInput
	confirmationInput  workflowapp.RepositoryConfirmationInput
}

func (f *fakeWorkflowRepositoryService) ListRepositories(context.Context) ([]workflowstore.RepositoryTarget, error) {
	return f.values, f.err
}

func (f *fakeWorkflowRepositoryService) GetRepository(context.Context, string) (workflowstore.RepositoryTarget, error) {
	return f.value, f.err
}

func (f *fakeWorkflowRepositoryService) InspectRepository(
	_ context.Context,
	input workflowapp.RepositoryInspectionInput,
) (workflowapp.RepositoryInspection, error) {
	f.inspectionInput = input
	return f.inspection, f.err
}

func (f *fakeWorkflowRepositoryService) ConfirmRepository(
	_ context.Context,
	input workflowapp.RepositoryConfirmationInput,
) (workflowapp.RepositoryRegistrationResult, error) {
	f.confirmationInput = input
	return f.registrationResult, f.err
}

func workflowRepositoryRouter(service WorkflowRepositoryService) http.Handler {
	return workflowRepositoryRouterWithLogger(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func workflowRepositoryRouterWithLogger(
	service WorkflowRepositoryService,
	logger *slog.Logger,
) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service, logger))
	return router
}

func TestWorkflowRepositoryListAndGetContractsRemainUnchanged(t *testing.T) {
	service := &fakeWorkflowRepositoryService{
		values: []workflowstore.RepositoryTarget{
			{
				RepoTarget: "relay",
				LocalPath:  "/repo",
				CreatedAt:  "created",
				UpdatedAt:  "updated",
			},
		},
		value: workflowstore.RepositoryTarget{
			RepoTarget: "relay",
			LocalPath:  "/repo",
			CreatedAt:  "created",
			UpdatedAt:  "updated",
		},
	}

	response := httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/repositories", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list response = %d %s", response.Code, response.Body.String())
	}
	var listBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if listBody["count"] != float64(1) {
		t.Fatalf("list count = %#v", listBody["count"])
	}
	items, ok := listBody["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("list items = %#v", listBody["items"])
	}
	item := items[0].(map[string]any)
	for key, want := range map[string]string{
		"repoTarget": "relay",
		"localPath":  "/repo",
		"createdAt":  "created",
		"updatedAt":  "updated",
	} {
		if item[key] != want {
			t.Fatalf("list item %s = %#v, want %q", key, item[key], want)
		}
	}

	response = httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/repositories/relay", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("get response = %d %s", response.Code, response.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody["repoTarget"] != "relay" || getBody["localPath"] != "/repo" {
		t.Fatalf("get body = %#v", getBody)
	}
}

func TestWorkflowRepositoryInspectionStateShapes(t *testing.T) {
	remote := workflowapp.RepositoryRemoteCandidate{
		Name:                "origin",
		URL:                 "git@example.com:owner/relay.git",
		SuggestedRepoTarget: "relay",
	}
	existing := workflowstore.RepositoryTarget{
		RepoTarget: "relay",
		LocalPath:  "/repo",
		CreatedAt:  "created",
		UpdatedAt:  "updated",
	}

	tests := []struct {
		name       string
		inspection workflowapp.RepositoryInspection
		present    []string
		absent     []string
	}{
		{
			name: "ready create",
			inspection: workflowapp.RepositoryInspection{
				State:                   workflowrepos.InspectionStateReady,
				SelectedPath:            "/repo/nested",
				ResolvedLocalPath:       "/repo",
				Remotes:                 []workflowapp.RepositoryRemoteCandidate{remote},
				SelectedRemote:          &remote,
				SuggestedRepoTarget:     "relay",
				RepoTarget:              "relay",
				RepoTargetSource:        workflowrepos.RepoTargetSourceRemoteBasename,
				RegistrationDisposition: workflowrepos.RegistrationDispositionCreate,
				ConfirmationHash:        strings.Repeat("a", 64),
				Notices:                 []string{},
			},
			present: []string{
				"selectedRemote",
				"suggestedRepoTarget",
				"repoTarget",
				"repoTargetSource",
				"registrationDisposition",
				"confirmationHash",
			},
			absent: []string{"existingRepository", "conflictKind", "targetOverrideReason"},
		},
		{
			name: "ready reuse",
			inspection: workflowapp.RepositoryInspection{
				State:                   workflowrepos.InspectionStateReady,
				SelectedPath:            "/repo",
				ResolvedLocalPath:       "/repo",
				Remotes:                 []workflowapp.RepositoryRemoteCandidate{remote},
				SelectedRemote:          &remote,
				SuggestedRepoTarget:     "relay",
				RepoTarget:              "relay",
				RepoTargetSource:        workflowrepos.RepoTargetSourceRemoteBasename,
				RegistrationDisposition: workflowrepos.RegistrationDispositionReuse,
				ExistingRepository:      &existing,
				ConfirmationHash:        strings.Repeat("b", 64),
				Notices:                 []string{},
			},
			present: []string{
				"repoTarget",
				"registrationDisposition",
				"existingRepository",
				"confirmationHash",
			},
			absent: []string{"conflictKind", "targetOverrideReason"},
		},
		{
			name: "needs remote selection",
			inspection: workflowapp.RepositoryInspection{
				State:             workflowrepos.InspectionStateNeedsRemoteSelection,
				SelectedPath:      "/repo",
				ResolvedLocalPath: "/repo",
				Remotes:           []workflowapp.RepositoryRemoteCandidate{remote},
				Notices:           []string{},
			},
			absent: []string{
				"selectedRemote",
				"suggestedRepoTarget",
				"targetOverrideReason",
				"repoTarget",
				"repoTargetSource",
				"registrationDisposition",
				"existingRepository",
				"conflictKind",
				"confirmationHash",
			},
		},
		{
			name: "needs target override",
			inspection: workflowapp.RepositoryInspection{
				State:                workflowrepos.InspectionStateNeedsTargetOverride,
				SelectedPath:         "/repo",
				ResolvedLocalPath:    "/repo",
				Remotes:              []workflowapp.RepositoryRemoteCandidate{remote},
				SelectedRemote:       &remote,
				TargetOverrideReason: workflowrepos.TargetOverrideReasonUnsupportedRemote,
				Notices: []string{
					`Remote "origin" uses an unsupported URL.`,
				},
			},
			present: []string{"selectedRemote", "targetOverrideReason"},
			absent: []string{
				"suggestedRepoTarget",
				"repoTarget",
				"repoTargetSource",
				"registrationDisposition",
				"existingRepository",
				"conflictKind",
				"confirmationHash",
			},
		},
		{
			name: "conflict",
			inspection: workflowapp.RepositoryInspection{
				State:               workflowrepos.InspectionStateConflict,
				SelectedPath:        "/other",
				ResolvedLocalPath:   "/other",
				Remotes:             []workflowapp.RepositoryRemoteCandidate{remote},
				SelectedRemote:      &remote,
				SuggestedRepoTarget: "relay",
				RepoTarget:          "relay",
				RepoTargetSource:    workflowrepos.RepoTargetSourceRemoteBasename,
				ExistingRepository:  &existing,
				ConflictKind:        workflowrepos.ConflictKindTarget,
				Notices:             []string{},
			},
			present: []string{
				"repoTarget",
				"existingRepository",
				"conflictKind",
			},
			absent: []string{
				"targetOverrideReason",
				"registrationDisposition",
				"confirmationHash",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeWorkflowRepositoryService{inspection: tt.inspection}
			response := httptest.NewRecorder()
			workflowRepositoryRouter(service).ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodPost,
					"/repositories/inspect",
					strings.NewReader(`{"localPath":"/repo","remoteName":"origin","repoTargetOverride":"override"}`),
				),
			)
			if response.Code != http.StatusOK {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if service.inspectionInput.LocalPath != "/repo" ||
				service.inspectionInput.RemoteName != "origin" ||
				service.inspectionInput.RepoTargetOverride != "override" {
				t.Fatalf("inspection input = %+v", service.inspectionInput)
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["state"] != tt.inspection.State {
				t.Fatalf("state = %#v", body["state"])
			}
			for _, field := range tt.present {
				if _, ok := body[field]; !ok {
					t.Fatalf("field %q is absent from %#v", field, body)
				}
			}
			for _, field := range tt.absent {
				if _, ok := body[field]; ok {
					t.Fatalf("field %q must be absent from %#v", field, body)
				}
			}
		})
	}
}

func TestWorkflowRepositoryConfirmCreatedAndReusedResponses(t *testing.T) {
	for _, tt := range []struct {
		outcome string
		status  int
	}{
		{outcome: workflowrepos.RegistrationOutcomeCreated, status: http.StatusCreated},
		{outcome: workflowrepos.RegistrationOutcomeReused, status: http.StatusOK},
	} {
		t.Run(tt.outcome, func(t *testing.T) {
			service := &fakeWorkflowRepositoryService{
				registrationResult: workflowapp.RepositoryRegistrationResult{
					Outcome: tt.outcome,
					Repository: workflowstore.RepositoryTarget{
						RepoTarget: "relay",
						LocalPath:  "/repo",
						CreatedAt:  "created",
						UpdatedAt:  "updated",
					},
				},
			}
			response := httptest.NewRecorder()
			workflowRepositoryRouter(service).ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodPost,
					"/repositories",
					strings.NewReader(`{"localPath":"/repo","remoteName":"origin","repoTargetOverride":"","expectedConfirmationHash":"hash"}`),
				),
			)
			if response.Code != tt.status {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if service.confirmationInput.ExpectedConfirmationHash != "hash" {
				t.Fatalf("confirmation input = %+v", service.confirmationInput)
			}
			if !strings.Contains(response.Body.String(), `"outcome":"`+tt.outcome+`"`) {
				t.Fatalf("body = %s", response.Body.String())
			}
		})
	}
}

func TestWorkflowRepositoryMissingConfirmationHashUsesCreateEndpoint(t *testing.T) {
	service := &fakeWorkflowRepositoryService{
		err: errors.New("confirmation hash is required"),
	}
	response := httptest.NewRecorder()
	workflowRepositoryRouter(service).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/repositories",
			strings.NewReader(`{"localPath":"/repo","remoteName":"origin","repoTargetOverride":"","expectedConfirmationHash":""}`),
		),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "BAD_REQUEST" {
		t.Fatalf("error = %#v, want BAD_REQUEST", body["error"])
	}
	if _, ok := body["details"]; ok {
		t.Fatalf("missing confirmation response included confirmation-required details: %#v", body)
	}
	if service.confirmationInput.LocalPath != "/repo" ||
		service.confirmationInput.RemoteName != "origin" ||
		service.confirmationInput.RepoTargetOverride != "" ||
		service.confirmationInput.ExpectedConfirmationHash != "" {
		t.Fatalf("confirmation input = %+v", service.confirmationInput)
	}
}

func TestWorkflowRepositoryConfirmationErrorsIncludeCurrentInspection(t *testing.T) {
	inspection := workflowapp.RepositoryInspection{
		State:                   workflowrepos.InspectionStateReady,
		SelectedPath:            "/repo",
		ResolvedLocalPath:       "/repo",
		Remotes:                 []workflowapp.RepositoryRemoteCandidate{},
		RepoTarget:              "relay",
		RepoTargetSource:        workflowrepos.RepoTargetSourceOperatorOverride,
		RegistrationDisposition: workflowrepos.RegistrationDispositionCreate,
		ConfirmationHash:        strings.Repeat("c", 64),
		Notices:                 []string{},
	}
	for _, reason := range []string{"stale", "conflict", "not_ready"} {
		t.Run(reason, func(t *testing.T) {
			service := &fakeWorkflowRepositoryService{
				err: &workflowrepos.ConfirmationError{
					Reason:     reason,
					Inspection: inspection,
				},
			}
			response := httptest.NewRecorder()
			workflowRepositoryRouter(service).ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodPost,
					"/repositories",
					strings.NewReader(`{"localPath":"/repo","expectedConfirmationHash":"old"}`),
				),
			)
			if response.Code != http.StatusConflict ||
				!strings.Contains(response.Body.String(), `"error":"CONFIRMATION_REQUIRED"`) ||
				!strings.Contains(response.Body.String(), `"inspection"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkflowRepositoryTypedErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "not found",
			err:    sql.ErrNoRows,
			status: http.StatusNotFound,
			code:   "NOT_FOUND",
		},
		{
			name:   "invalid path",
			err:    fmtError(workflowrepos.ErrInvalidRepositoryPath, "outside worktree"),
			status: http.StatusUnprocessableEntity,
			code:   "INVALID_REPOSITORY_PATH",
		},
		{
			name:   "git unavailable",
			err:    workflowrepos.ErrGitUnavailable,
			status: http.StatusServiceUnavailable,
			code:   "GIT_UNAVAILABLE",
		},
		{
			name:   "git timeout",
			err:    workflowrepos.ErrGitTimeout,
			status: http.StatusServiceUnavailable,
			code:   "GIT_UNAVAILABLE",
		},
		{
			name:   "invalid target",
			err:    errors.New("repository target contains whitespace"),
			status: http.StatusBadRequest,
			code:   "BAD_REQUEST",
		},
		{
			name:   "raw storage constraint",
			err:    errors.New("UNIQUE constraint failed"),
			status: http.StatusInternalServerError,
			code:   "INTERNAL_ERROR",
		},
		{
			name:   "unexpected",
			err:    errors.New("disk failed"),
			status: http.StatusInternalServerError,
			code:   "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeWorkflowRepositoryService{err: tt.err}
			response := httptest.NewRecorder()
			workflowRepositoryRouter(service).ServeHTTP(
				response,
				httptest.NewRequest(
					http.MethodPost,
					"/repositories/inspect",
					strings.NewReader(`{"localPath":"/repo"}`),
				),
			)
			if response.Code != tt.status ||
				!strings.Contains(response.Body.String(), `"error":"`+tt.code+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkflowRepositoryUnexpectedFailureIsLoggedWithoutClientDisclosure(t *testing.T) {
	internalDetail := "database failure: credential=internal-only"
	service := &fakeWorkflowRepositoryService{err: errors.New(internalDetail)}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	response := httptest.NewRecorder()
	workflowRepositoryRouterWithLogger(service, logger).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/repositories/inspect",
			strings.NewReader(`{"localPath":"/repo"}`),
		),
	)

	if response.Code != http.StatusInternalServerError ||
		!strings.Contains(response.Body.String(), `"error":"INTERNAL_ERROR"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), internalDetail) {
		t.Fatalf("client response exposed internal error detail: %s", response.Body.String())
	}
	logged := logs.String()
	for _, expected := range []string{
		"repository operation failed",
		internalDetail,
		`"method":"POST"`,
		`"path":"/repositories/inspect"`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("structured log %q does not contain %q", logged, expected)
		}
	}
}

func TestWorkflowRepositoryStrictRequestDecoding(t *testing.T) {
	for _, body := range []string{
		`{"localPath":"/repo","unknown":true}`,
		`{"localPath":"/repo"} {"localPath":"/other"}`,
		`not-json`,
	} {
		response := httptest.NewRecorder()
		workflowRepositoryRouter(&fakeWorkflowRepositoryService{}).ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, "/repositories/inspect", strings.NewReader(body)),
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q response = %d %s", body, response.Code, response.Body.String())
		}
	}
}

func fmtError(base error, detail string) error {
	return errors.Join(base, errors.New(detail))
}
