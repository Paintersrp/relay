package workflowstore

import (
	"context"
	"strings"
	"testing"
)

func TestDeliveryPlanPersistenceUnitsDependenciesAndTicketLink(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)

	var projectID, workspaceID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO projects (project_id, name) VALUES ('project-delivery-plan', 'Delivery Plan') RETURNING id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspaces (workspace_id, project_row_id, feature_slug) VALUES ('workspace-delivery-plan', ?, 'delivery-plan') RETURNING id`, projectID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('plan-repo', 'C:/plan-repo', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, "workspace-delivery-plan")
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)

	var artifactRowID, manifestRowID, revisionRowID, packetRowID, candidateRowID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-plan-test', ?, 'feature-discovery/workspace-delivery-plan/plan/plan.json', ?, 'application/json', 4) RETURNING id`, workspaceID, sha).Scan(&artifactRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_artifacts (discovery_artifact_id, workspace_row_id, relative_path, sha256, media_type, size_bytes) VALUES ('discovery-artifact-plan-manifest', ?, 'feature-discovery/workspace-delivery-plan/plan/manifest.json', ?, 'application/vnd.relay.feature-discovery-closure+json', 4) RETURNING id`, workspaceID, sha).Scan(&manifestRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_integrated_discovery_revisions (discovery_revision_id, workspace_row_id, revision_number, artifact_row_id, created_identity) VALUES ('discovery-revision-plan-test', ?, 1, ?, 'plan-test') RETURNING id`, workspaceID, artifactRowID).Scan(&revisionRowID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO feature_workspace_discovery_closure_packets (closure_packet_id, workspace_row_id, closing_revision_row_id, destination, manifest_artifact_row_id, manifest_sha256, manifest_size_bytes, manifest_media_type) VALUES ('discovery-packet-plan-test', ?, ?, 'shared_design', ?, ?, 4, 'application/vnd.relay.feature-discovery-closure+json') RETURNING id`, workspaceID, revisionRowID, manifestRowID, sha).Scan(&packetRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE feature_workspaces SET current_discovery_revision_row_id = ?, current_discovery_closure_packet_row_id = ?, version = version + 1 WHERE id = ?`, revisionRowID, packetRowID, workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO planning_candidates (candidate_id, workspace_row_id, family, filename, artifact_row_id, artifact_sha256, artifact_size_bytes, discovery_closure_packet_row_id, repo_target, branch, base_commit, destination, created_identity) VALUES ('candidate-plan-test', ?, 'delivery_plan', 'delivery-plan.delivery-plan.json', ?, ?, 4, ?, 'plan-repo', 'main', ?, 'shared_design', 'plan-test') RETURNING id`, workspaceID, artifactRowID, sha, packetRowID, strings.Repeat("b", 40)).Scan(&candidateRowID); err != nil {
		t.Fatal(err)
	}

	var plan DeliveryPlan
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		plan, err = tx.CreateDeliveryPlan(ctx, CreateDeliveryPlanParams{
			PlanID: NewDeliveryPlanID(), WorkspaceRowID: workspaceID, CandidateRowID: candidateRowID,
			ArtifactRowID: artifactRowID, ArtifactSha256: sha, ArtifactSizeBytes: 4,
			FeatureSlug: "delivery-plan", Goal: "Plan the bounded outcome.", Context: "Plan context.", CreatedIdentity: "plan-test",
		})
		if err != nil {
			return err
		}
		first, err := tx.CreateDeliveryPlanUnit(ctx, CreateDeliveryPlanUnitParams{PlanRowID: plan.ID, Sequence: 1, UnitID: "P1-T1", Goal: "First unit."})
		if err != nil {
			return err
		}
		second, err := tx.CreateDeliveryPlanUnit(ctx, CreateDeliveryPlanUnitParams{PlanRowID: plan.ID, Sequence: 2, UnitID: "P1-T2", Goal: "Second unit."})
		if err != nil {
			return err
		}
		if _, err := tx.CreateDeliveryPlanUnitDependency(ctx, CreateDeliveryPlanUnitDependencyParams{UnitRowID: second.ID, Sequence: 1, DependsOnUnitRowID: first.ID}); err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, CreateDeliveryTicketParams{TicketID: "P1-T2", WorkspaceRowID: workspaceID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		if _, err := tx.CreateDeliveryTicketPlanUnitLink(ctx, CreateDeliveryTicketPlanUnitLinkParams{LinkID: NewDeliveryPlanUnitLinkID(), PlanRowID: plan.ID, UnitRowID: second.ID, DeliveryTicketRowID: ticket.ID}); err != nil {
			return err
		}
		// The discovery-pointer fixture update above advanced the workspace
		// version by one, so currentness records on the fresh version.
		updated, err := tx.SetCurrentDeliveryPlan(ctx, plan.ID, workspace.WorkspaceID, workspace.Version+1)
		if err != nil {
			return err
		}
		if !updated.CurrentDeliveryPlanRowID.Valid || updated.CurrentDeliveryPlanRowID.Int64 != plan.ID || updated.Version != workspace.Version+2 {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.GetDeliveryPlanByRowID(ctx, plan.ID)
	if err != nil || loaded.Goal != "Plan the bounded outcome." || loaded.CandidateRowID != candidateRowID {
		t.Fatalf("plan read = %#v, %v", loaded, err)
	}
	units, err := store.ListDeliveryPlanUnitsByPlan(ctx, plan.ID)
	if err != nil || len(units) != 2 || units[0].UnitID != "P1-T1" || units[1].UnitID != "P1-T2" {
		t.Fatalf("plan units = %#v, %v", units, err)
	}
	dependencies, err := store.ListDeliveryPlanUnitDependenciesByUnit(ctx, units[1].ID)
	if err != nil || len(dependencies) != 1 || dependencies[0].DependsOnUnitRowID != units[0].ID {
		t.Fatalf("plan dependencies = %#v, %v", dependencies, err)
	}
	link, err := store.GetDeliveryTicketPlanUnitLinkByUnitRowID(ctx, units[1].ID)
	if err != nil || link.PlanRowID != plan.ID || link.UnitRowID != units[1].ID {
		t.Fatalf("plan unit link = %#v, %v", link, err)
	}
	if _, err := store.GetDeliveryTicketPlanUnitLinkByTicketRowID(ctx, link.DeliveryTicketRowID); err != nil {
		t.Fatalf("ticket link = %v", err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, "workspace-delivery-plan")
	if err != nil || !current.CurrentDeliveryPlanRowID.Valid || current.CurrentDeliveryPlanRowID.Int64 != plan.ID {
		t.Fatalf("workspace currentness = %#v, %v", current.CurrentDeliveryPlanRowID, err)
	}

	if _, err := store.DB().Exec(`UPDATE delivery_plans SET goal = 'mutated' WHERE id = ?`, plan.ID); err == nil {
		t.Fatal("delivery plan was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM delivery_plans WHERE id = ?`, plan.ID); err == nil {
		t.Fatal("delivery plan was deletable")
	}
	if _, err := store.DB().Exec(`UPDATE delivery_plan_units SET unit_id = 'mutated' WHERE id = ?`, units[0].ID); err == nil {
		t.Fatal("plan unit was mutable")
	}
	if _, err := store.DB().Exec(`DELETE FROM delivery_plan_unit_dependencies WHERE id = ?`, dependencies[0].ID); err == nil {
		t.Fatal("plan dependency was deletable")
	}
	if _, err := store.DB().Exec(`UPDATE delivery_ticket_plan_unit_links SET link_id = 'mutated' WHERE id = ?`, link.ID); err == nil {
		t.Fatal("plan unit link was mutable")
	}
	// A second Ticket cannot realize the same planned unit.
	ticket2, err := store.GetDeliveryTicketByTicketID(ctx, "P1-T2")
	if err != nil {
		t.Fatal(err)
	}
	otherTicket, err := store.GetDeliveryTicketByTicketID(ctx, "P1-T2")
	if err != nil || ticket2.ID != otherTicket.ID {
		t.Fatalf("fixture ticket = %#v %#v %v", ticket2, otherTicket, err)
	}
}
