package operations

import (
	"context"
	"testing"

	featureapp "relay/internal/app/features"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type fakeFeatureCompletionPackets struct{ request MutationRequest }

func (f *fakeFeatureCompletionPackets) AuthorizeMutation(_ context.Context, request MutationRequest) (MutationAuthorization, error) {
	f.request = request
	return MutationAuthorization{Allowed: true}, nil
}

type fakeFeatureCompletionOwner struct {
	completed featureapp.CompletionInput
}

func (f *fakeFeatureCompletionOwner) EvaluateCompletion(context.Context, string) (featureapp.CompletionStatus, error) {
	return featureapp.CompletionStatus{}, nil
}
func (f *fakeFeatureCompletionOwner) Complete(_ context.Context, input featureapp.CompletionInput) (featureapp.CompletionResult, error) {
	f.completed = input
	return featureapp.CompletionResult{Workspace: workflowstore.FeatureWorkspace{WorkspaceID: input.WorkspaceID, Version: input.ExpectedVersion + 1}}, nil
}

func TestFeatureCompletionAdmissionBindsPacketPayloadAndExactDependency(t *testing.T) {
	packets := &fakeFeatureCompletionPackets{}
	owner := &fakeFeatureCompletionOwner{}
	service, err := NewFeatureCompletionWorkflowService(packets, owner)
	if err != nil {
		t.Fatal(err)
	}
	complete := featureapp.CompletionInput{WorkspaceID: "workspace-1", ExpectedVersion: 4, OperatorConfirmed: true}
	payload, err := FeatureCompletionPayloadSHA256(complete)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), FeatureCompletionOperationInput{
		Admission: FeatureCompletionOperationRequest{
			PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID,
			Action: registry.FeatureCompletionActionComplete, WorkspaceID: complete.WorkspaceID,
			ExpectedVersion: complete.ExpectedVersion, PayloadSHA256: payload,
			RequiredDependencies: featureCompletionDependencies(complete),
		},
		Complete: complete,
	})
	if err != nil || packets.request.Action != registry.FeatureCompletionActionComplete ||
		len(packets.request.RequiredDependencies) != 1 || owner.completed != complete {
		t.Fatalf("result err=%v request=%#v owner=%#v", err, packets.request, owner.completed)
	}
}

func TestFeatureCompletionAdmissionRejectsAChangedWorkspaceDependency(t *testing.T) {
	complete := featureapp.CompletionInput{WorkspaceID: "workspace-1", ExpectedVersion: 4, OperatorConfirmed: true}
	payload, err := FeatureCompletionPayloadSHA256(complete)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateFeatureCompletionOperationRequest(FeatureCompletionOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID,
		Action: registry.FeatureCompletionActionComplete, WorkspaceID: complete.WorkspaceID,
		ExpectedVersion: complete.ExpectedVersion, PayloadSHA256: payload,
		RequiredDependencies: []DependencyRequirement{{Class: featureCompletionDependencyClass, Key: "workspace:workspace-1:version:3"}},
	})
	if err == nil {
		t.Fatal("changed completion dependency was accepted")
	}
}
