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
	return f.status, nil
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

func TestWorkflowAuditPacketReturnsExactTicketObligations(t *testing.T) {
	service := &fakeWorkflowAuditService{
		current: appaudits.GetWorkflowAuditPacketResult{
			Run:         workflowstore.Run{RunID: "run-test", Status: workflowstore.RunStatusAuditReady},
			Packet:      workflowstore.AuditPacket{AuditPacketID: "packet-test", Status: workflowstore.AuditPacketStatusCurrent},
			PacketBytes: []byte(`{"schema_version":"2.0","artifacts":[{"artifact_reference":"artifact-ticket-package","artifact_type":"ticket_package_evidence"}]}`),
		},
		artifact: appaudits.GetWorkflowAuditArtifactResult{Content: []byte(`{
			"schema_version":"1.0",
			"package":{"package_id":"package-1","package_sha256":"sha-package","workspace_id":"workspace-1","feature_slug":"feature","selection_id":"selection-1","selection_state":"consumed","authority":{"authority_revision_id":"authority-1","sha256":"sha-authority"},"source":{"closure_id":"closure-1","commit_oid":"commit-1"}},
			"tickets":[{"sequence":1,"ticket_id":"T1","delivery_ticket_revision_row_id":2,"revision_number":1,"member_sha256":"sha-member","approval":{"approval_id":"approval-1","approval_basis_sha256":"sha-approval","authority_revision_row_id":3,"source_closure_row_id":4},"design_brief":{"artifact_reference":"brief-1","sha256":"sha-brief"}}],
			"mutation_leases":[],"bundle_integration":{"run_id":"run-test","execution_package_id":"package-1","selection_id":"selection-1","selection_state":"consumed","approved_run_status":"package_linked"}
		}`)},
	}
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/runs/run-test/audit/packet", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ticketId":"T1"`) || !strings.Contains(response.Body.String(), `"packageId":"package-1"`) {
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

var _ = errors.Is
