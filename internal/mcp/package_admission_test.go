package mcp

import (
	"context"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	"relay/internal/operations/registry"
)

type fakePackageMCPAuthorizer struct{ request appoperations.MutationRequest }

func (f *fakePackageMCPAuthorizer) AuthorizeMutation(_ context.Context, request appoperations.MutationRequest) (appoperations.MutationAuthorization, error) {
	f.request = request
	return appoperations.MutationAuthorization{Allowed: true}, nil
}

func TestPackageOperationIdentityBindsPacketActionAndDependencies(t *testing.T) {
	identity := PackageOperationIdentity{
		MutationID: "mutation-1", ExpectedPacketID: "packet-1", OperationID: string(registry.LocalOperatorTicketWorkflowOperationID),
		Action: string(registry.PackageActionPrepare), SelectionID: "selection-1", PayloadSHA256: strings.Repeat("a", 64),
		RequiredDependencies: []TicketPacketDependency{{Class: "execution_package_selection", Key: "selection:selection-1"}},
	}
	first, err := identity.SemanticRequestSHA256()
	if err != nil || len(first) != 64 {
		t.Fatalf("semantic identity = %q, %v", first, err)
	}
	decoded, err := DecodePackageOperationIdentity([]byte(`{"mutation_id":"mutation-1","expected_packet_id":"packet-1","operation_id":"local_operator.ticket_workflow","action":"prepare_execution_package","selection_id":"selection-1","payload_sha256":"` + strings.Repeat("a", 64) + `","required_dependencies":[{"class":"execution_package_selection","key":"selection:selection-1"}]}`))
	if err != nil || decoded.SelectionID != identity.SelectionID {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
	packet := &fakePackageMCPAuthorizer{}
	service, err := appoperations.NewPackageAdmissionService(packet)
	if err != nil {
		t.Fatal(err)
	}
	admitter, err := NewPackagePacketAdmitter(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, digest, err := admitter.Admit(context.Background(), identity); err != nil || digest != first {
		t.Fatalf("admit digest=%q err=%v", digest, err)
	}
	if packet.request.Action != registry.PackageActionPrepare || packet.request.OperationID != registry.LocalOperatorTicketWorkflowOperationID {
		t.Fatalf("packet request = %#v", packet.request)
	}
}
