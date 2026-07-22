package cutover

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	appcutover "relay/internal/app/cutover"
)

type stubReadService struct{}

func (stubReadService) State(context.Context) (*appcutover.State, bool, error) {
	return nil, false, nil
}
func (stubReadService) Readiness(context.Context, string) (*appcutover.Readiness, error) {
	return &appcutover.Readiness{}, nil
}
func (stubReadService) History(context.Context) ([]appcutover.State, error) {
	return []appcutover.State{}, nil
}

type stubWorkflowService struct {
	prepared bool
}

func (service *stubWorkflowService) Prepare(context.Context, appcutover.PrepareRequest) (*appcutover.State, error) {
	service.prepared = true
	return &appcutover.State{ActivationID: "cutover-test", Status: "prepared"}, nil
}
func (*stubWorkflowService) Activate(context.Context, appcutover.ActivationRequest) (*appcutover.State, error) {
	return &appcutover.State{}, nil
}
func (*stubWorkflowService) Rollback(context.Context, appcutover.RollbackRequest) (*appcutover.State, error) {
	return &appcutover.State{}, nil
}
func (*stubWorkflowService) RecordRollForwardEvidence(context.Context, appcutover.RollForwardEvidenceRequest) error {
	return nil
}

func TestStateReturnsInactiveWhenNoActivation(t *testing.T) {
	handler := NewWorkflowHandler(stubReadService{}, &stubWorkflowService{})
	response := httptest.NewRecorder()
	handler.State(response, httptest.NewRequest(http.MethodGet, "/api/cutover/state", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("state status = %d", response.Code)
	}
}

func TestPrepareRejectsUnknownFields(t *testing.T) {
	service := &stubWorkflowService{}
	handler := NewWorkflowHandler(stubReadService{}, service)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/cutover/prepare", bytes.NewBufferString(`{"activationId":"cutover-test","unknown":true}`))
	handler.Prepare(response, request)
	if response.Code != http.StatusBadRequest || service.prepared {
		t.Fatalf("prepare response = %d prepared=%v", response.Code, service.prepared)
	}
}
