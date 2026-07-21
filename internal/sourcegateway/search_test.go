package sourcegateway

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"relay/internal/app/operations"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func searchRequest(mode SearchMode, literal []byte) SearchRequest {
	request := SearchRequest{
		PacketID:        "opkt-fidelity",
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		RepositoryKey:   "relay",
		Mode:            mode,
		Limit:           MaxSearchPageMatches,
		Budget:          SearchBudget{ExaminedObjects: 64, ExaminedBytes: 8 << 20},
	}
	if mode == SearchModeTextLiteral {
		request.TextLiteral = string(literal)
	} else {
		request.ByteLiteral = append([]byte(nil), literal...)
	}
	return request
}

func searchPathReference(path []byte) PathReference {
	return PathReference{PathID: pathID(path), InlineBase64: canonicalInline(path)}
}

func TestSearchByteLiteralReturnsOverlapsInUnsignedPathOrderAndAllowsPageSizeChanges(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	dirTree := strings.Repeat("3", 40)
	aBlob := strings.Repeat("4", 40)
	zBlob := strings.Repeat("5", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {{Name: []byte("dir"), Mode: "040000", ObjectType: "tree", ObjectOID: dirTree}, {Name: []byte("z.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: zBlob}},
			dirTree:  {{Name: []byte("a.txt"), Mode: "100755", ObjectType: "blob", ObjectOID: aBlob}},
		},
		blobs: map[string][]byte{aBlob: []byte("aaaa"), zBlob: []byte("aa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1))
	request := searchRequest(SearchModeByteLiteral, []byte("aa"))
	request.Prefixes = []PathReference{searchPathReference([]byte("dir")), searchPathReference([]byte("dir/a.txt"))}
	request.Limit = 1
	var matches []SearchMatch
	for page := 0; page < 8; page++ {
		result, err := service.Search(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			if result.Cursor != "" {
				t.Fatal("complete result returned a cursor")
			}
			break
		}
		if result.Completion != SearchCompletionPageIncomplete || result.Cursor == "" {
			t.Fatalf("page=%d result=%#v", page, result)
		}
		request.Cursor = result.Cursor
		request.Limit = 2
	}
	if len(matches) != 3 {
		t.Fatalf("matches=%#v", matches)
	}
	for index, match := range matches {
		if match.Path.Display != "dir/a.txt" || match.ByteOffset != int64(index) || match.OccurrenceOrdinal != int64(index) || match.MatchLength != 2 || len(match.MatchID) != 64 || match.FileMode != "100755" {
			t.Fatalf("match[%d]=%#v", index, match)
		}
	}
}

func TestSearchTextValidationExcludesInvalidAndTruncatedUTF8ButKeepsNUL(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	badBlob := strings.Repeat("3", 40)
	truncatedBlob := strings.Repeat("4", 40)
	goodBlob := strings.Repeat("5", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("bad"), Mode: "100644", ObjectType: "blob", ObjectOID: badBlob}, {Name: []byte("good"), Mode: "100644", ObjectType: "blob", ObjectOID: goodBlob}, {Name: []byte("truncated"), Mode: "100644", ObjectType: "blob", ObjectOID: truncatedBlob}}},
		blobs: map[string][]byte{badBlob: {'x', 0xff, 'x'}, goodBlob: {'x', 0, 'x'}, truncatedBlob: {'x', 0xe2, 0x82}},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1))
	result, err := service.Search(context.Background(), searchRequest(SearchModeTextLiteral, []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion != SearchCompletionComplete || result.ExaminedObjects != 3 || len(result.Matches) != 2 || result.Matches[0].Path.Display != "good" || result.Matches[1].ByteOffset != 2 {
		t.Fatalf("result=%#v", result)
	}
}

func TestSearchBudgetsResumeAtTheFirstUndecidedCandidateAndAllowZeroMatches(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	firstBlob := strings.Repeat("3", 40)
	secondBlob := strings.Repeat("4", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: firstBlob}, {Name: []byte("b"), Mode: "100644", ObjectType: "blob", ObjectOID: secondBlob}}},
		blobs: map[string][]byte{firstBlob: []byte("zzzz"), secondBlob: []byte("x")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1))
	request := searchRequest(SearchModeByteLiteral, []byte("x"))
	request.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}
	first, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Completion != SearchCompletionBudgetIncomplete || first.Cursor == "" || !first.ByteBudgetExhausted || first.ObjectBudgetExhausted || len(first.Matches) != 0 || first.ExaminedObjects != 1 || first.ExaminedBytes != 4 {
		t.Fatalf("first=%#v", first)
	}
	request.Cursor = first.Cursor
	second, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Completion != SearchCompletionComplete || len(second.Matches) != 1 || second.Matches[0].Path.Display != "b" {
		t.Fatalf("second=%#v", second)
	}

	request = searchRequest(SearchModeByteLiteral, []byte("q"))
	request.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 8}
	object, err := service.Search(context.Background(), request)
	if err != nil || object.Completion != SearchCompletionBudgetIncomplete || !object.ObjectBudgetExhausted || object.ByteBudgetExhausted || object.Cursor == "" {
		t.Fatalf("object=%#v err=%v", object, err)
	}
}

func TestSearchRequestClosurePrefixesAndComponentBoundaries(t *testing.T) {
	valid := []SearchRequest{searchRequest(SearchModeTextLiteral, []byte("é")), searchRequest(SearchModeByteLiteral, []byte{0, 0xff})}
	for index, request := range valid {
		if _, err := validateSearchRequest(request); err != nil {
			t.Fatalf("valid[%d]: %v", index, err)
		}
	}
	cases := []struct {
		name    string
		request SearchRequest
		code    string
	}{
		{"empty text", SearchRequest{Mode: SearchModeTextLiteral, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, CodeInvalidRequest},
		{"empty bytes", SearchRequest{Mode: SearchModeByteLiteral, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, CodeInvalidRequest},
		{"both query forms", SearchRequest{Mode: SearchModeTextLiteral, TextLiteral: "x", ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, CodeInvalidRequest},
		{"invalid text", SearchRequest{Mode: SearchModeTextLiteral, TextLiteral: string([]byte{0xff}), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, CodeInvalidRequest},
		{"unsupported mode", SearchRequest{Mode: "regex", TextLiteral: "x", Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, CodeInvalidRequest},
		{"zero budget", SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 0, ExaminedBytes: 4}}, CodeInvalidRange},
		{"too-small budget", SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("needle"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 5}}, CodeInvalidRange},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateSearchRequest(test.request); ErrorCode(err) != test.code {
				t.Fatalf("error=%v code=%q want=%q", err, ErrorCode(err), test.code)
			}
		})
	}
	for _, test := range []struct {
		path, prefix []byte
		want         bool
	}{{[]byte("dir/file"), []byte("dir"), true}, {[]byte("directory/file"), []byte("dir"), false}, {[]byte("*literal/file"), []byte("*literal"), true}, {[]byte{0xff, 'x', '/', 'y'}, []byte{0xff, 'x'}, true}} {
		if got := pathHasComponentPrefix(test.path, test.prefix); got != test.want {
			t.Fatalf("path=%x prefix=%x got=%v want=%v", test.path, test.prefix, got, test.want)
		}
	}
}

func TestSearchCursorBindsAuthorityRequestBudgetCoordinateAndObjectSize(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	authority := fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1)
	service := newFidelityService(t, vault, authority)
	request := searchRequest(SearchModeByteLiteral, []byte("aa"))
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	payload, err := service.cursors.Decode(first.Cursor)
	if err != nil || !payload.SearchObjectSizeKnown || payload.SearchObjectSize != 4 {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}

	pageSize := request
	pageSize.Cursor = first.Cursor
	pageSize.Limit = 2
	if _, err := service.Search(context.Background(), pageSize); err != nil {
		t.Fatalf("page-size-only resume failed: %v", err)
	}
	mutations := []func(*SearchRequest){
		func(r *SearchRequest) { r.ByteLiteral = []byte("ab") },
		func(r *SearchRequest) { r.Budget.ExaminedBytes++ },
		func(r *SearchRequest) { r.Budget.ExaminedObjects++ },
		func(r *SearchRequest) { r.Prefixes = []PathReference{searchPathReference([]byte("a"))} },
	}
	for index, mutate := range mutations {
		changed := request
		changed.Cursor = first.Cursor
		mutate(&changed)
		if _, err := service.Search(context.Background(), changed); ErrorCode(err) != CodeInvalidCursor {
			t.Fatalf("mutation %d error=%v", index, err)
		}
	}
	changedAuthority := authority
	changedAuthority.Summary.PacketSHA256 = strings.Repeat("b", 64)
	changedService := newFidelityService(t, vault, changedAuthority)
	request.Cursor = first.Cursor
	if _, err := changedService.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("authority mutation error=%v", err)
	}

	payload.NextOffset = -1
	tampered, err := service.cursors.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = tampered
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("coordinate mutation error=%v", err)
	}
}

func TestSearchRetainedReadIntegrityCancellationAndDefensiveCopies(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1)
	base := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	wrapped := &searchVaultWrapper{base: base, blob: func(ctx context.Context, request sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
		if ctx.Err() != nil {
			return sourcevault.ReadRetainedBlobRangeResult{}, &sourcevault.Error{Code: sourcevault.CodeOperationCancelled}
		}
		value, err := base.ReadRetainedBlobRange(ctx, request)
		value.BlobOID = strings.Repeat("9", 40)
		return value, err
	}}
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, wrapped, nil)
	if _, err := service.Search(context.Background(), searchRequest(SearchModeByteLiteral, []byte("a"))); ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("object mismatch error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, wrapped, nil).Search(ctx, searchRequest(SearchModeByteLiteral, []byte("a"))); err == nil {
		t.Fatal("cancelled search succeeded")
	}

	literal := []byte("aa")
	prefix := searchPathReference([]byte("a"))
	request := searchRequest(SearchModeByteLiteral, literal)
	request.Prefixes = []PathReference{prefix}
	result, err := newFidelityService(t, base, authority).Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	literal[0] = 'z'
	prefix.PathID = strings.Repeat("f", 64)
	if result.QueryID == searchQueryID(SearchModeByteLiteral, literal) || len(result.Matches) == 0 {
		t.Fatal("request buffers were not copied")
	}
}

type searchVaultWrapper struct {
	base VaultReader
	tree func(context.Context, sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error)
	blob func(context.Context, sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error)
}

func (v *searchVaultWrapper) ReadRetainedTree(ctx context.Context, request sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
	if v.tree != nil {
		return v.tree(ctx, request)
	}
	return v.base.ReadRetainedTree(ctx, request)
}

func (v *searchVaultWrapper) ReadRetainedBlobRange(ctx context.Context, request sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
	if v.blob != nil {
		return v.blob(ctx, request)
	}
	return v.base.ReadRetainedBlobRange(ctx, request)
}

func TestSearchLifecycleStatesAndConcurrentResults(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	base := fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1)
	var expected []string
	for _, state := range []string{"active", "superseded", "closed"} {
		authority := base
		authority.Summary.LifecycleState = state
		result, err := newFidelityService(t, vault, authority).Search(context.Background(), searchRequest(SearchModeByteLiteral, []byte("aa")))
		if err != nil || result.Source.LifecycleState != state {
			t.Fatalf("state=%s result=%#v err=%v", state, result, err)
		}
		ids := matchIDs(result.Matches)
		if expected == nil {
			expected = ids
		} else if !bytes.Equal([]byte(strings.Join(expected, ",")), []byte(strings.Join(ids, ","))) {
			t.Fatalf("state=%s ids=%v want=%v", state, ids, expected)
		}
	}
	service := newFidelityService(t, vault, base)
	request := searchRequest(SearchModeByteLiteral, []byte("aa"))
	results := make([]SearchResult, 8)
	errors := make([]error, 8)
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], errors[index] = service.Search(context.Background(), request)
		}(index)
	}
	group.Wait()
	for index := range results {
		if errors[index] != nil || results[index].Completion != SearchCompletionComplete || len(results[index].Matches) != 3 {
			t.Fatalf("worker=%d result=%#v err=%v", index, results[index], errors[index])
		}
	}
}

func matchIDs(matches []SearchMatch) []string {
	result := make([]string, len(matches))
	for index, match := range matches {
		result[index] = match.MatchID
	}
	return result
}

func TestSearchRealRetainedGitSurvivesSourceRemoval(t *testing.T) {
	ctx := context.Background()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "init")
	runSearchGit(t, repo, "config", "user.name", "Relay Search Tests")
	runSearchGit(t, repo, "config", "user.email", "relay-search@example.test")
	runSearchGit(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(repo, "text.txt"), []byte("alpha alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "raw.bin"), []byte{0xff, 0, 0xff, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "add", "-A")
	runSearchGit(t, repo, "commit", "-m", "fixture")
	commitOID := runSearchGit(t, repo, "rev-parse", "HEAD")
	treeOID := runSearchGit(t, repo, "rev-parse", "HEAD^{tree}")

	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{RepoTarget: "relay", LocalPath: repo, ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	target, err := store.GetRepositoryTarget(ctx, "relay")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := sourcevault.Open(ctx, filepath.Join(root, "vaults"), store)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := manager.ImportClosure(ctx, sourcevault.ImportRequest{Revision: workflowrepos.ResolvedRevision{RepositoryTarget: target, RevisionSource: workflowrepos.RevisionSourceExplicitCommit, RepositoryTargetConfigurationVersion: target.ConfigurationVersion, CommitOID: commitOID, TreeOID: treeOID}})
	if err != nil {
		t.Fatal(err)
	}
	retention, err := manager.RetainClosure(ctx, sourcevault.RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "packet-search"})
	if err != nil {
		t.Fatal(err)
	}
	relationship := workflowstore.OperationPacketVaultRelationship{ID: 1, PublicationID: "publication-fidelity", PacketRowID: 11, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:primary", OwnerIdentity: retention.OwnerIdentity, RetentionRowID: retention.ID, ClosureRowID: imported.Closure.ID, VaultRowID: imported.Vault.ID, CommitOID: imported.CommitOID, TreeOID: imported.TreeOID}
	authority := fidelityAuthority(imported.CommitOID, imported.TreeOID, "", relationship.ID)
	authority.Relationship = relationship
	service := newFidelityService(t, manager, authority)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	text, err := service.Search(ctx, searchRequest(SearchModeTextLiteral, []byte("alpha")))
	if err != nil || text.Completion != SearchCompletionComplete || len(text.Matches) != 2 || text.Matches[0].Path.Display != "text.txt" {
		t.Fatalf("text=%#v err=%v", text, err)
	}
	binary, err := service.Search(ctx, searchRequest(SearchModeByteLiteral, []byte{0xff, 0}))
	if err != nil || binary.Completion != SearchCompletionComplete || len(binary.Matches) != 2 || binary.Matches[0].Path.Display != "raw.bin" {
		t.Fatalf("binary=%#v err=%v", binary, err)
	}
}

func runSearchGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func newSearchService(t *testing.T, resolver AuthorityResolver, vault VaultReader, selectors SelectorStore) *Service {
	t.Helper()
	if selectors == nil {
		selectors = &fidelitySelectorFake{values: map[string]workflowstore.SourcePathSelector{}}
	}
	codec, err := NewHMACCursorCodec([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(resolver, vault, selectors, codec)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type searchAuthorityResolver struct {
	authority operations.SourceReadAuthority
	err       error
}

func (r searchAuthorityResolver) ResolveSourceReadAuthority(context.Context, operations.ResolveSourceReadAuthorityRequest) (operations.SourceReadAuthority, error) {
	if r.err != nil {
		return operations.SourceReadAuthority{}, r.err
	}
	return r.authority, nil
}

func TestSearchCorpusModesPathsAndSelectors(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	dirTree := strings.Repeat("3", 40)
	longPath := bytes.Repeat([]byte{'l'}, MaxInlinePathBytes+1)
	arbitraryPath := []byte{0xff, 'x'}
	ids := map[string]string{}
	for index, value := range []string{"4", "5", "6", "7", "8", "9", "a", "b", "c", "d", "e", "f"} {
		ids[value] = strings.Repeat(value, 40)
		_ = index
	}
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("symlink"), Mode: "120000", ObjectType: "blob", ObjectOID: ids["4"]},
				{Name: []byte("regular"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["5"]},
				{Name: []byte("gitlink"), Mode: "160000", ObjectType: "commit", ObjectOID: ids["6"]},
				{Name: []byte("executable"), Mode: "100755", ObjectType: "blob", ObjectOID: ids["7"]},
				{Name: []byte("binary"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["8"]},
				{Name: []byte("empty"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["9"]},
				{Name: []byte("one"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["a"]},
				{Name: []byte("lfs"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["b"]},
				{Name: []byte("nul"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["c"]},
				{Name: []byte("*literal"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["d"]},
				{Name: arbitraryPath, Mode: "100644", ObjectType: "blob", ObjectOID: ids["e"]},
				{Name: longPath, Mode: "100644", ObjectType: "blob", ObjectOID: ids["f"]},
				{Name: []byte("dir"), Mode: "040000", ObjectType: "tree", ObjectOID: dirTree},
			},
			dirTree: {{Name: []byte("file"), Mode: "100644", ObjectType: "blob", ObjectOID: ids["4"]}},
		},
		blobs: map[string][]byte{
			ids["4"]: []byte("needle"), ids["5"]: []byte("needle"), ids["7"]: []byte("needle"),
			ids["8"]: append([]byte("needle"), 0xff), ids["9"]: {}, ids["a"]: []byte("x"),
			ids["b"]: []byte("version https://git-lfs.github.com/spec/v1\noid sha256:needle\n"),
			ids["c"]: {'n', 'e', 'e', 'd', 'l', 'e', 0}, ids["d"]: []byte("needle"),
			ids["e"]: []byte("needle"), ids["f"]: []byte("needle"),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1))
	request := searchRequest(SearchModeByteLiteral, []byte("needle"))
	result, err := service.Search(context.Background(), request)
	if err != nil || result.Completion != SearchCompletionComplete || result.ExaminedObjects != 12 {
		t.Fatalf("byte result=%#v err=%v", result, err)
	}
	for _, path := range [][]byte{[]byte("binary"), []byte("dir/file"), []byte("executable"), []byte("lfs"), []byte("nul"), []byte("regular"), []byte("symlink"), []byte("*literal"), arbitraryPath, longPath} {
		if !containsMatchPath(result.Matches, path) {
			t.Fatalf("missing byte path=%x", path)
		}
	}
	if containsMatchPath(result.Matches, []byte("gitlink")) || containsMatchPath(result.Matches, []byte("empty")) {
		t.Fatal("non-blob candidate entered byte results")
	}

	text, err := service.Search(context.Background(), searchRequest(SearchModeTextLiteral, []byte("needle")))
	if err != nil || text.Completion != SearchCompletionComplete {
		t.Fatalf("text result=%#v err=%v", text, err)
	}
	if containsMatchPath(text.Matches, []byte("binary")) || !containsMatchPath(text.Matches, []byte("nul")) || !containsMatchPath(text.Matches, []byte("lfs")) {
		t.Fatalf("text corpus filtering=%#v", text.Matches)
	}

	cases := []struct {
		name   string
		prefix []PathReference
		want   []byte
		count  int
	}{
		{"exact file", []PathReference{searchPathReference([]byte("dir/file"))}, []byte("dir/file"), 1},
		{"directory", []PathReference{searchPathReference([]byte("dir"))}, []byte("dir/file"), 1},
		{"wildcard literal", []PathReference{searchPathReference([]byte("*literal"))}, []byte("*literal"), 1},
		{"arbitrary", []PathReference{searchPathReference(arbitraryPath)}, arbitraryPath, 1},
		{"long selector", nil, longPath, 1},
		{"absent", []PathReference{searchPathReference([]byte("missing"))}, nil, 0},
	}
	longIdentity, err := service.makePathIdentity(context.Background(), fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1), longPath)
	if err != nil {
		t.Fatal(err)
	}
	cases[4].prefix = []PathReference{referenceFromIdentity(longIdentity)}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := searchRequest(SearchModeByteLiteral, []byte("needle"))
			request.Prefixes = test.prefix
			result, err := service.Search(context.Background(), request)
			if err != nil || result.Completion != SearchCompletionComplete || len(result.Matches) != test.count {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if test.count == 1 && result.Matches[0].Path.PathID != pathID(test.want) {
				t.Fatalf("path=%#v want=%x", result.Matches[0].Path, test.want)
			}
		})
	}
}

func TestSearchExactAccountingAndMultibyteScanBoundary(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	validOID := strings.Repeat("3", 40)
	invalidOID := strings.Repeat("4", 40)
	emptyOID := strings.Repeat("5", 40)
	boundaryOID := strings.Repeat("6", 40)
	boundaryPrefix := bytes.Repeat([]byte{'a'}, int(textValidationChunkBytes)-1)
	boundaryContent := append(append([]byte(nil), boundaryPrefix...), []byte("éneedle")...)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("empty"), Mode: "100644", ObjectType: "blob", ObjectOID: emptyOID},
			{Name: []byte("invalid"), Mode: "100644", ObjectType: "blob", ObjectOID: invalidOID},
			{Name: []byte("valid"), Mode: "100644", ObjectType: "blob", ObjectOID: validOID},
			{Name: []byte("boundary"), Mode: "100644", ObjectType: "blob", ObjectOID: boundaryOID},
		}},
		blobs: map[string][]byte{validOID: []byte("abcabc"), invalidOID: {'a', 0xff, 'a'}, emptyOID: {}, boundaryOID: boundaryContent},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1))
	byteRequest := searchRequest(SearchModeByteLiteral, []byte("bc"))
	byteRequest.Prefixes = []PathReference{searchPathReference([]byte("valid"))}
	byteResult, err := service.Search(context.Background(), byteRequest)
	if err != nil || byteResult.ExaminedObjects != 1 || byteResult.ExaminedBytes != 11 || len(byteResult.Matches) != 2 {
		t.Fatalf("byte accounting=%#v err=%v", byteResult, err)
	}
	textRequest := searchRequest(SearchModeTextLiteral, []byte("bc"))
	textRequest.Prefixes = byteRequest.Prefixes
	textResult, err := service.Search(context.Background(), textRequest)
	if err != nil || textResult.ExaminedObjects != 1 || textResult.ExaminedBytes != 17 || len(textResult.Matches) != 2 {
		t.Fatalf("text accounting=%#v err=%v", textResult, err)
	}
	invalidRequest := searchRequest(SearchModeTextLiteral, []byte("a"))
	invalidRequest.Prefixes = []PathReference{searchPathReference([]byte("invalid"))}
	invalidResult, err := service.Search(context.Background(), invalidRequest)
	if err != nil || invalidResult.ExaminedBytes != 3 || len(invalidResult.Matches) != 0 {
		t.Fatalf("invalid accounting=%#v err=%v", invalidResult, err)
	}
	emptyRequest := searchRequest(SearchModeByteLiteral, []byte("x"))
	emptyRequest.Prefixes = []PathReference{searchPathReference([]byte("empty"))}
	emptyResult, err := service.Search(context.Background(), emptyRequest)
	if err != nil || emptyResult.ExaminedBytes != 0 || emptyResult.Completion != SearchCompletionComplete {
		t.Fatalf("empty accounting=%#v err=%v", emptyResult, err)
	}
	boundaryRequest := searchRequest(SearchModeTextLiteral, []byte("éneedle"))
	boundaryRequest.Prefixes = []PathReference{searchPathReference([]byte("boundary"))}
	boundaryResult, err := service.Search(context.Background(), boundaryRequest)
	if err != nil || len(boundaryResult.Matches) != 1 || boundaryResult.Matches[0].ByteOffset != int64(len(boundaryPrefix)) {
		t.Fatalf("boundary result=%#v err=%v", boundaryResult, err)
	}
}

func TestSearchObservedObjectSizeMustRemainStableAcrossReadsAndResume(t *testing.T) {
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(strings.Repeat("1", 40), rootTree, "", 1)
	base := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	calls := 0
	changing := &searchVaultWrapper{base: base, blob: func(ctx context.Context, request sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
		calls++
		value, err := base.ReadRetainedBlobRange(ctx, request)
		if calls > 1 {
			value.TotalSize++
		}
		return value, err
	}}
	if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, changing, nil).Search(context.Background(), searchRequest(SearchModeByteLiteral, []byte("a"))); ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("within-call error=%v", err)
	}
	calls = 0
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, changing, nil)
	request := searchRequest(SearchModeByteLiteral, []byte("a"))
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	request.Cursor = first.Cursor
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("resume error=%v", err)
	}
}

func containsMatchPath(matches []SearchMatch, path []byte) bool {
	want := pathID(path)
	for _, match := range matches {
		if match.Path.PathID == want {
			return true
		}
	}
	return false
}
