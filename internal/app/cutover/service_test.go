package cutover

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"relay/internal/mcp/routecontracts"
	workflowstore "relay/internal/store/workflow"
)

func TestStateInertPrepared(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := svc.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no current activation in fresh store")
	}
	closed, err := svc.IsLegacyAdmissionClosed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("legacy admission must be open before activation")
	}
}

func TestNormalizeGatewayConfigurationRequiresExactPublicSurfaceMembership(t *testing.T) {
	configuration := validGatewayConfigurationFixture(t)
	normalized, err := normalizeGatewayConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if !validLowerHex(normalized.ConfigurationSHA256, 64) || len(normalized.AppSurfaces) != 3 || len(normalized.Routes) != 7 || len(normalized.RouteMemberships) != 7 || len(normalized.AppSurfaceMappings) != 3 {
		t.Fatalf("configuration=%#v", normalized)
	}
	missing := validGatewayConfigurationFixture(t)
	missing.RouteMemberships = missing.RouteMemberships[:6]
	if _, err := normalizeGatewayConfiguration(missing); !errors.Is(err, ErrCutoverConfigurationInvalid) {
		t.Fatalf("missing membership error = %v", err)
	}
	mismatched := validGatewayConfigurationFixture(t)
	mismatched.AppSurfaceMappings[1].PublicPath = "/mcp/auditor"
	if _, err := normalizeGatewayConfiguration(mismatched); !errors.Is(err, ErrCutoverConfigurationInvalid) {
		t.Fatalf("mismatched public mapping error = %v", err)
	}
	staleDigest := validGatewayConfigurationFixture(t)
	staleDigest.AppSurfaces[0].ManifestSHA256 = strings.Repeat("0", 64)
	if _, err := normalizeGatewayConfiguration(staleDigest); !errors.Is(err, ErrCutoverConfigurationInvalid) {
		t.Fatalf("stale app digest error = %v", err)
	}
}

func validGatewayConfigurationFixture(t *testing.T) GatewayConfigurationIdentity {
	t.Helper()
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := routecontracts.BuildAppSurfaceManifests(routes)
	if err != nil {
		t.Fatal(err)
	}
	configuration := GatewayConfigurationIdentity{
		RelayRepository: "Paintersrp/relay", RelayCommitOID: strings.Repeat("6", 40),
		StandingRepository: "Paintersrp/relay-specs", StandingCommitOID: routes.Manifests[0].StandingAuthority.Commit,
		TopologyVersion:     appSurfaceTopologyVersion,
		StandingAuthorities: []StandingAuthorityIdentity{},
		DependencyOutcomes: []DependencyOutcomeIdentity{
			{1, "CURRENT-MCP-SURFACES", 2, "completed_accepted", strings.Repeat("e", 64)},
			{2, "PRIVATE-TRANSPORT-TRACE", 2, "completed_accepted", strings.Repeat("f", 64)},
			{3, "STANDING-AUTHORITY", 2, "completed_accepted", strings.Repeat("1", 64)},
		},
	}
	standing := map[string]routecontracts.StandingAuthorityIdentity{}
	for index, route := range routes.Manifests {
		configuration.Routes = append(configuration.Routes, RouteIdentity{
			Sequence: int64(index + 1), RoutePath: route.RoutePath, Role: route.Role, SurfaceContractID: route.SurfaceContract,
			ManifestSHA256: route.ManifestSHA256, AuthorityCommitOID: route.StandingAuthority.Commit, AuthorityBlobOID: route.StandingAuthority.BlobOID,
		})
		standing[route.Role] = route.StandingAuthority
	}
	for index, surface := range surfaces.Surfaces {
		configuration.AppSurfaces = append(configuration.AppSurfaces, AppSurfaceIdentity{Sequence: int64(index + 1), Surface: string(surface.Surface), PublicPath: surface.PublicPath, ManifestSHA256: surface.ManifestSHA256})
		configuration.AppSurfaceMappings = append(configuration.AppSurfaceMappings, AppSurfaceMappingIdentity{
			Sequence: int64(index + 1), MappingID: string(surface.Surface), PublicSurface: string(surface.Surface), PublicPath: surface.PublicPath,
			ListenerIdentity: "127.0.0.1:1810" + string(rune('1'+index)), UpstreamIdentity: "http://127.0.0.1:8080" + surface.PublicPath,
			HealthEvidenceSHA256: strings.Repeat("4", 64), TraceEvidenceSHA256: strings.Repeat("5", 64),
		})
		for _, route := range surface.MemberRoutes {
			configuration.RouteMemberships = append(configuration.RouteMemberships, RouteMembershipIdentity{RoutePath: route.RoutePath, PublicSurface: string(surface.Surface)})
		}
	}
	for _, role := range []string{"auditor", "planner", "wayfinder"} {
		authority := standing[role]
		configuration.StandingAuthorities = append(configuration.StandingAuthorities, StandingAuthorityIdentity{
			Role: role, Repository: "Paintersrp/relay-specs", CommitOID: routes.Manifests[0].StandingAuthority.Commit,
			Path: authority.Path, BlobOID: authority.BlobOID, ContentSHA256: strings.Repeat("9", 64),
		})
	}
	return configuration
}

func TestLegacyGatewayTopologyRemainsHistoricalButCannotPrepareOrActivate(t *testing.T) {
	ctx := context.Background()
	store, teardown := testStore(t)
	defer teardown()
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}

	legacy, err := normalizeGatewayConfiguration(legacyGatewayConfigurationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.TopologyVersion != "" {
		t.Fatalf("legacy topology version = %q", legacy.TopologyVersion)
	}
	activation := seedPreparedLegacyGatewayActivation(t, ctx, store, legacy)

	history, err := svc.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].GatewayConfiguration == nil {
		t.Fatalf("legacy history = %#v", history)
	}
	historical := history[0].GatewayConfiguration
	if historical.ConfigurationSHA256 != legacy.ConfigurationSHA256 || historical.TopologyVersion != "" ||
		len(historical.Routes) != 7 || len(historical.Mappings) != 7 ||
		len(historical.AppSurfaces) != 0 || len(historical.RouteMemberships) != 0 || len(historical.AppSurfaceMappings) != 0 {
		t.Fatalf("legacy configuration history = %#v", historical)
	}

	readiness, err := svc.Readiness(ctx, activation.CutoverActivationID)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Ready || readiness.GatewayConfiguration != nil || len(readiness.ConfigurationErrors) != 1 || readiness.ConfigurationErrors[0] != ErrCutoverConfigurationInvalid.Error() {
		t.Fatalf("legacy readiness = %#v", readiness)
	}
	if _, err := svc.Activate(ctx, ActivationRequest{ActivationID: activation.CutoverActivationID}); !errors.Is(err, ErrCutoverNotReady) {
		t.Fatalf("legacy activation error = %v", err)
	}
	if _, err := svc.Prepare(ctx, PrepareRequest{ActivationID: "cutover-legacy-reprepare", GatewayConfiguration: legacy}); !errors.Is(err, ErrCutoverConfigurationInvalid) {
		t.Fatalf("legacy prepare error = %v", err)
	}
}

func legacyGatewayConfigurationFixture(t *testing.T) GatewayConfigurationIdentity {
	t.Helper()
	configuration := validGatewayConfigurationFixture(t)
	configuration.TopologyVersion = ""
	configuration.AppSurfaces = nil
	configuration.RouteMemberships = nil
	configuration.AppSurfaceMappings = nil
	for index, mappingID := range []string{
		"wayfinder-workspace", "wayfinder-discovery", "wayfinder-investigation",
		"planner-authoring", "planner-frontier", "auditor-review", "auditor-audit",
	} {
		route := configuration.Routes[index]
		configuration.Mappings = append(configuration.Mappings, MappingIdentity{
			Sequence: int64(index + 1), MappingID: mappingID, RoutePath: route.RoutePath,
			ListenerIdentity: "127.0.0.1:1810" + string(rune('1'+index)), UpstreamIdentity: "http://127.0.0.1:8080" + route.RoutePath,
			HealthEvidenceSHA256: strings.Repeat("4", 64), TraceEvidenceSHA256: strings.Repeat("5", 64),
		})
	}
	return configuration
}

func seedPreparedLegacyGatewayActivation(t *testing.T, ctx context.Context, store *workflowstore.Store, configuration GatewayConfigurationIdentity) workflowstore.CutoverActivation {
	t.Helper()
	const timestamp = "2026-07-22T12:00:00.000000000Z"
	transitionPlanSHA256 := strings.Repeat("c", 64)
	authoritySHA256 := strings.Repeat("d", 64)
	var activation workflowstore.CutoverActivation
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{
			RepoTarget: "relay", LocalPath: "/repo", ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true},
		}); err != nil {
			return err
		}
		vault, err := tx.GetOrCreateSourceVault(ctx, workflowstore.CreateSourceVaultParams{
			VaultID: workflowstore.NewSourceVaultID(), RepoTarget: "relay", RelativePath: "repositories/relay.git",
		})
		if err != nil {
			return err
		}
		closureID := workflowstore.NewSourceVaultClosureID()
		acquired, err := tx.AcquireSourceVaultClosure(ctx, workflowstore.AcquireSourceVaultClosureParams{
			VaultRowID: vault.ID, ClosureID: closureID, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40),
			RefName: "refs/relay/closures/" + closureID, StartedAt: timestamp,
		})
		if err != nil {
			return err
		}
		closure, err := tx.TransitionSourceVaultClosure(ctx, workflowstore.TransitionSourceVaultClosureParams{
			ClosureID: acquired.Closure.ClosureID, ExpectedState: workflowstore.SourceVaultClosureStateImporting,
			NextState: workflowstore.SourceVaultClosureStateReady, TransitionAt: timestamp,
		})
		if err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: "project-legacy-gateway", Name: "Legacy gateway"})
		if err != nil {
			return err
		}
		workspace, err := tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{
			WorkspaceID: "workspace-legacy-gateway", ProjectRowID: project.ID, FeatureSlug: "legacy-gateway",
		})
		if err != nil {
			return err
		}
		plan, err := tx.CreatePlan(ctx, workflowstore.CreatePlanParams{
			ProjectRowID: project.ID, PlanID: "plan-legacy-gateway", FeatureSlug: workspace.FeatureSlug, CanonicalSHA256: strings.Repeat("e", 64),
		})
		if err != nil {
			return err
		}
		artifact, err := tx.CreateArtifact(ctx, workflowstore.CreateArtifactParams{
			ArtifactID: "artifact-legacy-transition-plan", OwnerType: workflowstore.ArtifactOwnerPlan,
			PlanRowID: sql.NullInt64{Int64: plan.ID, Valid: true}, Kind: "transition_plan",
			RelativePath: "plans/legacy-gateway/transition-plan.json", MediaType: "application/json", SHA256: transitionPlanSHA256, SizeBytes: 1,
		})
		if err != nil {
			return err
		}
		authority, err := tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{
			AuthorityRevisionID: "authority-legacy-gateway-1", WorkspaceRowID: workspace.ID, RevisionNumber: 1,
			SourceClosureRowID: sql.NullInt64{Int64: closure.ID, Valid: true},
		})
		if err != nil {
			return err
		}
		layer, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{
			AuthorityRevisionRowID: authority.ID, LayerKind: "plan", Sequence: 1,
			ArtifactRowID: sql.NullInt64{Int64: artifact.ID, Valid: true}, ArtifactSha256: transitionPlanSHA256,
		})
		if err != nil {
			return err
		}
		workspace, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, authority.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		ticket, err := tx.CreateDeliveryTicket(ctx, workflowstore.CreateDeliveryTicketParams{
			TicketID: "P9-T1", WorkspaceRowID: workspace.ID, ExternalPriority: 1,
		})
		if err != nil {
			return err
		}
		revision, err := tx.CreateDeliveryTicketRevision(ctx, workflowstore.CreateDeliveryTicketRevisionParams{
			DeliveryTicketRowID: ticket.ID, RevisionNumber: 1, RepoTarget: "relay", Branch: "main", BaseCommit: closure.CommitOID,
			SourceClosureRowID: closure.ID, SourcePath: "tickets/p9-t1.r1.delivery-ticket.json",
			Goal: "Preserve a legacy gateway configuration.", Context: "Historical cutover configuration.", TransitionApplicability: "required",
		})
		if err != nil {
			return err
		}
		if _, err := tx.SetDeliveryTicketCurrentRevision(ctx, ticket.TicketID, revision.ID); err != nil {
			return err
		}
		activation, err = tx.CreateCutoverActivation(
			ctx, "cutover-legacy-gateway", workspace.ID, revision.ID, ticket.TicketID, revision.RevisionNumber,
			layer.ID, transitionPlanSHA256, authority.ID, authority.AuthorityRevisionID, authority.RevisionNumber, authoritySHA256, "eligible",
		)
		if err != nil {
			return err
		}
		if err := tx.CreateCutoverGatewayConfiguration(ctx, activation.ID, toStoreGatewayConfiguration(configuration)); err != nil {
			return err
		}
		if _, err := tx.CreateCutoverPrerequisite(ctx, activation.ID, 1, "Historical configuration is retained.", "Legacy evidence is present."); err != nil {
			return err
		}
		if _, err := tx.CreateCutoverObligation(ctx, activation.ID, "activation", 1, "Do not activate legacy topology.", "Current topology validation is required."); err != nil {
			return err
		}
		if _, err := tx.CreateCutoverObligation(ctx, activation.ID, "rollback", 1, "Preserve rollback evidence.", "Rollback remains eligible."); err != nil {
			return err
		}
		_, err = tx.CreateCutoverRollForwardCriterion(ctx, activation.ID, 1, "A supported topology activates only after readiness.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return activation
}

func TestLegacyGateAllowsBeforeActivation(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, _ := NewService(store)
	gate := NewLegacyGate(svc)
	decision, err := gate.AllowNewPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("expected legacy gate to allow before activation")
	}
}

func testStore(t *testing.T) (*workflowstore.Store, func()) {
	t.Helper()
	store, err := workflowstore.Open("file::memory:?cache=shared", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, func() { store.Close() }
}
