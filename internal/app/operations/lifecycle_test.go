package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type lifecycleTestIDs struct{ next atomic.Int64 }

func (g *lifecycleTestIDs) PacketID() string { return fmt.Sprintf("opkt-pass6-%d", g.next.Add(1)) }
func (g *lifecycleTestIDs) ArtifactID() string {
	return fmt.Sprintf("artifact-pass6-%d", g.next.Add(1))
}

type lifecycleTestClock struct{ next atomic.Int64 }

func (c *lifecycleTestClock) Now() time.Time {
	return time.Unix(1_800_000_000, c.next.Add(1)).UTC()
}

type lifecycleTestFetcher struct {
	mu    sync.Mutex
	files map[string][]byte
	calls int
	fail  bool
	hook  func()
}

func (f *lifecycleTestFetcher) FetchFile(_ context.Context, file fileacquisition.FileParameter) (fileacquisition.FetchedFile, error) {
	f.mu.Lock()
	f.calls++
	if f.fail {
		f.mu.Unlock()
		return fileacquisition.FetchedFile{}, errors.New("fetch must not run")
	}
	data, ok := f.files[file.FileID]
	if !ok {
		f.mu.Unlock()
		return fileacquisition.FetchedFile{}, errors.New("unknown test file")
	}
	hook := f.hook
	f.hook = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return fileacquisition.FetchedFile{Bytes: append([]byte(nil), data...)}, nil
}

func (f *lifecycleTestFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *lifecycleTestFetcher) setFail(value bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = value
}

func (f *lifecycleTestFetcher) setHook(hook func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hook = hook
}

type lifecycleFixture struct {
	ctx          context.Context
	store        *workflowstore.Store
	repositories *workflowrepos.Registry
	service      *LifecycleService
	publications *AuthorityPublicationService
	fetcher      *lifecycleTestFetcher
	projectID    string
	projectRepo  string
	specsRepo    string
}

func openLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projectRepo := makeLifecycleGitRepo(t, filepath.Join(root, "project-repo"), map[string]string{"README.md": "project source\n"})
	specsRepo := makeLifecycleGitRepo(t, filepath.Join(root, "relay-specs"), map[string]string{
		"planner-source-manifest.json":        `{"manifest_version":"1.0","domains":{"requirements":["contracts/cross-cutting.md","contracts/requirements.md"],"design":["contracts/cross-cutting.md","contracts/requirements-to-design.md","contracts/design.md"]}}` + "\n",
		"contracts/cross-cutting.md":          "# Cross-cutting\n",
		"contracts/requirements.md":           "# Requirements\n",
		"contracts/requirements-to-design.md": "# Requirements to Design\n",
		"contracts/design.md":                 "# Design\n",
	})
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	registerLifecycleRepo(t, ctx, store, repositories, "project", projectRepo)
	registerLifecycleRepo(t, ctx, store, repositories, "relay-specs", specsRepo)

	projectID := "project-pass6"
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.CreateProject(ctx, workflowstore.CreateProjectParams{ProjectID: projectID, Name: "PASS-6", Description: "lifecycle integration"})
		if err != nil {
			return err
		}
		_, err = tx.AttachProjectRepository(ctx, project.ID, "project")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	vaults, err := sourcevault.Open(ctx, filepath.Join(root, "source-vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	publications, err := NewAuthorityPublicationService(store, vaults)
	if err != nil {
		t.Fatal(err)
	}
	ids := &lifecycleTestIDs{}
	clock := &lifecycleTestClock{}
	fetcher := &lifecycleTestFetcher{files: make(map[string][]byte)}
	service, err := NewLifecycleService(LifecycleDependencies{
		Store: store, Repositories: repositories, Vaults: vaults, Publications: publications,
		FileFetcher: fetcher, IDs: ids, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return lifecycleFixture{
		ctx:          ctx,
		store:        store,
		repositories: repositories,
		service:      service,
		publications: publications,
		fetcher:      fetcher,
		projectID:    projectID,
		projectRepo:  projectRepo,
		specsRepo:    specsRepo,
	}
}

func makeLifecycleGitRepo(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runLifecycleGit(t, path, "init", "-b", "main")
	runLifecycleGit(t, path, "config", "user.email", "relay@example.test")
	runLifecycleGit(t, path, "config", "user.name", "Relay Test")
	for name, content := range files {
		full := filepath.Join(path, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runLifecycleGit(t, path, "add", ".")
	runLifecycleGit(t, path, "commit", "-m", "fixture")
	return path
}

func runLifecycleGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func registerLifecycleRepo(t *testing.T, ctx context.Context, store *workflowstore.Store, repositories *workflowrepos.Registry, key, path string) {
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

func lifecycleRequirementsIdentity(projectID string) semanticidentity.CreateOperationPacket {
	text := "Author exact requirements"
	sha := lifecycleSHA([]byte(text))
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	return semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		ProjectID:       projectID,
		Inputs: []semanticidentity.InputBinding{{
			InputName:      "confirmed_intent",
			SourceKind:     "inline_text",
			DisplayName:    "intent.txt",
			MediaType:      "text/plain",
			ExpectedSHA256: sha,
			Source:         semanticidentity.InputBindingSource{Text: text},
		}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "confirmed_intent", InputName: "confirmed_intent", SubjectSHA256: sha, Confirmed: true},
			{Kind: "sensitive_data_clearance", InputName: "confirmed_intent", Clearance: &clearance},
		},
	}
}

func lifecycleDesignIdentity(projectID string, sha string) semanticidentity.CreateOperationPacket {
	index := int64(0)
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	return semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.design",
		ProjectID:       projectID,
		InputFileCount:  1,
		DeclaredFiles:   []semanticidentity.DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}},
		Inputs: []semanticidentity.InputBinding{{
			InputName:      "approved_requirements",
			SourceKind:     "uploaded_file",
			DisplayName:    "requirements.md",
			MediaType:      "text/markdown",
			ExpectedSHA256: sha,
			Source:         semanticidentity.InputBindingSource{FileIndex: &index},
		}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "approved_artifact", InputName: "approved_requirements", SubjectSHA256: sha, Approved: true},
			{Kind: "sensitive_data_clearance", InputName: "approved_requirements", Clearance: &clearance},
		},
	}
}

func TestLifecycleGovernanceOnlyDirtyWorktreeRemainsValid(t *testing.T) {
	fixture := openLifecycleFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.specsRepo, "governance-only-dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{
		MutationID: "create-governance-only-dirty",
		Identity:   lifecycleRequirementsIdentity(fixture.projectID),
	})
	if err != nil || result.Replay || result.Packet.Summary.PacketID == "" {
		t.Fatalf("create = %#v err=%v", result, err)
	}
}

func TestLifecycleDirtyTargetedProjectWorktreeFails(t *testing.T) {
	fixture := openLifecycleFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.projectRepo, "project-dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := []byte("approved requirements")
	fixture.fetcher.files["dirty-project"] = data
	_, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{
		MutationID: "create-dirty-project",
		Identity:   lifecycleDesignIdentity(fixture.projectID, lifecycleSHA(data)),
		Files: []fileacquisition.FileParameter{{
			FileID:   "dirty-project",
			FileName: "requirements.md",
			MIMEType: "text/markdown",
		}},
	})
	if err == nil {
		t.Fatal("expected dirty targeted project failure")
	}
}

func TestLifecycleRelaySpecsAsProjectRequiresCleanWorktree(t *testing.T) {
	fixture := openLifecycleFixture(t)
	project, err := fixture.store.GetProjectByProjectID(fixture.ctx, fixture.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.WithTx(fixture.ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.AttachProjectRepository(fixture.ctx, project.ID, "relay-specs")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.specsRepo, "dual-role-dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := []byte("approved requirements")
	fixture.fetcher.files["dirty-dual-role-specs"] = data
	_, err = fixture.service.Create(fixture.ctx, CreateLifecycleInput{
		MutationID: "create-dirty-dual-role-specs",
		Identity:   lifecycleDesignIdentity(fixture.projectID, lifecycleSHA(data)),
		Files: []fileacquisition.FileParameter{{
			FileID:   "dirty-dual-role-specs",
			FileName: "requirements.md",
			MIMEType: "text/markdown",
		}},
	})
	if err == nil {
		t.Fatal("expected dirty relay-specs Project repository failure")
	}
}

func TestLifecycleGovernanceAuthorityChangeBeforePublicationFailsClosed(t *testing.T) {
	fixture := openLifecycleFixture(t)
	data := []byte("approved requirements")
	sha := lifecycleSHA(data)
	index := int64(0)
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	identity := semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.design",
		ProjectID:       fixture.projectID,
		InputFileCount:  1,
		DeclaredFiles:   []semanticidentity.DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}},
		Inputs: []semanticidentity.InputBinding{{
			InputName:      "approved_requirements",
			SourceKind:     "uploaded_file",
			DisplayName:    "requirements.md",
			MediaType:      "text/markdown",
			ExpectedSHA256: sha,
			Source:         semanticidentity.InputBindingSource{FileIndex: &index},
		}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "approved_artifact", InputName: "approved_requirements", SubjectSHA256: sha, Approved: true},
			{Kind: "sensitive_data_clearance", InputName: "approved_requirements", Clearance: &clearance},
		},
	}
	fixture.fetcher.files["requirements-authority-change"] = data
	fixture.fetcher.setHook(func() {
		path := filepath.Join(fixture.specsRepo, "contracts", "requirements.md")
		if err := os.WriteFile(path, []byte("# Requirements changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runLifecycleGit(t, fixture.specsRepo, "add", "contracts/requirements.md")
		runLifecycleGit(t, fixture.specsRepo, "commit", "-m", "change governance authority")
	})
	_, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{
		MutationID: "create-governance-authority-change",
		Identity:   identity,
		Files: []fileacquisition.FileParameter{{
			FileID:   "requirements-authority-change",
			FileName: "requirements.md",
			MIMEType: "text/markdown",
		}},
	})
	if err == nil {
		t.Fatal("expected stale governance authority failure")
	}
	if strings.Contains(err.Error(), fixture.specsRepo) || strings.Contains(err.Error(), "requirements.md") {
		t.Fatalf("error leaked repository diagnostics: %v", err)
	}
}

func TestLifecycleCreateCloseAndReplay(t *testing.T) {
	fixture := openLifecycleFixture(t)
	text := "Author exact requirements"
	sha := lifecycleSHA([]byte(text))
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	identity := semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: fixture.projectID,
		Inputs: []semanticidentity.InputBinding{{InputName: "confirmed_intent", SourceKind: "inline_text", DisplayName: "intent.txt", MediaType: "text/plain", ExpectedSHA256: sha, Source: semanticidentity.InputBindingSource{Text: text}}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "confirmed_intent", InputName: "confirmed_intent", SubjectSHA256: sha, Confirmed: true},
			{Kind: "sensitive_data_clearance", InputName: "confirmed_intent", Clearance: &clearance},
		},
	}
	created, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: "create-requirements", Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if created.Replay || created.Packet.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || len(created.Packet.DocumentBytes) == 0 {
		t.Fatalf("created = %#v", created)
	}
	integrity, err := fixture.store.GetOperationPacketPublicationIntegrityByMutationKey(fixture.ctx, workflowstore.MCPMutationKey{SurfaceContractID: "planner-authoring.v1", ToolName: string(registry.MutationToolCreateOperationPacket), MutationID: "create-requirements"})
	if err != nil || integrity.Packet.PacketID != created.Packet.Summary.PacketID || len(integrity.RetainedArtifacts) != 1 || len(integrity.VaultRelationships) < 4 {
		t.Fatalf("integrity = %#v err=%v", integrity, err)
	}

	closed, err := fixture.service.Close(fixture.ctx, CloseLifecycleInput{MutationID: "close-requirements", Identity: semanticidentity.CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: created.Packet.Summary.PacketID}})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Replay || closed.Packet.LifecycleState != workflowstore.OperationPacketLifecycleClosed {
		t.Fatalf("closed = %#v", closed)
	}
	replay, err := fixture.service.Close(fixture.ctx, CloseLifecycleInput{MutationID: "close-requirements", Identity: semanticidentity.CloseOperationPacket{SurfaceContract: "planner-authoring.v1", ExpectedPacketID: created.Packet.Summary.PacketID}})
	if err != nil || !replay.Replay || replay.Mutation.ResultSHA256 != closed.Mutation.ResultSHA256 || replay.Packet.PacketID != closed.Packet.PacketID {
		t.Fatalf("close replay = %#v err=%v", replay, err)
	}
}

func TestLifecycleCreateReplayPrecedesUploadedFileAcquisition(t *testing.T) {
	fixture := openLifecycleFixture(t)
	data := []byte("approved requirements")
	sha := lifecycleSHA(data)
	fixture.fetcher.files["requirements"] = data
	index := int64(0)
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	identity := semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.design", ProjectID: fixture.projectID,
		InputFileCount: 1, DeclaredFiles: []semanticidentity.DeclaredFile{{FileIndex: 0, ExpectedSHA256: sha}},
		Inputs: []semanticidentity.InputBinding{{InputName: "approved_requirements", SourceKind: "uploaded_file", DisplayName: "requirements.md", MediaType: "text/markdown", ExpectedSHA256: sha, Source: semanticidentity.InputBindingSource{FileIndex: &index}}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "approved_artifact", InputName: "approved_requirements", SubjectSHA256: sha, Approved: true},
			{Kind: "sensitive_data_clearance", InputName: "approved_requirements", Clearance: &clearance},
		},
	}
	request := CreateLifecycleInput{MutationID: "create-design", Identity: identity, Files: []fileacquisition.FileParameter{{FileID: "requirements", FileName: "requirements.md", MIMEType: "text/markdown"}}}
	first, err := fixture.service.Create(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.fetcher.setFail(true)
	second, err := fixture.service.Create(fixture.ctx, request)
	if err != nil || !second.Replay || second.Packet.Summary.PacketID != first.Packet.Summary.PacketID || fixture.fetcher.callCount() != 1 {
		t.Fatalf("replay = %#v calls=%d err=%v", second, fixture.fetcher.callCount(), err)
	}
}

func lifecycleSHA(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func TestLifecycleConstructorRequiresCompleteDependencies(t *testing.T) {
	if _, err := NewLifecycleService(LifecycleDependencies{}); err == nil {
		t.Fatal("expected constructor failure")
	}
}
