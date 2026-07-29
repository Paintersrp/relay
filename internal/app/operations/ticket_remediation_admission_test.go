package operations

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/app/features"
	apptickets "relay/internal/app/tickets"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func TestTicketWorkflowRemediationPublishesBothTicketShapes(t *testing.T) {
	for _, test := range []struct {
		name        string
		ticketID    string
		expected    int64
		replacement bool
	}{
		{name: "direct replacement", ticketID: "TICKET-REMEDIATION", expected: 1, replacement: true},
		{name: "separate remediation ticket", ticketID: "TICKET-REMEDIATION-NEW", expected: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRemediationLifecycleFixture(t)
			planner, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "authoring-" + strings.ReplaceAll(test.name, " ", "-"), Identity: remediationIdentity(fixture)})
			if err != nil {
				t.Fatal(err)
			}
			packetService, err := NewService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			operator := createRemediationOperatorPacket(t, fixture)
			if operator.Summary.Role != registry.Role("local_operator") || operator.Summary.OperationID != registry.LocalOperatorTicketWorkflowOperationID || operator.Summary.SurfaceContract != registry.LocalOperatorTicketWorkflowSurface {
				t.Fatalf("operator packet summary = %#v", operator.Summary)
			}
			plannerDocument, err := decodeCanonicalAuthoringPacket(planner.Packet.DocumentBytes, planner.Packet.Summary.PacketSHA256)
			if err != nil {
				t.Fatal(err)
			}
			if len(plannerDocument.AllowedActions) != 0 {
				t.Fatalf("planner remediation packet mutation actions = %#v", plannerDocument.AllowedActions)
			}
			ticketService, err := apptickets.NewService(fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			workflow, err := NewTicketWorkflowService(packetService, ticketService)
			if err != nil {
				t.Fatal(err)
			}

			seedBytes, authorityBytes := readRemediationInputs(t, fixture, planner.Packet)
			if len(seedBytes) == 0 || len(authorityBytes) == 0 {
				t.Fatal("remediation retained inputs were not readable")
			}
			retainedSeed, err := packetService.ReadVerifiedRetainedInput(fixture.ctx, planner.Packet.Summary.PacketID, remediationSeedInputName)
			if err != nil || !bytes.Equal(retainedSeed, seedBytes) {
				t.Fatalf("retained remediation seed = %q err=%v", retainedSeed, err)
			}
			retainedAuthority, err := packetService.ReadVerifiedRetainedInput(fixture.ctx, planner.Packet.Summary.PacketID, currentAuthorityInputName)
			if err != nil || !bytes.Equal(retainedAuthority, authorityBytes) {
				t.Fatalf("retained authority = %q err=%v", retainedAuthority, err)
			}
			before := remediationAdmissionCounts(t, fixture)
			auditedTicketBefore, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, fixture.ticket.ID)
			if err != nil {
				t.Fatal(err)
			}
			auditedRevisionBefore, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, fixture.revision.ID)
			if err != nil {
				t.Fatal(err)
			}
			auditedRevisionsBefore, err := fixture.store.ListDeliveryTicketRevisions(fixture.ctx, fixture.ticket.ID)
			if err != nil {
				t.Fatal(err)
			}
			ticketsBefore, err := fixture.store.ListDeliveryTicketsByWorkspace(fixture.ctx, fixture.workspace.ID)
			if err != nil {
				t.Fatal(err)
			}
			publish := apptickets.PublishInput{
				WorkspaceID: fixture.workspace.WorkspaceID, TicketID: test.ticketID, ExternalPriority: fixture.ticket.ExternalPriority,
				ExpectedRevisionNumber: test.expected, RemediationSeedID: fixture.seed.RemediationSeedID,
				Revision: apptickets.RevisionInput{
					RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
					SourcePath: "tickets/remediation-authored.json", Goal: "Retain the exact caller-authored remediation revision.",
					Context: "The remediation packet authoring context is retained and verified.", TransitionApplicability: "required",
					CancellationReason: "",
					CanonicalJSON:      []byte(`{"ticket":"` + test.ticketID + `","caller":"exact"}`), RenderedMarkdown: []byte("# Exact remediation\n"),
					Members: []apptickets.RevisionMemberInput{
						{Kind: "scope_in", Path: "internal/app/tickets/service.go", Text: "Preserve the audited behavior."},
						{Kind: "validation_intent", Path: "internal/app/operations/ticket_remediation_admission_test.go", Text: "Verify the exact remediation publication."},
					},
					Dependencies: []apptickets.DependencyInput{{RevisionRowID: fixture.revision.ID, Outcome: "satisfied"}},
				},
			}
			ref := RemediationAuthoringReference{PacketID: planner.Packet.Summary.PacketID, ExpectedPacketSHA256: planner.Packet.Summary.PacketSHA256}
			payload, err := TicketPublishPayloadSHA256WithRemediation(publish, ref)
			if err != nil {
				t.Fatal(err)
			}
			result, err := workflow.Publish(fixture.ctx, TicketPublishOperationInput{
				Admission: TicketOperationRequest{
					PacketID: operator.Summary.PacketID, OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionPublish,
					WorkspaceID: publish.WorkspaceID, TicketID: publish.TicketID, ExpectedRevisionNumber: publish.ExpectedRevisionNumber,
					SourceClosureRowID: publish.Revision.SourceClosureRowID, ExternalPriority: publish.ExternalPriority, PayloadSHA256: payload,
				},
				Publish: publish, RemediationAuthoringReference: ref,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.RemediationReopening == nil || result.RemediationReopening.ReopeningRevisionRowID != result.Revision.ID {
				t.Fatalf("remediation reopening = %#v", result.RemediationReopening)
			}
			wantKind := "remediation_ticket"
			if test.replacement {
				wantKind = "replacement_ticket_revision"
			}
			if result.RemediationReopening.ReopeningKind != wantKind || publish.RemediationSeedID != fixture.seed.RemediationSeedID {
				t.Fatalf("reopening = %#v publish = %#v", result.RemediationReopening, publish)
			}
			if result.Ticket.TicketID != publish.TicketID || result.Ticket.ExternalPriority != publish.ExternalPriority || result.Ticket.WorkspaceRowID != fixture.workspace.ID {
				t.Fatalf("published ticket fields = %#v", result.Ticket)
			}
			if result.Revision.RevisionNumber != publish.ExpectedRevisionNumber+1 || result.Revision.CancellationReason != (sql.NullString{}) ||
				result.Revision.RepoTarget != publish.Revision.RepoTarget || result.Revision.Branch != publish.Revision.Branch || result.Revision.BaseCommit != publish.Revision.BaseCommit ||
				result.Revision.SourceClosureRowID != publish.Revision.SourceClosureRowID || result.Revision.SourcePath != publish.Revision.SourcePath || result.Revision.Goal != publish.Revision.Goal ||
				result.Revision.Context != publish.Revision.Context || result.Revision.TransitionApplicability != publish.Revision.TransitionApplicability {
				t.Fatalf("caller-authored revision fields were not retained = %#v", result.Revision)
			}
			members, err := fixture.store.ListDeliveryTicketRevisionMembers(fixture.ctx, result.Revision.ID)
			if err != nil || len(members) != len(publish.Revision.Members) {
				t.Fatalf("revision members = %#v err=%v", members, err)
			}
			for index, want := range publish.Revision.Members {
				got := members[index]
				if got.RevisionRowID != result.Revision.ID || got.Sequence != int64(index+1) || got.MemberKind != want.Kind || got.MemberPath != (sql.NullString{String: want.Path, Valid: true}) || got.MemberText != want.Text {
					t.Fatalf("revision member %d = %#v", index, got)
				}
			}
			dependencies, err := fixture.store.ListDeliveryTicketRevisionDependencies(fixture.ctx, result.Revision.ID)
			if err != nil || len(dependencies) != len(publish.Revision.Dependencies) {
				t.Fatalf("revision dependencies = %#v err=%v", dependencies, err)
			}
			for index, want := range publish.Revision.Dependencies {
				got := dependencies[index]
				if got.RevisionRowID != result.Revision.ID || got.Sequence != int64(index+1) || got.DependsOnRevisionRowID != want.RevisionRowID || got.Outcome != want.Outcome {
					t.Fatalf("revision dependency %d = %#v", index, got)
				}
			}
			for _, artifact := range []struct {
				path string
				want []byte
			}{{result.Canonical.RelativePath, publish.Revision.CanonicalJSON}, {result.Rendered.RelativePath, publish.Revision.RenderedMarkdown}} {
				got, readErr := os.ReadFile(filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(artifact.path)))
				if readErr != nil || !bytes.Equal(got, artifact.want) {
					t.Fatalf("artifact %s = %q err=%v", artifact.path, got, readErr)
				}
			}
			if test.replacement {
				if result.Ticket.ID != fixture.ticket.ID || result.Revision.ReplacesRevisionRowID != (sql.NullInt64{Int64: fixture.revision.ID, Valid: true}) {
					t.Fatalf("replacement identity = ticket %#v revision %#v", result.Ticket, result.Revision)
				}
			} else {
				auditedTicket, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, fixture.ticket.ID)
				if err != nil {
					t.Fatal(err)
				}
				auditedRevision, err := fixture.store.GetDeliveryTicketRevisionByRowID(fixture.ctx, fixture.revision.ID)
				if err != nil || !reflect.DeepEqual(auditedTicket, auditedTicketBefore) || !reflect.DeepEqual(auditedRevision, auditedRevisionBefore) {
					t.Fatalf("audited rows changed: ticket=%#v revision=%#v err=%v", auditedTicket, auditedRevision, err)
				}
				if result.Ticket.ID == auditedTicket.ID || result.Revision.DeliveryTicketRowID == auditedTicket.ID {
					t.Fatal("separate remediation reused the audited ticket")
				}
				auditedRevisionsAfter, err := fixture.store.ListDeliveryTicketRevisions(fixture.ctx, fixture.ticket.ID)
				if err != nil || !reflect.DeepEqual(auditedRevisionsAfter, auditedRevisionsBefore) {
					t.Fatalf("audited ticket revisions changed: before=%#v after=%#v", auditedRevisionsBefore, auditedRevisionsAfter)
				}
				ticketsAfter, err := fixture.store.ListDeliveryTicketsByWorkspace(fixture.ctx, fixture.workspace.ID)
				if err != nil || len(ticketsAfter) != len(ticketsBefore)+1 {
					t.Fatalf("workspace tickets after remediation = %#v err=%v", ticketsAfter, err)
				}
				for _, ticket := range ticketsBefore {
					found := false
					for _, after := range ticketsAfter {
						if after.ID == ticket.ID {
							found = true
							if !reflect.DeepEqual(after, ticket) {
								t.Fatalf("existing ticket changed: before=%#v after=%#v", ticket, after)
							}
						}
					}
					if !found {
						t.Fatalf("existing ticket %d disappeared", ticket.ID)
					}
				}
			}
			if got, err := fixture.store.GetAuditRemediationSeedReopening(fixture.ctx, fixture.seed.ID); err != nil || got.ReopeningRevisionRowID != result.Revision.ID {
				t.Fatalf("seed reopening = %#v err=%v", got, err)
			}
			after := remediationAdmissionCounts(t, fixture)
			for _, table := range []string{"delivery_ticket_revision_approvals", "delivery_ticket_selections", "execution_packages", "runs", "plans", "plan_passes", "execution_attempts"} {
				if before[table] != after[table] {
					t.Fatalf("successful remediation changed %s: %d -> %d", table, before[table], after[table])
				}
			}
			if test.replacement {
				current, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, fixture.ticket.ID)
				if err != nil || !current.CurrentRevisionRowID.Valid || current.CurrentRevisionRowID.Int64 != result.Revision.ID {
					t.Fatalf("audited ticket current revision = %#v err=%v", current, err)
				}
			} else {
				current, err := fixture.store.GetDeliveryTicketByRowID(fixture.ctx, fixture.ticket.ID)
				if err != nil || !current.CurrentRevisionRowID.Valid || current.CurrentRevisionRowID.Int64 != fixture.revision.ID {
					t.Fatalf("audited ticket changed = %#v err=%v", current, err)
				}
			}
			second := publish
			second.ExpectedRevisionNumber = result.Revision.RevisionNumber
			second.Revision.Dependencies = []apptickets.DependencyInput{{RevisionRowID: result.Revision.ID, Outcome: "satisfied"}}
			second.Revision.CanonicalJSON = []byte(`{"ticket":"second-remediation"}`)
			second.Revision.RenderedMarkdown = []byte("# Second remediation\n")
			secondPayload, err := TicketPublishPayloadSHA256WithRemediation(second, ref)
			if err != nil {
				t.Fatal(err)
			}
			beforeSecond := remediationPublicationStateSnapshot(t, fixture)
			if _, err := workflow.Publish(fixture.ctx, TicketPublishOperationInput{Admission: TicketOperationRequest{
				PacketID: operator.Summary.PacketID, OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionPublish,
				WorkspaceID: second.WorkspaceID, TicketID: second.TicketID, ExpectedRevisionNumber: second.ExpectedRevisionNumber,
				SourceClosureRowID: second.Revision.SourceClosureRowID, ExternalPriority: second.ExternalPriority, PayloadSHA256: secondPayload,
			}, Publish: second, RemediationAuthoringReference: ref}); !errors.Is(err, apptickets.ErrRemediationSeed) {
				t.Fatalf("second publication error = %v", err)
			}
			assertRemediationPublicationState(t, fixture, beforeSecond)
		})
	}
}

func createRemediationOperatorPacket(t *testing.T, fixture remediationLifecycleFixture) PacketView {
	t.Helper()
	operation, ok := registry.Lookup(registry.LocalOperatorTicketWorkflowOperationID)
	if !ok {
		t.Fatal("local operator ticket operation is unavailable")
	}
	pathBytes := []byte("manifest.json")
	path := remediationTestPath(pathBytes)
	manifestBytes := []byte("manifest")
	manifestSHA := digestBytes(manifestBytes)
	document := packet.Document{
		SchemaVersion: packet.SchemaVersion, CreatedAt: "2026-07-29T00:00:00.000000000Z", Role: operation.Role,
		OperationID: operation.OperationID, SurfaceContract: operation.SurfaceContract,
		SurfaceManifestSHA256: mustSurfaceManifest(operation.SurfaceContract),
		Output:                packet.OutputContract{OutputKind: operation.OutputKind, OutputPersistence: operation.OutputPersistence},
		Project:               packet.ProjectBinding{ProjectID: fixture.projectID}, SourcePolicy: operation.SourcePolicy, HistoricalAuthority: operation.HistoricalAuthority,
		AllowedActions: operation.AllowedNonSourceActions,
		Repositories:   []packet.RepositoryBinding{{RepositoryKey: "project", RepositoryTarget: "project", BindingOrder: 1, RepositoryTargetConfigurationVersion: 1, RevisionSource: packet.RevisionSourceExplicitCommit, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40)}},
		RelaySpecs:     packet.GovernanceBinding{RepositoryKey: "relay-specs", RepositoryTarget: "relay-specs", Reserved: true, RepositoryTargetConfigurationVersion: 1, RevisionSource: packet.RevisionSourceExplicitCommit, CommitOID: strings.Repeat("c", 40), TreeOID: strings.Repeat("d", 40)},
		ManifestDomain: packet.ManifestDomainBinding{ManifestPath: path, ManifestBlobOID: strings.Repeat("e", 40), ManifestSHA256: manifestSHA, Domain: operation.ManifestDomain, Members: []packet.ManifestMember{{MemberOrder: 1, Path: path, BlobOID: strings.Repeat("f", 40), ByteSize: int64(len(manifestBytes)), SHA256: manifestSHA}}},
		ReadinessState: packet.ReadinessReady,
	}
	snapshot, err := packet.NewSnapshot(document)
	if err != nil {
		t.Fatal(err)
	}
	packetID := "opkt-operator-remediation"
	artifactID := "artifact-operator-remediation"
	batch, err := fixture.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("operation-packets", packetID)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("operation_packet_document", "operation-packet.json", packet.MediaType, snapshot.Bytes())
	if err != nil {
		_ = batch.Rollback()
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("PRAGMA ignore_check_constraints = ON"); err != nil {
		_ = batch.Rollback()
		t.Fatal(err)
	}
	var ignoreChecks int
	if err := fixture.store.DB().QueryRow("PRAGMA ignore_check_constraints").Scan(&ignoreChecks); err != nil || ignoreChecks != 1 {
		_ = batch.Rollback()
		t.Fatalf("ignore_check_constraints = %d err=%v", ignoreChecks, err)
	}
	defer func() { _, _ = fixture.store.DB().Exec("PRAGMA ignore_check_constraints = OFF") }()
	if err := fixture.store.CommitArtifactBatch(fixture.ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(fixture.ctx, workflowstore.CreateOperationPacketArtifactParams{ArtifactID: artifactID, Kind: file.Kind, RelativePath: file.RelativePath, MediaType: file.MediaType, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		created, err := tx.CreateOperationPacket(fixture.ctx, workflowstore.CreateOperationPacketParams{PacketID: packetID, PacketSHA256: snapshot.SHA256(), SchemaVersion: packet.SchemaVersion, Role: string(operation.Role), OperationID: string(operation.OperationID), SurfaceContractID: string(operation.SurfaceContract), ProjectID: fixture.projectID, ReadinessState: packet.ReadinessReady, CreatedAt: document.CreatedAt, PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(fixture.ctx, workflowstore.AttachOperationPacketDependencyParams{PacketRowID: created.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(fixture.ctx, packetID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func createWrongPlannerSurfacePacket(t *testing.T, fixture remediationLifecycleFixture) PacketView {
	t.Helper()
	operation, ok := registry.Lookup(remediationPlannerOperation)
	if !ok {
		t.Fatal("planner remediation operation is unavailable")
	}
	const packetID = "opkt-planner-wrong-surface"
	const artifactID = "artifact-planner-wrong-surface"
	source, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "matrix-wrong-surface-source", Identity: remediationIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeCanonicalAuthoringPacket(source.Packet.DocumentBytes, source.Packet.Summary.PacketSHA256)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := fixture.store.ArtifactStore().Begin(filepath.ToSlash(filepath.Join("operation-packets", packetID)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("operation_packet_document", "operation-packet.json", packet.MediaType, source.Packet.DocumentBytes)
	if err != nil {
		_ = batch.Rollback()
		t.Fatal(err)
	}
	if _, err := fixture.store.DB().Exec("PRAGMA ignore_check_constraints = ON"); err != nil {
		_ = batch.Rollback()
		t.Fatal(err)
	}
	defer func() { _, _ = fixture.store.DB().Exec("PRAGMA ignore_check_constraints = OFF") }()
	if err := fixture.store.CommitArtifactBatch(fixture.ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(fixture.ctx, workflowstore.CreateOperationPacketArtifactParams{ArtifactID: artifactID, Kind: file.Kind, RelativePath: file.RelativePath, MediaType: file.MediaType, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		created, err := tx.CreateOperationPacket(fixture.ctx, workflowstore.CreateOperationPacketParams{PacketID: packetID, PacketSHA256: source.Packet.Summary.PacketSHA256, SchemaVersion: packet.SchemaVersion, Role: "planner", OperationID: string(operation.OperationID), SurfaceContractID: "planner-ticket-frontier.v1", ProjectID: fixture.projectID, ReadinessState: packet.ReadinessReady, CreatedAt: document.CreatedAt, PacketArtifactRowID: artifact.ID})
		if err != nil {
			return err
		}
		_, err = tx.AttachOperationPacketDependency(fixture.ctx, workflowstore.AttachOperationPacketDependencyParams{PacketRowID: created.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: artifact.ArtifactID, Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(fixture.ctx, packetID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func remediationTestPath(value []byte) packet.PathIdentity {
	digest := sha256.New()
	_, _ = digest.Write([]byte("relay.git-path.v1"))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(value)
	return packet.PathIdentity{PathID: hex.EncodeToString(digest.Sum(nil)), ByteLength: int64(len(value)), PathBytesBase64: base64.StdEncoding.EncodeToString(value)}
}

func remediationAdmissionCounts(t *testing.T, fixture remediationLifecycleFixture) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, table := range []string{"delivery_ticket_revision_approvals", "delivery_ticket_selections", "execution_packages", "runs", "plans", "plan_passes", "execution_attempts"} {
		var count int
		if err := fixture.store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

type remediationPublicationHarness struct {
	fixture       remediationLifecycleFixture
	packetService *Service
	ticketService *apptickets.Service
	workflow      *TicketWorkflowService
	planner       PacketView
	operator      PacketView
	publish       apptickets.PublishInput
	ref           RemediationAuthoringReference
	admission     TicketOperationRequest
}

func newRemediationPublicationHarness(t *testing.T) remediationPublicationHarness {
	t.Helper()
	fixture := newRemediationLifecycleFixture(t)
	plannerResult, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "matrix-authoring", Identity: remediationIdentity(fixture)})
	if err != nil {
		t.Fatal(err)
	}
	packetService, err := NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	ticketService, err := apptickets.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := NewTicketWorkflowService(packetService, ticketService)
	if err != nil {
		t.Fatal(err)
	}
	operator := createRemediationOperatorPacket(t, fixture)
	publish := apptickets.PublishInput{
		WorkspaceID: fixture.workspace.WorkspaceID, TicketID: fixture.ticket.TicketID, ExternalPriority: fixture.ticket.ExternalPriority, ExpectedRevisionNumber: 1,
		RemediationSeedID: fixture.seed.RemediationSeedID,
		Revision: apptickets.RevisionInput{
			RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
			SourcePath: "tickets/matrix-remediation.json", Goal: "Publish the exact remediation revision.", Context: "The matrix binds both authorities.",
			TransitionApplicability: "required", CancellationReason: "",
			CanonicalJSON: []byte(`{"ticket":"matrix","exact":true}`), RenderedMarkdown: []byte("# Matrix remediation\n"),
			Members: []apptickets.RevisionMemberInput{
				{Kind: "scope_in", Path: "internal/app/tickets/service.go", Text: "Preserve ticket publication semantics."},
				{Kind: "validation_intent", Path: "internal/app/operations/ticket_remediation_admission_test.go", Text: "Reject invalid dual-authority requests atomically."},
			},
			Dependencies: []apptickets.DependencyInput{{RevisionRowID: fixture.revision.ID, Outcome: "satisfied"}},
		},
	}
	ref := RemediationAuthoringReference{PacketID: plannerResult.Packet.Summary.PacketID, ExpectedPacketSHA256: plannerResult.Packet.Summary.PacketSHA256}
	harness := remediationPublicationHarness{fixture: fixture, packetService: packetService, ticketService: ticketService, workflow: workflow, planner: plannerResult.Packet, operator: operator, publish: publish, ref: ref}
	harness.refreshAdmission(t)
	return harness
}

func (h *remediationPublicationHarness) refreshAdmission(t *testing.T) {
	t.Helper()
	payload, err := TicketPublishPayloadSHA256WithRemediation(h.publish, h.ref)
	if err != nil {
		t.Fatal(err)
	}
	h.admission = TicketOperationRequest{
		PacketID: h.operator.Summary.PacketID, OperationID: registry.LocalOperatorTicketWorkflowOperationID, Action: registry.TicketActionPublish,
		WorkspaceID: h.publish.WorkspaceID, TicketID: h.publish.TicketID, ExpectedRevisionNumber: h.publish.ExpectedRevisionNumber,
		SourceClosureRowID: h.publish.Revision.SourceClosureRowID, ExternalPriority: h.publish.ExternalPriority, PayloadSHA256: payload,
	}
}

func TestTicketWorkflowRemediationAdmissionRejectionMatrixIsAtomic(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*testing.T, *remediationPublicationHarness)
	}{
		{name: "local-operator packet admission failure", prepare: func(_ *testing.T, h *remediationPublicationHarness) {
			h.admission.PacketID = h.planner.Summary.PacketID
		}},
		{name: "unknown Planner packet", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			h.ref.PacketID = "planner-packet-unknown"
			h.refreshAdmission(t)
		}},
		{name: "wrong Planner packet SHA-256", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			h.ref.ExpectedPacketSHA256 = strings.Repeat("f", 64)
			h.refreshAdmission(t)
		}},
		{name: "closed Planner packet", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			if _, err := h.fixture.service.Close(h.fixture.ctx, CloseLifecycleInput{MutationID: "matrix-close", Identity: semanticidentity.CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: h.planner.Summary.PacketID}}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "superseded Planner packet", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			if _, err := h.fixture.service.Refresh(h.fixture.ctx, RefreshLifecycleInput{MutationID: "matrix-refresh", PriorPacketID: h.planner.Summary.PacketID, Identity: remediationRefreshIdentity(h.fixture, h.planner.Summary.PacketID)}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong Planner operation", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			other := createLifecycleRequirementsPacket(t, h.fixture.lifecycleFixture, "matrix-requirements")
			h.ref = RemediationAuthoringReference{PacketID: other.Packet.Summary.PacketID, ExpectedPacketSHA256: other.Packet.Summary.PacketSHA256}
			h.refreshAdmission(t)
		}},
		{name: "wrong Planner surface", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			other := createWrongPlannerSurfacePacket(t, h.fixture)
			h.ref = RemediationAuthoringReference{PacketID: other.Summary.PacketID, ExpectedPacketSHA256: other.Summary.PacketSHA256}
			h.refreshAdmission(t)
		}},
		{name: "seed ID mismatch", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			h.publish.RemediationSeedID = "remediation-seed-other"
			h.refreshAdmission(t)
		}},
		{name: "workspace mismatch", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			h.publish.WorkspaceID = "workspace-other"
			h.refreshAdmission(t)
		}},
		{name: "current authority changed after packet creation", prepare: func(t *testing.T, h *remediationPublicationHarness) { replaceRemediationAuthority(t, &h.fixture) }},
		{name: "publication source closure differs from current authority", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			h.publish.Revision.SourceClosureRowID++
			h.refreshAdmission(t)
		}},
		{name: "consumed seed", prepare: func(t *testing.T, h *remediationPublicationHarness) {
			result, err := h.workflow.Publish(h.fixture.ctx, TicketPublishOperationInput{Admission: h.admission, Publish: h.publish, RemediationAuthoringReference: h.ref})
			if err != nil {
				t.Fatal(err)
			}
			h.publish.ExpectedRevisionNumber = result.Revision.RevisionNumber
			h.publish.Revision.CanonicalJSON = []byte(`{"ticket":"matrix-consumed"}`)
			h.publish.Revision.RenderedMarkdown = []byte("# Consumed\n")
			h.refreshAdmission(t)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRemediationPublicationHarness(t)
			tc.prepare(t, &h)
			before := remediationPublicationStateSnapshot(t, h.fixture)
			if _, err := h.workflow.Publish(h.fixture.ctx, TicketPublishOperationInput{Admission: h.admission, Publish: h.publish, RemediationAuthoringReference: h.ref}); err == nil {
				t.Fatal("invalid remediation publication was accepted")
			}
			assertRemediationPublicationState(t, h.fixture, before)
		})
	}
}

func replaceRemediationAuthority(t *testing.T, fixture *remediationLifecycleFixture) {
	t.Helper()
	service, err := features.NewService(fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.store.GetFeatureWorkspaceByWorkspaceID(fixture.ctx, fixture.workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	approvals := make([]features.AuthorityLayerInput, 0, len(fixture.authorityLayers))
	for _, layer := range fixture.authorityLayers {
		approval, err := service.RecordAuthorityApproval(fixture.ctx, features.RecordAuthorityApprovalInput{
			WorkspaceID: fixture.workspace.WorkspaceID, Family: layer.kind, ArtifactRowID: sql.NullInt64{Int64: layer.artifact.ID, Valid: true}, ArtifactSHA256: layer.artifact.SHA256,
			OperatorConfirmationEvidence: "The replacement authority is approved for atomic admission coverage.",
		})
		if err != nil {
			t.Fatal(err)
		}
		approvals = append(approvals, features.AuthorityLayerInput{Kind: layer.kind, ArtifactRowID: sql.NullInt64{Int64: layer.artifact.ID, Valid: true}, ArtifactSHA256: layer.artifact.SHA256, SourceClosureID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}})
	}
	if _, _, err := service.PublishAuthority(fixture.ctx, features.PublishAuthorityInput{WorkspaceID: fixture.workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: fixture.closure.ID, Valid: true}, Layers: approvals}); err != nil {
		t.Fatal(err)
	}
}

type remediationPublicationState struct {
	tables map[string]string
	trees  map[string]string
}

func remediationPublicationStateSnapshot(t *testing.T, fixture remediationLifecycleFixture) remediationPublicationState {
	t.Helper()
	state := remediationPublicationState{tables: map[string]string{}, trees: map[string]string{}}
	for _, table := range []string{"delivery_tickets", "delivery_ticket_revisions", "delivery_ticket_revision_members", "delivery_ticket_revision_dependencies", "delivery_ticket_revision_approvals", "delivery_ticket_selections", "audit_remediation_seed_reopenings", "feature_workspace_completion_reopenings", "execution_packages", "runs", "plans", "plan_passes", "execution_attempts"} {
		rows, err := fixture.store.DB().Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		values := []string{strings.Join(columns, "\x1f")}
		for rows.Next() {
			cells := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range cells {
				pointers[index] = &cells[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			parts := make([]string, len(cells))
			for index, cell := range cells {
				parts[index] = fmt.Sprintf("%T:%v", cell, cell)
			}
			values = append(values, strings.Join(parts, "\x1f"))
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		state.tables[table] = strings.Join(values, "\x00")
	}
	for _, root := range []string{"delivery-tickets", ".staging"} {
		state.trees[root] = remediationArtifactTree(t, filepath.Join(fixture.store.ArtifactStore().Root(), root), root)
	}
	return state
}

func remediationArtifactTree(t *testing.T, root, name string) string {
	t.Helper()
	entries := []string{}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := name
		if relative != "." {
			key = filepath.ToSlash(filepath.Join(name, relative))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%s:%o:%d", entry.Type().String(), info.Mode().Perm(), info.Size())
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + digestBytes(data)
		}
		entries = append(entries, key+"="+value)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "\x00")
}

func assertRemediationPublicationState(t *testing.T, fixture remediationLifecycleFixture, want remediationPublicationState) {
	t.Helper()
	got := remediationPublicationStateSnapshot(t, fixture)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("failed publication changed state: got=%#v want=%#v", got, want)
	}
}
