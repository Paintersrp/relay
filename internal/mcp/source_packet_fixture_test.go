package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	appoperations "relay/internal/app/operations"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/routecontracts"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcegateway"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

// The packet-authorized snapshot fixture publishes one ready Wayfinder packet
// against commit A, then advances the working repository to commit B. Every
// source read must keep returning commit A.
const (
	sourceSnapshotSurface       = "wayfinder-discovery.v1"
	sourceSnapshotOperation     = "wayfinder.discovery"
	sourceSnapshotProject       = "project-source-snapshot"
	sourceSnapshotRepository    = "project-repository"
	sourceSnapshotUnbound       = "unbound-repository"
	sourceSnapshotNestedPath    = "internal/deep/nested/marker.txt"
	sourceSnapshotNestedMarker  = "RELAY_NESTED_SNAPSHOT_MARKER_4F1C"
	sourceSnapshotCommitBMarker = "RELAY_COMMIT_B_ONLY_MARKER_9A2D"
	sourceSnapshotReadmeA       = "wayfinder snapshot\n"
	sourceSnapshotReadmeB       = "wayfinder snapshot advanced\n"
	sourceSnapshotNotes         = "notes alpha\n"
)

var sourceSnapshotNestedBytes = "nested source " + sourceSnapshotNestedMarker + " tail\nalpha alpha\n"

// sourceSnapshotCommitAPaths is the exact recursive tree of commit A in full
// path byte order.
var sourceSnapshotCommitAPaths = []string{
	"README.md",
	"docs",
	"docs/notes.md",
	"internal",
	"internal/deep",
	"internal/deep/nested",
	"internal/deep/nested/marker.txt",
}

type sourcePacketFixture struct {
	ctx         context.Context
	server      *Server
	dispatchers RouteDispatchers
	manifest    routecontracts.RouteManifest
	packetID    string
	commitA     string
	commitB     string
	repository  string
	unbound     string
	secretPaths []string
}

func openSourcePacketFixture(t *testing.T) sourcePacketFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := coldStartStore(t, root, false)
	t.Cleanup(func() { _ = store.Close() })

	repositoryPath := filepath.Join(root, "project-repository")
	newSourceSnapshotRepository(t, repositoryPath, map[string]string{
		"README.md":              sourceSnapshotReadmeA,
		"docs/notes.md":          sourceSnapshotNotes,
		sourceSnapshotNestedPath: sourceSnapshotNestedBytes,
	})
	commitA := sourceSnapshotGit(t, repositoryPath, "rev-parse", "HEAD")

	unboundPath := filepath.Join(root, "unbound-repository")
	newSourceSnapshotRepository(t, unboundPath, map[string]string{"README.md": "unauthorized repository\n"})

	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	registerSourceSnapshotRepository(t, ctx, store, repositories, sourceSnapshotRepository, repositoryPath)
	registerSourceSnapshotRepository(t, ctx, store, repositories, sourceSnapshotUnbound, unboundPath)
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: sourceSnapshotProject, Name: "Source Snapshot", Description: "packet-authorized source reads"})
		if err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, sourceSnapshotRepository)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	vaultRoot := filepath.Join(root, "source-vault")
	vaults, err := sourcevault.Open(ctx, vaultRoot, store)
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
		return fileacquisition.FetchedFile{}, errors.New("source snapshot fixture has no file inputs")
	}), packets)
	if err != nil {
		t.Fatal(err)
	}
	lifecycleHandler, err := NewOperationPacketLifecycleHandler(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := sourcegateway.NewHMACCursorCodec([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	source, err := sourcegateway.NewService(packets, vaults, store, codec)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := routecontracts.BuildMCPRouteManifests()
	if err != nil {
		t.Fatal(err)
	}
	dispatchers, err := NewRouteDispatchers(routes, RouteDispatchServices{Packets: packets, Lifecycle: lifecycleHandler, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	manifest := coldStartRoute(t, routes, sourceSnapshotSurface)
	handlers, err := BuildRouteHandlers(manifest, dispatchers)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServerForRoute(nil, nil, manifest, handlers)
	if err != nil {
		t.Fatal(err)
	}

	create := coldStartCall(t, server, "create_operation_packet", map[string]any{
		"surface_contract":    sourceSnapshotSurface,
		"mutation_id":         "source-snapshot-create",
		"operation_id":        sourceSnapshotOperation,
		"project_id":          sourceSnapshotProject,
		"inputs":              []any{},
		"workflow_references": []any{},
		"attestations":        []any{},
	})
	var created CreateOperationPacketResult
	coldStartDecode(t, create, &created)
	if created.Packet.Summary.PacketID == "" {
		t.Fatal("fixture packet was not published")
	}

	// The live repository advances only after publication so every later read
	// must resolve the retained commit A closure.
	appendSourceSnapshotCommit(t, repositoryPath)
	commitB := sourceSnapshotGit(t, repositoryPath, "rev-parse", "HEAD")
	if commitA == commitB {
		t.Fatal("fixture did not advance the working repository")
	}

	return sourcePacketFixture{
		ctx: ctx, server: server, dispatchers: dispatchers, manifest: manifest,
		packetID: created.Packet.Summary.PacketID, commitA: commitA, commitB: commitB,
		repository: sourceSnapshotRepository, unbound: sourceSnapshotUnbound,
		secretPaths: []string{root, repositoryPath, unboundPath, vaultRoot},
	}
}

func newSourceSnapshotRepository(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceSnapshotGit(t, path, "init", "-b", "main")
	sourceSnapshotGit(t, path, "config", "user.email", "relay@example.test")
	sourceSnapshotGit(t, path, "config", "user.name", "Relay Test")
	for name, content := range files {
		full := filepath.Join(path, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sourceSnapshotGit(t, path, "add", "-A")
	sourceSnapshotGit(t, path, "commit", "-m", "snapshot a")
}

func appendSourceSnapshotCommit(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte(sourceSnapshotReadmeB), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "b-only.txt"), []byte("commit b "+sourceSnapshotCommitBMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceSnapshotGit(t, path, "add", "-A")
	sourceSnapshotGit(t, path, "commit", "-m", "snapshot b")
}

func sourceSnapshotGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func registerSourceSnapshotRepository(t *testing.T, ctx context.Context, store *workflowstore.Store, repositories *workflowrepos.Registry, key, path string) {
	t.Helper()
	target, err := repositories.Register(ctx, key, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConfigureRepositoryTarget(ctx, workflowstore.ConfigureRepositoryTargetParams{RepoTarget: key, ExpectedConfigurationVersion: target.ConfigurationVersion, ConfiguredBranchRef: "refs/heads/main"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// sourceRequest is the schema-conformant argument set shared by the source
// reads. The surface contract is only ever verified against the mounted route.
func (f sourcePacketFixture) sourceRequest(members map[string]any) map[string]any {
	request := map[string]any{
		"surface_contract": sourceSnapshotSurface,
		"packet_id":        f.packetID,
		"repository_key":   f.repository,
	}
	for name, value := range members {
		request[name] = value
	}
	return request
}

// dispatch invokes the real route dispatcher, exercising the strict request
// decoder and the route-bound surface check without the legacy pre-validator.
func (f sourcePacketFixture) dispatch(t *testing.T, tool string, arguments map[string]any) ToolCallResult {
	t.Helper()
	handler, ok := f.dispatchers.Handlers[f.manifest.RoutePath][tool]
	if !ok || handler == nil {
		t.Fatalf("dispatcher for %s/%s is missing", f.manifest.RoutePath, tool)
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return handler(json.RawMessage(raw))
}

// callSource invokes one source tool through the mounted route server.
func (f sourcePacketFixture) callSource(t *testing.T, tool string, arguments map[string]any, target any) {
	t.Helper()
	coldStartDecode(t, coldStartCall(t, f.server, tool, arguments), target)
}

// callSourceText returns the exact successful runtime response document.
func (f sourcePacketFixture) callSourceText(t *testing.T, tool string, arguments map[string]any) string {
	t.Helper()
	response := coldStartCall(t, f.server, tool, arguments)
	if response.Error != nil {
		t.Fatalf("%s failed: %v", tool, response.Error)
	}
	raw, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result ToolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("%s response = %#v", tool, result)
	}
	return result.Content[0].Text
}

// sourceRejection reports whether one route server call was rejected and
// returns the exact bounded reason text.
func sourceRejection(t *testing.T, response Response) (bool, string) {
	t.Helper()
	if response.Error != nil {
		return true, response.Error.Message
	}
	raw, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result ToolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	text := ""
	if len(result.Content) == 1 {
		text = result.Content[0].Text
	}
	return result.IsError, text
}

func sourcePathArgument(path string) map[string]any {
	digest := sha256.New()
	digest.Write([]byte(sourcegateway.PathIdentityVersion))
	digest.Write([]byte{0})
	digest.Write([]byte(path))
	return map[string]any{
		"path_id":       hex.EncodeToString(digest.Sum(nil)),
		"inline_base64": base64.StdEncoding.EncodeToString([]byte(path)),
	}
}

type sourceIdentityView struct {
	PacketID               string `json:"PacketID"`
	PacketSHA256           string `json:"PacketSHA256"`
	LifecycleState         string `json:"LifecycleState"`
	SurfaceContract        string `json:"SurfaceContract"`
	OperationID            string `json:"OperationID"`
	ProjectID              string `json:"ProjectID"`
	RepositoryKey          string `json:"RepositoryKey"`
	DependencyKey          string `json:"DependencyKey"`
	AnchorName             string `json:"AnchorName"`
	PublicationID          string `json:"PublicationID"`
	VaultRelationshipRowID int64  `json:"VaultRelationshipRowID"`
	CommitOID              string `json:"CommitOID"`
	TreeOID                string `json:"TreeOID"`
}

type sourcePathIdentityView struct {
	Version      string `json:"Version"`
	PathID       string `json:"PathID"`
	ByteLength   int64  `json:"ByteLength"`
	InlineBase64 string `json:"InlineBase64"`
	SelectorID   string `json:"SelectorID"`
	Display      string `json:"Display"`
	DisplayValid bool   `json:"DisplayValid"`
}

type sourceTreeEntryView struct {
	Path       sourcePathIdentityView `json:"Path"`
	Basename   sourcePathIdentityView `json:"Basename"`
	Mode       string                 `json:"Mode"`
	ObjectType string                 `json:"ObjectType"`
	ObjectOID  string                 `json:"ObjectOID"`
	Directory  bool                   `json:"Directory"`
}

type listSourceTreeView struct {
	Source    sourceIdentityView     `json:"Source"`
	Directory sourcePathIdentityView `json:"Directory"`
	Entries   []sourceTreeEntryView  `json:"Entries"`
	Complete  bool                   `json:"Complete"`
	Cursor    string                 `json:"Cursor"`
}

type searchSourceMatchView struct {
	MatchID           string                 `json:"MatchID"`
	Path              sourcePathIdentityView `json:"Path"`
	FileMode          string                 `json:"FileMode"`
	BlobOID           string                 `json:"BlobOID"`
	ByteOffset        int64                  `json:"ByteOffset"`
	MatchLength       int64                  `json:"MatchLength"`
	OccurrenceOrdinal int64                  `json:"OccurrenceOrdinal"`
}

type searchSourceView struct {
	Source                sourceIdentityView      `json:"Source"`
	Mode                  string                  `json:"Mode"`
	QueryID               string                  `json:"QueryID"`
	FilterID              string                  `json:"FilterID"`
	Matches               []searchSourceMatchView `json:"Matches"`
	ExaminedObjects       int64                   `json:"ExaminedObjects"`
	ExaminedBytes         int64                   `json:"ExaminedBytes"`
	ObjectBudgetExhausted bool                    `json:"ObjectBudgetExhausted"`
	ByteBudgetExhausted   bool                    `json:"ByteBudgetExhausted"`
	Completion            string                  `json:"Completion"`
	Cursor                string                  `json:"Cursor"`
}

type readSourceTextSegmentView struct {
	StartOffset   int64  `json:"StartOffset"`
	EndOffset     int64  `json:"EndOffset"`
	Bytes         []byte `json:"Bytes"`
	Terminator    []byte `json:"Terminator"`
	ContinuesLine bool   `json:"ContinuesLine"`
	LineComplete  bool   `json:"LineComplete"`
	FinalLine     bool   `json:"FinalLine"`
}

type readSourceTextView struct {
	Source     sourceIdentityView          `json:"Source"`
	Path       sourcePathIdentityView      `json:"Path"`
	Mode       string                      `json:"Mode"`
	ObjectOID  string                      `json:"ObjectOID"`
	Segments   []readSourceTextSegmentView `json:"Segments"`
	Offset     int64                       `json:"Offset"`
	NextOffset int64                       `json:"NextOffset"`
	TotalSize  int64                       `json:"TotalSize"`
	Complete   bool                        `json:"Complete"`
	Cursor     string                      `json:"Cursor"`
}

func sourceTreePaths(view listSourceTreeView) []string {
	paths := make([]string, 0, len(view.Entries))
	for _, entry := range view.Entries {
		paths = append(paths, entry.Path.Display)
	}
	return paths
}

func sourceTextBytes(view readSourceTextView) []byte {
	data := make([]byte, 0, view.TotalSize)
	for _, segment := range view.Segments {
		data = append(data, segment.Bytes...)
		data = append(data, segment.Terminator...)
	}
	return data
}
