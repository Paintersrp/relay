package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/routecontracts"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestWayfinderDiscoveryColdStartPacketDispatcherFlow(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repositoryPath := coldStartGitRepository(t, filepath.Join(root, "project-repository"))
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	target, err := repositories.Register(ctx, "project-repository", repositoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{RepoTarget: "project-repository", ExpectedConfigurationVersion: target.ConfigurationVersion, ConfiguredBranchRef: "refs/heads/main"}); err != nil {
			return err
		}
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: "project-wayfinder", Name: "Wayfinder", Description: "cold start"})
		if err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, "project-repository")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	vaults, err := sourcevault.Open(ctx, filepath.Join(root, "source-vault"), store)
	if err != nil {
		t.Fatal(err)
	}
	publications, err := appoperations.NewAuthorityPublicationService(store, vaults)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := appoperations.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := appoperations.NewDefaultLifecycleService(store, repositories, vaults, publications, fileacquisition.FetchFunc(func(context.Context, fileacquisition.FileParameter) (fileacquisition.FetchedFile, error) {
		return fileacquisition.FetchedFile{}, errors.New("cold-start test has no file inputs")
	}), packets)
	if err != nil {
		t.Fatal(err)
	}
	lifecycleHandler, err := NewOperationPacketLifecycleHandler(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Packets: packets, Lifecycle: lifecycleHandler})
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, "wayfinder-discovery.v1")
	handlers, err := BuildRouteHandlers(manifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForRoute(nil, nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}

	create := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract":    "wayfinder-discovery.v1",
		"mutation_id":         "cold-start-create",
		"operation_id":        "wayfinder.discovery",
		"project_id":          "project-wayfinder",
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
	})
	var created CreateOperationPacketResult
	coldStartDecode(t, create, &created)
	packetID := created.Packet.Summary.PacketID
	if packetID == "" {
		t.Fatal("create response did not contain a packet id")
	}
	if created.Mutation.ResultKind == "" || created.Mutation.ResultSHA256 == "" || created.Mutation.CommittedAt == "" {
		t.Fatalf("create response omitted mutation metadata: %#v", created.Mutation)
	}

	active := coldStartCall(t, server, "get_active_operation_packet", map[string]any{
		"surface_contract": "wayfinder-discovery.v1",
		"project_id":       "project-wayfinder",
		"operation_id":     "wayfinder.discovery",
	})
	var activeView OperationPacketView
	coldStartDecode(t, active, &activeView)
	if activeView.Summary.PacketID != packetID || activeView.Summary.SurfaceContract != "wayfinder-discovery.v1" || activeView.Summary.OperationID != "wayfinder.discovery" {
		t.Fatalf("active packet = %#v", activeView.Summary)
	}

	listed := coldStartCall(t, server, "list_operation_repositories", map[string]any{"surface_contract": "wayfinder-discovery.v1", "packet_id": packetID})
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
		t.Fatalf("repository result = %#v", repositoryResult)
	}
	repository := repositoryResult.Repositories[0]
	if repository.RepositoryKey != "project-repository" || repository.CommitOID == "" || repository.TreeOID == "" || repository.RevisionSource != "configured_working_branch" || repository.ConfiguredWorkingBranchRef != "refs/heads/main" || repository.RepositoryTargetConfigurationVersion < 1 {
		t.Fatalf("repository authority = %#v", repository)
	}

	unknown := coldStartCall(t, server, "list_operation_repositories", map[string]any{"surface_contract": "wayfinder-discovery.v1", "packet_id": packetID, "unknown": true})
	if unknown.Error == nil {
		t.Fatal("unknown input field was accepted")
	}
	foreignOperation := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract":    "wayfinder-discovery.v1",
		"mutation_id":         "cold-start-foreign-operation",
		"operation_id":        "wayfinder.workspace",
		"project_id":          "project-wayfinder",
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
	})
	if foreignOperation.Error == nil {
		t.Fatal("foreign operation was accepted")
	}
	foreignSurface := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract":    "wayfinder-workspace.v1",
		"mutation_id":         "cold-start-foreign-surface",
		"operation_id":        "wayfinder.discovery",
		"project_id":          "project-wayfinder",
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
	})
	if foreignSurface.Error == nil {
		t.Fatal("foreign surface was accepted")
	}
}

func coldStartGitRepository(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	coldStartGit(t, path, "init", "-b", "main")
	coldStartGit(t, path, "config", "user.email", "relay@example.test")
	coldStartGit(t, path, "config", "user.name", "Relay Test")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("wayfinder discovery\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coldStartGit(t, path, "add", ".")
	coldStartGit(t, path, "commit", "-m", "cold start")
	return path
}

func coldStartGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func coldStartRoute(t *testing.T, routes routecontracts.RouteSet, surface string) routecontracts.RouteManifest {
	t.Helper()
	for _, route := range routes.Manifests {
		if route.SurfaceContract == surface {
			return route
		}
	}
	t.Fatalf("route %s is missing", surface)
	return routecontracts.RouteManifest{}
}

func coldStartCall(t *testing.T, server *Server, tool string, arguments map[string]any) Response {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(ToolCallParams{Name: tool, Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	response := server.handleToolsCall(Request{ID: json.RawMessage(`1`), Params: params})
	return response
}

func coldStartDecode(t *testing.T, result Response, target any) {
	t.Helper()
	if result.Error != nil {
		t.Fatalf("tool call failed: %v", result.Error)
	}
	var toolResult ToolCallResult
	raw, err := json.Marshal(result.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &toolResult); err != nil {
		t.Fatal(err)
	}
	if len(toolResult.Content) != 1 {
		t.Fatalf("tool content = %#v", toolResult.Content)
	}
	decoder := json.NewDecoder(strings.NewReader(toolResult.Content[0].Text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatal(fmt.Errorf("strict result decode: %w; text=%s", err, toolResult.Content[0].Text))
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatal("result contains trailing JSON")
	}
}
