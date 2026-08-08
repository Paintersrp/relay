package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/app/mcpcomposition"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/routecontracts"
	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

// TestCreateOperationPacketInputFilesSnakeCaseDecodeReachesFileAcquisition
// drives create_operation_packet through the real route dispatcher and packet
// lifecycle so the strict request decoder must map the snake_case input_files
// members onto fileacquisition.FileParameter before the acquisition fetcher
// sees them.
func TestCreateOperationPacketInputFilesSnakeCaseDecodeReachesFileAcquisition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := coldStartStore(t, root, false)
	t.Cleanup(func() { _ = store.Close() })

	repositoryPath := filepath.Join(root, sourceSnapshotRepository)
	newSourceSnapshotRepository(t, repositoryPath, map[string]string{"README.md": sourceSnapshotReadmeA})
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	registerSourceSnapshotRepository(t, ctx, store, repositories, sourceSnapshotRepository, repositoryPath)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{
			ProjectID:   "project-input-files-decode",
			Name:        "Input files decode",
			Description: "snake_case input_files decoding",
		})
		if err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, sourceSnapshotRepository)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	fileBytes := []byte("# Requirements Document\n\nfake uploaded bytes\n")
	digest := coldStartDigest(fileBytes)
	const (
		fileURL  = "https://files.example/requirements-document.md"
		fileID   = "file-requirements-document-1"
		fileType = "text/markdown"
		fileName = "Requirements Document.md"
	)

	var fetched []fileacquisition.FileParameter
	policy, err := mcpcomposition.Open(ctx, filepath.Join(root, "source-vault"), store, []byte("input-files-decode-cursor-key-000000"), fileacquisition.FetchFunc(func(_ context.Context, file fileacquisition.FileParameter) (fileacquisition.FetchedFile, error) {
		fetched = append(fetched, file)
		return fileacquisition.FetchedFile{Bytes: append([]byte(nil), fileBytes...)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	lifecycleHandler, err := NewOperationPacketLifecycleHandler(policy.Lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Packets: policy.Packets, Lifecycle: lifecycleHandler})
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, "wayfinder-discovery.v1")
	handlers, err := BuildRouteHandlers(manifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForRoute(nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}

	createArguments := func(mutationID string, fileObject map[string]any) map[string]any {
		return map[string]any{
			"surface_contract": "wayfinder-discovery.v1",
			"mutation_id":      mutationID,
			"operation_id":     "wayfinder.discovery",
			"project_id":       "project-input-files-decode",
			"inputs": []any{map[string]any{
				"input_name": "resolution_input", "source_kind": "uploaded_file",
				"display_name": fileName, "media_type": fileType,
				"expected_sha256": digest, "source": map[string]any{"file_index": 0},
			}},
			"workflow_references": []any{},
			"attestations": []any{
				map[string]any{"kind": "exact_evidence", "input_name": "resolution_input", "subject_sha256": digest, "complete": true},
				map[string]any{"kind": "sensitive_data_clearance", "input_name": "resolution_input", "clearance": map[string]any{
					"policy_version": "relay.canonical-artifact-sensitive-data.v1", "confirmed": true, "subject_sha256": digest,
					"declaration": map[string]any{"password": false, "api_key_or_access_token": false, "refresh_token_or_session_material": false, "cookie_or_authorization_header": false, "private_or_ssh_key": false, "credential": false, "complete_secret_bearing_environment_file": false, "avoidable_signed_secret_bearing_url": false},
				}},
			},
			"input_files": []any{fileObject},
		}
	}

	t.Run("valid snake_case input_files reach file acquisition", func(t *testing.T) {
		response := coldStartCall(t, server, "create_operation_packet", createArguments("input-files-decode-valid", map[string]any{
			"download_url": fileURL,
			"file_id":      fileID,
			"mime_type":    fileType,
			"file_name":    fileName,
		}))
		var created CreateOperationPacketResult
		coldStartDecode(t, response, &created)
		if created.Packet.Summary.PacketID == "" || created.Packet.Summary.ReadinessState != "ready" || created.Packet.Summary.LifecycleState != "active" {
			t.Fatalf("created packet summary = %#v", created.Packet.Summary)
		}
		if len(fetched) != 1 {
			t.Fatalf("FetchFunc calls = %d, want 1", len(fetched))
		}
		got := fetched[0]
		if got.DownloadURL != fileURL || got.FileID != fileID || got.MIMEType != fileType || got.FileName != fileName {
			t.Fatalf("decoded FileParameter = %#v", got)
		}
	})

	t.Run("unknown input_files member is rejected before fetch", func(t *testing.T) {
		before := len(fetched)
		response := coldStartCall(t, server, "create_operation_packet", createArguments("input-files-decode-unknown", map[string]any{
			"download_url":   fileURL,
			"file_id":        fileID,
			"mime_type":      fileType,
			"file_name":      fileName,
			"mystery_member": "genuinely unknown",
		}))
		if response.Error != nil {
			t.Fatalf("unexpected transport error: %v", response.Error)
		}
		var toolResult ToolCallResult
		raw, err := json.Marshal(response.Result)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &toolResult); err != nil {
			t.Fatal(err)
		}
		if !toolResult.IsError {
			t.Fatalf("unknown input_files member was accepted: %#v", toolResult)
		}
		text := ""
		if len(toolResult.Content) == 1 {
			text = toolResult.Content[0].Text
		}
		if !strings.Contains(text, "unknown field") {
			t.Fatalf("strict decode rejection text = %q, want unknown field error", text)
		}
		if len(fetched) != before {
			t.Fatalf("FetchFunc invoked %d times for the rejected request, want 0", len(fetched)-before)
		}
	})
}
