package operations

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apptickets "relay/internal/app/tickets"
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
			publish := apptickets.PublishInput{
				WorkspaceID: fixture.workspace.WorkspaceID, TicketID: test.ticketID, ExternalPriority: 27,
				ExpectedRevisionNumber: test.expected, RemediationSeedID: fixture.seed.RemediationSeedID,
				Revision: apptickets.RevisionInput{
					RepoTarget: "project", Branch: "main", BaseCommit: fixture.closure.CommitOID, SourceClosureRowID: fixture.closure.ID,
					SourcePath: "tickets/remediation-authored.json", Goal: "Retain the exact caller-authored remediation revision.",
					Context: "The remediation packet authoring context is retained and verified.", TransitionApplicability: "required",
					CanonicalJSON: []byte(`{"ticket":"` + test.ticketID + `","caller":"exact"}`), RenderedMarkdown: []byte("# Exact remediation\n"),
					Members: []apptickets.RevisionMemberInput{{Kind: "scope_in", Path: "internal/app/tickets/service.go", Text: "Preserve the audited behavior."}},
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
			if test.replacement && (!result.Revision.ReplacesRevisionRowID.Valid || result.Revision.ReplacesRevisionRowID.Int64 != fixture.revision.ID) {
				t.Fatalf("replacement lineage = %#v", result.Revision)
			}
			if result.Revision.Goal != publish.Revision.Goal || result.Revision.Context != publish.Revision.Context || result.Revision.SourcePath != publish.Revision.SourcePath || result.Revision.SourceClosureRowID != publish.Revision.SourceClosureRowID {
				t.Fatalf("caller-authored revision fields were not retained = %#v", result.Revision)
			}
			members, err := fixture.store.ListDeliveryTicketRevisionMembers(fixture.ctx, result.Revision.ID)
			if err != nil || len(members) != len(publish.Revision.Members) || members[0].MemberKind != publish.Revision.Members[0].Kind || members[0].MemberPath.String != publish.Revision.Members[0].Path || members[0].MemberText != publish.Revision.Members[0].Text {
				t.Fatalf("revision members = %#v err=%v", members, err)
			}
			dependencies, err := fixture.store.ListDeliveryTicketRevisionDependencies(fixture.ctx, result.Revision.ID)
			if err != nil || len(dependencies) != len(publish.Revision.Dependencies) {
				t.Fatalf("revision dependencies = %#v err=%v", dependencies, err)
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
	if err := fixture.store.CommitArtifactBatch(fixture.ctx, batch, func(tx *workflowstore.Tx) error {
		artifact, err := tx.CreateOperationPacketArtifact(fixture.ctx, workflowstore.CreateOperationPacketArtifactParams{ArtifactID: artifactID, Kind: file.Kind, RelativePath: file.RelativePath, MediaType: file.MediaType, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
		if err != nil {
			return err
		}
		created, err := tx.CreateOperationPacket(fixture.ctx, workflowstore.CreateOperationPacketParams{PacketID: packetID, PacketSHA256: snapshot.SHA256(), SchemaVersion: packet.SchemaVersion, Role: "planner", OperationID: string(operation.OperationID), SurfaceContractID: string(operation.SurfaceContract), ProjectID: fixture.projectID, ReadinessState: packet.ReadinessReady, CreatedAt: document.CreatedAt, PacketArtifactRowID: artifact.ID})
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
