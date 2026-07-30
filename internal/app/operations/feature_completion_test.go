package operations

import (
	"context"
	"testing"

	featureapp "relay/internal/app/features"
	workflowstore "relay/internal/store/workflow"
)

type fakeFeatureCompletionOwner struct {
	evaluated string
	completed featureapp.CompletionInput
}

func (f *fakeFeatureCompletionOwner) EvaluateCompletion(_ context.Context, workspaceID string) (featureapp.CompletionStatus, error) {
	f.evaluated = workspaceID
	return featureapp.CompletionStatus{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: workspaceID}}, nil
}

func (f *fakeFeatureCompletionOwner) Complete(_ context.Context, input featureapp.CompletionInput) (featureapp.CompletionResult, error) {
	f.completed = input
	return featureapp.CompletionResult{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: input.WorkspaceID}}, nil
}

func TestFeatureCompletionDelegatesDirectlyAfterCutover(t *testing.T) {
	owner := &fakeFeatureCompletionOwner{}
	service, err := NewFeatureCompletionWorkflowService(owner)
	if err != nil {
		t.Fatal(err)
	}
	complete := featureapp.CompletionInput{WorkspaceID: "workspace-1", ExpectedVersion: 4, OperatorConfirmed: true}
	if _, err = service.Complete(context.Background(), complete); err != nil || owner.completed != complete {
		t.Fatalf("result err=%v owner=%#v", err, owner.completed)
	}
}
