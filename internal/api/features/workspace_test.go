package features

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	featureapp "relay/internal/app/features"
	appoperations "relay/internal/app/operations"
	wayfinder "relay/internal/app/wayfinder"
	"relay/internal/prototypeexecution"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWayfinder struct {
	detail wayfinder.WorkspaceDetail
	err    error
}

func (f *fakeWayfinder) CreateWorkspace(context.Context, wayfinder.CreateWorkspaceInput) (Workspace, error) {
	return Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) ReadWorkspace(context.Context, string) (wayfinder.WorkspaceDetail, error) {
	return f.detail, f.err
}
func (f *fakeWayfinder) AdmitInput(context.Context, wayfinder.AdmitInputInput) (AdmittedInput, Workspace, error) {
	return AdmittedInput{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) AddDestination(context.Context, wayfinder.AddDestinationInput) (Destination, Workspace, error) {
	return Destination{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) CreateDiscoveryTicket(context.Context, wayfinder.CreateDiscoveryTicketInput) (DiscoveryTicket, Workspace, error) {
	return DiscoveryTicket{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) ResolveDiscoveryTicket(context.Context, wayfinder.ResolveDiscoveryTicketInput) (Resolution, DiscoveryTicket, Workspace, error) {
	return Resolution{}, DiscoveryTicket{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}
func (f *fakeWayfinder) RouteWorkspace(context.Context, wayfinder.RouteWorkspaceInput) (RouteState, Workspace, error) {
	return RouteState{}, Workspace{WorkspaceID: f.detail.Workspace.WorkspaceID, FeatureSlug: f.detail.Workspace.FeatureSlug, State: f.detail.Workspace.State, Version: f.detail.Workspace.Version, CreatedAt: f.detail.Workspace.CreatedAt, UpdatedAt: f.detail.Workspace.UpdatedAt}, f.err
}

type fakeAuthority struct {
	revisions                            []featureapp.AuthorityRevisionDetail
	err                                  error
	prototype                            prototypeexecution.Result
	cleanupInput                         prototypeexecution.CleanupRequest
	anotherInput                         featureapp.PrepareAnotherPrototypeExecutionInput
	packetInput                          featureapp.PrepareQADiscoveryPacketInput
	evidenceInput                        featureapp.AdmitOperatorQAEvidenceInput
	wayfinderWorkspaceID, wayfinderRunID string
}

func (f *fakeAuthority) ReadAuthority(context.Context, string) ([]featureapp.AuthorityRevisionDetail, error) {
	return f.revisions, f.err
}
func (f *fakeAuthority) PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, Workspace, error) {
	return featureapp.AuthorityRevisionDetail{}, Workspace{}, f.err
}
func (f *fakeAuthority) RecordAuthorityApproval(context.Context, featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error) {
	return featureapp.RecordAuthorityApprovalResult{}, f.err
}

type fakeCompletion struct {
	status appoperations.FeatureCompletionStatus
	result appoperations.FeatureCompletionResult
	err    error
	input  featureapp.CompletionInput
}

func (f *fakeCompletion) Evaluate(context.Context, string) (appoperations.FeatureCompletionStatus, error) {
	return f.status, f.err
}
func (f *fakeCompletion) Complete(_ context.Context, input featureapp.CompletionInput) (appoperations.FeatureCompletionResult, error) {
	f.input = input
	if f.err == nil && f.result.Decision.Decision != "" {
		f.status.Workspace = f.result.Workspace
		f.status.CurrentDecision = &f.result.Decision
	}
	return f.result, f.err
}

func workspaceRouter(wayfinderService WayfinderService, authorityService AuthorityService, completionService CompletionService) http.Handler {
	router := chi.NewRouter()
	MountWorkspaceRoutes(router, NewWorkspaceHandler(wayfinderService, authorityService, completionService))
	return router
}

func TestWorkspaceDetailRouteReturnsResumableProjectionWithoutVaultPaths(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 3}, Investigations: []workflowstore.FeatureWorkspaceInvestigation{{InvestigationID: "investigation-api"}}}}
	response := httptest.NewRecorder()
	workspaceRouter(service, &fakeAuthority{}, &fakeCompletion{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"workspaceId":"workspace-api"`) || !strings.Contains(response.Body.String(), `"status":"not_recorded"`) || strings.Contains(strings.ToLower(response.Body.String()), "vault") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceMutationReturnsTypedVersionConflict(t *testing.T) {
	service := &fakeWayfinder{detail: wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-api", Version: 2}}, err: wayfinder.ErrVersionConflict}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/routes", strings.NewReader(`{"expectedVersion":1,"sequence":1,"state":"ready"}`))
	workspaceRouter(service, &fakeAuthority{}, &fakeCompletion{}).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"VERSION_CONFLICT"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceRoutesDoNotExposeDeliveryTicketOrPackageSurfaces(t *testing.T) {
	router := workspaceRouter(&fakeWayfinder{err: errors.New("unexpected")}, &fakeAuthority{}, &fakeCompletion{})
	for _, path := range []string{"/feature-workspaces/workspace-api/delivery-tickets", "/feature-workspaces/workspace-api/packages"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}

func TestWorkspaceCompletionShowsBlockersAndForwardsDirectCompletion(t *testing.T) {
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{
		Workspace:       appoperations.FeatureCompletionWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 3},
		Gates:           []appoperations.FeatureCompletionGate{{Name: "authority", Ready: true}, {Name: "audit", Ready: false}},
		CurrentDecision: &appoperations.FeatureCompletionDecision{CompletionDecisionID: "current-completion-api", AuthorityRevisionRowID: 2, SourceClosureRowID: 3, Decision: "completed"},
	}, result: appoperations.FeatureCompletionResult{
		Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: "workspace-api", FeatureSlug: "payments", State: "open", Version: 4},
		Decision:  appoperations.FeatureCompletionDecision{CompletionDecisionID: "completion-api", AuthorityRevisionRowID: 3, SourceClosureRowID: 4, Decision: "completed"},
	}}
	router := workspaceRouter(&fakeWayfinder{}, &fakeAuthority{}, completion)
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-api/completion", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"name":"audit","ready":false`) || !strings.Contains(status.Body.String(), `"completionDecisionId":"current-completion-api"`) {
		t.Fatalf("status = %d %s", status.Code, status.Body.String())
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/completion", strings.NewReader(`{"expectedVersion":3,"operatorConfirmed":true}`)))
	want := featureapp.CompletionInput{WorkspaceID: "workspace-api", ExpectedVersion: 3, OperatorConfirmed: true}
	if response.Code != http.StatusCreated || completion.input != want || !strings.Contains(response.Body.String(), `"completionDecisionId":"completion-api"`) {
		t.Fatalf("response = %d input = %#v want = %#v body = %s", response.Code, completion.input, want, response.Body.String())
	}
}

func TestWorkspaceCompletionRejectsLegacyPacketFields(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "packet ID", body: `{"packetId":"packet-api","expectedVersion":3,"operatorConfirmed":true}`},
		{name: "operation ID", body: `{"operationId":"local_operator.ticket_workflow","expectedVersion":3,"operatorConfirmed":true}`},
		{name: "required dependencies", body: `{"requiredDependencies":[],"expectedVersion":3,"operatorConfirmed":true}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			completion := &fakeCompletion{}
			response := httptest.NewRecorder()
			workspaceRouter(&fakeWayfinder{}, &fakeAuthority{}, completion).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/completion", strings.NewReader(test.body)))
			if response.Code != http.StatusBadRequest || completion.input != (featureapp.CompletionInput{}) {
				t.Fatalf("response = %d input = %#v body = %s", response.Code, completion.input, response.Body.String())
			}
		})
	}
}

func TestRecordApprovalRejectsInvalidEvidenceInAPI(t *testing.T) {
	badAuthority := &fakeAuthority{err: featureapp.ErrInvalidApprovalInput}
	router := workspaceRouter(&fakeWayfinder{}, badAuthority, &fakeCompletion{})

	emptyBody := `{"family":"requirements","artifactRowID":1,"artifactSHA256":"` + strings.Repeat("b", 64) + `","operatorConfirmationEvidence":""}`
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/authority-approvals", strings.NewReader(emptyBody)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty evidence status = %d body = %s", response.Code, response.Body.String())
	}

	whitespaceBody := `{"family":"design","artifactRowID":1,"artifactSHA256":"` + strings.Repeat("c", 64) + `","operatorConfirmationEvidence":"   "}`
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/authority-approvals", strings.NewReader(whitespaceBody)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("whitespace evidence status = %d body = %s", response.Code, response.Body.String())
	}
}

func (f *fakeAuthority) LaunchApprovedPrototype(context.Context, prototypeexecution.LaunchRequest) (prototypeexecution.Result, error) {
	return f.prototype, f.err
}
func (f *fakeAuthority) ReconcilePrototypeLaunch(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return f.prototype, f.err
}
func (f *fakeAuthority) CancelPrototypeExecution(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return f.prototype, f.err
}
func (f *fakeAuthority) SettlePrototypeTimeout(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return f.prototype, f.err
}

func TestPrototypeRuntimeRoutes(t *testing.T) {
	base := prototypeexecution.Result{Run: workflowstore.PrototypeRun{PrototypeRunID: "prototype-run-api", LifecycleState: "running", Version: 3}, Runtime: &workflowstore.PrototypeRuntime{RuntimeID: "prototype-runtime-api"}, Target: &workflowstore.PrototypeTarget{TargetID: "prototype-target-api"}, Lease: &workflowstore.PrototypeLease{LeaseToken: "prototype-lease-api"}}
	cases := []struct {
		name, path, body string
		err              error
		want             int
	}{
		{"launch success", "/feature-workspaces/w/prototype-runs/r/launch", `{"expectedRunVersion":2,"mutationIdentity":"launch"}`, nil, http.StatusAccepted},
		{"launch uncertainty", "/feature-workspaces/w/prototype-runs/r/launch", `{"expectedRunVersion":2,"mutationIdentity":"launch"}`, prototypeexecution.ErrLaunchUncertain, http.StatusConflict},
		{"reconcile", "/feature-workspaces/w/prototype-runs/r/reconcile", `{"expectedRunVersion":2,"mutationIdentity":"reconcile"}`, nil, http.StatusOK},
		{"cancel", "/feature-workspaces/w/prototype-runs/r/cancel", `{"expectedRunVersion":2,"mutationIdentity":"cancel"}`, nil, http.StatusOK},
		{"timeout", "/feature-workspaces/w/prototype-runs/r/timeout", `{"expectedRunVersion":2,"mutationIdentity":"timeout"}`, nil, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			workspaceRouter(&fakeWayfinder{}, &fakeAuthority{prototype: base, err: tc.err}, &fakeCompletion{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.err != nil {
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body["prototypeExecution"] == nil {
				t.Fatalf("response keys=%v", body)
			}
			var execution map[string]json.RawMessage
			if err := json.Unmarshal(body["prototypeExecution"], &execution); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"run", "runtime", "target", "lease", "evidenceBatches", "finalResult", "evidence"} {
				if execution[key] == nil {
					t.Fatalf("missing response key %q: %s", key, response.Body.String())
				}
			}
			if execution["resultMembers"] != nil {
				t.Fatalf("private resultMembers exposed: %s", response.Body.String())
			}
		})
	}
	for _, body := range []string{`{"expectedRunVersion":2,"mutationIdentity":"m","extra":true}`} {
		response := httptest.NewRecorder()
		workspaceRouter(&fakeWayfinder{}, &fakeAuthority{prototype: base}, &fakeCompletion{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/w/prototype-runs/r/launch", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid launch status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

func (f *fakeAuthority) ReconcilePrototypeCleanup(_ context.Context, input prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error) {
	f.cleanupInput = input
	return prototypeexecution.CleanupResult{Result: f.prototype}, f.err
}
func (f *fakeAuthority) PrepareAnotherPrototypeExecution(_ context.Context, input featureapp.PrepareAnotherPrototypeExecutionInput) (featureapp.PrototypeExecutionDetail, error) {
	f.anotherInput = input
	return featureapp.PrototypeExecutionDetail{}, f.err
}
func (f *fakeAuthority) PrepareQADiscoveryPacket(_ context.Context, input featureapp.PrepareQADiscoveryPacketInput) (featureapp.PrototypeQAPacketDetail, error) {
	f.packetInput = input
	return featureapp.PrototypeQAPacketDetail{}, f.err
}
func (f *fakeAuthority) AdmitOperatorQAEvidence(_ context.Context, input featureapp.AdmitOperatorQAEvidenceInput) (featureapp.PrototypeQAPacketDetail, error) {
	f.evidenceInput = input
	return featureapp.PrototypeQAPacketDetail{}, f.err
}
func (f *fakeAuthority) ReadPrototypeEvidenceForWayfinder(_ context.Context, workspaceID, runID string) (featureapp.PrototypeWayfinderEvidenceView, error) {
	f.wayfinderWorkspaceID, f.wayfinderRunID = workspaceID, runID
	return featureapp.PrototypeWayfinderEvidenceView{WorkspaceID: workspaceID, RunID: runID}, f.err
}

func TestPrototypePart3RoutesForwardBoundedInputsAndDTOs(t *testing.T) {
	authority := &fakeAuthority{prototype: prototypeexecution.Result{Run: workflowstore.PrototypeRun{PrototypeRunID: "run-api", LifecycleState: "closed", Version: 4}}}
	router := workspaceRouter(&fakeWayfinder{}, authority, &fakeCompletion{})
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"cleanup", http.MethodPost, "/feature-workspaces/workspace-api/prototype-runs/run-api/cleanup", `{"expectedRunVersion":4,"mutationIdentity":"cleanup-api"}`, http.StatusOK},
		{"another execution", http.MethodPost, "/feature-workspaces/workspace-api/prototype-runs/run-api/another-execution", `{"expectedPriorRunVersion":4,"mutationIdentity":"another-api","operatorConfirmationEvidence":"confirmed"}`, http.StatusCreated},
		{"QA packet", http.MethodPost, "/feature-workspaces/workspace-api/prototype-runs/run-api/qa-packets", `{"expectedRunVersion":4,"mutationIdentity":"packet-api","operatorPrompt":"review","validationInstructions":["confirm"]}`, http.StatusCreated},
		{"QA evidence", http.MethodPost, "/feature-workspaces/workspace-api/prototype-qa-packets/packet-api/evidence", `{"mutationIdentity":"evidence-api","operatorConfirmationEvidence":"confirmed","evidence":[{"semanticRole":"note","mediaType":"text/plain","content":"c2FmZQ==","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`, http.StatusCreated},
		{"Wayfinder evidence", http.MethodGet, "/feature-workspaces/workspace-api/prototype-runs/run-api/wayfinder-evidence", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if authority.cleanupInput.TriggerKind != "explicit" || authority.cleanupInput.WorkspaceID != "workspace-api" || authority.cleanupInput.RunID != "run-api" {
		t.Fatalf("cleanup input=%#v", authority.cleanupInput)
	}
	if authority.anotherInput.PriorRunID != "run-api" || authority.anotherInput.MutationIdentity != "another-api" {
		t.Fatalf("another input=%#v", authority.anotherInput)
	}
	if authority.packetInput.RunID != "run-api" || authority.packetInput.OperatorPrompt != "review" || len(authority.packetInput.ValidationInstructions) != 1 {
		t.Fatalf("packet input=%#v", authority.packetInput)
	}
	if authority.evidenceInput.QAPacketID != "packet-api" || len(authority.evidenceInput.Evidence) != 1 || string(authority.evidenceInput.Evidence[0].Content) != "safe" {
		t.Fatalf("evidence input=%#v", authority.evidenceInput)
	}
	if authority.wayfinderWorkspaceID != "workspace-api" || authority.wayfinderRunID != "run-api" {
		t.Fatalf("Wayfinder input=%q/%q", authority.wayfinderWorkspaceID, authority.wayfinderRunID)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-api/prototype-runs/run-api/qa-packets", strings.NewReader(`{"expectedRunVersion":4,"mutationIdentity":"packet-api","operatorPrompt":"review","validationInstructions":[],"extra":true}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown QA packet field status=%d body=%s", response.Code, response.Body.String())
	}
}

type fakeGuided struct {
	assessment    GuidedAssessment
	current       featureapp.FeatureCurrentnessDecision
	assessmentErr error
	recordInput   featureapp.RecordDiscoveryDestinationAssessmentInput
	closeInput    featureapp.CloseFeatureDiscoveryInput
	recordErr     error
	closeErr      error
}

func (f *fakeGuided) AssessDiscoveryDestination(context.Context, string) (GuidedAssessment, error) {
	return f.assessment, f.assessmentErr
}
func (f *fakeGuided) Currentness(context.Context, string) (featureapp.FeatureCurrentnessDecision, error) {
	return f.current, nil
}
func (f *fakeGuided) RecordDiscoveryDestinationAssessment(_ context.Context, input featureapp.RecordDiscoveryDestinationAssessmentInput) error {
	f.recordInput = input
	return f.recordErr
}
func (f *fakeGuided) CloseFeatureDiscovery(_ context.Context, input featureapp.CloseFeatureDiscoveryInput) error {
	f.closeInput = input
	return f.closeErr
}

func guidedRouter(wayfinderService WayfinderService, authorityService AuthorityService, completionService CompletionService, guidedService GuidedService) http.Handler {
	router := chi.NewRouter()
	MountWorkspaceRoutes(router, NewWorkspaceHandlerWithGuided(wayfinderService, authorityService, completionService, guidedService))
	return router
}

func TestGuidedGetProjectsSemanticStateWithExactlyOnePrimaryAndNoArtifactFields(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", FeatureSlug: "payments", State: "open", Version: 8}, Project: workflowstore.Project{ProjectID: "project-guided", Name: "Payments"}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateActive, Destination: featureapp.DiscoveryDestinationRequirements, Currentness: featureapp.DiscoveryCurrent, Rationale: "ready to close", CurrentRevisionID: "revision-secret"}, current: featureapp.FeatureCurrentnessDecision{Readiness: featureapp.FeatureCurrent, WorkspaceID: detail.Workspace.WorkspaceID, WorkspaceVersion: detail.Workspace.Version, Basis: "closure-packet:77/revision:88", HistoricalIdentity: "closure-packet:77/revision:88/authority:99/source:100"}}
	authority := &fakeAuthority{revisions: []featureapp.AuthorityRevisionDetail{{Revision: workflowstore.FeatureWorkspaceAuthorityRevision{ID: 41, RevisionNumber: 2}, Layers: []workflowstore.FeatureWorkspaceAuthorityLayer{{LayerKind: "requirements", ArtifactRowID: sql.NullInt64{Int64: 99, Valid: true}}}}}}
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, FeatureSlug: detail.Workspace.FeatureSlug, Version: detail.Workspace.Version}, Gates: []appoperations.FeatureCompletionGate{{Name: "closure", Ready: true}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, authority, completion, guided).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-guided/guided", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Guided struct {
			AvailableActions []struct {
				Primary bool `json:"primary"`
			} `json:"availableActions"`
			PrimaryAction string `json:"primaryAction"`
		} `json:"guided"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	primaryCount := 0
	for _, action := range body.Guided.AvailableActions {
		if action.Primary {
			primaryCount++
		}
	}
	if primaryCount != 1 || body.Guided.PrimaryAction != "close_discovery" {
		t.Fatalf("guided actions=%s", response.Body.String())
	}
	for _, forbidden := range []string{"artifactRowId", "artifactSha256", "approvalRowId", "revision-secret", "closure-packet:77/revision:88", "authority:99", "source:100"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatalf("guided response exposed %q: %s", forbidden, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), `"projectId":"project-guided"`) || !strings.Contains(response.Body.String(), `"destination":"requirements"`) {
		t.Fatalf("missing semantic projection: %s", response.Body.String())
	}
}

func TestGuidedProjectionProjectsRouteMaterialOpenAsBoolean(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", Version: 8}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateActive, Destination: featureapp.DiscoveryDestinationRequirements, CurrentRevisionID: "revision-guided", RouteMaterialOpen: []string{"ticket-1"}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, &fakeCompletion{}, guided).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feature-workspaces/workspace-guided/guided", nil))
	var body struct {
		Guided struct {
			Diagnostics struct {
				Discovery struct {
					RouteMaterialOpen bool `json:"routeMaterialOpen"`
				} `json:"discovery"`
			} `json:"diagnostics"`
		} `json:"guided"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || !body.Guided.Diagnostics.Discovery.RouteMaterialOpen {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGuidedActionDelegatesRevisionServerSideAndReturnsRefreshedProjection(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", FeatureSlug: "payments", State: "open", Version: 8}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateActive, Destination: featureapp.DiscoveryDestinationRequirements, CurrentRevisionID: "revision-server-owned"}, current: featureapp.FeatureCurrentnessDecision{Readiness: featureapp.FeatureCurrent}}
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, Gates: []appoperations.FeatureCompletionGate{{Name: "closure", Ready: true}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, completion, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"close_discovery","confirmation":true,"destination":"requirements"}`)))
	if response.Code != http.StatusOK || guided.closeInput.ExpectedRevisionID != "revision-server-owned" || guided.closeInput.ExpectedVersion != 8 || guided.closeInput.Destination != featureapp.DiscoveryDestinationRequirements || guided.closeInput.CreatedIdentity != "guided-operator" {
		t.Fatalf("status=%d input=%#v body=%s", response.Code, guided.closeInput, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"primaryAction":"close_discovery"`) {
		t.Fatalf("missing refreshed guided projection: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "revision-server-owned") {
		t.Fatalf("raw revision identifier leaked: %s", response.Body.String())
	}
}

func TestGuidedActionRequiresConfirmationAndPreservesTypedStaleHandling(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", Version: 8}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateActive, Destination: featureapp.DiscoveryDestinationRequirements, CurrentRevisionID: "revision-server-owned"}, current: featureapp.FeatureCurrentnessDecision{Readiness: featureapp.FeatureCurrent}, closeErr: featureapp.ErrDiscoveryStaleState}
	router := guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, &fakeCompletion{}, guided)
	missingConfirmation := httptest.NewRecorder()
	router.ServeHTTP(missingConfirmation, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"close_discovery","destination":"requirements"}`)))
	if missingConfirmation.Code != http.StatusBadRequest || guided.closeInput != (featureapp.CloseFeatureDiscoveryInput{}) {
		t.Fatalf("confirmation status=%d input=%#v body=%s", missingConfirmation.Code, guided.closeInput, missingConfirmation.Body.String())
	}
	falseConfirmation := httptest.NewRecorder()
	router.ServeHTTP(falseConfirmation, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"close_discovery","confirmation":false,"destination":"requirements"}`)))
	if falseConfirmation.Code != http.StatusBadRequest || guided.closeInput != (featureapp.CloseFeatureDiscoveryInput{}) {
		t.Fatalf("false confirmation status=%d input=%#v body=%s", falseConfirmation.Code, guided.closeInput, falseConfirmation.Body.String())
	}
	stale := httptest.NewRecorder()
	router.ServeHTTP(stale, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"close_discovery","confirmation":true}`)))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), `"error":"VERSION_CONFLICT"`) {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestGuidedActionBlocksNonPrimaryActionWithoutMutation(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", Version: 8}}
	guided := &fakeGuided{assessment: GuidedAssessment{
		State:             featureapp.DiscoveryStateActive,
		Destination:       featureapp.DiscoveryDestinationRequirements,
		CurrentRevisionID: "revision-guided",
		Blockers:          []string{"ticket-1"},
	}}
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, Gates: []appoperations.FeatureCompletionGate{{Name: "closure", Ready: true}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, completion, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"close_discovery","confirmation":true}`)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"GUIDED_ACTION_BLOCKED"`) || guided.closeInput != (featureapp.CloseFeatureDiscoveryInput{}) || guided.recordInput != (featureapp.RecordDiscoveryDestinationAssessmentInput{}) || completion.input != (featureapp.CompletionInput{}) {
		t.Fatalf("status=%d close=%#v record=%#v completion=%#v body=%s", response.Code, guided.closeInput, guided.recordInput, completion.input, response.Body.String())
	}
}

func TestGuidedActionCannotCompleteWhileDiscoveryPrimaryIsActive(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", Version: 8}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateActive, CurrentRevisionID: "revision-guided"}}
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, Gates: []appoperations.FeatureCompletionGate{{Name: "closure", Ready: true}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, completion, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"complete_feature","confirmation":true}`)))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"GUIDED_ACTION_BLOCKED"`) || completion.input != (featureapp.CompletionInput{}) {
		t.Fatalf("status=%d completion=%#v body=%s", response.Code, completion.input, response.Body.String())
	}
}

func TestGuidedCompletionReturnsRefreshedRecordedState(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", Version: 9}}
	guided := &fakeGuided{assessment: GuidedAssessment{State: featureapp.DiscoveryStateClosed, Currentness: featureapp.DiscoveryCurrent}, current: featureapp.FeatureCurrentnessDecision{Readiness: featureapp.FeatureCurrent}}
	completion := &fakeCompletion{status: appoperations.FeatureCompletionStatus{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, Version: 9}, Gates: []appoperations.FeatureCompletionGate{{Name: "closure", Ready: true}}}, result: appoperations.FeatureCompletionResult{Workspace: appoperations.FeatureCompletionWorkspace{WorkspaceID: detail.Workspace.WorkspaceID, Version: 10}, Decision: appoperations.FeatureCompletionDecision{CompletionDecisionID: "decision-guided", Decision: "completed"}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, completion, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":9,"action":"complete_feature","confirmation":true}`)))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"recorded":true`) || !strings.Contains(response.Body.String(), `"version":10`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

type richFakeGuided struct {
	fakeGuided
	projection featureapp.GuidedFeatureProjection
	result     featureapp.GuidedActionResult
	input      featureapp.GuidedActionInput
}

func (f *richFakeGuided) ReadGuidedProjection(context.Context, string) (featureapp.GuidedFeatureProjection, error) {
	return f.projection, nil
}
func (f *richFakeGuided) ExecuteGuidedAction(_ context.Context, input featureapp.GuidedActionInput) (featureapp.GuidedActionResult, error) {
	f.input = input
	return f.result, nil
}

func TestGuidedRichHandoffDelegatesInsteadOfNoOp(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", FeatureSlug: "payments", Version: 8}}
	guided := &richFakeGuided{projection: featureapp.GuidedFeatureProjection{Workspace: featureapp.GuidedWorkspaceSection{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, PrimaryAction: featureapp.GuidedFeatureActionAvailability{Action: featureapp.GuidedActionAuthorRequirements, Primary: true, Enabled: true}, Handoff: &featureapp.GuidedHandoff{Role: "author_requirements", ResumeRoute: "/feature-workspaces/{workspaceID}/guided", Summary: "review"}}, result: featureapp.GuidedActionResult{Projection: featureapp.GuidedFeatureProjection{Workspace: featureapp.GuidedWorkspaceSection{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, PrimaryAction: featureapp.GuidedFeatureActionAvailability{Action: featureapp.GuidedActionAuthorRequirements, Primary: true, Enabled: true}, Handoff: &featureapp.GuidedHandoff{Role: "author_requirements", ResumeRoute: "/feature-workspaces/{workspaceID}/guided", Summary: "review"}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, &fakeCompletion{}, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"author_requirements"}`)))
	if response.Code != http.StatusOK || guided.input.Action != "author_requirements" || !strings.Contains(response.Body.String(), `"resumeRoute":"/feature-workspaces/{workspaceID}/guided"`) || !strings.Contains(response.Body.String(), `"handoff"`) {
		t.Fatalf("status=%d input=%#v body=%s", response.Code, guided.input, response.Body.String())
	}
}

func TestGuidedReopenTransportCarriesNoClientDigest(t *testing.T) {
	detail := wayfinder.WorkspaceDetail{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: "workspace-guided", FeatureSlug: "payments", Version: 8}}
	guided := &richFakeGuided{projection: featureapp.GuidedFeatureProjection{Workspace: featureapp.GuidedWorkspaceSection{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, PrimaryAction: featureapp.GuidedFeatureActionAvailability{Action: featureapp.GuidedActionReopenDiscovery, Primary: true, Enabled: true, RequiresConfirmation: true}}, result: featureapp.GuidedActionResult{Projection: featureapp.GuidedFeatureProjection{Workspace: featureapp.GuidedWorkspaceSection{WorkspaceID: detail.Workspace.WorkspaceID, Version: detail.Workspace.Version}, PrimaryAction: featureapp.GuidedFeatureActionAvailability{Action: featureapp.GuidedActionReopenDiscovery, Primary: true, Enabled: true}}}}
	response := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, &fakeCompletion{}, guided).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"reopen_discovery","confirmation":true,"cause":"new exact evidence","markdown":"# Reopened discovery\n"}`)))
	if response.Code != http.StatusOK || guided.input.Action != "reopen_discovery" || !guided.input.Confirmation || guided.input.Cause != "new exact evidence" || string(guided.input.Markdown) != "# Reopened discovery\n" {
		t.Fatalf("status=%d input=%#v body=%s", response.Code, guided.input, response.Body.String())
	}
	// The guided request contract rejects any client-supplied digest outright.
	rejected := httptest.NewRecorder()
	guidedRouter(&fakeWayfinder{detail: detail}, &fakeAuthority{}, &fakeCompletion{}, guided).ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/feature-workspaces/workspace-guided/guided/actions", strings.NewReader(`{"expectedVersion":8,"action":"reopen_discovery","confirmation":true,"cause":"new exact evidence","markdown":"# Reopened discovery\n","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)))
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("client digest status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}
