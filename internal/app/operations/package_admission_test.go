package operations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type fakePackagePacketAuthorizer struct{ request MutationRequest }

func (f *fakePackagePacketAuthorizer) AuthorizeMutation(_ context.Context, request MutationRequest) (MutationAuthorization, error) {
	f.request = request
	return MutationAuthorization{Allowed: true}, nil
}

type fakePackageWorkflowOwner struct {
	prepared bool
	approved bool
	detail   packages.Detail
}

func (f *fakePackageWorkflowOwner) Prepare(_ context.Context, _ packages.PrepareInput) (packages.PrepareResult, error) {
	f.prepared = true
	return packages.PrepareResult{Package: f.detail.Package}, nil
}
func (f *fakePackageWorkflowOwner) Approve(_ context.Context, _ packages.ApproveInput) (packages.ApproveResult, error) {
	f.approved = true
	return packages.ApproveResult{Package: f.detail.Package}, nil
}
func (f *fakePackageWorkflowOwner) Get(_ context.Context, _ string) (packages.Detail, error) {
	return f.detail, nil
}

type fakeMutationLeaseReconciler struct{}

func (fakeMutationLeaseReconciler) ReconcileMutationLease(context.Context, string) (executor.WorkflowMutationLeaseReconcileResult, error) {
	return executor.WorkflowMutationLeaseReconcileResult{Released: true}, nil
}

func TestPackageWorkflowPrepareRequiresExactPacketDependencies(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.sqlite"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	packet := &fakePackagePacketAuthorizer{}
	owner := &fakePackageWorkflowOwner{detail: packages.Detail{Package: workflowstore.ExecutionPackage{PackageID: "package-1", PackageSha256: strings.Repeat("a", 64)}}}
	service, err := NewPackageWorkflowService(packet, owner, fakeMutationLeaseReconciler{}, store)
	if err != nil {
		t.Fatal(err)
	}
	input := packages.PrepareInput{
		SelectionID:        "selection-1",
		TicketDesignBriefs: []packages.ArtifactInput{{DisplayName: "feature.ticket-T1.r1.design-brief.md", ExpectedSHA256: strings.Repeat("b", 64), Bytes: []byte("# Brief\n")}},
		ExecutionSpec:      packages.ArtifactInput{DisplayName: "feature.execution-spec.json", ExpectedSHA256: strings.Repeat("c", 64), Bytes: []byte("{}")},
	}
	payload, err := PackagePreparePayloadSHA256(input)
	if err != nil {
		t.Fatal(err)
	}
	request := PackageOperationRequest{
		PacketID: "packet-1", OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.PackageActionPrepare,
		SelectionID: input.SelectionID, PayloadSHA256: payload, RequiredDependencies: packagePrepareDependencies(input),
	}
	if _, err := service.Prepare(context.Background(), PackagePrepareOperationInput{Admission: request, Prepare: input}); err != nil {
		t.Fatal(err)
	}
	if !owner.prepared || packet.request.Action != registry.PackageActionPrepare || !sameDependencies(packet.request.RequiredDependencies, packagePrepareDependencies(input)) {
		t.Fatalf("prepare owner=%t packet=%#v", owner.prepared, packet.request)
	}

	owner.prepared = false
	request.RequiredDependencies = request.RequiredDependencies[:1]
	if _, err := service.Prepare(context.Background(), PackagePrepareOperationInput{Admission: request, Prepare: input}); err == nil || owner.prepared {
		t.Fatalf("inexact dependencies admitted: err=%v prepared=%t", err, owner.prepared)
	}
}

func TestPackageOperationRegistryHasOneLocalOperatorOwner(t *testing.T) {
	for _, action := range []registry.AllowedAction{registry.PackageActionPrepare, registry.PackageActionApprove, registry.MutationLeaseActionReconcile} {
		operation, ok := registry.PackageOperationForAction(action)
		if !ok || operation.OperationID != registry.LocalOperatorTicketWorkflowOperationID || operation.Role != "local_operator" {
			t.Fatalf("operation for %s = %#v, %t", action, operation, ok)
		}
	}
}
