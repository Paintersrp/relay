package features

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestDiscoveryLifecycleAdoptionIsExplicitAndOneWay(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-lifecycle", "discovery-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID); err != nil || assessment.Currentness != DiscoveryNotClosed || assessment.State != "" {
		t.Fatalf("unadopted assessment = %#v, %v", assessment, err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("capability enabled adoption error = %v", err)
	}
	adoption, workspace, err := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
	if err != nil || adoption.WorkspaceRowID != workspace.ID {
		t.Fatalf("adoption = %#v, %#v, %v", adoption, workspace, err)
	}
	if _, _, err := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); !errors.Is(err, ErrDiscoveryAlreadyAdopted) {
		t.Fatalf("duplicate adoption error = %v", err)
	}
}

func TestDiscoveryLifecycleAdoptionRejectsActiveProductionMutation(t *testing.T) {
	ctx := context.Background()
	store, requirementsArtifact, _ := openFeatureServiceStore(t, ctx)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO repository_targets (repo_target, local_path, configured_branch_ref, configuration_version) VALUES ('relay', 'C:/relay', 'refs/heads/main', 1)`); err != nil {
		t.Fatal(err)
	}
	var vaultID, closureID int64
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vaults (vault_id, repo_target, relative_path) VALUES ('vault-discovery-adoption', 'relay', 'vaults/discovery-adoption') RETURNING id`).Scan(&vaultID); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `INSERT INTO source_vault_closures (closure_id, vault_row_id, commit_oid, tree_oid, generation, ref_name, state, import_started_at, verified_at) VALUES ('closure-discovery-adoption', ?, ?, ?, 1, 'refs/relay/closures/discovery-adoption', 'ready', '2026-08-05T00:00:00.000000000Z', '2026-08-05T00:00:01.000000000Z') RETURNING id`, vaultID, strings.Repeat("a", 40), strings.Repeat("b", 40)).Scan(&closureID); err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-adoption-production", "discovery-adoption-production")
	if err != nil {
		t.Fatal(err)
	}
	approval, err := service.RecordAuthorityApproval(ctx, RecordAuthorityApprovalInput{WorkspaceID: workspace.WorkspaceID, Family: "requirements", ArtifactRowID: sql.NullInt64{Int64: requirementsArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), OperatorConfirmationEvidence: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	publication, workspace, err := service.PublishAuthority(ctx, PublishAuthorityInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, Layers: []AuthorityLayerInput{{Kind: "requirements", ArtifactRowID: sql.NullInt64{Int64: requirementsArtifact, Valid: true}, ArtifactSHA256: strings.Repeat("b", 64), SourceClosureID: sql.NullInt64{Int64: closureID, Valid: true}, ApprovalRowID: sql.NullInt64{Int64: approval.Approval.ID, Valid: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		ticket, err := tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{TicketID: "TICKET-DISCOVERY-ADOPTION", WorkspaceRowID: workspace.ID, ExternalPriority: 1})
		if err != nil {
			return err
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: closureID, SourcePath: "tickets/discovery-adoption.delivery-ticket.json", Goal: "Exercise the discovery adoption production mutation guard.", Context: "An active execution must block lifecycle adoption.", TransitionApplicability: "not_required"})
		if err != nil {
			return err
		}
		if _, err = tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID); err != nil {
			return err
		}
		approval, err := tx.CreateDeliveryTicketRevisionApproval(ctx, workflowstore.CreateDeliveryTicketRevisionApprovalParams{ApprovalID: "approval-discovery-adoption", RevisionRowID: revision.ID, ApprovalKind: "delivery", ApprovalState: "approved", Rationale: "Approve the production mutation guard fixture.", SourceClosureRowID: closureID, AuthorityRevisionRowID: sql.NullInt64{Int64: publication.Revision.ID, Valid: true}})
		if err != nil {
			return err
		}
		selection, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-discovery-adoption", WorkspaceRowID: workspace.ID, State: "active", Rationale: "production mutation guard", SourceClosureRowID: sql.NullInt64{Int64: closureID, Valid: true}})
		if err != nil {
			return err
		}
		if _, err = tx.CreateDeliveryTicketSelectionMember(ctx, workflowstore.CreateDeliveryTicketSelectionMemberParams{SelectionRowID: selection.ID, Sequence: 1, RevisionRowID: revision.ID, ApprovalRowID: approval.ID}); err != nil {
			return err
		}
		packageRow, err := tx.CreateExecutionPackage(ctx, workflowstore.CreateExecutionPackageParams{PackageID: "package-discovery-adoption", SelectionRowID: selection.ID, WorkspaceRowID: workspace.ID, RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40), SourceClosureRowID: closureID, AuthorityRevisionRowID: publication.Revision.ID, PackageSha256: strings.Repeat("c", 64), AuthoritySha256: strings.Repeat("d", 64), SourceSha256: strings.Repeat("e", 64)})
		if err != nil {
			return err
		}
		run, err := tx.CreateRun(ctx, workflowstore.CreateRunParams{RunID: "run-discovery-adoption", FeatureSlug: workspace.FeatureSlug, RepoTarget: "relay", Status: workflowstore.RunStatusCreated, Branch: "main", BaseCommit: strings.Repeat("a", 40), CanonicalSHA256: strings.Repeat("1", 64)})
		if err != nil {
			return err
		}
		if _, err = tx.LinkRunToExecutionPackage(ctx, run.RunID, packageRow.ID); err != nil {
			return err
		}
		_, err = tx.CreateRepositoryBranchMutationLease(ctx, workflowstore.CreateRepositoryBranchMutationLeaseParams{LeaseID: "lease-discovery-adoption", RepoTarget: "relay", Branch: "main", OwnerKind: "run_execution", OwnerIdentity: run.RunID, UncertaintyState: workflowstore.RepositoryBranchMutationLeaseCertaintyCertain, ReconciliationState: workflowstore.RepositoryBranchMutationLeaseReconciliationNotRequired})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	before := workspace
	if _, err := store.GetDiscoveryLifecycleAdoption(ctx, before.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("adoption before rejection = %v", err)
	}
	if _, _, err := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); !errors.Is(err, ErrDiscoveryAdoptionProduction) {
		t.Fatalf("adoption error = %v", err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != before.Version {
		t.Fatalf("workspace version after rejected adoption = %d, want %d", current.Version, before.Version)
	}
	if current.DiscoveryCapabilityEnabled != before.DiscoveryCapabilityEnabled {
		t.Fatalf("discovery capability after rejected adoption = %d, want %d", current.DiscoveryCapabilityEnabled, before.DiscoveryCapabilityEnabled)
	}
	if current.CurrentDiscoveryRevisionRowID != before.CurrentDiscoveryRevisionRowID {
		t.Fatalf("current discovery revision after rejected adoption = %#v, want %#v", current.CurrentDiscoveryRevisionRowID, before.CurrentDiscoveryRevisionRowID)
	}
	if current.CurrentDiscoveryClosurePacketRowID != before.CurrentDiscoveryClosurePacketRowID {
		t.Fatalf("current discovery closure packet after rejected adoption = %#v, want %#v", current.CurrentDiscoveryClosurePacketRowID, before.CurrentDiscoveryClosurePacketRowID)
	}
	if current.Version != workspace.Version {
		t.Fatalf("workspace after rejected adoption = %#v, %v", current, err)
	}
	if _, err := store.GetDiscoveryLifecycleAdoption(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rejected adoption was persisted: %v", err)
	}
}

func TestDiscoveryLifecycleOptimisticConcurrencyHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-lifecycle-race", "lifecycle-race")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	run := func(operation func() error) int {
		var wait sync.WaitGroup
		wait.Add(2)
		results := make(chan error, 2)
		for range 2 {
			go func() {
				defer wait.Done()
				results <- operation()
			}()
		}
		wait.Wait()
		close(results)
		successes := 0
		for result := range results {
			if result == nil {
				successes++
			}
		}
		return successes
	}
	expectedAdoptionVersion := workspace.Version
	if winners := run(func() error {
		_, _, operationErr := service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: expectedAdoptionVersion, OperatorIdentity: "operator"})
		return operationErr
	}); winners != 1 {
		t.Fatalf("adoption winners = %d, want 1", winners)
	}
	workspace, err = store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# Concurrent closure\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	expectedClosureVersion := workspace.Version
	if winners := run(func() error {
		_, _, operationErr := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: expectedClosureVersion, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
		return operationErr
	}); winners != 1 {
		t.Fatalf("closure winners = %d, want 1", winners)
	}
	var packets int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?`, workspace.ID).Scan(&packets); err != nil || packets != 1 {
		t.Fatalf("closure packets = %d, err = %v", packets, err)
	}
}

func TestDiscoveryLifecycleClosesAllDestinationsAndReopensHistoricalPacket(t *testing.T) {
	destinations := []DiscoveryDestination{
		DiscoveryDestinationNoDeliveryWork,
		DiscoveryDestinationDirectDeliveryTicket,
		DiscoveryDestinationRequirements,
		DiscoveryDestinationSharedDesign,
		DiscoveryDestinationRequirementsThenSharedDesign,
		DiscoveryDestinationExistingRouteContinuation,
	}
	for index, destination := range destinations {
		t.Run(string(destination), func(t *testing.T) {
			ctx := context.Background()
			store, _, _ := openFeatureServiceStore(t, ctx)
			service, err := NewServiceWithIDs(store, &featureTestIDs{})
			if err != nil {
				t.Fatal(err)
			}
			workspace, err := createFeatureWorkspace(ctx, store, fmt.Sprintf("workspace-lifecycle-%d", index), fmt.Sprintf("lifecycle-%d", index))
			if err != nil {
				t.Fatal(err)
			}
			workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
			if err != nil {
				t.Fatal(err)
			}
			_, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("# Settled discovery\n")
			started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: destination, Continuation: "continue"})
			if err != nil {
				t.Fatal(err)
			}
			assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
			if err != nil || assessment.Destination != destination || assessment.State != DiscoveryStateActive {
				t.Fatalf("assessment = %#v, %v", assessment, err)
			}
			closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "operator"})
			if err != nil || len(closed.Members) != 1 || closed.Currentness != DiscoveryCurrent {
				t.Fatalf("closure = %#v, %v", closed, err)
			}
			verified, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
			if err != nil || verified.Currentness != DiscoveryCurrent {
				t.Fatalf("verified current packet = %#v, %v", verified, err)
			}
			replacement := []byte("# Reopened discovery\n")
			revision, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "new exact evidence", CreatedIdentity: "operator", SHA256: discoveryTestDigest(replacement), Markdown: replacement, Destination: destination})
			if err != nil || revision.PredecessorRevisionRowID.Int64 != started.Revision.ID || workspace.CurrentDiscoveryClosurePacketRowID.Valid {
				t.Fatalf("reopen = %#v, %#v, %v", revision, workspace, err)
			}
			historical, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
			if err != nil || historical.Currentness != DiscoveryHistorical {
				t.Fatalf("historical packet = %#v, %v", historical, err)
			}
		})
	}
}

func TestCanonicalDiscoveryManifestBindsMemberOrder(t *testing.T) {
	workspace := workflowstore.FeatureWorkspace{WorkspaceID: "workspace-canonical", FeatureSlug: "canonical"}
	revision := workflowstore.IntegratedDiscoveryRevision{DiscoveryRevisionID: "discovery-revision-canonical", CreatedAt: "2026-08-04T00:00:00.000Z"}
	first := discoveryPacketMemberBasis{Artifact: workflowstore.DiscoveryArtifact{DiscoveryArtifactID: "discovery-artifact-a", SHA256: string(make([]byte, 64)), MediaType: "text/plain", SizeBytes: 1}, OwnerFamily: "input", SourceIdentity: "input-a", SemanticRole: "input:001"}
	second := first
	second.Artifact.DiscoveryArtifactID = "discovery-artifact-b"
	second.SourceIdentity = "input-b"
	second.SemanticRole = "input:002"
	a := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{first, second})
	b := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{first, second})
	c := canonicalDiscoveryManifest("discovery-packet-canonical", workspace, revision, DiscoveryDestinationRequirements, []discoveryPacketMemberBasis{second, first})
	if string(a) != string(b) || string(a) == string(c) || len(a) == 0 || a[len(a)-1] != '\n' {
		t.Fatalf("canonical manifests are not stable and order-sensitive")
	}
}

func TestDiscoveryClosureRejectsStaleAndMismatchedRequestsWithoutPublication(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput
		want   error
	}{
		{name: "stale workspace", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.ExpectedVersion--
			return input
		}, want: ErrDiscoveryStaleState},
		{name: "stale revision", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.ExpectedRevisionID = "discovery-revision-stale"
			return input
		}, want: ErrDiscoveryStaleState},
		{name: "wrong destination", mutate: func(input CloseFeatureDiscoveryInput) CloseFeatureDiscoveryInput {
			input.Destination = DiscoveryDestinationSharedDesign
			return input
		}, want: ErrDiscoveryInvalidDestination},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
			input := test.mutate(CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			if _, _, err := service.CloseFeatureDiscovery(ctx, input); !errors.Is(err, test.want) {
				t.Fatalf("close error = %v, want %v", err, test.want)
			}
			assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
		})
	}
}

func TestDiscoveryClosureRejectsActiveAndBlockedWorkWithoutPublication(t *testing.T) {
	for _, state := range []string{"open", "blocked"} {
		t.Run(state, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-" + state, WorkspaceRowID: workspace.ID, TicketKey: state, Subject: state})
				if err != nil {
					return err
				}
				if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "investigation", false); err != nil {
					return err
				}
				if state == "blocked" {
					_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "blocked", ticket.Version)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
			if err != nil || assessment.State != map[string]DiscoveryState{"open": DiscoveryStateActive, "blocked": DiscoveryStateBlocked}[state] {
				t.Fatalf("assessment = %#v, %v", assessment, err)
			}
			beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
			_, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			want := ErrDiscoveryActiveOperation
			if state == "blocked" {
				want = ErrDiscoveryBlocked
			}
			if !errors.Is(err, want) {
				t.Fatalf("close error = %v, want %v", err, want)
			}
			assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
		})
	}
}

func TestDiscoveryClosureRequiresRouteMaterialIntegrationAndRestoresWithoutPublication(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	var resolution workflowstore.FeatureWorkspaceTicketResolution
	var artifactID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-route-pending", WorkspaceRowID: workspace.ID, TicketKey: "route-pending", Subject: "route-pending"})
		if err != nil {
			return err
		}
		if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "route", true); err != nil {
			return err
		}
		resolution, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-route-pending", TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSha256: strings.Repeat("b", 64)})
		if err != nil {
			return err
		}
		_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "resolved", ticket.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || len(assessment.PendingIntegrations) != 1 || assessment.PendingIntegrations[0] != resolution.ResolutionID {
		t.Fatalf("pending assessment = %#v, %v", assessment, err)
	}
	beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); !errors.Is(err, ErrDiscoveryPendingIntegration) {
		t.Fatalf("pending close error = %v", err)
	}
	assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err = discoveryLifecycleTicket(ctx, store, ticket.DiscoveryTicketID)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: current.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: resolution.ResolutionID, Consequence: "no_material_change", ExpectedWorkspaceVersion: current.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "route settled"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "investigation", false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err = service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || len(assessment.PendingIntegrations) != 0 {
		t.Fatalf("restored assessment = %#v, %v", assessment, err)
	}
	if workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		t.Fatal("restoration published a packet")
	}
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryClosureRequiresRouteMaterialEvidenceAndRestores(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	var artifactID int64
	if err := store.DB().QueryRowContext(ctx, `SELECT id FROM artifacts ORDER BY id LIMIT 1`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-route-evidence", WorkspaceRowID: workspace.ID, TicketKey: "route-evidence", Subject: "route-evidence"})
		if err != nil {
			return err
		}
		if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "route", true); err != nil {
			return err
		}
		_, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "resolved", ticket.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || len(assessment.RequiredEvidence) != 1 || assessment.RestorationActions[0] != "record_resolution:discovery-route-evidence" {
		t.Fatalf("missing evidence assessment = %#v, %v", assessment, err)
	}
	beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); !errors.Is(err, ErrDiscoveryClosureIneligible) {
		t.Fatalf("missing evidence close = %v", err)
	}
	assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-route-evidence", TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sql.NullInt64{Int64: artifactID, Valid: true}, ArtifactSha256: strings.Repeat("b", 64)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err = service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || len(assessment.RequiredEvidence) != 0 || len(assessment.PendingIntegrations) != 1 {
		t.Fatalf("recorded evidence assessment = %#v, %v", assessment, err)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err = discoveryLifecycleTicket(ctx, store, ticket.DiscoveryTicketID)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.IntegrateDiscoveryResult(ctx, IntegrateDiscoveryResultInput{WorkspaceID: current.WorkspaceID, TicketID: ticket.DiscoveryTicketID, ResolutionID: "resolution-route-evidence", Consequence: "no_material_change", ExpectedWorkspaceVersion: current.Version, ExpectedWorkItemVersion: ticket.Version, EvidenceBasis: "recorded evidence"}); err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "investigation", false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		t.Fatal("restoration published a packet")
	}
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryClosureRejectsOpenRouteMaterialWork(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-route-open", WorkspaceRowID: workspace.ID, TicketKey: "route-open", Subject: "route-open"})
		if err != nil {
			return err
		}
		_, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "route", true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || len(assessment.RouteMaterialOpen) != 1 || assessment.RouteMaterialOpen[0] != "discovery-route-open" {
		t.Fatalf("route-open assessment = %#v, %v", assessment, err)
	}
	beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); !errors.Is(err, ErrDiscoveryClosureIneligible) {
		t.Fatalf("route-open close = %v", err)
	}
	assertNoDiscoveryClosurePublication(t, ctx, store, workspace, beforeRoutes)
}

func TestDiscoveryClosureRejectsLegacyUnboundRevision(t *testing.T) {
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-legacy-unbound", "legacy-unbound")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateFeatureWorkspaceRouteState(ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: "route-legacy", WorkspaceRowID: workspace.ID, Sequence: 1, WorkspaceVersion: workspace.Version + 1, State: "ready"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	_, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("# Legacy\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := service.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil || assessment.Currentness != DiscoveryLegacyUnbound || assessment.RestorationActions[len(assessment.RestorationActions)-1] != "replace_integrated_revision_with_settled_destination" {
		t.Fatalf("legacy assessment = %#v, %v", assessment, err)
	}
	if _, _, err = service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: started.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"}); !errors.Is(err, ErrDiscoveryLegacyUnbound) {
		t.Fatalf("legacy close = %v", err)
	}
	assertNoDiscoveryClosurePublication(t, ctx, store, workspace, 1)
}

func TestDiscoveryPacketRetrievalFailsClosedForCorruptManifestAndWrongWorkspace(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if verified, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID); err != nil || verified.Currentness != DiscoveryCurrent {
		t.Fatalf("valid current packet = %#v, %v", verified, err)
	}
	other, err := createFeatureWorkspace(ctx, store, "workspace-discovery-other", "discovery-other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadDiscoveryClosurePacket(ctx, other.WorkspaceID, closed.Packet.ClosurePacketID); !errors.Is(err, ErrDiscoveryCrossWorkspace) {
		t.Fatalf("wrong workspace error = %v", err)
	}
	manifest, err := store.GetDiscoveryArtifactByRowID(ctx, closed.Packet.ManifestArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(manifest.RelativePath)), []byte("corrupt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if packet, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID); !errors.Is(err, ErrDiscoveryManifestIntegrity) || packet.Packet.ID != 0 {
		t.Fatalf("corrupt manifest packet = %#v, error = %v", packet, err)
	}
}

func TestDiscoveryPacketRetrievalFailsClosedForPersistedMetadataAndMemberBytes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *workflowstore.Store, workflowstore.DiscoveryClosurePacket, workflowstore.DiscoveryClosurePacketMember)
		want   error
	}{
		{name: "manifest metadata", mutate: func(t *testing.T, store *workflowstore.Store, packet workflowstore.DiscoveryClosurePacket, _ workflowstore.DiscoveryClosurePacketMember) {
			if _, err := store.DB().Exec(`DROP TRIGGER discovery_packet_update_immutable`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`UPDATE feature_workspace_discovery_closure_packets SET manifest_sha256 = ? WHERE id = ?`, strings.Repeat("c", 64), packet.ID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrDiscoveryManifestIntegrity},
		{name: "member metadata", mutate: func(t *testing.T, store *workflowstore.Store, _ workflowstore.DiscoveryClosurePacket, member workflowstore.DiscoveryClosurePacketMember) {
			if _, err := store.DB().Exec(`DROP TRIGGER discovery_packet_member_update_immutable`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.DB().Exec(`UPDATE feature_workspace_discovery_closure_packet_members SET sha256 = ? WHERE id = ?`, strings.Repeat("c", 64), member.ID); err != nil {
				t.Fatal(err)
			}
		}, want: ErrDiscoveryMemberIntegrity},
		{name: "member bytes", mutate: func(t *testing.T, store *workflowstore.Store, _ workflowstore.DiscoveryClosurePacket, member workflowstore.DiscoveryClosurePacketMember) {
			artifact, err := store.GetDiscoveryArtifactByRowID(context.Background(), member.ArtifactRowID)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath)), []byte("corrupt member\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, want: ErrDiscoveryMemberUnavailable},
		{name: "member unavailable", mutate: func(t *testing.T, store *workflowstore.Store, _ workflowstore.DiscoveryClosurePacket, member workflowstore.DiscoveryClosurePacketMember) {
			artifact, err := store.GetDiscoveryArtifactByRowID(context.Background(), member.ArtifactRowID)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))); err != nil {
				t.Fatal(err)
			}
		}, want: ErrDiscoveryMemberUnavailable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
			closed, _, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, store, closed.Packet, closed.Members[0])
			packet, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
			if !errors.Is(err, test.want) || packet.Packet.ID != 0 {
				t.Fatalf("retrieval = %#v, %v; want %v", packet, err, test.want)
			}
		})
	}
}

func TestDiscoveryClosureManifestExactlyMatchesPersistedMembers(t *testing.T) {
	ctx, _, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, _, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		PacketID string `json:"packet_id"`
		Members  []struct {
			Sequence       int64  `json:"sequence"`
			OwnerFamily    string `json:"owner_family"`
			SourceIdentity string `json:"source_identity"`
			SHA256         string `json:"sha256"`
			SizeBytes      int64  `json:"size_bytes"`
			MediaType      string `json:"media_type"`
			SemanticRole   string `json:"semantic_role"`
		} `json:"members"`
	}
	if err := json.Unmarshal(closed.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.PacketID != closed.Packet.ClosurePacketID || len(manifest.Members) != len(closed.Members) {
		t.Fatalf("manifest = %#v, members = %#v", manifest, closed.Members)
	}
	for index, row := range closed.Members {
		member := manifest.Members[index]
		if member.Sequence != row.Sequence || member.OwnerFamily != row.OwnerFamily || member.SourceIdentity != row.SourceIdentity || member.SHA256 != row.Sha256 || member.SizeBytes != row.SizeBytes || member.MediaType != row.MediaType || member.SemanticRole != row.SemanticRole {
			t.Fatalf("manifest member %d = %#v, persisted = %#v", index, member, row)
		}
	}
}

func TestDiscoveryClosurePacketIncludesEverySupportedSourceFamily(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	// Use a real retained artifact for each external source; closure members must
	// bind their retained bytes rather than only the source table metadata.
	makeArtifact := func(id, relative string, content []byte) int64 {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(relative))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(relative)), content, 0o600); err != nil {
			t.Fatal(err)
		}
		var planID, artifactID int64
		if err := store.DB().QueryRowContext(ctx, `SELECT id FROM plans LIMIT 1`).Scan(&planID); err != nil {
			t.Fatal(err)
		}
		if err := store.DB().QueryRowContext(ctx, `INSERT INTO artifacts (artifact_id, owner_type, plan_row_id, kind, relative_path, media_type, sha256, size_bytes) VALUES (?, 'plan', ?, 'evidence', ?, 'text/plain', ?, ?) RETURNING id`, id, planID, relative, discoveryTestDigest(content), len(content)).Scan(&artifactID); err != nil {
			t.Fatal(err)
		}
		return artifactID
	}
	inputBytes, routeBytes, evidenceBytes := []byte("input\n"), []byte("route\n"), []byte("evidence\n")
	inputArtifact := makeArtifact("artifact-mixed-input", "mixed/input.txt", inputBytes)
	routeArtifact := makeArtifact("artifact-mixed-route", "mixed/route.txt", routeBytes)
	evidenceArtifact := makeArtifact("artifact-mixed-evidence", "mixed/evidence.txt", evidenceBytes)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateFeatureWorkspaceAdmittedInput(ctx, workflowstore.CreateFeatureWorkspaceAdmittedInputParams{AdmittedInputID: "input-mixed", WorkspaceRowID: workspace.ID, Sequence: 1, InputName: "mixed", InputRole: "evidence", SourceKind: "relay_artifact", ArtifactRowID: sql.NullInt64{Int64: inputArtifact, Valid: true}, ArtifactSha256: sql.NullString{String: discoveryTestDigest(inputBytes), Valid: true}}); err != nil {
			return err
		}
		ticket, err := tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: "discovery-mixed-route", WorkspaceRowID: workspace.ID, TicketKey: "mixed-route", Subject: "mixed route"})
		if err != nil {
			return err
		}
		if _, err = tx.UpsertDiscoveryWorkItemMetadata(ctx, ticket.ID, "route", true); err != nil {
			return err
		}
		resolution, err := tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: "resolution-mixed-route", TicketRowID: ticket.ID, Sequence: 1, ResolutionKind: "resolved", ArtifactRowID: sql.NullInt64{Int64: routeArtifact, Valid: true}, ArtifactSha256: discoveryTestDigest(routeBytes)})
		if err != nil {
			return err
		}
		if _, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", "resolved", ticket.Version); err != nil {
			return err
		}
		if _, err = tx.CreateDiscoveryIntegrationConsequence(ctx, workflowstore.DiscoveryIntegrationConsequence{IntegrationConsequenceID: "integration-mixed-route", WorkspaceRowID: workspace.ID, TicketRowID: ticket.ID, ResolutionRowID: resolution.ID, ConsequenceKind: "no_material_change", EvidenceBasis: "mixed source"}); err != nil {
			return err
		}
		_, err = tx.CreateFeatureWorkspaceInvestigation(ctx, workflowstore.CreateFeatureWorkspaceInvestigationParams{InvestigationID: "investigation-mixed", WorkspaceRowID: workspace.ID, Sequence: 1, InvestigationKind: "artifact", ArtifactRowID: sql.NullInt64{Int64: evidenceArtifact, Valid: true}, ArtifactSHA256: discoveryTestDigest(evidenceBytes)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	closed, _, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		owner, source, role string
		bytes               []byte
	}{{"admitted_input", "input-mixed", "admitted_input:001", inputBytes}, {"integrated_discovery", revision.DiscoveryRevisionID, "closing_revision", []byte("# Settled discovery\n")}, {"discovery_resolution", "resolution-mixed-route", "route_result:discovery-mixed-route:001", routeBytes}, {"discovery_evidence", "investigation-mixed", "represented_evidence:001", evidenceBytes}}
	if len(closed.Members) != len(want) {
		t.Fatalf("members = %#v", closed.Members)
	}
	for i, member := range closed.Members {
		if member.Sequence != int64(i+1) || member.OwnerFamily != want[i].owner || member.SourceIdentity != want[i].source || member.SemanticRole != want[i].role || member.Sha256 != discoveryTestDigest(want[i].bytes) || member.SizeBytes != int64(len(want[i].bytes)) || member.MediaType != "text/markdown" && member.MediaType != "text/plain" {
			t.Fatalf("member %d = %#v", i, member)
		}
	}
	verified, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
	if err != nil || fmt.Sprint(verified.Members) != fmt.Sprint(closed.Members) {
		t.Fatalf("persisted member order = %#v, %v", verified.Members, err)
	}
	var manifest struct {
		Members []struct {
			Sequence int64  `json:"sequence"`
			Owner    string `json:"owner_family"`
			Source   string `json:"source_identity"`
			SHA      string `json:"sha256"`
			Size     int64  `json:"size_bytes"`
			Media    string `json:"media_type"`
			Role     string `json:"semantic_role"`
		} `json:"members"`
	}
	if err := json.Unmarshal(closed.Manifest, &manifest); err != nil || len(manifest.Members) != len(closed.Members) {
		t.Fatalf("manifest = %s, %v", closed.Manifest, err)
	}
	for i, member := range manifest.Members {
		row := closed.Members[i]
		if member.Sequence != row.Sequence || member.Owner != row.OwnerFamily || member.Source != row.SourceIdentity || member.SHA != row.Sha256 || member.Size != row.SizeBytes || member.Media != row.MediaType || member.Role != row.SemanticRole {
			t.Fatalf("manifest member %d = %#v, persisted = %#v", i, member, row)
		}
	}
}

func TestDiscoveryReopenRejectsStaleWrongDigestAndMalformedReplacement(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := createFeatureWorkspace(ctx, store, "workspace-reopen-other", "reopen-other")
	if err != nil {
		t.Fatal(err)
	}
	other, err = service.SetIntegratedDiscoveryCapability(ctx, other.WorkspaceID, other.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, other, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	otherContent := []byte("# Other discovery\n")
	otherStarted, other, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, Markdown: otherContent, SHA256: discoveryTestDigest(otherContent), CreatedIdentity: "operator", Destination: DiscoveryDestinationRequirements})
	if err != nil {
		t.Fatal(err)
	}
	otherClosed, _, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: other.WorkspaceID, ExpectedVersion: other.Version, ExpectedRevisionID: otherStarted.Revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Replacement\n")
	base := ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "new evidence", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements}
	for _, test := range []struct {
		name  string
		input ReopenFeatureDiscoveryInput
		want  error
	}{
		{name: "other workspace packet", input: func() ReopenFeatureDiscoveryInput {
			value := base
			value.ExpectedPacketID = otherClosed.Packet.ClosurePacketID
			return value
		}(), want: ErrDiscoveryStalePacket},
		{name: "digest mismatch", input: func() ReopenFeatureDiscoveryInput {
			value := base
			value.SHA256 = strings.Repeat("0", 64)
			return value
		}(), want: ErrInvalidDiscoveryConsequence},
		{name: "empty replacement", input: func() ReopenFeatureDiscoveryInput { value := base; value.Markdown = nil; return value }(), want: ErrInvalidDiscoveryConsequence},
		{name: "missing cause", input: func() ReopenFeatureDiscoveryInput { value := base; value.Cause = ""; return value }(), want: ErrInvalidDiscoveryConsequence},
		{name: "missing identity", input: func() ReopenFeatureDiscoveryInput { value := base; value.CreatedIdentity = ""; return value }(), want: ErrInvalidDiscoveryConsequence},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := service.ReopenFeatureDiscovery(ctx, test.input); !errors.Is(err, test.want) {
				t.Fatalf("reopen error = %v, want %v", err, test.want)
			}
		})
	}
	newRevision, workspace, err := service.ReopenFeatureDiscovery(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "again", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements}); !errors.Is(err, ErrDiscoveryNotClosed) {
		t.Fatalf("historical packet reopen = %v", err)
	}
	if newRevision.PredecessorRevisionRowID.Int64 != revision.ID {
		t.Fatalf("replacement revision = %#v", newRevision)
	}
}

func TestHistoricalDiscoveryMemberCorruptionPreventsRetrievalAndReopen(t *testing.T) {
	ctx, store, service, workspace, revision := adoptedDiscoveryLifecycle(t, DiscoveryDestinationRequirements)
	closed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: revision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Reopened\n")
	artifact, err := store.GetDiscoveryArtifactByRowID(ctx, closed.Members[0].ArtifactRowID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath)), []byte("corrupt historical member\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID); !errors.Is(err, ErrDiscoveryMemberUnavailable) {
		t.Fatalf("historical retrieval = %v", err)
	}
	var revisionsBefore int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_integrated_discovery_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}
	beforeRoutes := discoveryRouteCount(t, store, workspace.ID)
	var eventsBefore, completionsBefore int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_reopen_events WHERE workspace_row_id = ?`, workspace.ID).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id IN (SELECT id FROM feature_workspace_completion_decisions WHERE workspace_row_id = ?)`, workspace.ID).Scan(&completionsBefore); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "again", CreatedIdentity: "operator", Markdown: replacement, SHA256: discoveryTestDigest(replacement), Destination: DiscoveryDestinationRequirements}); !errors.Is(err, ErrDiscoveryMemberUnavailable) {
		t.Fatalf("corrupt current packet reopen = %v", err)
	}
	var revisionsAfter int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_integrated_discovery_revisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&revisionsAfter); err != nil || revisionsAfter != revisionsBefore {
		t.Fatalf("replacement revisions = %d, %v; want %d", revisionsAfter, err, revisionsBefore)
	}
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.Version != workspace.Version || !current.CurrentDiscoveryClosurePacketRowID.Valid || current.CurrentDiscoveryClosurePacketRowID.Int64 != closed.Packet.ID || current.CurrentDiscoveryRevisionRowID.Int64 != revision.ID {
		t.Fatalf("workspace after corrupt reopen = %#v, %v", current, err)
	}
	if got := discoveryRouteCount(t, store, workspace.ID); got != beforeRoutes {
		t.Fatalf("routes after corrupt reopen = %d, want %d", got, beforeRoutes)
	}
	var eventsAfter, completionsAfter int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_reopen_events WHERE workspace_row_id = ?`, workspace.ID).Scan(&eventsAfter); err != nil || eventsAfter != eventsBefore {
		t.Fatalf("reopen events = %d, %v; want %d", eventsAfter, err, eventsBefore)
	}
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_completion_reopenings WHERE completion_decision_row_id IN (SELECT id FROM feature_workspace_completion_decisions WHERE workspace_row_id = ?)`, workspace.ID).Scan(&completionsAfter); err != nil || completionsAfter != completionsBefore {
		t.Fatalf("completion reopenings = %d, %v; want %d", completionsAfter, err, completionsBefore)
	}
}

func TestDiscoveryReopenPreservesHistoricalPacketAndReclosureUsesNewPacket(t *testing.T) {
	ctx, store, service, workspace, revision, closed, completion := completedClosedDiscoveryLifecycle(t)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateDeliveryTicketSelection(ctx, workflowstore.CreateDeliveryTicketSelectionParams{SelectionID: "selection-discovery-reclosure", WorkspaceRowID: workspace.ID, State: "superseded", Rationale: "historical selection must remain inactive"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	selection, err := store.GetDeliveryTicketSelectionBySelectionID(ctx, "selection-discovery-reclosure")
	if err != nil {
		t.Fatal(err)
	}
	beforeAuthority := workspace.CurrentAuthorityRevisionRowID
	beforeRoute := workspace.CurrentRouteStateRowID
	var decisionsBefore int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_completion_decisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&decisionsBefore); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("# Reopened discovery\n")
	if _, _, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, Cause: "new evidence", CreatedIdentity: "operator", OperatorConfirmed: false, SHA256: discoveryTestDigest(replacement), Markdown: replacement}); !errors.Is(err, ErrDiscoveryReopenConfirmation) {
		t.Fatalf("unconfirmed reopen error = %v", err)
	}
	newRevision, workspace, err := service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, Cause: "new evidence", CreatedIdentity: "operator", OperatorConfirmed: true, SHA256: discoveryTestDigest(replacement), Markdown: replacement, Destination: DiscoveryDestinationRequirements})
	if err != nil || newRevision.PredecessorRevisionRowID.Int64 != revision.ID || workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		t.Fatalf("reopen = %#v, %#v, %v", newRevision, workspace, err)
	}
	historical, err := service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
	if err != nil || historical.Currentness != DiscoveryHistorical {
		t.Fatalf("historical packet = %#v, %v", historical, err)
	}
	if _, err := store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("completion remains current after reopen: %v", err)
	}
	var persisted workflowstore.FeatureWorkspaceCompletionDecision
	if err := store.DB().QueryRowContext(ctx, `SELECT id, completion_decision_id, workspace_row_id, authority_revision_row_id, discovery_closure_packet_row_id, decision, created_at FROM feature_workspace_completion_decisions WHERE id = ?`, completion.ID).Scan(&persisted.ID, &persisted.CompletionDecisionID, &persisted.WorkspaceRowID, &persisted.AuthorityRevisionRowID, &persisted.DiscoveryClosurePacketRowID, &persisted.Decision, &persisted.CreatedAt); err != nil || persisted.ID != completion.ID || !persisted.DiscoveryClosurePacketRowID.Valid || persisted.DiscoveryClosurePacketRowID.Int64 != closed.Packet.ID {
		t.Fatalf("historical completion after reopen = %#v, %v", persisted, err)
	}
	reclosed, workspace, err := service.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedRevisionID: newRevision.DiscoveryRevisionID, Destination: DiscoveryDestinationRequirements, CreatedIdentity: "operator"})
	if err != nil || reclosed.Packet.ID == closed.Packet.ID || workspace.CurrentDiscoveryClosurePacketRowID.Int64 != reclosed.Packet.ID || len(reclosed.Members) != 2 {
		t.Fatalf("reclosure = %#v, %#v, %v", reclosed, workspace, err)
	}
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid || workspace.CurrentDiscoveryClosurePacketRowID.Int64 != reclosed.Packet.ID {
		t.Fatalf("replacement packet is not current: %#v", workspace)
	}
	if workspace.CurrentAuthorityRevisionRowID != beforeAuthority {
		t.Fatalf("current authority after reclosure = %#v, want %#v", workspace.CurrentAuthorityRevisionRowID, beforeAuthority)
	}
	if !workspace.CurrentRouteStateRowID.Valid || workspace.CurrentRouteStateRowID == beforeRoute {
		t.Fatalf("current route after reclosure = %#v, want discovery lifecycle transition from %#v", workspace.CurrentRouteStateRowID, beforeRoute)
	}
	route, err := store.GetFeatureWorkspaceRouteStateByRowID(ctx, workspace.CurrentRouteStateRowID.Int64)
	if err != nil || route.State != "ready" {
		t.Fatalf("current route after reclosure = %#v, %v", route, err)
	}
	currentAuthority, err := store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, beforeAuthority.Int64)
	if err != nil || currentAuthority.ID != beforeAuthority.Int64 {
		t.Fatalf("current authority revision after reclosure = %#v, %v", currentAuthority, err)
	}
	if _, err := store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("historical completion became current after reclosure: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT id, completion_decision_id, workspace_row_id, authority_revision_row_id, discovery_closure_packet_row_id, decision, created_at FROM feature_workspace_completion_decisions WHERE id = ?`, completion.ID).Scan(&persisted.ID, &persisted.CompletionDecisionID, &persisted.WorkspaceRowID, &persisted.AuthorityRevisionRowID, &persisted.DiscoveryClosurePacketRowID, &persisted.Decision, &persisted.CreatedAt); err != nil || persisted.ID != completion.ID || !persisted.DiscoveryClosurePacketRowID.Valid || persisted.DiscoveryClosurePacketRowID.Int64 != closed.Packet.ID {
		t.Fatalf("historical completion after reclosure = %#v, %v", persisted, err)
	}
	var decisionsAfter int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_completion_decisions WHERE workspace_row_id = ?`, workspace.ID).Scan(&decisionsAfter); err != nil || decisionsAfter != decisionsBefore {
		t.Fatalf("completion decisions after reclosure = %d, %v; want %d", decisionsAfter, err, decisionsBefore)
	}
	currentSelection, err := store.GetDeliveryTicketSelectionByRowID(ctx, selection.ID)
	if err != nil || currentSelection.State != selection.State || currentSelection.State != "superseded" {
		t.Fatalf("historical selection after reclosure = %#v, %v", currentSelection, err)
	}
	for _, table := range []string{"execution_packages", "runs", "execution_attempts", "audit_packets", "audit_decisions"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("downstream %s after reclosure = %d, %v", table, count, err)
		}
	}
	for index, member := range reclosed.Members {
		if member.Sequence != int64(index+1) || member.OwnerFamily == "" || member.SourceIdentity == "" || member.SemanticRole == "" || member.Sha256 == "" || member.SizeBytes < 0 || member.MediaType == "" {
			t.Fatalf("reclosure member %d = %#v", index, member)
		}
	}
	if reclosed.Members[0].OwnerFamily != "integrated_discovery" || reclosed.Members[0].SourceIdentity != newRevision.DiscoveryRevisionID || reclosed.Members[1].OwnerFamily != "discovery_reopen" {
		t.Fatalf("reclosure member basis = %#v", reclosed.Members)
	}
	historical, err = service.ReadDiscoveryClosurePacket(ctx, workspace.WorkspaceID, closed.Packet.ClosurePacketID)
	if err != nil || historical.Currentness != DiscoveryHistorical {
		t.Fatalf("historical packet after reclosure = %#v, %v", historical, err)
	}
	staleReplacement := []byte("# Stale reopen\n")
	if _, _, err = service.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, ExpectedPacketID: closed.Packet.ClosurePacketID, OperatorConfirmed: true, Cause: "stale packet", CreatedIdentity: "operator", SHA256: discoveryTestDigest(staleReplacement), Markdown: staleReplacement, Destination: DiscoveryDestinationRequirements}); !errors.Is(err, ErrDiscoveryStalePacket) {
		t.Fatalf("stale historical packet reopen = %v", err)
	}
}

func adoptedDiscoveryLifecycle(t *testing.T, destination DiscoveryDestination) (context.Context, *workflowstore.Store, *Service, workflowstore.FeatureWorkspace, workflowstore.IntegratedDiscoveryRevision) {
	t.Helper()
	ctx := context.Background()
	store, _, _ := openFeatureServiceStore(t, ctx)
	service, err := NewServiceWithIDs(store, &featureTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := createFeatureWorkspace(ctx, store, "workspace-discovery-proof", "discovery-proof")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = service.SetIntegratedDiscoveryCapability(ctx, workspace.WorkspaceID, workspace.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, workspace, err = service.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, OperatorIdentity: "operator"}); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Settled discovery\n")
	started, workspace, err := service.StartIntegratedDiscovery(ctx, StartIntegratedDiscoveryInput{WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, Markdown: content, SHA256: discoveryTestDigest(content), CreatedIdentity: "operator", Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, store, service, workspace, started.Revision
}

func assertNoDiscoveryClosurePublication(t *testing.T, ctx context.Context, store *workflowstore.Store, workspace workflowstore.FeatureWorkspace, routes int) {
	t.Helper()
	current, err := store.GetFeatureWorkspaceByWorkspaceID(ctx, workspace.WorkspaceID)
	if err != nil || current.CurrentDiscoveryClosurePacketRowID.Valid || current.Version != workspace.Version {
		t.Fatalf("workspace after rejected closure = %#v, %v", current, err)
	}
	var packets, members int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?`, workspace.ID).Scan(&packets); err != nil || packets != 0 {
		t.Fatalf("closure packets = %d, %v", packets, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM feature_workspace_discovery_closure_packet_members WHERE closure_packet_row_id IN (SELECT id FROM feature_workspace_discovery_closure_packets WHERE workspace_row_id = ?)`, workspace.ID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("closure members = %d, %v", members, err)
	}
	if got := discoveryRouteCount(t, store, workspace.ID); got != routes {
		t.Fatalf("route transitions = %d, want %d", got, routes)
	}
}

func discoveryRouteCount(t *testing.T, store *workflowstore.Store, workspaceRowID int64) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM feature_workspace_route_states WHERE workspace_row_id = ?`, workspaceRowID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func discoveryLifecycleTicket(ctx context.Context, store *workflowstore.Store, ticketID string) (workflowstore.FeatureWorkspaceDiscoveryTicket, error) {
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var err error
		ticket, err = tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, ticketID)
		return err
	})
	return ticket, err
}
