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
	prepare    appaudits.PrepareWorkflowAuditResult
	prepareErr error
	status     appaudits.WorkflowAuditStatus
}

func (f *fakeWorkflowAuditService) Prepare(context.Context, appaudits.PrepareWorkflowAuditInput) (appaudits.PrepareWorkflowAuditResult, error) {
	return f.prepare, f.prepareErr
}
func (f *fakeWorkflowAuditService) GetStatus(context.Context, string) (appaudits.WorkflowAuditStatus, error) {
	return f.status, nil
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
	request := httptest.NewRequest(http.MethodPost, "/workflow/runs/run-test/audit/prepare", strings.NewReader(`{"audited_commit":"`+strings.Repeat("b", 40)+`"}`))
	response := httptest.NewRecorder()
	workflowAuditRouter(service).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "packet-test") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestWorkflowAuditPrepareMapsStaleToConflict(t *testing.T) {
	service := &fakeWorkflowAuditService{prepareErr: appaudits.ErrWorkflowAuditPacketStale}
	request := httptest.NewRequest(http.MethodPost, "/workflow/runs/run-test/audit/prepare", strings.NewReader(`{"audited_commit":"`+strings.Repeat("b", 40)+`"}`))
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
	workflowAuditRouter(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/workflow/runs/run-test/audit/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "current_packet") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

var _ = errors.Is
