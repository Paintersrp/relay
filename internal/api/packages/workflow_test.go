package packages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	apppackages "relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type apiPacketAuthorizer struct{ request appoperations.MutationRequest }

func (f *apiPacketAuthorizer) AuthorizeMutation(_ context.Context, request appoperations.MutationRequest) (appoperations.MutationAuthorization, error) {
	f.request = request
	return appoperations.MutationAuthorization{Allowed: true}, nil
}

type apiPackageOwner struct{ detail apppackages.Detail }

func (f *apiPackageOwner) Prepare(_ context.Context, _ apppackages.PrepareInput) (apppackages.PrepareResult, error) {
	return apppackages.PrepareResult{Package: f.detail.Package}, nil
}
func (f *apiPackageOwner) Approve(_ context.Context, _ apppackages.ApproveInput) (apppackages.ApproveResult, error) {
	return apppackages.ApproveResult{Package: f.detail.Package}, nil
}
func (f *apiPackageOwner) Get(_ context.Context, _ string) (apppackages.Detail, error) {
	return f.detail, nil
}

type apiLeaseReconciler struct{}

func (apiLeaseReconciler) ReconcileMutationLease(context.Context, string) (executor.WorkflowMutationLeaseReconcileResult, error) {
	return executor.WorkflowMutationLeaseReconcileResult{Released: true}, nil
}

func TestPrepareRouteUsesPacketAdmittedPackageOwner(t *testing.T) {
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	packet := &apiPacketAuthorizer{}
	owner := &apiPackageOwner{detail: apppackages.Detail{Package: workflowstore.ExecutionPackage{PackageID: "package-api", PackageSha256: strings.Repeat("a", 64)}}}
	service, err := appoperations.NewPackageWorkflowService(packet, owner, apiLeaseReconciler{}, store)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewWorkflowHandler(service)
	body := `{"packetId":"packet-api","operationId":"local_operator.ticket_workflow","selectionId":"selection-api","ticketDesignBriefs":[{"displayName":"feature.ticket-T1.r1.design-brief.md","expectedSha256":"` + strings.Repeat("b", 64) + `","bytesBase64":"` + base64.StdEncoding.EncodeToString([]byte("# Brief\n")) + `"}],"executionSpec":{"displayName":"feature.execution-spec.json","expectedSha256":"` + strings.Repeat("c", 64) + `","bytesBase64":"` + base64.StdEncoding.EncodeToString([]byte("{}")) + `"},"requiredDependencies":[{"class":"execution_package_selection","key":"selection:selection-api"},{"class":"execution_package_execution_spec","key":"feature.execution-spec.json:` + strings.Repeat("c", 64) + `"},{"class":"execution_package_ticket_design_brief","key":"feature.ticket-T1.r1.design-brief.md:` + strings.Repeat("b", 64) + `"}]}`
	request := httptest.NewRequest(http.MethodPost, "/execution-packages", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.Prepare(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if packet.request.OperationID != registry.LocalOperatorTicketWorkflowOperationID || packet.request.Action != registry.PackageActionPrepare {
		t.Fatalf("packet request = %#v", packet.request)
	}
}

func TestLeaseViewCarriesOwnerRunIDWithoutRuntimeMetadata(t *testing.T) {
	raw, err := json.Marshal(appoperations.MutationLeaseView{LeaseID: "lease-retained", RunID: "run-blocked", OwnerRunID: "run-lease-owner"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"runId":"run-blocked"`) || !strings.Contains(text, `"ownerRunId":"run-lease-owner"`) {
		t.Fatalf("lease JSON = %s", text)
	}
	for _, forbidden := range []string{"ownerIdentity", "localPath", "process", "reconciliationNote"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("lease JSON exposed %q: %s", forbidden, text)
		}
	}
}
