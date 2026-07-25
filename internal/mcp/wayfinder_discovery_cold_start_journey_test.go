package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"relay/internal/app/mcpcomposition"
	workflowprojects "relay/internal/app/projects/workflow"
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
	})
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := BuildRouteHandlers(manifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForRoute(nil, nil, manifest, handlers)
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

	create := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract":    sourceSnapshotSurface,
		"mutation_id":         "wayfinder-discovery-cold-start",
		"operation_id":        sourceSnapshotOperation,
		"project_id":          project.ProjectID,
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
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
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
