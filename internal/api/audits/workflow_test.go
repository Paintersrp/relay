package audits

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appaudits "relay/internal/app/audits"
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type fakeWorkflowAuditService struct {
	prepare       appaudits.PrepareWorkflowAuditResult
	prepareErr    error
	status        appaudits.WorkflowAuditStatus
	statusErr     error
	current       appaudits.GetWorkflowAuditPacketResult
	currentErr    error
	artifact      appaudits.GetWorkflowAuditArtifactResult
	artifactErr   error
	decision      appaudits.RecordWorkflowAuditDecisionResult
	decisionErr   error
	decisionInput appaudits.RecordWorkflowAuditDecisionInput
}

func (f *fakeWorkflowAuditService) Prepare(context.Context, appaudits.PrepareWorkflowAuditInput) (appaudits.PrepareWorkflowAuditResult, error) {
	return f.prepare, f.prepareErr
}
func (f *fakeWorkflowAuditService) GetStatus(context.Context, string) (appaudits.WorkflowAuditStatus, error) {
	return f.status, f.statusErr
}
func (f *fakeWorkflowAuditService) GetCurrentPacket(context.Context, string) (appaudits.GetWorkflowAuditPacketResult, error) {
	return f.current, f.currentErr
}
func (f *fakeWorkflowAuditService) GetCurrentArtifact(context.Context, appaudits.GetWorkflowAuditArtifactInput) (appaudits.GetWorkflowAuditArtifactResult, error) {
	return f.artifact, f.artifactErr
}
func (f *fakeWorkflowAuditService) RecordDecision(_ context.Context, input appaudits.RecordWorkflowAuditDecisionInput) (appaudits.RecordWorkflowAuditDecisionResult, error) {
	f.decisionInput = input
	return f.decision, f.decisionErr
}

func workflowAuditRouter(service WorkflowAuditService) http.Handler {
	router := chi.NewRouter()
	MountWorkflowRoutes(router, NewWorkflowHandler(service))
	return router
}

func TestWorkflowAuditPrepareReturnsPacketIdentity(t *testing.T) {
	service := &fakeWorkflowAuditService{prepare: appaudits.PrepareWorkflowAuditResult{
		Run: workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusAuditReady},
		Packet: workflowstore.AuditPacket{
			AuditPacketID: "packet-test",
			AuditedCommit: strings.Repeat("b", 40),
			PacketSHA256:  strings.Repeat("c", 64),
			Status:        workflowstore.AuditPacketStatusCurrent,
		},
		Artifact: workflowstore.Artifact{ArtifactID: "artifact-test", Kind: "audit_packet", SHA256: strings.Repeat("c", 64), SizeBytes: 10},
	}}
	request := httptest.NewRequest(http.MethodPost, "/runs/run-test/audit/prepare", strings.NewReader(`{"auditedCommit":"`+strings.Repeat("b", 40)+`"}`))
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated ||
		!strings.Contains(response.Body.String(), `"auditPacketId":"packet-test"`) ||
		!strings.Contains(response.Body.String(), `"/api/artifacts/artifact-test/content"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowAuditPrepareMapsStaleToConflict(t *testing.T) {
	service := &fakeWorkflowAuditService{prepareErr: appaudits.ErrWorkflowAuditPacketStale}
	request := httptest.NewRequest(http.MethodPost, "/runs/run-test/audit/prepare", strings.NewReader(`{"auditedCommit":"`+strings.Repeat("b", 40)+`"}`))
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "AUDIT_CONFLICT") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowAuditStatusReturnsCurrentAndLatest(t *testing.T) {
	packet := workflowstore.AuditPacket{AuditPacketID: "packet-test", Status: workflowstore.AuditPacketStatusCurrent}
	service := &fakeWorkflowAuditService{status: appaudits.WorkflowAuditStatus{
		RunID: "run-test", RunStatus: workflowstore.RunStatusAuditReady,
		CurrentPacket: &packet, LatestPacket: &packet,
	}}
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/run-test/audit/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"currentPacket"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowAuditPacketReturnsPackageDocumentObject(t *testing.T) {
	service := &fakeWorkflowAuditService{current: appaudits.GetWorkflowAuditPacketResult{
		Run:      workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusAuditReady},
		Packet:   workflowstore.AuditPacket{AuditPacketID: "packet-test", Status: workflowstore.AuditPacketStatusCurrent},
		Document: []byte(`{"schema_version":"3.0","run":{"run_id":1}}`),
	}}
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/run-test/audit/packet", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"document":{"schema_version":"3.0"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowAuditDecisionRequiresExactConfirmationInput(t *testing.T) {
	service := &fakeWorkflowAuditService{decision: appaudits.RecordWorkflowAuditDecisionResult{
		Run:              workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusNeedsRevision},
		Packet:           workflowstore.AuditPacket{AuditPacketID: "packet-test", PacketSHA256: strings.Repeat("c", 64)},
		Decision:         workflowstore.AuditDecision{AuditDecisionID: "decision-test", AuditedCommit: strings.Repeat("b", 40), PacketSHA256: strings.Repeat("c", 64), Decision: workflowstore.AuditDecisionNeedsRevision, Rationale: "revision required"},
		RemediationSeeds: []workflowstore.AuditRemediationSeed{{RemediationSeedID: "seed-test", AuditPacketRowID: 1, ExecutionPackageRowID: 2, AuditedCommit: strings.Repeat("b", 40)}},
	}}
	body := `{"auditPacketId":"packet-test","packetSha256":"` + strings.Repeat("c", 64) + `","auditedCommit":"` + strings.Repeat("b", 40) + `","decision":"needs_revision","rationale":"revision required","materialFindings":[{"source":"both","summary":"missing proof","evidence":"packet evidence","required_remediation":"supply proof"}],"observations":["non-blocking"],"operatorConfirmed":true}`
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/runs/run-test/audit/decision", strings.NewReader(body)))
	if response.Code != http.StatusCreated || !service.decisionInput.OperatorConfirmed || len(service.decisionInput.MaterialFindings) != 1 || !strings.Contains(response.Body.String(), `"remediationSeedId":"seed-test"`) {
		t.Fatalf("response = %d input = %#v body = %s", response.Code, service.decisionInput, response.Body.String())
	}
}

func TestWorkflowAuditDecisionForwardsFindingSourcesUnchanged(t *testing.T) {
	for _, source := range []string{"implementation", "governing_package", "both"} {
		t.Run(source, func(t *testing.T) {
			service := &fakeWorkflowAuditService{decision: appaudits.RecordWorkflowAuditDecisionResult{Run: workflowstore.Run{RunID: "run-test"}, Packet: workflowstore.AuditPacket{AuditPacketID: "packet-test"}, Decision: workflowstore.AuditDecision{AuditDecisionID: "decision-test"}}}
			body := `{"auditPacketId":"packet-test","packetSha256":"` + strings.Repeat("c", 64) + `","auditedCommit":"` + strings.Repeat("b", 40) + `","decision":"needs_revision","rationale":"revision required","materialFindings":[{"source":"` + source + `","summary":"missing proof","evidence":"packet evidence","required_remediation":"supply proof"}],"operatorConfirmed":true}`
			response := httptest.NewRecorder()
			workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/runs/run-test/audit/decision", strings.NewReader(body)))
			if response.Code != http.StatusCreated {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			want := appaudits.WorkflowAuditMaterialFinding{Source: source, Summary: "missing proof", Evidence: "packet evidence", RequiredRemediation: "supply proof"}
			if len(service.decisionInput.MaterialFindings) != 1 || service.decisionInput.MaterialFindings[0] != want {
				t.Fatalf("received findings = %#v, want %#v", service.decisionInput.MaterialFindings, want)
			}
		})
	}
}

func TestWorkflowAuditEndpointsMapPackageRequired(t *testing.T) {
	packageRequired := appaudits.ErrWorkflowAuditPackageRequired
	tests := []struct {
		name, method, path, body string
	}{
		{"prepare", http.MethodPost, "/runs/run-test/audit/prepare", `{"auditedCommit":"` + strings.Repeat("b", 40) + `"}`},
		{"status", http.MethodGet, "/runs/run-test/audit/status", ""},
		{"packet", http.MethodGet, "/runs/run-test/audit/packet", ""},
		{"decision", http.MethodPost, "/runs/run-test/audit/decision", `{"auditPacketId":"packet","packetSha256":"` + strings.Repeat("c", 64) + `","auditedCommit":"` + strings.Repeat("b", 40) + `","decision":"accepted","rationale":"accepted","operatorConfirmed":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeWorkflowAuditService{prepareErr: packageRequired, statusErr: packageRequired, currentErr: packageRequired, decisionErr: packageRequired}
			response := httptest.NewRecorder()
			workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"AUDIT_PACKAGE_REQUIRED"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

var _ = errors.Is
