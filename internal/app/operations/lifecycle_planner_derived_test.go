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
	case "planner.ticket_design_brief":
		selected, selectedAttestations := plannerDerivedArtifactInput(t, f, "selected_delivery_ticket")
		dependencies, dependencyAttestations := plannerDerivedArtifactInput(t, f, "completed_dependency_outcomes")
		dependencyAttestations[0].Kind = "completed_dependency_outcomes"
		dependencyAttestations[0].Approved = false
		dependencyAttestations[0].Complete = true
		identity.Inputs = []semanticidentity.InputBinding{selected, dependencies}
		identity.Attestations = append(selectedAttestations, dependencyAttestations...)
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
	data, _, _ := readPlannerDerivedInput(t, f, result, "current_feature_workspace_route")
	expected, _ := canonicalJSON(featureWorkspaceRouteInput{f.workspace.WorkspaceID, f.workspace.FeatureSlug, f.workspace.Version, "open", f.route.RouteStateID, f.route.Sequence, f.route.WorkspaceVersion, "ready"})
	if !bytes.Equal(data, expected) {
		t.Fatalf("data=%s want=%s", data, expected)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(result.Packet.DocumentBytes, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["relay_specs"]) != "{}" || string(document["manifest_domain"]) != "{}" {
		t.Fatalf("manifestless governance was fabricated: %s %s", document["relay_specs"], document["manifest_domain"])
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

func TestLifecycleTicketDesignBriefMaterializesCurrentSelectionIdentity(t *testing.T) {
	f := newPlannerDerivedFixture(t, true)
	ticket, revision := createPlannerTicket(t, f, "not_required", true)
	result := createPlannerDerivedPacket(t, f, "planner.ticket_design_brief", ticket.TicketID)
	data, input, _ := readPlannerDerivedInput(t, f, result, "current_selection_identity")
	expected, _ := canonicalJSON(selectionIdentityInput{f.workspace.WorkspaceID, f.workspace.FeatureSlug, f.workspace.Version, "selection-planner-derived", "active", ticket.TicketID, revision.ID, revision.RevisionNumber, "approval-planner-derived", "authority-planner-derived", f.closure.ClosureID})
	if !bytes.Equal(data, expected) || input.InputRole != "authority" || input.AttestationKind != "derived_authority" {
		t.Fatalf("input=%#v data=%s", input, data)
	}

	t.Run("no_active_selection", func(t *testing.T) {
		invalid := newPlannerDerivedFixture(t, true)
		invalidTicket, _ := createPlannerTicket(t, invalid, "not_required", false)
		assertPlannerCreateInvalid(t, invalid, "planner.ticket_design_brief", invalidTicket.TicketID)
	})
}

func assertPlannerCreateInvalid(t *testing.T, f plannerDerivedFixture, operationID registry.OperationID, ticketID string) {
	t.Helper()
	operation, _ := registry.Lookup(operationID)
	identity := semanticidentity.CreateOperationPacket{SurfaceContract: operation.SurfaceContract, OperationID: operationID, ProjectID: f.projectID, WorkflowReferences: f.workflowReferences(t, ticketID)}
	if operationID == "planner.transition_plan" {
		input, attestations := plannerDerivedArtifactInput(t, f, "selected_delivery_ticket")
		identity.Inputs, identity.Attestations = []semanticidentity.InputBinding{input}, attestations
	} else {
		selected, selectedAttestations := plannerDerivedArtifactInput(t, f, "selected_delivery_ticket")
		dependencies, dependencyAttestations := plannerDerivedArtifactInput(t, f, "completed_dependency_outcomes")
		dependencyAttestations[0].Kind, dependencyAttestations[0].Approved, dependencyAttestations[0].Complete = "completed_dependency_outcomes", false, true
		identity.Inputs = []semanticidentity.InputBinding{selected, dependencies}
		identity.Attestations = append(selectedAttestations, dependencyAttestations...)
	}
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
		if _, err = f.store.DB().Exec(`UPDATE feature_workspaces SET version = version + 1 WHERE id = ?`, f.workspace.ID); err != nil {
			t.Fatal(err)
		}
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

	t.Run("selection_or_authority_changed", func(t *testing.T) {
		f := newPlannerDerivedFixture(t, true)
		ticket, _ := createPlannerTicket(t, f, "not_required", true)
		operation, _ := registry.Lookup("planner.ticket_design_brief")
		workflow := resolvedPlannerWorkflow(t, f, ticket.TicketID)
		prepared, err := f.service.materializeDerivedInputs(f.ctx, operation, workflow, &retainedBuilder{ids: f.service.ids})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.store.WithTx(f.ctx, func(tx *workflowstore.Tx) error {
			authority, createErr := tx.CreateFeatureWorkspaceAuthorityRevision(f.ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: "authority-planner-derived-replacement", WorkspaceRowID: f.workspace.ID, RevisionNumber: 2, SourceClosureRowID: sql.NullInt64{Int64: f.closure.ID, Valid: true}})
			if createErr != nil {
				return createErr
			}
			_, createErr = tx.SetFeatureWorkspaceAuthorityRevision(f.ctx, authority.ID, f.workspace.WorkspaceID, f.workspace.Version)
			return createErr
		}); err != nil {
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
