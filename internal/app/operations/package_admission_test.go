package operations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/packages"
	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testfixtures"
)

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

func TestPackageWorkflowPrepareDelegatesDirectly(t *testing.T) {
	store, err := workflowstore.Open(filepath.Join(t.TempDir(), "workflow.sqlite"), filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner := &fakePackageWorkflowOwner{detail: packages.Detail{Package: workflowstore.ExecutionPackage{PackageID: "package-1", PackageSha256: strings.Repeat("a", 64)}}}
	service, err := NewPackageWorkflowService(owner, fakeMutationLeaseReconciler{}, store)
	if err != nil {
		t.Fatal(err)
	}
	input := packages.PrepareInput{
		SelectionID:       "selection-1",
		TicketDesignBrief: packages.ArtifactInput{DisplayName: "feature.ticket-T1.r1.design-brief.md", ExpectedSHA256: strings.Repeat("b", 64), Bytes: []byte(testfixtures.TicketDesignBrief)},
	}
	if _, err := service.Prepare(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !owner.prepared {
		t.Fatalf("prepare owner=%t", owner.prepared)
	}
}
