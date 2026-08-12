package tickets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

// planGroundedTicketJSON builds a canonical Delivery Ticket v2 candidate whose
// ticket_id and depends_on realize one planned unit of the fixture Plan.
func planGroundedTicketJSON(featureSlug, ticketID string, revision int64, dependsOn string) []byte {
	return []byte(`{"schema_version":"2.0","feature_slug":"` + featureSlug + `","ticket_id":"` + ticketID + `","revision":` + strconv.FormatInt(revision, 10) + `,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Realize the planned unit.","context":"Plan-grounded ticket authoring.","scope":{"in_scope":["Realize the planned unit."],"out_of_scope":["Change the planned topology."]},"depends_on":` + dependsOn + `,"required_invariants":["The Ticket realizes the current Plan unit."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/tickets","obligation":"Implement the planned unit outcome.","prerequisites":[]}],"proof_obligations":["Prove the planned outcome."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/tickets","expected":"Tests pass."}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["The planned outcome is delivered."]}`)
}

// planGroundedCandidateFixture builds a workspace with a current approved
// Delivery Plan (P3-T1 and P3-T2 where P3-T2 depends on P3-T1) and a ready
// source closure and authority, then returns the store, workspace, and the
// current workspace version.
func planGroundedCandidateFixture(t *testing.T) (*Service, *workflowstore.Store, string, workflowstore.SourceVaultClosure, string) {
	t.Helper()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	ctx := context.Background()
	setFrontierCurrentPlan(t, ctx, store, workspaceID, []FrontierPlanUnit{
		{UnitID: "P3-T1", DependsOn: []string{}},
		{UnitID: "P3-T2", DependsOn: []string{"P3-T1"}},
	})
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, workspaceID, closure, authorityID
}

// seedPlanGroundedTicketCandidate inserts an immutable delivery_ticket
// planning candidate bound to the current closure and authority basis with its
// exact candidate approval, mirroring the review-ready explicit approval
// transition of the planning candidate lifecycle. The candidate bytes are
// staged as a real artifact-store file so the production read verifies them.
func seedPlanGroundedTicketCandidate(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string, candidateBytes []byte, ticketID string, revision int64) (candidateID, approvalID string, workspaceVersion int64) {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := store.ArtifactStore().Begin("feature-discovery/" + workspace.WorkspaceID + "/" + ticketID + "-" + digestBytes(candidateBytes)[:16])
	if err != nil {
		t.Fatal(err)
	}
	file, err := batch.Stage("planning_candidate_delivery_ticket", "candidate.json", "application/json", candidateBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.Promote(); err != nil {
		t.Fatal(err)
	}
	if err := batch.PrepareCommit(); err != nil {
		t.Fatal(err)
	}
	batch.Commit()
	var artifactRowID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES (?, ?, ?, ?, 'application/json', ?) RETURNING id`,
		"discovery-artifact-grounded-"+ticketID+"-"+file.SHA256[:16], workspace.ID, file.RelativePath, file.SHA256, file.SizeBytes).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	var packetRowID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ? ORDER BY id DESC LIMIT 1`, workspace.ID).Scan(&packetRowID); err != nil {
		t.Fatal(err)
	}
	candidateID = "candidate-grounded-" + ticketID + "-" + file.SHA256[:16]
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO planning_candidates (candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, authority_revision_row_id, repo_target, branch, base_commit, destination, created_identity) VALUES (?, ?, 'delivery_ticket', ?, ?, ?, ?, ?, ?, 'relay', 'main', ?, 'direct_delivery_ticket', 'planner')`,
		candidateID, workspace.ID, workspace.FeatureSlug+".ticket-"+ticketID+".r"+strconv.FormatInt(revision, 10)+".delivery-ticket.json", artifactRowID, file.SHA256, file.SizeBytes, packetRowID, workspace.CurrentAuthorityRevisionRowID, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	approvalID = "candidate-approval-grounded-" + ticketID + "-" + file.SHA256[:16]
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO planning_candidate_approvals (approval_id, candidate_row_id, candidate_artifact_row_id, candidate_sha256, candidate_size_bytes, operator_confirmation_evidence, created_identity) VALUES (?, (SELECT id FROM planning_candidates WHERE candidate_id = ?), ?, ?, ?, 'exact candidate confirmed', 'operator')`,
		approvalID, candidateID, artifactRowID, file.SHA256, file.SizeBytes); err != nil {
		t.Fatal(err)
	}
	return candidateID, approvalID, workspace.Version
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func TestPlanGroundedTicketProductionBindsCurrentPlannedUnit(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := planGroundedCandidateFixture(t)
	// The planned dependency unit is realized through the ordinary route first
	// (equivalent to explicit ordinary authoring outside the candidate flow).
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P3-T1", 60, 0, "realized")

	bytes := planGroundedTicketJSON("ticket", "P3-T2", 1, `[{"ticket_id":"P3-T1","revision":1}]`)
	candidateID, approvalID, workspaceVersion := seedPlanGroundedTicketCandidate(t, ctx, store, workspaceID, bytes, "P3-T2", 1)
	produced, err := service.PromoteApprovedDeliveryTicketCandidate(ctx, CandidateProductionInput{CandidateID: candidateID, ApprovalID: approvalID, ExpectedVersion: workspaceVersion, ExternalPriority: 50, CreatedIdentity: "planner"})
	if err != nil {
		t.Fatal(err)
	}
	if produced.Ticket.TicketID != "P3-T2" || produced.Revision.RevisionNumber != 1 {
		t.Fatalf("produced = %#v", produced)
	}
	link, err := store.GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, produced.Ticket.ID)
	if err != nil {
		t.Fatalf("plan unit link = %v", err)
	}
	plan, err := store.GetDeliveryPlanByRowID(ctx, workspaceCurrentPlanRowID(t, ctx, store, workspaceID))
	if err != nil {
		t.Fatal(err)
	}
	if link.PlanRowID != plan.ID {
		t.Fatalf("link plan = %d, want %d", link.PlanRowID, plan.ID)
	}
	units, err := store.ListDeliveryPlanUnitsByPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var unitRowID int64
	for _, unit := range units {
		if unit.UnitID == "P3-T2" {
			unitRowID = unit.ID
		}
	}
	if link.UnitRowID != unitRowID {
		t.Fatalf("link unit = %d, want %d", link.UnitRowID, unitRowID)
	}
}

func TestPlanGroundedTicketProductionRejectsUnboundAndDivergentCandidates(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := planGroundedCandidateFixture(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P3-T1", 60, 0, "realized")

	t.Run("unbound ticket id", func(t *testing.T) {
		bytes := planGroundedTicketJSON("ticket", "P9-T9", 1, `[]`)
		candidateID, approvalID, workspaceVersion := seedPlanGroundedTicketCandidate(t, ctx, store, workspaceID, bytes, "P9-T9", 1)
		if _, err := service.PromoteApprovedDeliveryTicketCandidate(ctx, CandidateProductionInput{CandidateID: candidateID, ApprovalID: approvalID, ExpectedVersion: workspaceVersion, ExternalPriority: 10, CreatedIdentity: "planner"}); !errors.Is(err, ErrPlanUnitUnbound) {
			t.Fatalf("unbound production = %v", err)
		}
		if _, err := store.GetDeliveryTicketByTicketID(ctx, "P9-T9"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("unbound production created a ticket: %v", err)
		}
	})

	t.Run("divergent topology", func(t *testing.T) {
		bytes := planGroundedTicketJSON("ticket", "P3-T2", 1, `[]`)
		candidateID, approvalID, workspaceVersion := seedPlanGroundedTicketCandidate(t, ctx, store, workspaceID, bytes, "P3-T2", 1)
		if _, err := service.PromoteApprovedDeliveryTicketCandidate(ctx, CandidateProductionInput{CandidateID: candidateID, ApprovalID: approvalID, ExpectedVersion: workspaceVersion, ExternalPriority: 10, CreatedIdentity: "planner"}); !errors.Is(err, ErrPlanTopologyDivergence) {
			t.Fatalf("divergent production = %v", err)
		}
		if _, err := store.GetDeliveryTicketByTicketID(ctx, "P3-T2"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("divergent production created a ticket: %v", err)
		}
	})

	t.Run("unknown dependency in topology", func(t *testing.T) {
		bytes := planGroundedTicketJSON("ticket", "P3-T2", 1, `[{"ticket_id":"P9-T9","revision":1}]`)
		candidateID, approvalID, workspaceVersion := seedPlanGroundedTicketCandidate(t, ctx, store, workspaceID, bytes, "P3-T2", 1)
		if _, err := service.PromoteApprovedDeliveryTicketCandidate(ctx, CandidateProductionInput{CandidateID: candidateID, ApprovalID: approvalID, ExpectedVersion: workspaceVersion, ExternalPriority: 10, CreatedIdentity: "planner"}); !errors.Is(err, ErrPlanTopologyDivergence) {
			t.Fatalf("unknown-dependency production = %v", err)
		}
	})
}

func TestOrdinaryPublishRemainsValidUnderCurrentPlan(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := planGroundedCandidateFixture(t)
	// Ordinary authoring (the remediation route) never binds a planned unit:
	// it publishes any ticket identity and topology without Plan grounding.
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P3-T1", 60, 0, "planned realized")
	published, err := service.Publish(ctx, publishInput(workspaceID, "P9-R1", 30, 0, closure, "remediation", ""))
	if err != nil {
		t.Fatal(err)
	}
	if published.Ticket.TicketID != "P9-R1" {
		t.Fatalf("ordinary publication = %#v", published)
	}
	if _, err := store.GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, published.Ticket.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ordinary ticket gained a plan unit link: %v", err)
	}
	// A replacement revision of a realized planned ticket through the ordinary
	// route also remains valid and does not alter the recorded unit binding.
	replacement, err := service.Publish(ctx, publishInput(workspaceID, "P3-T1", 60, 1, closure, "replacement", ""))
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Revision.RevisionNumber != 2 {
		t.Fatalf("replacement revision = %#v", replacement)
	}
	if _, err := store.GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, replacement.Ticket.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("ordinary replacement gained a plan unit link: %v", err)
	}
}

func workspaceCurrentPlanRowID(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string) int64 {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.CurrentDeliveryPlanRowID.Valid {
		t.Fatal("workspace has no current delivery plan")
	}
	return workspace.CurrentDeliveryPlanRowID.Int64
}

var _ = workflowstore.DeliveryPlan{}
