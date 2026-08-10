package packages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"relay/internal/api/shared"
	appoperations "relay/internal/app/operations"
	apppackages "relay/internal/app/packages"
	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type apiPackageOwner struct {
	detail        apppackages.Detail
	preparedInput *apppackages.PrepareInput
	approvedInput *apppackages.ApproveInput
}

func (f *apiPackageOwner) Prepare(_ context.Context, input apppackages.PrepareInput) (apppackages.PrepareResult, error) {
	f.preparedInput = &input
	return apppackages.PrepareResult{Package: f.detail.Package}, nil
}

func (f *apiPackageOwner) Approve(_ context.Context, input apppackages.ApproveInput) (apppackages.ApproveResult, error) {
	f.approvedInput = &input
	return apppackages.ApproveResult{Package: f.detail.Package}, nil
}

func (f *apiPackageOwner) Get(_ context.Context, _ string) (apppackages.Detail, error) {
	return f.detail, nil
}

type apiLeaseReconciler struct{}

func (apiLeaseReconciler) ReconcileMutationLease(context.Context, string) (executor.WorkflowMutationLeaseReconcileResult, error) {
	return executor.WorkflowMutationLeaseReconcileResult{Released: true}, nil
}

func newWorkflowRouter(t *testing.T, owner *apiPackageOwner) http.Handler {
	t.Helper()
	service, err := appoperations.NewPackageWorkflowService(owner, apiLeaseReconciler{}, &workflowstore.Store{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func testPackageOwner() *apiPackageOwner {
	return &apiPackageOwner{detail: apppackages.Detail{Package: workflowstore.ExecutionPackage{
		PackageID:     "package-api",
		PackageSha256: strings.Repeat("a", 64),
	}}}
}

func jsonRequestBody(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestPrepareRouteForwardsDirectPackageInput(t *testing.T) {
	owner := testPackageOwner()
	router := newWorkflowRouter(t, owner)
	operationsBytes := []byte(`{"operations":[]}`)
	body := prepareRequest{
		SelectionID: "selection-api",
		DeterministicOperations: &artifactRequest{
			DisplayName:    "feature.ticket-T1.r1.deterministic-operations.json",
			ExpectedSHA256: strings.Repeat("c", 64),
			BytesBase64:    base64.StdEncoding.EncodeToString(operationsBytes),
		},
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/execution-packages", strings.NewReader(jsonRequestBody(t, body))))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.preparedInput == nil {
		t.Fatal("package prepare input was not forwarded")
	}
	input := owner.preparedInput
	if input.SelectionID != body.SelectionID {
		t.Fatalf("prepare input = %#v", input)
	}
	if input.DeterministicOperations == nil || input.DeterministicOperations.DisplayName != body.DeterministicOperations.DisplayName || input.DeterministicOperations.ExpectedSHA256 != body.DeterministicOperations.ExpectedSHA256 || string(input.DeterministicOperations.Bytes) != string(operationsBytes) {
		t.Fatalf("deterministic operations = %#v", input.DeterministicOperations)
	}
}

func TestApproveRouteForwardsDirectPackageInput(t *testing.T) {
	owner := testPackageOwner()
	router := newWorkflowRouter(t, owner)
	body := approveRequest{
		ExpectedPackageSha256:        strings.Repeat("d", 64),
		OperatorConfirmationEvidence: "operator confirmed package-api",
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/execution-packages/package-api/approvals", strings.NewReader(jsonRequestBody(t, body))))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if owner.approvedInput == nil {
		t.Fatal("package approval input was not forwarded")
	}
	if owner.approvedInput.PackageID != "package-api" || owner.approvedInput.ExpectedPackageSha256 != body.ExpectedPackageSha256 || owner.approvedInput.OperatorConfirmationEvidence != body.OperatorConfirmationEvidence {
		t.Fatalf("approve input = %#v", owner.approvedInput)
	}
}

func TestWorkflowRoutesRejectLegacyPacketFields(t *testing.T) {
	owner := testPackageOwner()
	router := newWorkflowRouter(t, owner)
	brief := map[string]string{
		"displayName":    "feature.ticket-T1.r1.design-brief.md",
		"expectedSha256": strings.Repeat("b", 64),
		"bytesBase64":    base64.StdEncoding.EncodeToString([]byte("# Brief\n")),
	}
	for _, tc := range []struct {
		name string
		path string
		body any
	}{
		{
			name: "prepare packet ID",
			path: "/execution-packages",
			body: map[string]any{
				"selectionId":       "selection-api",
				"ticketDesignBrief": brief,
				"packetId":          "packet-api",
			},
		},
		{
			name: "approve operation ID",
			path: "/execution-packages/package-api/approvals",
			body: map[string]any{
				"expectedPackageSha256":        strings.Repeat("d", 64),
				"operatorConfirmationEvidence": "operator confirmed package-api",
				"operationId":                  "local_operator.ticket_workflow",
			},
		},
		{
			name: "reconcile required dependencies",
			path: "/runs/run-api/mutation-lease/reconcile",
			body: map[string]any{
				"leaseId":              "lease-api",
				"requiredDependencies": []any{},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(jsonRequestBody(t, tc.body))))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if owner.preparedInput != nil || owner.approvedInput != nil {
		t.Fatalf("legacy field request reached owner: prepare=%#v approve=%#v", owner.preparedInput, owner.approvedInput)
	}
}

func TestReconcileRouteRequiresExactNonblankLeaseID(t *testing.T) {
	owner := testPackageOwner()
	router := newWorkflowRouter(t, owner)
	for _, tc := range []struct {
		name    string
		leaseID string
	}{
		{name: "missing"},
		{name: "blank", leaseID: "   "},
		{name: "outer whitespace", leaseID: " lease-api "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/runs/run-api/mutation-lease/reconcile", strings.NewReader(jsonRequestBody(t, reconcileRequest{LeaseID: tc.leaseID}))))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var errorResponse shared.ErrorShape
			if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
				t.Fatal(err)
			}
			if errorResponse.Message != "A nonblank mutation lease ID is required" {
				t.Fatalf("error response = %#v", errorResponse)
			}
		})
	}
}

func TestWritePackageErrorUsesDirectDomainConflictMessages(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		status    int
		errorCode string
		message   string
	}{
		{
			name:      "package basis",
			err:       apppackages.ErrPackageBasisChanged,
			status:    http.StatusConflict,
			errorCode: "CONFLICT",
			message:   "Execution package basis is stale or already linked to a Run",
		},
		{
			name:      "missing lease",
			err:       appoperations.ErrNoActiveMutationLease,
			status:    http.StatusConflict,
			errorCode: "LEASE_CONFLICT",
			message:   "Mutation lease is missing, stale, or does not match the Run",
		},
		{
			name:      "mismatched lease",
			err:       appoperations.ErrMutationLeaseConflict,
			status:    http.StatusConflict,
			errorCode: "LEASE_CONFLICT",
			message:   "Mutation lease is missing, stale, or does not match the Run",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writePackageError(response, tc.err)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var errorResponse shared.ErrorShape
			if err := json.Unmarshal(response.Body.Bytes(), &errorResponse); err != nil {
				t.Fatal(err)
			}
			if errorResponse.Error != tc.errorCode || errorResponse.Message != tc.message {
				t.Fatalf("error response = %#v", errorResponse)
			}
			if strings.Contains(strings.ToLower(errorResponse.Message), "packet") || strings.Contains(strings.ToLower(errorResponse.Message), "admission") {
				t.Fatalf("legacy conflict wording = %q", errorResponse.Message)
			}
		})
	}
}
