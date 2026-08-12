package tickets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func frontierService(t *testing.T) (*Service, *workflowstore.Store, string, workflowstore.SourceVaultClosure, string) {
	t.Helper()
	store, workspaceID, closure, authorityID := ticketFixture(t)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return service, store, workspaceID, closure, authorityID
}

func TestFrontierV2EmptyWorkspaceIsValidEmptyFrontier(t *testing.T) {
	ctx := context.Background()
	service, _, _, _, _ := frontierService(t)
	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if frontier.FeatureSlug != "ticket" || frontier.RequestedUnitID != nil || frontier.CurrentPlan != nil ||
		len(frontier.Entries) != 0 || len(frontier.ProgramCandidates) != 0 {
		t.Fatalf("empty frontier = %#v", frontier)
	}
	raw, err := json.Marshal(frontier)
	if err != nil {
		t.Fatal(err)
	}
	if order := jsonKeyOrder(raw, "feature_slug", "requested_unit_id", "current_plan", "entries", "program_candidates"); order != "" {
		t.Fatalf("top-level order: %s; raw = %s", order, raw)
	}
}

func TestFrontierV2OrdersTicketsByTicketIDWhenNoPlan(t *testing.T) {
	ctx := context.Background()
	service, _, workspaceID, closure, authorityID := frontierService(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-B", 50, 0, "b")
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 60, 0, "a")
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-C", 40, 0, "c")

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 3 {
		t.Fatalf("entries = %#v", frontier.Entries)
	}
	for index, want := range []string{"P4-A", "P4-B", "P4-C"} {
		if got := *frontier.Entries[index].TicketID; got != want {
			t.Fatalf("entry %d = %q, want %q", index, got, want)
		}
		if frontier.Entries[index].UnitID != nil || frontier.Entries[index].Revision == nil || frontier.Entries[index].SHA256 == nil {
			t.Fatalf("entry %d identity = %#v", index, frontier.Entries[index])
		}
		if frontier.Entries[index].State != FrontierStateEligible {
			t.Fatalf("entry %d state = %q", index, frontier.Entries[index].State)
		}
	}
	if got := strings.Join(frontier.ProgramCandidates, ","); got != "P4-A,P4-B,P4-C" {
		t.Fatalf("program candidates = %q", got)
	}
	if frontier.CurrentPlan != nil {
		t.Fatalf("unexpected current plan = %#v", frontier.CurrentPlan)
	}
}

func TestFrontierV2ReportsAuthoredAndBlockedDependency(t *testing.T) {
	ctx := context.Background()
	service, _, workspaceID, closure, authorityID := frontierService(t)

	dependency, err := service.Publish(ctx, publishInput(workspaceID, "P4-T1", 70, 0, closure, "dependency", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: dependency.Ticket.TicketID, RevisionRowID: dependency.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "dependency approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, publishInput(workspaceID, "P4-T2", 60, 0, closure, "authored", "")); err != nil {
		t.Fatal(err)
	}
	dependentInput := publishInput(workspaceID, "P4-T3", 50, 0, closure, "dependent", "")
	dependentInput.Revision.Dependencies = []DependencyInput{{RevisionRowID: dependency.Revision.ID, Outcome: "satisfied"}}
	dependent, err := service.Publish(ctx, dependentInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: dependent.Ticket.TicketID, RevisionRowID: dependent.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "dependent approved"}); err != nil {
		t.Fatal(err)
	}

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierV2Entry{}
	for _, entry := range frontier.Entries {
		byID[*entry.TicketID] = entry
	}
	if entry := byID["P4-T2"]; entry.State != FrontierStateAuthored || entry.Revision != nil || entry.SHA256 != nil {
		t.Fatalf("unapproved ticket state = %#v", entry)
	}
	blocked := byID["P4-T3"]
	if blocked.State != FrontierStateBlocked || blocked.BlockReason == nil || *blocked.BlockReason != frontierBlockDependencyUnmet {
		t.Fatalf("dependent state = %#v", blocked)
	}
	if strings.Join(blocked.DependsOn, ",") != "P4-T1" || strings.Join(blocked.UnmetDependencies, ",") != "P4-T1" {
		t.Fatalf("dependent dependencies = %#v", blocked)
	}
	eligible := byID["P4-T1"]
	if eligible.State != FrontierStateEligible {
		t.Fatalf("dependency state = %#v", eligible)
	}
	if strings.Join(frontier.ProgramCandidates, ",") != "P4-T1" {
		t.Fatalf("program candidates = %#v", frontier.ProgramCandidates)
	}
}

func TestFrontierV2GoverningAuthorityBlockWhenSourceBasisUnavailable(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := frontierService(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-T1", 60, 0, "ready")

	// The closure state machine permits ready -> releasing -> released only,
	// and a non-ready closure must not carry verified_at.
	if _, err := store.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'releasing', verified_at = NULL WHERE id = ?`, closure.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE source_vault_closures SET state = 'released', released_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, closure.ID); err != nil {
		t.Fatal(err)
	}
	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 1 {
		t.Fatalf("entries = %#v", frontier.Entries)
	}
	entry := frontier.Entries[0]
	if entry.State != FrontierStateBlocked || entry.BlockReason == nil || *entry.BlockReason != frontierBlockGoverningAuthorityMissing {
		t.Fatalf("entry = %#v", entry)
	}
	if len(frontier.ProgramCandidates) != 0 {
		t.Fatalf("candidates = %#v", frontier.ProgramCandidates)
	}
}

func TestFrontierV2SelectedEntryKeepsRecordedRouteFact(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := frontierService(t)
	published := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-T1", 60, 0, "selected")
	// Resolve the approval identity before the transaction: the store owns a
	// single database connection, so a pool query inside an open transaction
	// would wait on the transaction's own connection.
	approvalRowID := mustFrontierApprovalRowID(t, ctx, store, published.Revision.ID)

	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-frontier", WorkspaceRowID: published.Ticket.WorkspaceRowID, State: "active", Rationale: "select ready ticket"})
		if err != nil {
			return err
		}
		_, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: published.Revision.ID, ApprovalRowID: approvalRowID})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 1 || frontier.Entries[0].State != FrontierStateSelected {
		t.Fatalf("entries = %#v", frontier.Entries)
	}
	if len(frontier.ProgramCandidates) != 0 {
		t.Fatalf("selected ticket must not be a program candidate: %#v", frontier.ProgramCandidates)
	}
}

func TestFrontierV2FilterNarrowsEntriesOnly(t *testing.T) {
	ctx := context.Background()
	service, _, workspaceID, closure, authorityID := frontierService(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 60, 0, "a")
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-B", 50, 0, "b")

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket", RequestedUnitID: "P4-A"})
	if err != nil {
		t.Fatal(err)
	}
	if frontier.RequestedUnitID == nil || *frontier.RequestedUnitID != "P4-A" {
		t.Fatalf("requested_unit_id echo = %#v", frontier.RequestedUnitID)
	}
	if len(frontier.Entries) != 1 || *frontier.Entries[0].TicketID != "P4-A" {
		t.Fatalf("filtered entries = %#v", frontier.Entries)
	}
	if got := strings.Join(frontier.ProgramCandidates, ","); got != "P4-A,P4-B" {
		t.Fatalf("whole-workspace candidates = %q", got)
	}
}

func TestFrontierV2WellFormedFilterNamingNothingIsEmptyProjection(t *testing.T) {
	ctx := context.Background()
	service, _, workspaceID, closure, authorityID := frontierService(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 60, 0, "a")

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket", RequestedUnitID: "P9-Z9"})
	if err != nil {
		t.Fatal(err)
	}
	if frontier.RequestedUnitID == nil || *frontier.RequestedUnitID != "P9-Z9" || len(frontier.Entries) != 0 {
		t.Fatalf("no-match projection = %#v", frontier)
	}
	if got := strings.Join(frontier.ProgramCandidates, ","); got != "P4-A" {
		t.Fatalf("whole-workspace candidates = %q", got)
	}
}

func TestFrontierV2RejectsMalformedRequests(t *testing.T) {
	ctx := context.Background()
	service, _, _, _, _ := frontierService(t)
	for _, input := range []FrontierV2Input{
		{ProjectID: "", FeatureSlug: "ticket"},
		{ProjectID: "project-ticket", FeatureSlug: ""},
		{ProjectID: "project-ticket", FeatureSlug: "Ticket"},
		{ProjectID: "project-ticket", FeatureSlug: "ticket", RequestedUnitID: "lower"},
	} {
		if _, err := service.ReadFrontier(ctx, input); err == nil {
			t.Fatalf("malformed request admitted: %#v", input)
		}
	}
	if _, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-missing", FeatureSlug: "ticket"}); err == nil {
		t.Fatal("unknown project admitted")
	}
	if _, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "missing"}); err == nil {
		t.Fatal("unknown feature_slug admitted")
	}
}

func TestFrontierV2DownstreamUnlocksFollowTransitiveDependencies(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := frontierService(t)

	completed := publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-A", 60, 0, "completed")
	satisfyFrontierRevision(t, ctx, store, completed.Revision.ID)

	mid := publishInput(workspaceID, "P4-B", 50, 0, closure, "mid", "")
	mid.Revision.Dependencies = []DependencyInput{{RevisionRowID: completed.Revision.ID, Outcome: "satisfied"}}
	midPublished, err := service.Publish(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: midPublished.Ticket.TicketID, RevisionRowID: midPublished.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "mid approved"}); err != nil {
		t.Fatal(err)
	}

	leaf := publishInput(workspaceID, "P4-C", 40, 0, closure, "leaf", "")
	leaf.Revision.Dependencies = []DependencyInput{{RevisionRowID: midPublished.Revision.ID, Outcome: "satisfied"}}
	leafPublished, err := service.Publish(ctx, leaf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: leafPublished.Ticket.TicketID, RevisionRowID: leafPublished.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "leaf approved"}); err != nil {
		t.Fatal(err)
	}

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FrontierV2Entry{}
	for _, entry := range frontier.Entries {
		byID[*entry.TicketID] = entry
	}
	if got := strings.Join(byID["P4-A"].DownstreamUnits, ","); got != "P4-B,P4-C" {
		t.Fatalf("P4-A downstream = %q", got)
	}
	if got := strings.Join(byID["P4-B"].DownstreamUnits, ","); got != "P4-C" {
		t.Fatalf("P4-B downstream = %q", got)
	}
	if len(byID["P4-C"].DownstreamUnits) != 0 {
		t.Fatalf("P4-C downstream = %#v", byID["P4-C"].DownstreamUnits)
	}
	if byID["P4-A"].State != FrontierStateCompleted || byID["P4-B"].State != FrontierStateEligible {
		t.Fatalf("states = %#v", frontier.Entries)
	}
	blocked := byID["P4-C"]
	if blocked.State != FrontierStateBlocked || blocked.BlockReason == nil || *blocked.BlockReason != frontierBlockDependencyUnmet ||
		strings.Join(blocked.UnmetDependencies, ",") != "P4-B" {
		t.Fatalf("leaf state = %#v", blocked)
	}
}

func TestFrontierV2PlannedUnitsPrecedeTicketsWithCurrentPlan(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := frontierService(t)
	publishApprovedTicket(t, ctx, service, workspaceID, closure, authorityID, "P4-T1", 60, 0, "realized")
	setFrontierCurrentPlan(t, ctx, store, workspaceID, []FrontierPlanUnit{
		{UnitID: "P4-T1", DependsOn: []string{"P4-T2"}},
		{UnitID: "P4-T2", DependsOn: []string{}},
		{UnitID: "P4-T3", DependsOn: []string{"P4-T1", "P4-T2"}},
	})

	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if frontier.CurrentPlan == nil || frontier.CurrentPlan.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("current plan = %#v", frontier.CurrentPlan)
	}
	if len(frontier.Entries) != 3 {
		t.Fatalf("entries = %#v", frontier.Entries)
	}
	first := frontier.Entries[0]
	if first.UnitID == nil || *first.UnitID != "P4-T1" || first.TicketID == nil || *first.TicketID != "P4-T1" {
		t.Fatalf("realized entry identity = %#v", first)
	}
	if first.State != FrontierStateEligible {
		t.Fatalf("realized entry state = %#v", first)
	}
	second := frontier.Entries[1]
	if second.UnitID == nil || *second.UnitID != "P4-T2" || second.TicketID != nil || second.State != FrontierStatePlanned {
		t.Fatalf("planned-only entry = %#v", second)
	}
	if strings.Join(second.DependsOn, ",") != "" || len(second.UnmetDependencies) != 0 {
		t.Fatalf("planned dependency lists = %#v", second)
	}
	third := frontier.Entries[2]
	if third.State != FrontierStatePlanned || strings.Join(third.DependsOn, ",") != "P4-T1,P4-T2" {
		t.Fatalf("planned topology entry = %#v", third)
	}
	if got := strings.Join(third.DownstreamUnits, ","); got != "" {
		t.Fatalf("P4-T3 downstream = %q", got)
	}
	if got := strings.Join(first.DownstreamUnits, ","); got != "P4-T3" {
		t.Fatalf("P4-T1 downstream = %q", got)
	}
	if len(frontier.ProgramCandidates) != 1 || frontier.ProgramCandidates[0] != "P4-T1" {
		t.Fatalf("program candidates = %#v", frontier.ProgramCandidates)
	}
}

func TestFrontierV2CancelledRealizedTicketProjectsAsPlanned(t *testing.T) {
	ctx := context.Background()
	service, store, workspaceID, closure, authorityID := frontierService(t)
	first, err := service.Publish(ctx, publishInput(workspaceID, "P4-T1", 60, 0, closure, "first", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, ApproveInput{TicketID: first.Ticket.TicketID, RevisionRowID: first.Revision.ID, AuthorityRevisionID: authorityID, Rationale: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, publishInput(workspaceID, "P4-T1", 60, 1, closure, "cancelled", "operator cancelled")); err != nil {
		t.Fatal(err)
	}
	setFrontierCurrentPlan(t, ctx, store, workspaceID, []FrontierPlanUnit{{UnitID: "P4-T1", DependsOn: []string{}}})
	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 1 {
		t.Fatalf("entries = %#v", frontier.Entries)
	}
	entry := frontier.Entries[0]
	if entry.State != FrontierStatePlanned || entry.TicketID != nil || entry.UnitID == nil || *entry.UnitID != "P4-T1" || entry.Revision != nil {
		t.Fatalf("cancelled realized ticket = %#v", entry)
	}
}

func TestFrontierV2CancelledTicketWithoutPlanAppearsNowhere(t *testing.T) {
	ctx := context.Background()
	service, _, workspaceID, closure, _ := frontierService(t)
	if _, err := service.Publish(ctx, publishInput(workspaceID, "P4-T1", 60, 0, closure, "cancelled", "operator cancelled")); err != nil {
		t.Fatal(err)
	}
	frontier, err := service.ReadFrontier(ctx, FrontierV2Input{ProjectID: "project-ticket", FeatureSlug: "ticket"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier.Entries) != 0 || len(frontier.ProgramCandidates) != 0 {
		t.Fatalf("cancelled ticket leaked into projection = %#v", frontier)
	}
}

func TestFrontierV2EntryJSONFieldOrderIsCanonical(t *testing.T) {
	entry := plannedUnitEntry(FrontierPlanUnit{UnitID: "P4-T1", DependsOn: []string{}})
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if order := jsonKeyOrder(raw, "unit_id", "ticket_id", "revision", "sha256", "state", "block_reason", "depends_on", "unmet_dependencies", "downstream_units"); order != "" {
		t.Fatalf("entry order: %s; raw = %s", order, raw)
	}
}

// --- helpers ---

// setFrontierCurrentPlan writes the durable current approved Delivery Plan
// rows for the workspace fixture. The Delivery Plan promotion lifecycle is
// concurrently owned; this fixture records the same durable rows directly and
// satisfies the exact binding guards.
func setFrontierCurrentPlan(t *testing.T, ctx context.Context, store *workflowstore.Store, workspaceID string, units []FrontierPlanUnit) {
	t.Helper()
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	var artifactRowID, manifestRowID, revisionRowID, packetRowID, candidateRowID, planRowID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES (?, ?, ?, ?, 'application/json', ?) RETURNING id`, "discovery-artifact-frontier-plan", workspace.ID, "feature-discovery/"+workspace.WorkspaceID+"/plan/checkout.delivery-plan.json", sha, 11).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES (?, ?, ?, ?, 'application/vnd.relay.feature-discovery-closure+json', ?) RETURNING id`, "discovery-artifact-frontier-manifest", workspace.ID, "feature-discovery/"+workspace.WorkspaceID+"/plan/manifest.json", sha, 11).Scan(&manifestRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity) VALUES ('discovery-revision-frontier-plan', ?, 1, ?, 'frontier-test') RETURNING id`, workspace.ID, artifactRowID).Scan(&revisionRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES ('discovery-packet-frontier-plan', ?, ?, 'direct_delivery_ticket', ?, ?, 11, 'application/vnd.relay.feature-discovery-closure+json') RETURNING id`, workspace.ID, revisionRowID, manifestRowID, sha).Scan(&packetRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revisionRowID, packetRowID, workspace.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO planning_candidates (candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, repo_target, branch, base_commit, destination, created_identity) VALUES ('candidate-frontier-plan', ?, 'delivery_plan', 'checkout.delivery-plan.json', ?, ?, 11, ?, 'relay', 'main', ?, 'direct_delivery_ticket', 'frontier-test') RETURNING id`, workspace.ID, artifactRowID, sha, packetRowID, strings.Repeat("a", 40)).Scan(&candidateRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_plans (plan_id, workspace_row_id, candidate_row_id, artifact_row_id, artifact_sha256, artifact_size_bytes, feature_slug, goal, context, created_identity) VALUES ('delivery-plan-frontier', ?, ?, ?, ?, 11, 'ticket', 'Plan the bounded outcome.', 'Plan context.', 'frontier-test') RETURNING id`, workspace.ID, candidateRowID, artifactRowID, sha).Scan(&planRowID); err != nil {
		t.Fatal(err)
	}
	unitRowIDs := make([]int64, 0, len(units))
	for index, unit := range units {
		var unitRowID int64
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO delivery_plan_units (plan_row_id, sequence, unit_id, goal) VALUES (?, ?, ?, ?) RETURNING id`, planRowID, index+1, unit.UnitID, "Planned goal for "+unit.UnitID).Scan(&unitRowID); err != nil {
			t.Fatal(err)
		}
		unitRowIDs = append(unitRowIDs, unitRowID)
	}
	unitRowByID := make(map[string]int64, len(units))
	for index, unit := range units {
		unitRowByID[unit.UnitID] = unitRowIDs[index]
	}
	for index, unit := range units {
		for dependencyIndex, dependency := range unit.DependsOn {
			if _, err := store.DB().ExecContext(ctx, `INSERT INTO delivery_plan_unit_dependencies (unit_row_id, sequence, depends_on_unit_row_id) VALUES (?, ?, ?)`, unitRowIDs[index], dependencyIndex+1, unitRowByID[dependency]); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Re-read the workspace: the discovery-pointer update above advanced the
	// version, and currentness recording must target the fresh version.
	workspace, err = store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetCurrentDeliveryPlan(ctx, planRowID, workspaceID, workspace.Version); err != nil {
		t.Fatal(err)
	}
}

func mustFrontierApprovalRowID(t *testing.T, ctx context.Context, store *workflowstore.Store, revisionRowID int64) int64 {
	t.Helper()
	approvals, err := store.ListDeliveryTicketRevisionApprovals(ctx, revisionRowID)
	if err != nil {
		t.Fatal(err)
	}
	for _, approval := range approvals {
		if approval.ApprovalKind == "delivery" && approval.ApprovalState == "approved" {
			return approval.ID
		}
	}
	t.Fatalf("no approved delivery approval for revision %d", revisionRowID)
	return 0
}

func satisfyFrontierRevision(t *testing.T, ctx context.Context, store *workflowstore.Store, revisionRowID int64) {
	t.Helper()
	// The satisfaction guard requires the complete audit/package chain; the
	// frontier only reads the recorded completion fact, so the fixture records
	// the row directly with the guard suspended (the established
	// satisfaction-seeding pattern).
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER IF EXISTS delivery_ticket_revision_satisfaction_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO delivery_ticket_revision_satisfactions (delivery_ticket_revision_row_id, audit_ticket_revision_decision_row_id) VALUES (?, ?)`, revisionRowID, revisionRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
}

// jsonKeyOrder verifies raw contains the keys in the given relative order and
// returns a description when the order differs.
func jsonKeyOrder(raw []byte, keys ...string) string {
	position := -1
	for _, key := range keys {
		index := strings.Index(string(raw), `"`+key+`"`)
		if index < 0 {
			return "missing key " + key
		}
		if index <= position {
			return "key " + key + " out of order"
		}
		position = index
	}
	return ""
}
