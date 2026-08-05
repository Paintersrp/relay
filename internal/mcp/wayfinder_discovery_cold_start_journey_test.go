package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/mcpcomposition"
	workflowprojects "relay/internal/app/projects/workflow"
	appwayfinder "relay/internal/app/wayfinder"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/routecontracts"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

func TestWayfinderDiscoveryColdStartJourney(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := coldStartStore(t, root, false)
	t.Cleanup(func() { _ = store.Close() })

	repositoryPath := filepath.Join(root, sourceSnapshotRepository)
	newSourceSnapshotRepository(t, repositoryPath, map[string]string{
		"README.md":              sourceSnapshotReadmeA,
		sourceSnapshotNestedPath: sourceSnapshotNestedBytes,
	})
	commitA := sourceSnapshotGit(t, repositoryPath, "rev-parse", "HEAD")

	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	registerSourceSnapshotRepository(t, ctx, store, repositories, sourceSnapshotRepository, repositoryPath)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{
			ProjectID:   sourceSnapshotProject,
			Name:        "Wayfinder discovery",
			Description: "cold-start composition",
		})
		if err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, sourceSnapshotRepository)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	policy, err := mcpcomposition.Open(ctx, filepath.Join(root, "source-vault"), store, []byte("wayfinder-discovery-cold-start-cursor-key"), fileacquisition.FetchFunc(func(context.Context, fileacquisition.FileParameter) (fileacquisition.FetchedFile, error) {
		return fileacquisition.FetchedFile{}, errors.New("cold-start journey has no file inputs")
	}))
	if err != nil {
		t.Fatal(err)
	}
	lifecycleHandler, err := NewOperationPacketLifecycleHandler(policy.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	wayfinder, err := appwayfinder.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := workflowprojects.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, sourceSnapshotSurface)
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{
		Projects:  projects,
		Packets:   policy.Packets,
		Lifecycle: lifecycleHandler,
		Source:    policy.Source,
		Wayfinder: wayfinder,
		Tickets:   nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := BuildRouteHandlers(manifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForRoute(nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}

	var listedProjects struct {
		Projects []workflowstore.Project `json:"projects"`
	}
	coldStartDecode(t, coldStartCall(t, server, "list_projects", map[string]any{}), &listedProjects)
	if len(listedProjects.Projects) != 1 || listedProjects.Projects[0].Status != workflowstore.ProjectStatusActive {
		t.Fatalf("projects = %#v", listedProjects.Projects)
	}
	project := listedProjects.Projects[0]
	if project.ProjectID == "" || project.ProjectID == "project-wayfinder" || project.ProjectID == "sourceSnapshotProject" {
		t.Fatalf("project identity = %#v", project)
	}

	resolutionInput := "packet-backed resolution evidence"
	resolutionDigest := coldStartDigest([]byte(resolutionInput))
	create := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract": sourceSnapshotSurface,
		"mutation_id":      "wayfinder-discovery-cold-start",
		"operation_id":     sourceSnapshotOperation,
		"project_id":       project.ProjectID,
		"inputs": []any{map[string]any{
			"input_name": "resolution_input", "source_kind": "inline_text", "display_name": "resolution.txt",
			"media_type": "text/plain", "expected_sha256": resolutionDigest,
			"source": map[string]any{"text": resolutionInput},
		}},
		"workflow_references": []any{},
		"attestations": []any{
			map[string]any{
				"kind": "exact_evidence", "input_name": "resolution_input", "subject_sha256": resolutionDigest, "complete": true,
			},
			map[string]any{
				"kind": "sensitive_data_clearance", "input_name": "resolution_input",
				"clearance": map[string]any{
					"policy_version": "relay.canonical-artifact-sensitive-data.v1", "confirmed": true,
					"subject_sha256": resolutionDigest,
					"declaration": map[string]any{
						"password": false, "api_key_or_access_token": false,
						"refresh_token_or_session_material": false, "cookie_or_authorization_header": false,
						"private_or_ssh_key": false, "credential": false,
						"complete_secret_bearing_environment_file": false,
						"avoidable_signed_secret_bearing_url":      false,
					},
				},
			},
		},
	})
	var created CreateOperationPacketResult
	coldStartDecode(t, create, &created)
	packetID := created.Packet.Summary.PacketID
	if packetID == "" {
		t.Fatal("create response did not contain a direct packet id")
	}
	if created.Packet.Summary.SurfaceContract != sourceSnapshotSurface || created.Packet.Summary.OperationID != sourceSnapshotOperation || created.Packet.Summary.ProjectID != project.ProjectID {
		t.Fatalf("created packet summary = %#v", created.Packet.Summary)
	}

	workspaceManifest := coldStartRoute(t, routes, "wayfinder-workspace.v1")
	workspaceHandlers, err := BuildRouteHandlers(workspaceManifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	workspaceServer, err := NewServerForRoute(nil, workspaceManifest, workspaceHandlers)
	if err != nil {
		t.Fatal(err)
	}
	workspacePacketResponse := coldStartCall(t, workspaceServer, "create_operation_packet", map[string]any{
		"surface_contract": "wayfinder-workspace.v1",
		"mutation_id":      "wayfinder-discovery-workspace-route-packet",
		"operation_id":     "wayfinder.workspace",
		"project_id":       project.ProjectID,
		"inputs":           []any{}, "workflow_references": []any{}, "attestations": []any{},
	})
	var workspacePacket CreateOperationPacketResult
	coldStartDecode(t, workspacePacketResponse, &workspacePacket)
	workspacePacketID := workspacePacket.Packet.Summary.PacketID
	if workspacePacketID == "" || workspacePacket.Packet.Summary.SurfaceContract != "wayfinder-workspace.v1" {
		t.Fatalf("workspace-route packet = %#v", workspacePacket.Packet.Summary)
	}

	workspace, err := wayfinder.CreateWorkspace(ctx, appwayfinder.CreateWorkspaceInput{ProjectID: project.ProjectID, FeatureSlug: "discovery-feature"})
	if err != nil {
		t.Fatal(err)
	}
	foreignProjectID := project.ProjectID + "-foreign"
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: foreignProjectID, Name: "Foreign project", Description: "cross-project rejection"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	foreignWorkspace, err := wayfinder.CreateWorkspace(ctx, appwayfinder.CreateWorkspaceInput{ProjectID: foreignProjectID, FeatureSlug: "foreign-feature"})
	if err != nil {
		t.Fatal(err)
	}
	ticket, workspaceAfterTicket, err := wayfinder.CreateDiscoveryTicket(ctx, appwayfinder.CreateDiscoveryTicketInput{
		WorkspaceID: workspace.WorkspaceID, ExpectedVersion: workspace.Version, TicketKey: "discover-resolution", Subject: "Resolve packet evidence", DependsOnTicketIDs: []string{}, DependencyKind: "informs",
	})
	if err != nil {
		t.Fatal(err)
	}

	resolveRequest := map[string]any{
		"packet_id": workspacePacketID, "workspace_id": workspaceAfterTicket.WorkspaceID,
		"expected_version": workspaceAfterTicket.Version, "ticket_id": ticket.DiscoveryTicketID,
		"expected_ticket_ver": ticket.Version, "resolution_sequence": 1, "resolution_kind": "resolved",
		"input_name": "resolution_input",
	}
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", cloneWayfinderRequest(resolveRequest)))

	foreignWorkspaceRequest := cloneWayfinderRequest(resolveRequest)
	foreignWorkspaceRequest["workspace_id"] = foreignWorkspace.WorkspaceID
	foreignWorkspaceRequest["expected_version"] = foreignWorkspace.Version
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", foreignWorkspaceRequest))

	missingInputRequest := cloneWayfinderRequest(resolveRequest)
	missingInputRequest["packet_id"] = packetID
	missingInputRequest["input_name"] = "missing_input"
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", missingInputRequest))

	staleWorkspaceRequest := cloneWayfinderRequest(resolveRequest)
	staleWorkspaceRequest["packet_id"] = packetID
	staleWorkspaceRequest["expected_version"] = workspaceAfterTicket.Version - 1
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", staleWorkspaceRequest))

	active := coldStartCall(t, server, "get_active_operation_packet", map[string]any{
		"surface_contract": sourceSnapshotSurface,
		"project_id":       project.ProjectID,
		"operation_id":     sourceSnapshotOperation,
	})
	var activeView OperationPacketView
	coldStartDecode(t, active, &activeView)
	if activeView.Summary.PacketID != packetID {
		t.Fatalf("active packet id = %q want %q", activeView.Summary.PacketID, packetID)
	}

	listed := coldStartCall(t, server, "list_operation_repositories", map[string]any{
		"surface_contract": sourceSnapshotSurface,
		"packet_id":        packetID,
	})
	var repositoryResult struct {
		Packet       OperationPacketSummary `json:"packet"`
		Repositories []struct {
			RepositoryKey                        string            `json:"repository_key"`
			RepositoryTarget                     string            `json:"repository_target"`
			BindingOrder                         int64             `json:"binding_order"`
			RevisionSource                       string            `json:"revision_source"`
			ConfiguredWorkingBranchRef           string            `json:"configured_working_branch_ref"`
			RepositoryTargetConfigurationVersion int64             `json:"repository_target_configuration_version"`
			CommitOID                            string            `json:"commit_oid"`
			TreeOID                              string            `json:"tree_oid"`
			Anchors                              []json.RawMessage `json:"anchors"`
		} `json:"repositories"`
	}
	coldStartDecode(t, listed, &repositoryResult)
	if repositoryResult.Packet.PacketID != packetID || len(repositoryResult.Repositories) != 1 {
		t.Fatalf("repository projection = %#v", repositoryResult)
	}
	repository := repositoryResult.Repositories[0]
	if repository.RepositoryKey == "" || repository.CommitOID == "" || repository.TreeOID == "" || repository.CommitOID != commitA || repository.Anchors == nil || len(repository.Anchors) != 0 {
		t.Fatalf("repository authority = %#v want commit %s", repository, commitA)
	}

	appendSourceSnapshotCommit(t, repositoryPath)
	commitB := sourceSnapshotGit(t, repositoryPath, "rev-parse", "HEAD")
	if commitB == commitA {
		t.Fatal("repository did not advance to commit B")
	}

	fixture := sourcePacketFixture{
		server: server, manifest: manifest, packetID: packetID,
		commitA: commitA, commitB: commitB, repository: repository.RepositoryKey,
	}

	var tree listSourceTreeView
	fixture.callSource(t, "list_source_tree", fixture.sourceRequest(map[string]any{
		"operation_id": sourceSnapshotOperation,
		"recursive":    true,
		"limit":        512,
	}), &tree)
	paths := sourceTreePaths(tree)
	if !containsString(paths, sourceSnapshotNestedPath) || containsString(paths, "b-only.txt") {
		t.Fatalf("tree paths = %v", paths)
	}
	assertSourceCommitA(t, fixture, tree.Source, "list_source_tree")

	var found searchSourceView
	fixture.callSource(t, "search_source", fixture.sourceRequest(map[string]any{
		"operation_id":     sourceSnapshotOperation,
		"mode":             "text_literal",
		"text_literal":     sourceSnapshotNestedMarker,
		"limit":            8,
		"examined_objects": 64,
		"examined_bytes":   1 << 20,
	}), &found)
	if found.Completion != "complete" || len(found.Matches) != 1 || found.Matches[0].Path.Display != sourceSnapshotNestedPath {
		t.Fatalf("nested marker search = %#v", found)
	}
	assertSourceCommitA(t, fixture, found.Source, "search_source")

	var newer searchSourceView
	fixture.callSource(t, "search_source", fixture.sourceRequest(map[string]any{
		"operation_id":     sourceSnapshotOperation,
		"mode":             "text_literal",
		"text_literal":     sourceSnapshotCommitBMarker,
		"limit":            8,
		"examined_objects": 64,
		"examined_bytes":   1 << 20,
	}), &newer)
	if newer.Completion != "complete" || len(newer.Matches) != 0 {
		t.Fatalf("commit B marker search = %#v", newer)
	}
	assertSourceCommitA(t, fixture, newer.Source, "search_source")

	var text readSourceTextView
	fixture.callSource(t, "read_source_text", fixture.sourceRequest(map[string]any{
		"operation_id": sourceSnapshotOperation,
		"path":         sourcePathArgument(sourceSnapshotNestedPath),
		"limit":        4096,
	}), &text)
	if string(sourceTextBytes(text)) != sourceSnapshotNestedBytes {
		t.Fatalf("nested bytes = %q want %q", sourceTextBytes(text), sourceSnapshotNestedBytes)
	}
	assertSourceCommitA(t, fixture, text.Source, "read_source_text")

	rejected, reason := sourceRejection(t, coldStartCall(t, server, "read_source_text", fixture.sourceRequest(map[string]any{
		"operation_id": sourceSnapshotOperation,
		"path":         sourcePathArgument("b-only.txt"),
		"limit":        4096,
	})))
	if !rejected || reason == "" {
		t.Fatalf("commit B-only read rejected=%v reason=%q", rejected, reason)
	}

	resolveRequest["packet_id"] = packetID
	var resolved map[string]json.RawMessage
	coldStartDecode(t, coldStartCall(t, server, "resolve_discovery_ticket", resolveRequest), &resolved)
	var readback wayfinderWorkspaceReadback
	coldStartDecode(t, coldStartCall(t, workspaceServer, "read_workspace", map[string]any{"workspace_id": workspaceAfterTicket.WorkspaceID}), &readback)
	if readback.Workspace.WorkspaceID != workspaceAfterTicket.WorkspaceID || readback.Workspace.ProjectID != project.ProjectID || readback.Workspace.Version != workspaceAfterTicket.Version+1 || len(readback.Tickets) != 1 || readback.Tickets[0].TicketID != ticket.DiscoveryTicketID || len(readback.Tickets[0].Resolutions) != 1 || readback.Tickets[0].Resolutions[0].Digest != resolutionDigest {
		t.Fatalf("resolved workspace readback = %#v", readback)
	}
	encodedReadback, err := json.Marshal(readback)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedReadback) == "" || containsInternalWayfinderFields(string(encodedReadback)) {
		t.Fatalf("workspace readback leaked internal fields: %s", encodedReadback)
	}

	staleTicketRequest := cloneWayfinderRequest(resolveRequest)
	staleTicketRequest["expected_version"] = readback.Workspace.Version
	staleTicketRequest["expected_ticket_ver"] = ticket.Version
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", staleTicketRequest))

	secondTicket, workspaceAfterSecondTicket, err := wayfinder.CreateDiscoveryTicket(ctx, appwayfinder.CreateDiscoveryTicketInput{
		WorkspaceID: readback.Workspace.WorkspaceID, ExpectedVersion: readback.Workspace.Version, TicketKey: "missing-resolution-artifact", Subject: "Reject broken retained evidence", DependsOnTicketIDs: []string{}, DependencyKind: "informs",
	})
	if err != nil {
		t.Fatal(err)
	}
	var retainedPath string
	if err := store.DB().QueryRowContext(ctx, `SELECT retained.relative_path FROM operation_packet_artifact_bindings AS binding JOIN operation_packet_retained_artifacts AS retained ON retained.id = binding.retained_artifact_row_id JOIN operation_packets AS packet ON packet.id = binding.packet_row_id WHERE packet.packet_id = ? AND binding.dependency_class = 'input_artifact' AND binding.dependency_key = 'resolution_input'`, packetID).Scan(&retainedPath); err != nil {
		t.Fatal(err)
	}
	retainedFile := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(retainedPath))
	if err := os.WriteFile(retainedFile, []byte("tampered retained evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	brokenArtifactRequest := cloneWayfinderRequest(resolveRequest)
	brokenArtifactRequest["workspace_id"] = workspaceAfterSecondTicket.WorkspaceID
	brokenArtifactRequest["expected_version"] = workspaceAfterSecondTicket.Version
	brokenArtifactRequest["ticket_id"] = secondTicket.DiscoveryTicketID
	brokenArtifactRequest["expected_ticket_ver"] = secondTicket.Version
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", brokenArtifactRequest))
	if err := os.Remove(retainedFile); err != nil {
		t.Fatal(err)
	}
	requireColdStartToolError(t, coldStartCall(t, server, "resolve_discovery_ticket", brokenArtifactRequest))
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsInternalWayfinderFields(value string) bool {
	for _, field := range []string{"row_id", "retained_artifact", "valid"} {
		if strings.Contains(value, field) {
			return true
		}
	}
	return false
}

func coldStartDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func requireColdStartToolError(t *testing.T, response Response) {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected transport error: %v", response.Error)
	}
	var result ToolCallResult
	raw, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got %#v", result)
	}
}
