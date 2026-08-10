package operations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type plannerDerivedFixture struct {
	lifecycleFixture
	closure   workflowstore.SourceVaultClosure
	workspace workflowstore.FeatureWorkspace
	route     workflowstore.FeatureWorkspaceRouteState
	run       workflowstore.Run
}

func newPlannerDerivedFixture(t *testing.T, withAuthority bool) plannerDerivedFixture {
	t.Helper()
	fixture := plannerDerivedFixture{lifecycleFixture: openLifecycleFixture(t)}
	revision, err := fixture.repositories.ResolveRevision(fixture.ctx, workflowrepos.RevisionRequest{RepoTarget: "project"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := fixture.service.vaults.ImportClosure(fixture.ctx, sourcevault.ImportRequest{Revision: revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.closure = imported.Closure
	project, err := fixture.store.GetProjectByProjectID(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		fixture.workspace, err = tx.CreateFeatureWorkspace(fixture.ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: "workspace-planner-derived", ProjectRowID: project.ID, FeatureSlug: "planner-derived"})
		if err != nil {
			return err
		}
		fixture.run, err = tx.CreateRun(fixture.ctx, workflowstore.CreateRunParams{RunID: "run-planner-derived", FeatureSlug: fixture.workspace.FeatureSlug, RepoTarget: "project", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: fixture.closure.CommitOID, CanonicalSHA256: strings.Repeat("a", 64)})
		if err != nil {
			return err
		}
		if withAuthority {
			authority, createErr := tx.CreateFeatureWorkspaceAuthorityRevision(fixture.ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: "authority-planner-derived", WorkspaceRowID: fixture.workspace.ID, RevisionNumber: 1, SourceClosureRowID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}})
			if createErr != nil {
				return createErr
			}
			fixture.workspace, err = tx.SetFeatureWorkspaceAuthorityRevision(fixture.ctx, authority.ID, fixture.workspace.WorkspaceID, fixture.workspace.Version)
			if err != nil {
				return err
			}
		}
		fixture.route, err = tx.CreateFeatureWorkspaceRouteState(fixture.ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: "route-planner-derived", WorkspaceRowID: fixture.workspace.ID, Sequence: 1, WorkspaceVersion: fixture.workspace.Version + 1, State: "ready"})
		if err != nil {
			return err
		}
		fixture.workspace, err = tx.AdvanceFeatureWorkspaceRouteState(fixture.ctx, fixture.route.ID, "open", fixture.workspace.WorkspaceID, fixture.workspace.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f plannerDerivedFixture) workflowReferences(t *testing.T, ticketID string) []semanticidentity.WorkflowReferenceRequest {
	t.Helper()
	references := []semanticidentity.WorkflowReferenceRequest{{Kind: "feature_workspace", WorkspaceID: f.workspace.WorkspaceID}}
	if ticketID != "" {
		references = append(references, semanticidentity.WorkflowReferenceRequest{Kind: "delivery_ticket", WorkspaceID: f.workspace.WorkspaceID, TicketID: ticketID})
	}
	return references
}

func plannerDerivedInlineInput(name, text string) (semanticidentity.InputBinding, []semanticidentity.AttestationRequest) {
	sha := lifecycleSHA([]byte(text))
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	return semanticidentity.InputBinding{InputName: name, SourceKind: "inline_text", DisplayName: name + ".txt", MediaType: "text/plain", ExpectedSHA256: sha, Source: semanticidentity.InputBindingSource{Text: text}}, []semanticidentity.AttestationRequest{{Kind: "confirmed_intent", InputName: name, SubjectSHA256: sha, Confirmed: true}, {Kind: "sensitive_data_clearance", InputName: name, Clearance: &clearance}}
}

func plannerDerivedArtifactInput(t *testing.T, f plannerDerivedFixture, name string) (semanticidentity.InputBinding, []semanticidentity.AttestationRequest) {
	t.Helper()
	artifact := createRemediationArtifact(t, f.store, f.ctx, f.run.ID, name, name, []byte(`{"fixture":"`+name+`"}`+"\n"))
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: artifact.SHA256, Confirmed: true}
	return semanticidentity.InputBinding{InputName: name, SourceKind: "relay_artifact", DisplayName: name + ".json", MediaType: "application/json", ExpectedSHA256: artifact.SHA256, Source: semanticidentity.InputBindingSource{ArtifactID: artifact.ArtifactID}}, []semanticidentity.AttestationRequest{{Kind: "approved_artifact", InputName: name, SubjectSHA256: artifact.SHA256, Approved: true}, {Kind: "sensitive_data_clearance", InputName: name, Clearance: &clearance}}
}

func createPlannerDerivedPacket(t *testing.T, f plannerDerivedFixture, operationID registry.OperationID, ticketID string) CreateLifecycleResult {
	t.Helper()
	operation, ok := registry.Lookup(operationID)
	if !ok {
		t.Fatal("operation missing")
	}
	identity := semanticidentity.CreateOperationPacket{SurfaceContract: operation.SurfaceContract, OperationID: operationID, ProjectID: f.projectID, WorkflowReferences: f.workflowReferences(t, ticketID)}
	switch operationID {
	case "planner.delivery_ticket":
		input, attestations := plannerDerivedInlineInput("confirmed_delivery_boundary", "Author the exact delivery boundary.")
		identity.Inputs, identity.Attestations = []semanticidentity.InputBinding{input}, attestations
	case "planner.transition_plan":
		input, attestations := plannerDerivedArtifactInput(t, f, "selected_delivery_ticket")
		identity.Inputs, identity.Attestations = []semanticidentity.InputBinding{input}, attestations
	}
	result, err := f.service.Create(f.ctx, CreateLifecycleInput{MutationID: "create-" + string(operationID), Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readPlannerDerivedInput(t *testing.T, f plannerDerivedFixture, result CreateLifecycleResult, name string) ([]byte, packet.InputBinding, workflowstore.OperationPacketArtifactBinding) {
	t.Helper()
	var document struct {
		Inputs []struct {
			InputName       string                   `json:"input_name"`
			InputRole       registry.InputRole       `json:"input_role"`
			SourceKind      registry.InputSourceKind `json:"source_kind"`
			DisplayName     string                   `json:"display_name"`
			MediaType       string                   `json:"media_type"`
			SHA256          string                   `json:"sha256"`
			SizeBytes       int64                    `json:"size_bytes"`
			AttestationKind registry.AttestationKind `json:"attestation_kind"`
			Source          struct {
				Kind       registry.InputSourceKind `json:"kind"`
				ArtifactID string                   `json:"artifact_id"`
			} `json:"source"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(result.Packet.DocumentBytes, &document); err != nil {
		t.Fatal(err)
	}
	var input packet.InputBinding
	for _, candidate := range document.Inputs {
		if candidate.InputName == name {
			input = packet.InputBinding{InputName: candidate.InputName, InputRole: candidate.InputRole, SourceKind: candidate.SourceKind, DisplayName: candidate.DisplayName, MediaType: candidate.MediaType, SHA256: candidate.SHA256, SizeBytes: candidate.SizeBytes, AttestationKind: candidate.AttestationKind, Source: packet.InputSource{Kind: candidate.Source.Kind, ArtifactID: candidate.Source.ArtifactID}}
			break
		}
	}
	if input.InputName == "" {
		t.Fatalf("missing input %q", name)
	}
	publication, err := f.store.GetOperationPacketPublicationByPacketID(f.ctx, result.Packet.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	integrity, err := f.store.GetOperationPacketPublicationIntegrity(f.ctx, publication.PublicationID)
	if err != nil {
		t.Fatal(err)
	}
	var retained workflowstore.OperationPacketRetainedArtifact
	var binding workflowstore.OperationPacketArtifactBinding
	for _, candidate := range integrity.RetainedArtifacts {
		if candidate.ArtifactID == input.Source.ArtifactID {
			retained = candidate
			break
		}
	}
	for _, candidate := range integrity.Bindings {
		if candidate.DependencyKey == name {
			binding = candidate
			break
		}
	}
	data, err := os.ReadFile(filepath.Join(f.store.ArtifactStore().Root(), filepath.FromSlash(retained.RelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if retained.Kind != workflowstore.OperationPacketRetainedArtifactWorkflowSnapshot || retained.MediaType != "application/json" || retained.ArtifactID != input.Source.ArtifactID || retained.SHA256 != input.SHA256 || retained.SizeBytes != input.SizeBytes || lifecycleSHA(data) != input.SHA256 || binding.DependencyClass != workflowstore.OperationPacketDependencyWorkflowSnapshot || binding.DependencyKey != name {
		t.Fatalf("input=%#v retained=%#v binding=%#v", input, retained, binding)
	}
	return data, input, binding
}

func TestLifecycleDeliveryTicketMaterializesCurrentFeatureWorkspaceRoute(t *testing.T) {
	f := newPlannerDerivedFixture(t, false)
	result := createPlannerDerivedPacket(t, f, "planner.delivery_ticket", "")
	data, input, _ := readPlannerDerivedInput(t, f, result, "current_feature_workspace_route")
	expected, _ := canonicalJSON(featureWorkspaceRouteInput{f.workspace.WorkspaceID, f.workspace.FeatureSlug, f.workspace.Version, "open", f.route.RouteStateID, f.route.Sequence, f.route.WorkspaceVersion, "ready"})
	if !bytes.Equal(data, expected) || input.SourceKind != packet.InputSourceInlineText || input.InputRole != "evidence" || input.AttestationKind != "derived_authority" {
		t.Fatalf("input=%#v data=%s", input, data)
	}
}

func TestLifecycleTicketFrontierMaterializesCurrentFeatureWorkspaceRoute(t *testing.T) {
	f := newPlannerDerivedFixture(t, false)
	result := createPlannerDerivedPacket(t, f, "planner.ticket_frontier", "")
	if result.Replay || result.Packet.Summary.OperationID != "planner.ticket_frontier" || result.Packet.Summary.SurfaceContract != "planner-ticket-frontier.v1" || result.Packet.Summary.Role != "planner" {
		t.Fatalf("frontier packet = %#v", result)
	}
	mutation, err := f.store.GetMCPMutationResult(f.ctx, workflowstore.MCPMutationKey{SurfaceContractID: "planner-ticket-frontier.v1", ToolName: string(registry.MutationToolCreateOperationPacket), MutationID: "create-planner.ticket_frontier"})
	if err != nil || mutation.SurfaceContractID != "planner-ticket-frontier.v1" || mutation.ToolName != string(registry.MutationToolCreateOperationPacket) || mutation.MutationID != "create-planner.ticket_frontier" || mutation.ResultIdentityJSON != string(result.Mutation.ResultIdentityJSON) || mutation.ResultSHA256 != result.Mutation.ResultSHA256 {
		t.Fatalf("persisted frontier mutation = %#v, err=%v", mutation, err)
	}
	data, _, _ := readPlannerDerivedInput(t, f, result, "current_feature_workspace_route")
	expected, _ := canonicalJSON(featureWorkspaceRouteInput{f.workspace.WorkspaceID, f.workspace.FeatureSlug, f.workspace.Version, "open", f.route.RouteStateID, f.route.Sequence, f.route.WorkspaceVersion, "ready"})
	if !bytes.Equal(data, expected) {
		t.Fatalf("data=%s want=%s", data, expected)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(result.Packet.DocumentBytes, &document); err != nil {
		t.Fatal(err)
	}
	var relaySpecs struct {
		RepositoryKey string `json:"repository_key"`
		CommitOID     string `json:"commit_oid"`
	}
	var manifestDomain struct {
		Domain  string            `json:"domain"`
		Members []json.RawMessage `json:"members"`
	}
	if err := json.Unmarshal(document["relay_specs"], &relaySpecs); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(document["manifest_domain"], &manifestDomain); err != nil {
		t.Fatal(err)
	}
	if relaySpecs.RepositoryKey != "" || relaySpecs.CommitOID != "" || manifestDomain.Domain != "" || len(manifestDomain.Members) != 0 {
		t.Fatalf("manifestless governance was fabricated: %s %s", document["relay_specs"], document["manifest_domain"])
	}
	var before int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM operation_packets`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	operation, _ := registry.Lookup("planner.ticket_frontier")
	replay, err := f.service.Create(f.ctx, CreateLifecycleInput{MutationID: "create-planner.ticket_frontier", Identity: semanticidentity.CreateOperationPacket{SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID, ProjectID: f.projectID, WorkflowReferences: f.workflowReferences(t, "")}})
	if err != nil || !replay.Replay || replay.Packet.Summary.PacketID != result.Packet.Summary.PacketID {
		t.Fatalf("frontier replay = %#v, err=%v", replay, err)
	}
	var after int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM operation_packets`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("replay published a second packet: before=%d after=%d", before, after)
	}
}

func createPlannerTicket(t *testing.T, f plannerDerivedFixture, applicability string, selectTicket bool) (workflowstore.DeliveryTicket, workflowstore.DeliveryTicketRevision) {
	t.Helper()
	var ticket workflowstore.DeliveryTicket
	var revision workflowstore.DeliveryTicketRevision
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateDeliveryTicket(f.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-PLANNER-DERIVED", WorkspaceRowID: f.workspace.ID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err = tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID, SourceClosureRowID: f.closure.ID, SourcePath: "tickets/planner-derived.json", Goal: "Author the selected ticket.", Context: "Exact current authority.", TransitionApplicability: applicability})
		if err != nil {
			return err
		}
		if _, err = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, revision.ID); err != nil || !selectTicket {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(f.ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{ApprovalID: "approval-planner-derived", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved", Rationale: "Exact revision approved.", SourceClosureRowID: f.closure.ID, AuthorityRevisionRowID: f.workspace.CurrentAuthorityRevisionRowID})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(f.ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-planner-derived", WorkspaceRowID: f.workspace.ID, State: "active", Rationale: "Select exact revision.", SourceClosureRowID: sql.NullInt64{Int64: f.closure.ID, Valid: true}})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(f.ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ticket, revision
}

func TestLifecycleTransitionPlanMaterializesCurrentTransitionApplicability(t *testing.T) {
	f := newPlannerDerivedFixture(t, false)
	ticket, revision := createPlannerTicket(t, f, "required", false)
	result := createPlannerDerivedPacket(t, f, "planner.transition_plan", ticket.TicketID)
	data, input, _ := readPlannerDerivedInput(t, f, result, "current_transition_applicability")
	expected, _ := canonicalJSON(transitionApplicabilityInput{f.workspace.WorkspaceID, f.workspace.FeatureSlug, f.workspace.Version, ticket.TicketID, revision.ID, revision.RevisionNumber, f.closure.ClosureID, "required"})
	if !bytes.Equal(data, expected) || input.InputRole != "authority" || input.AttestationKind != "derived_authority" {
		t.Fatalf("input=%#v data=%s", input, data)
	}

	t.Run("not_required", func(t *testing.T) {
		invalid := newPlannerDerivedFixture(t, false)
		invalidTicket, _ := createPlannerTicket(t, invalid, "not_required", false)
		assertPlannerCreateInvalid(t, invalid, "planner.transition_plan", invalidTicket.TicketID)
	})
}

func assertPlannerCreateInvalid(t *testing.T, f plannerDerivedFixture, operationID registry.OperationID, ticketID string) {
	t.Helper()
	operation, _ := registry.Lookup(operationID)
	identity := semanticidentity.CreateOperationPacket{SurfaceContract: operation.SurfaceContract, OperationID: operationID, ProjectID: f.projectID, WorkflowReferences: f.workflowReferences(t, ticketID)}
	input, attestations := plannerDerivedArtifactInput(t, f, "selected_delivery_ticket")
	identity.Inputs, identity.Attestations = []semanticidentity.InputBinding{input}, attestations
	_, err := f.service.Create(f.ctx, CreateLifecycleInput{MutationID: "invalid-" + string(operationID), Identity: identity})
	var lifecycleErr *Error
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Code != CodeInvalidPacketDocument {
		t.Fatalf("err=%v", err)
	}
}

func TestCurrentPlannerDerivedInputRevalidationRejectsAuthorityDrift(t *testing.T) {
	t.Run("workspace_route_changed", func(t *testing.T) {
		f := newPlannerDerivedFixture(t, false)
		operation, _ := registry.Lookup("planner.delivery_ticket")
		workflow := resolvedPlannerWorkflow(t, f, "")
		prepared, err := f.service.materializeDerivedInputs(f.ctx, operation, workflow, &retainedBuilder{ids: f.service.ids})
		if err != nil {
			t.Fatal(err)
		}
		advancePlannerRoute(t, &f, "ready")
		assertPlannerRevalidationInvalid(t, f.ctx, f.service, operation, workflow, prepared)
	})

	t.Run("ticket_revision_changed", func(t *testing.T) {
		f := newPlannerDerivedFixture(t, false)
		ticket, _ := createPlannerTicket(t, f, "required", false)
		operation, _ := registry.Lookup("planner.transition_plan")
		workflow := resolvedPlannerWorkflow(t, f, ticket.TicketID)
		prepared, err := f.service.materializeDerivedInputs(f.ctx, operation, workflow, &retainedBuilder{ids: f.service.ids})
		if err != nil {
			t.Fatal(err)
		}
		currentTicket, err := f.store.GetDeliveryTicketByTicketID(f.ctx, ticket.TicketID)
		if err != nil || !currentTicket.CurrentRevisionRowID.Valid {
			t.Fatalf("current ticket: %#v err=%v", currentTicket, err)
		}
		if err = f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
			replacement, createErr := tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 2, ReplacesRevisionRowID: currentTicket.CurrentRevisionRowID, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID, SourceClosureRowID: f.closure.ID, SourcePath: "tickets/replacement.json", Goal: "Replace revision.", Context: "Authority drift.", TransitionApplicability: "required"})
			if createErr != nil {
				return createErr
			}
			_, createErr = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, replacement.ID)
			return createErr
		}); err != nil {
			t.Fatal(err)
		}
		assertPlannerRevalidationInvalid(t, f.ctx, f.service, operation, workflow, prepared)
	})

	t.Run("transition_applicability_changed", func(t *testing.T) {
		f := newPlannerDerivedFixture(t, false)
		ticket, _ := createPlannerTicket(t, f, "required", false)
		operation, _ := registry.Lookup("planner.transition_plan")
		workflow := resolvedPlannerWorkflow(t, f, ticket.TicketID)
		prepared, err := f.service.materializeDerivedInputs(f.ctx, operation, workflow, &retainedBuilder{ids: f.service.ids})
		if err != nil {
			t.Fatal(err)
		}
		current, err := f.store.GetDeliveryTicketByTicketID(f.ctx, ticket.TicketID)
		if err != nil || !current.CurrentRevisionRowID.Valid {
			t.Fatalf("current ticket = %#v, err=%v", current, err)
		}
		if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
			replacement, createErr := tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 2, ReplacesRevisionRowID: current.CurrentRevisionRowID, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID, SourceClosureRowID: f.closure.ID, SourcePath: "tickets/not-required.json", Goal: "Replace transition applicability.", Context: "Authority drift.", TransitionApplicability: "not_required"})
			if createErr != nil {
				return createErr
			}
			_, createErr = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, replacement.ID)
			return createErr
		}); err != nil {
			t.Fatal(err)
		}
		assertPlannerRevalidationInvalid(t, f.ctx, f.service, operation, workflow, prepared)
	})

	t.Run("source_closure_state_changed", func(t *testing.T) {
		f := newPlannerDerivedFixture(t, false)
		ticket, _ := createPlannerTicket(t, f, "required", false)
		operation, _ := registry.Lookup("planner.transition_plan")
		workflow := resolvedPlannerWorkflow(t, f, ticket.TicketID)
		prepared, err := f.service.materializeDerivedInputs(f.ctx, operation, workflow, &retainedBuilder{ids: f.service.ids})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.DB().Exec(`UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'operation_cancelled', verified_at = NULL WHERE id = ?`, f.closure.ID); err != nil {
			t.Fatal(err)
		}
		assertPlannerRevalidationInvalid(t, f.ctx, f.service, operation, workflow, prepared)
	})
}

func resolvedPlannerWorkflow(t *testing.T, f plannerDerivedFixture, ticketID string) workflowPreparation {
	t.Helper()
	project, err := f.store.GetProjectByProjectID(f.ctx, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := f.service.prepareWorkflowReferences(f.ctx, project, f.workflowReferences(t, ticketID))
	if err != nil {
		t.Fatal(err)
	}
	return workflow
}

func assertPlannerRevalidationInvalid(t *testing.T, ctx context.Context, service *LifecycleService, operation registry.OperationDefinition, workflow workflowPreparation, prepared derivedPreparation) {
	t.Helper()
	err := service.revalidateCurrentPlannerDerivedInput(ctx, operation, workflow, prepared)
	var lifecycleErr *Error
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Code != CodeInvalidPacketDocument {
		t.Fatalf("err=%v", err)
	}
}

func TestCurrentFeatureWorkspaceRouteRejectsPersistedAuthorityMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*testing.T, *plannerDerivedFixture, *workflowPreparation)
	}{
		{"workspace_has_no_current_route", func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
			mustExec(t, f, `UPDATE feature_workspaces SET current_route_state_row_id = NULL, version = version + 1 WHERE id = ?`, f.workspace.ID)
		}},
		{"current_route_belongs_to_another_workspace", func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
			other := createPlannerWorkspace(t, f, "workspace-planner-route-other", "planner-route-other")
			mustExec(t, f, `DROP TRIGGER feature_workspace_current_route_guard`)
			mustExec(t, f, `UPDATE feature_workspaces SET current_route_state_row_id = ?, version = version + 1 WHERE id = ?`, other.CurrentRouteStateRowID, f.workspace.ID)
		}},
		{"workspace_is_closed", func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
			mustExec(t, f, `UPDATE feature_workspaces SET state = 'closed', version = version + 1 WHERE id = ?`, f.workspace.ID)
		}},
		{"route_state_discovery", persistedRouteState("discovery")},
		{"route_state_blocked", persistedRouteState("blocked")},
		{"route_state_resolved", persistedRouteState("resolved")},
		{"route_state_closed", persistedRouteState("closed")},
		{"resolved_reference_names_obsolete_route", func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
			advancePlannerRoute(t, f, "ready")
		}},
		{"route_sequence_differs_from_reference", func(_ *testing.T, _ *plannerDerivedFixture, workflow *workflowPreparation) {
			workflow.references[0].RouteSequence++
		}},
		{"route_workspace_version_differs_from_reference", func(_ *testing.T, _ *plannerDerivedFixture, workflow *workflowPreparation) {
			workflow.references[0].RouteWorkspaceVersion++
		}},
		{"route_workspace_version_differs_from_current_workspace", func(t *testing.T, f *plannerDerivedFixture, workflow *workflowPreparation) {
			mustExec(t, f, `DROP TRIGGER feature_workspace_route_state_update_immutable`)
			mustExec(t, f, `UPDATE feature_workspace_route_states SET workspace_version = workspace_version + 100 WHERE id = ?`, f.route.ID)
			workflow.references[0].RouteWorkspaceVersion += 100
		}},
		{"workspace_version_differs_from_resolved_reference", func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
			mustExec(t, f, `UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, f.workspace.ID)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := newPlannerDerivedFixture(t, false)
			workflow := resolvedPlannerWorkflow(t, f, "")
			test.edit(t, &f, &workflow)
			_, _, err := f.service.loadCurrentFeatureWorkspaceRoute(f.ctx, workflow)
			assertPlannerLoaderInvalid(t, err, plannerAuthorityReason(test.name))
			assertNoPlannerPackets(t, f)
		})
	}
}

func TestCurrentTransitionApplicabilityRejectsPersistedAuthorityMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*testing.T, *plannerDerivedFixture, workflowstore.DeliveryTicket, workflowstore.DeliveryTicketRevision, *workflowPreparation)
	}{
		{"applicability_not_required", func(t *testing.T, f *plannerDerivedFixture, ticket workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, workflow *workflowPreparation) {
			replacePlannerTicketRevision(t, f, ticket, "not_required")
			*workflow = resolvedPlannerWorkflow(t, *f, ticket.TicketID)
		}},
		{"revision_cancelled", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision, _ *workflowPreparation) {
			mustExec(t, f, `DROP TRIGGER delivery_ticket_revision_update_immutable`)
			mustExec(t, f, `UPDATE delivery_ticket_revisions SET cancellation_reason = 'cancelled' WHERE id = ?`, revision.ID)
		}},
		{"referenced_revision_no_longer_current", func(t *testing.T, f *plannerDerivedFixture, ticket workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, _ *workflowPreparation) {
			replacePlannerTicketRevision(t, f, ticket, "required")
		}},
		{"ticket_belongs_to_another_workspace", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, workflow *workflowPreparation) {
			other := createPlannerWorkspace(t, f, "workspace-planner-ticket-other", "planner-ticket-other")
			ticket, _ := createPlannerTicketInWorkspace(t, *f, other, "TICKET-PLANNER-OTHER", "required")
			project, _ := f.store.GetProjectByProjectID(f.ctx, f.projectID)
			foreign, err := f.service.resolveWorkflowReference(f.ctx, project, semanticidentity.WorkflowReferenceRequest{Kind: "delivery_ticket", WorkspaceID: other.WorkspaceID, TicketID: ticket.TicketID})
			if err != nil {
				t.Fatal(err)
			}
			workflow.references[1] = foreign
		}},
		{"revision_belongs_to_another_ticket", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, workflow *workflowPreparation) {
			_, otherRevision := createPlannerTicketInWorkspace(t, *f, f.workspace, "TICKET-PLANNER-SECOND", "required")
			workflow.references[1].RevisionID = otherRevision.ID
		}},
		{"revision_number_differs_from_reference", func(_ *testing.T, _ *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, workflow *workflowPreparation) {
			workflow.references[1].RevisionNumber++
		}},
		{"closure_not_ready", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, _ *workflowPreparation) {
			mustExec(t, f, `UPDATE source_vault_closures SET state = 'unavailable', failure_reason = 'operation_cancelled', verified_at = NULL WHERE id = ?`, f.closure.ID)
		}},
		{"closure_id_differs_from_reference", func(_ *testing.T, _ *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, workflow *workflowPreparation) {
			workflow.references[1].SourceClosureID = "closure-obsolete"
		}},
		{"workspace_route_no_longer_ready", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, _ *workflowPreparation) {
			advancePlannerRoute(t, f, "blocked")
		}},
		{"workspace_route_or_version_changed_after_resolution", func(t *testing.T, f *plannerDerivedFixture, _ workflowstore.DeliveryTicket, _ workflowstore.DeliveryTicketRevision, _ *workflowPreparation) {
			advancePlannerRoute(t, f, "ready")
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := newPlannerDerivedFixture(t, false)
			ticket, revision := createPlannerTicket(t, f, "required", false)
			workflow := resolvedPlannerWorkflow(t, f, ticket.TicketID)
			test.edit(t, &f, ticket, revision, &workflow)
			_, _, err := f.service.loadCurrentTransitionApplicability(f.ctx, workflow)
			assertPlannerLoaderInvalid(t, err, "")
			assertNoPlannerPackets(t, f)
		})
	}
}

func persistedRouteState(state string) func(*testing.T, *plannerDerivedFixture, *workflowPreparation) {
	return func(t *testing.T, f *plannerDerivedFixture, _ *workflowPreparation) {
		advancePlannerRoute(t, f, state)
	}
}

func advancePlannerRoute(t *testing.T, f *plannerDerivedFixture, state string) {
	t.Helper()
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		route, err := tx.CreateFeatureWorkspaceRouteState(f.ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: "route-planner-derived-next", WorkspaceRowID: f.workspace.ID, Sequence: f.route.Sequence + 1, WorkspaceVersion: f.workspace.Version + 1, State: state})
		if err != nil {
			return err
		}
		workspace, err := tx.AdvanceFeatureWorkspaceRouteState(f.ctx, route.ID, "open", f.workspace.WorkspaceID, f.workspace.Version)
		if err == nil {
			f.workspace, f.route = workspace, route
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func createPlannerWorkspace(t *testing.T, f *plannerDerivedFixture, workspaceID, slug string) workflowstore.FeatureWorkspace {
	t.Helper()
	project, err := f.store.GetProjectByProjectID(f.ctx, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	var workspace workflowstore.FeatureWorkspace
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		workspace, createErr = tx.CreateFeatureWorkspace(f.ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: workspaceID, ProjectRowID: project.ID, FeatureSlug: slug})
		if createErr != nil {
			return createErr
		}
		route, createErr := tx.CreateFeatureWorkspaceRouteState(f.ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: "route-" + slug, WorkspaceRowID: workspace.ID, Sequence: 1, WorkspaceVersion: 2, State: "ready"})
		if createErr != nil {
			return createErr
		}
		workspace, createErr = tx.AdvanceFeatureWorkspaceRouteState(f.ctx, route.ID, "open", workspace.WorkspaceID, workspace.Version)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func createPlannerTicketInWorkspace(t *testing.T, f plannerDerivedFixture, workspace workflowstore.FeatureWorkspace, ticketID, applicability string) (workflowstore.DeliveryTicket, workflowstore.DeliveryTicketRevision) {
	t.Helper()
	var ticket workflowstore.DeliveryTicket
	var revision workflowstore.DeliveryTicketRevision
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateDeliveryTicket(f.ctx, workflowstore.CreateDeliveryTicketParams{TicketID: ticketID, WorkspaceRowID: workspace.ID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err = tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID, SourceClosureRowID: f.closure.ID, SourcePath: "tickets/" + strings.ToLower(ticketID) + ".json", Goal: "Author the selected ticket.", Context: "Exact current authority.", TransitionApplicability: applicability})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, revision.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ticket, revision
}

func replacePlannerTicketRevision(t *testing.T, f *plannerDerivedFixture, ticket workflowstore.DeliveryTicket, applicability string) workflowstore.DeliveryTicketRevision {
	t.Helper()
	var revision workflowstore.DeliveryTicketRevision
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		current, err := tx.GetDeliveryTicketByTicketID(f.ctx, ticket.TicketID)
		if err != nil {
			return err
		}
		revision, err = tx.CreateDeliveryTicketRevision(f.ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 2, ReplacesRevisionRowID: current.CurrentRevisionRowID, RepoTarget: "project", Branch: "main", BaseCommit: f.closure.CommitOID, SourceClosureRowID: f.closure.ID, SourcePath: "tickets/replacement.json", Goal: "Replace revision.", Context: "Authority drift.", TransitionApplicability: applicability})
		if err != nil {
			return err
		}
		_, err = tx.SetDeliveryTicketCurrentRevision(f.ctx, ticket.TicketID, revision.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return revision
}

func mustExec(t *testing.T, f *plannerDerivedFixture, query string, args ...any) {
	t.Helper()
	if _, err := f.store.DB().Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func currentPlannerSelection(t *testing.T, f plannerDerivedFixture) (workflowstore.DeliveryTicketSelection, workflowstore.DeliveryTicketSelectionMember, workflowstore.DeliveryTicketRevisionApproval) {
	t.Helper()
	selection, err := f.store.GetDeliveryTicketSelectionBySelectionID(f.ctx, "selection-planner-derived")
	if err != nil {
		t.Fatal(err)
	}
	members, err := f.store.ListDeliveryTicketSelectionMembers(f.ctx, selection.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("selection members=%#v err=%v", members, err)
	}
	approvals, err := f.store.ListDeliveryTicketRevisionApprovals(f.ctx, members[0].RevisionRowID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range approvals {
		if candidate.ID == members[0].ApprovalRowID {
			return selection, members[0], candidate
		}
	}
	t.Fatal("selection approval missing")
	return workflowstore.DeliveryTicketSelection{}, workflowstore.DeliveryTicketSelectionMember{}, workflowstore.DeliveryTicketRevisionApproval{}
}

func createPlannerApproval(t *testing.T, f plannerDerivedFixture, revision workflowstore.DeliveryTicketRevision, authority sql.NullInt64) workflowstore.DeliveryTicketRevisionApproval {
	t.Helper()
	var approval workflowstore.DeliveryTicketRevisionApproval
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		var err error
		approval, err = tx.CreateDeliveryTicketRevisionApproval(f.ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{ApprovalID: "approval-planner-derived-other", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved", Rationale: "Exact revision approved.", SourceClosureRowID: f.closure.ID, AuthorityRevisionRowID: authority})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return approval
}

func assertPlannerSelectionMember(t *testing.T, f plannerDerivedFixture, memberID, revisionID, approvalID, approvalRevisionID int64) {
	t.Helper()
	var member workflowstore.DeliveryTicketSelectionMember
	if err := f.store.DB().QueryRow(`SELECT id, selection_row_id, sequence, revision_row_id, approval_row_id, created_at FROM delivery_ticket_selection_members WHERE id = ?`, memberID).Scan(&member.ID, &member.SelectionRowID, &member.Sequence, &member.RevisionRowID, &member.ApprovalRowID, &member.CreatedAt); err != nil || member.Sequence != 1 || member.RevisionRowID != revisionID || member.ApprovalRowID != approvalID {
		t.Fatalf("member=%#v err=%v", member, err)
	}
	approval, err := f.store.GetDeliveryTicketRevisionApprovalByRowID(f.ctx, approvalID)
	if err != nil || approval.RevisionRowID != approvalRevisionID || approval.ApprovalKind != "delivery" || approval.ApprovalState != "approved" || approval.SourceClosureRowID != f.closure.ID || !approval.AuthorityRevisionRowID.Valid || approval.AuthorityRevisionRowID != f.workspace.CurrentAuthorityRevisionRowID {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
}

func createPlannerAuthority(t *testing.T, f plannerDerivedFixture, workspace workflowstore.FeatureWorkspace, authorityID string) workflowstore.FeatureWorkspaceAuthorityRevision {
	t.Helper()
	var authority workflowstore.FeatureWorkspaceAuthorityRevision
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		existing, err := tx.ListFeatureWorkspaceAuthorityRevisions(f.ctx, workspace.ID)
		if err != nil {
			return err
		}
		authority, err = tx.CreateFeatureWorkspaceAuthorityRevision(f.ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: authorityID, WorkspaceRowID: workspace.ID, RevisionNumber: int64(len(existing) + 1), SourceClosureRowID: sql.NullInt64{Int64: f.closure.ID, Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return authority
}

func createPlannerAlternateClosure(t *testing.T, f plannerDerivedFixture) int64 {
	t.Helper()
	var id int64
	if err := f.store.DB().QueryRow(`INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-planner-derived-alternate', ?, ?, ?, 2, 'refs/relay/closures/planner-derived-alternate', 'ready', '2026-07-24T21:00:00.000000000Z', '2026-07-24T21:00:00.000000000Z') RETURNING id`, f.closure.VaultRowID, strings.Repeat("b", 40), strings.Repeat("c", 40)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func createPlannerDependency(t *testing.T, f plannerDerivedFixture, revisionRowID, dependencyRowID int64) {
	t.Helper()
	if err := f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketRevisionDependency(f.ctx, workflowstore.CreateDeliveryTicketRevisionDependencyParams{RevisionRowID: revisionRowID, Sequence: 1, DependsOnRevisionRowID: dependencyRowID, Outcome: "satisfied"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func insertPlannerSatisfaction(t *testing.T, f plannerDerivedFixture, revisionRowID int64) {
	t.Helper()
	mustExec(t, &f, `DROP TRIGGER delivery_ticket_revision_satisfaction_guard`)
	mustExec(t, &f, `PRAGMA foreign_keys = OFF`)
	mustExec(t, &f, `INSERT INTO delivery_ticket_revision_satisfactions (delivery_ticket_revision_row_id, audit_ticket_revision_decision_row_id) VALUES (?, 999)`, revisionRowID)
	mustExec(t, &f, `PRAGMA foreign_keys = ON`)
}

func assertPlannerForeignDependency(t *testing.T, f plannerDerivedFixture, revisionID int64, ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision) {
	t.Helper()
	dependencies, err := f.store.ListDeliveryTicketRevisionDependencies(f.ctx, revisionID)
	currentTicket, ticketErr := f.store.GetDeliveryTicketByTicketID(f.ctx, ticket.TicketID)
	if err != nil || ticketErr != nil || len(dependencies) != 1 || dependencies[0].DependsOnRevisionRowID != revision.ID || dependencies[0].Outcome != "satisfied" || !currentTicket.CurrentRevisionRowID.Valid || currentTicket.CurrentRevisionRowID.Int64 != revision.ID {
		t.Fatalf("dependencies=%#v ticket=%#v err=%v ticketErr=%v", dependencies, currentTicket, err, ticketErr)
	}
	if _, err := f.store.GetDeliveryTicketRevisionSatisfaction(f.ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
}

func assertPlannerLoaderInvalid(t *testing.T, err error, reason string) {
	t.Helper()
	var lifecycleErr *Error
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Code != CodeInvalidPacketDocument {
		t.Fatalf("err=%v", err)
	}
	if reason != "" && lifecycleErr.Reason != reason {
		t.Fatalf("reason=%q, want %q", lifecycleErr.Reason, reason)
	}
}

func plannerAuthorityReason(name string) string {
	switch name {
	case "resolved_reference_names_obsolete_route":
		return "route_id_mismatch"
	case "route_sequence_differs_from_reference":
		return "route_sequence_mismatch"
	case "route_workspace_version_differs_from_reference":
		return "route_workspace_version_mismatch"
	case "workspace_version_differs_from_resolved_reference":
		return "workspace_version_mismatch"
	case "active_selection_targets_another_revision":
		return "selection_target_mismatch"
	case "approval_belongs_to_another_revision":
		return "approval_revision_mismatch"
	case "dependency_ticket_belongs_to_another_workspace":
		return "dependency_workspace_mismatch"
	case "dependency_satisfaction_missing":
		return "dependency_satisfaction_missing"
	default:
		return ""
	}
}

func assertNoPlannerPackets(t *testing.T, f plannerDerivedFixture) {
	t.Helper()
	var count int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM operation_packets`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid authority reached publication: packets=%d", count)
	}
}
