package sourcegateway

import (
	"bytes"
	"context"
	"database/sql"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"relay/internal/app/operations"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func TestSearchByteLiteralReturnsOverlapsAcrossPageSizeChanges(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	dirTree := strings.Repeat("3", 40)
	aBlob := strings.Repeat("4", 40)
	zBlob := strings.Repeat("5", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("dir"), Mode: "040000", ObjectType: "tree", ObjectOID: dirTree},
				{Name: []byte("z.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: zBlob},
			},
			dirTree: {
				{Name: []byte("a.txt"), Mode: "100755", ObjectType: "blob", ObjectOID: aBlob},
			},
		},
		blobs: map[string][]byte{aBlob: []byte("aaaa"), zBlob: []byte("aa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	prefix := []byte("dir")
	exact := []byte("dir/a.txt")
	request := SearchRequest{
		PacketID:        "opkt-fidelity",
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		RepositoryKey:   "relay",
		Mode:            SearchModeByteLiteral,
		ByteLiteral:     []byte("aa"),
		Prefixes: []PathReference{
			{PathID: pathID(prefix), InlineBase64: canonicalInline(prefix)},
			{PathID: pathID(exact), InlineBase64: canonicalInline(exact)},
		},
		Limit:  1,
		Budget: SearchBudget{ExaminedObjects: 4, ExaminedBytes: 64},
	}
	var matches []SearchMatch
	for page := 0; ; page++ {
		result, err := service.Search(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			if result.Cursor != "" {
				t.Fatal("complete search returned a cursor")
			}
			break
		}
		if result.Completion != SearchCompletionPageIncomplete || result.Cursor == "" {
			t.Fatalf("page %d completion=%q cursor=%q", page, result.Completion, result.Cursor)
		}
		request.Cursor = result.Cursor
		request.Limit = 2
	}
	if len(matches) != 3 {
		t.Fatalf("matches = %#v", matches)
	}
	for index, match := range matches {
		if match.ByteOffset != int64(index) || match.OccurrenceOrdinal != int64(index) || match.Path.Display != "dir/a.txt" || match.FileMode != "100755" || match.MatchLength != 2 || len(match.MatchID) != 64 {
			t.Fatalf("match[%d] = %#v", index, match)
		}
	}
}

func TestSearchTextLiteralExcludesInvalidUTF8AndNUL(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	badBlob := strings.Repeat("3", 40)
	truncatedBlob := strings.Repeat("4", 40)
	goodBlob := strings.Repeat("5", 40)
	nulPrefixBlob := strings.Repeat("6", 40)
	nulMiddleBlob := strings.Repeat("7", 40)
	nulSuffixBlob := strings.Repeat("8", 40)
	nulMultibyteBlob := strings.Repeat("9", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("bad.bin"), Mode: "100644", ObjectType: "blob", ObjectOID: badBlob},
			{Name: []byte("good.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: goodBlob},
			{Name: []byte("nul-middle.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: nulMiddleBlob},
			{Name: []byte("nul-multibyte.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: nulMultibyteBlob},
			{Name: []byte("nul-prefix.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: nulPrefixBlob},
			{Name: []byte("nul-suffix.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: nulSuffixBlob},
			{Name: []byte("truncated.bin"), Mode: "100644", ObjectType: "blob", ObjectOID: truncatedBlob},
		}},
		blobs: map[string][]byte{
			badBlob:          {'x', 0xff, 'x'},
			truncatedBlob:    {'x', 0xe2, 0x82},
			goodBlob:         []byte("x\u00e9x"),
			nulPrefixBlob:    {0, 'x'},
			nulMiddleBlob:    {'x', 0, 'x'},
			nulSuffixBlob:    {'x', 0},
			nulMultibyteBlob: []byte("\u00e9\x00\u00e9"),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	result, err := service.Search(context.Background(), SearchRequest{
		PacketID:        "opkt-fidelity",
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		RepositoryKey:   "relay",
		Mode:            SearchModeTextLiteral,
		TextLiteral:     "x",
		Limit:           8,
		Budget:          SearchBudget{ExaminedObjects: 8, ExaminedBytes: 256},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion != SearchCompletionComplete || len(result.Matches) != 2 || result.Matches[0].Path.Display != "good.txt" || result.Matches[0].ByteOffset != 0 || result.Matches[1].ByteOffset != 3 || result.ExaminedObjects != 7 {
		t.Fatalf("result = %#v", result)
	}
	for _, excluded := range [][]byte{[]byte("bad.bin"), []byte("truncated.bin"), []byte("nul-prefix.txt"), []byte("nul-middle.txt"), []byte("nul-suffix.txt"), []byte("nul-multibyte.txt")} {
		if containsMatchPath(result.Matches, excluded) {
			t.Fatalf("text-ineligible path entered text results: %v %#v", excluded, result.Matches)
		}
	}

	bytesResult, err := service.Search(context.Background(), byteSearchRequest([]byte{0}))
	if err != nil || bytesResult.Completion != SearchCompletionComplete || len(bytesResult.Matches) != 4 {
		t.Fatalf("byte result = %#v err=%v", bytesResult, err)
	}
	wantOffsets := map[string]int64{
		pathID([]byte("nul-prefix.txt")):    0,
		pathID([]byte("nul-middle.txt")):    1,
		pathID([]byte("nul-suffix.txt")):    1,
		pathID([]byte("nul-multibyte.txt")): 2,
	}
	for _, match := range bytesResult.Matches {
		want, ok := wantOffsets[match.Path.PathID]
		if !ok || match.ByteOffset != want || match.MatchLength != 1 {
			t.Fatalf("unexpected NUL byte match = %#v", match)
		}
		delete(wantOffsets, match.Path.PathID)
	}
	if len(wantOffsets) != 0 {
		t.Fatalf("missing NUL byte paths = %#v", wantOffsets)
	}
}

func TestSearchBudgetContinuationCanReturnZeroMatches(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID},
			},
		},
		blobs: map[string][]byte{blobOID: []byte("zzzz")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	request := SearchRequest{
		PacketID:        "opkt-fidelity",
		SurfaceContract: "planner-authoring.v1",
		OperationID:     "planner.requirements",
		RepositoryKey:   "relay",
		Mode:            SearchModeByteLiteral,
		ByteLiteral:     []byte("a"),
		Limit:           8,
		Budget:          SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4},
	}
	first, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Completion != SearchCompletionBudgetIncomplete || len(first.Matches) != 0 || !first.ByteBudgetExhausted || first.Cursor == "" || first.ExaminedBytes != 4 {
		t.Fatalf("first = %#v", first)
	}
	request.Cursor = first.Cursor
	second, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Completion != SearchCompletionComplete || len(second.Matches) != 0 || second.Cursor != "" {
		t.Fatalf("second = %#v", second)
	}
}

func TestSearchObjectBudgetStopsBeforeNextCandidateWithoutFalseByteExhaustion(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	firstBlob := strings.Repeat("3", 40)
	secondBlob := strings.Repeat("4", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: firstBlob},
			{Name: []byte("b.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: secondBlob},
		}},
		blobs: map[string][]byte{firstBlob: []byte("z"), secondBlob: []byte("x")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	request := SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 8, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 8}}
	first, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Completion != SearchCompletionBudgetIncomplete || !first.ObjectBudgetExhausted || first.ByteBudgetExhausted || first.Cursor == "" || len(first.Matches) != 0 || first.ExaminedObjects != 1 {
		t.Fatalf("first = %#v", first)
	}
	request.Cursor = first.Cursor
	second, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Completion != SearchCompletionComplete || len(second.Matches) != 1 || second.Matches[0].Path.Display != "b.txt" || second.Matches[0].ByteOffset != 0 {
		t.Fatalf("second = %#v", second)
	}
}

func TestSearchCursorRejectsQueryAndCoordinateChanges(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID},
			},
		},
		blobs: map[string][]byte{blobOID: []byte("aaaa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	request := SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeByteLiteral, ByteLiteral: []byte("aa"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 2, ExaminedBytes: 32}}
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	request.Cursor = first.Cursor
	request.ByteLiteral = []byte("ab")
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("query mismatch error = %v", err)
	}
	request.ByteLiteral = []byte("aa")
	value, err := service.cursors.Decode(first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	value.NextOffset--
	tampered, err := service.cursors.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = tampered
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("coordinate mismatch error = %v", err)
	}
}

func TestSearchRejectsDuplicatePrefixesAndNonProgressingBudgets(t *testing.T) {
	path := []byte("dir")
	request := SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("needle"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 5}}
	if _, err := validateSearchRequest(request); ErrorCode(err) != CodeInvalidRange {
		t.Fatalf("small budget error = %v", err)
	}
	request.Budget.ExaminedBytes = 8
	request.Prefixes = make([]PathReference, MaxTreePageEntries+1)
	if _, err := validateSearchRequest(request); ErrorCode(err) != CodeInvalidRange {
		t.Fatalf("prefix count error = %v", err)
	}
	request.Prefixes = nil
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	service := newFidelityService(t, &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {}}, blobs: map[string][]byte{}, nodes: map[string]sourcevault.RetainedCommitNode{}}, fidelityAuthority(commitOID, rootTree, "", 1))
	request.PacketID = "opkt-fidelity"
	request.SurfaceContract = "planner-authoring.v1"
	request.OperationID = "planner.requirements"
	request.RepositoryKey = "relay"
	request.Budget.ExaminedBytes = 8
	request.Prefixes = []PathReference{
		{PathID: pathID(path), InlineBase64: canonicalInline(path)},
		{PathID: pathID(path), InlineBase64: canonicalInline(path)},
	}
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidRequest {
		t.Fatalf("duplicate prefix error = %v", err)
	}
}

func TestSearchConcurrentRequestsAreIndependent(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID},
			},
		},
		blobs: map[string][]byte{blobOID: []byte("aaaa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	request := SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeByteLiteral, ByteLiteral: []byte("aa"), Limit: 8, Budget: SearchBudget{ExaminedObjects: 4, ExaminedBytes: 64}}
	const workers = 8
	results := make([]SearchResult, workers)
	errors := make([]error, workers)
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
		if errors[index] != nil {
			t.Fatalf("worker %d: %v", index, errors[index])
		}
		if results[index].Completion != SearchCompletionComplete || len(results[index].Matches) != 3 {
			t.Fatalf("worker %d result = %#v", index, results[index])
		}
		for matchIndex := range results[0].Matches {
			if results[index].Matches[matchIndex].MatchID != results[0].Matches[matchIndex].MatchID {
				t.Fatalf("worker %d match %d identity changed", index, matchIndex)
			}
		}
	}
}

func TestSearchUsesRetainedGitAfterSourceRepositoryRemoval(t *testing.T) {
	ctx := context.Background()
	repo := newSearchGitRepository(t)
	if err := os.MkdirAll(filepath.Join(repo, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "nested", "text.txt"), []byte("alpha alpha\n"), 0o600); err != nil {
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
		_, createErr := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{RepoTarget: "relay", LocalPath: repo, ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true}})
		return createErr
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

	text, err := service.Search(ctx, SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeTextLiteral, TextLiteral: "alpha", Limit: 8, Budget: SearchBudget{ExaminedObjects: 8, ExaminedBytes: 256}})
	if err != nil {
		t.Fatal(err)
	}
	if text.Completion != SearchCompletionComplete || len(text.Matches) != 2 || text.Matches[0].Path.Display != "nested/text.txt" || text.Matches[0].ByteOffset != 0 || text.Matches[1].ByteOffset != 6 {
		t.Fatalf("text search = %#v", text)
	}
	binary, err := service.Search(ctx, SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeByteLiteral, ByteLiteral: []byte{0xff, 0}, Limit: 8, Budget: SearchBudget{ExaminedObjects: 8, ExaminedBytes: 256}})
	if err != nil {
		t.Fatal(err)
	}
	if binary.Completion != SearchCompletionComplete || len(binary.Matches) != 2 || binary.Matches[0].Path.Display != "raw.bin" || binary.Matches[0].ByteOffset != 0 || binary.Matches[1].ByteOffset != 2 {
		t.Fatalf("byte search = %#v", binary)
	}
}

func newSearchGitRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "init")
	runSearchGit(t, repo, "config", "user.name", "Relay Search Tests")
	runSearchGit(t, repo, "config", "user.email", "relay-search@example.test")
	runSearchGit(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
	return repo
}

func runSearchGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	command := exec.Command("git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(commandArgs, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestSearchRequestContractIsClosedAndBounded(t *testing.T) {
	valid := []SearchRequest{
		{Mode: SearchModeTextLiteral, TextLiteral: "ascii", Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 8}},
		{Mode: SearchModeTextLiteral, TextLiteral: "\u00e9", Limit: MaxSearchPageMatches, Budget: SearchBudget{ExaminedObjects: math.MaxInt64, ExaminedBytes: math.MaxInt64}},
		{Mode: SearchModeByteLiteral, ByteLiteral: []byte{0, 0xff}, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}},
	}
	for index, request := range valid {
		literal, err := validateSearchRequest(request)
		if err != nil || len(literal) == 0 {
			t.Fatalf("valid[%d] literal=%v err=%v", index, literal, err)
		}
	}

	oversized := bytes.Repeat([]byte{'x'}, int(MaxSearchLiteralBytes)+1)
	tooManyPrefixes := make([]PathReference, MaxTreePageEntries+1)
	cases := []struct {
		name string
		req  SearchRequest
		code string
	}{
		{name: "empty text", req: SearchRequest{Mode: SearchModeTextLiteral, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRequest},
		{name: "empty bytes", req: SearchRequest{Mode: SearchModeByteLiteral, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRequest},
		{name: "both forms", req: SearchRequest{Mode: SearchModeTextLiteral, TextLiteral: "x", ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRequest},
		{name: "invalid text", req: SearchRequest{Mode: SearchModeTextLiteral, TextLiteral: string([]byte{0xff}), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRequest},
		{name: "unsupported mode", req: SearchRequest{Mode: "regex", TextLiteral: "x", Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRequest},
		{name: "oversized literal", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: oversized, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: int64(len(oversized))}}, code: CodeInvalidRange},
		{name: "zero page", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRange},
		{name: "oversized page", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: MaxSearchPageMatches + 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRange},
		{name: "zero objects", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedBytes: 4}}, code: CodeInvalidRange},
		{name: "negative objects", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: -1, ExaminedBytes: 4}}, code: CodeInvalidRange},
		{name: "zero bytes", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1}}, code: CodeInvalidRange},
		{name: "negative bytes", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: -1}}, code: CodeInvalidRange},
		{name: "nonprogressing text budget", req: SearchRequest{Mode: SearchModeTextLiteral, TextLiteral: "x", Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 3}}, code: CodeInvalidRange},
		{name: "nonprogressing byte budget", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("needle"), Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 5}}, code: CodeInvalidRange},
		{name: "too many prefixes", req: SearchRequest{Mode: SearchModeByteLiteral, ByteLiteral: []byte("x"), Prefixes: tooManyPrefixes, Limit: 1, Budget: SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}}, code: CodeInvalidRange},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateSearchRequest(test.req); ErrorCode(err) != test.code {
				t.Fatalf("error=%v code=%q want=%q", err, ErrorCode(err), test.code)
			}
		})
	}
}

func TestSearchPrefixSemanticsCoverRootExactDirectoryLiteralAndSelectors(t *testing.T) {
	componentCases := []struct {
		name   string
		path   []byte
		prefix []byte
		want   bool
	}{
		{name: "root", path: []byte("dir/file"), prefix: nil, want: true},
		{name: "exact file", path: []byte("dir/file"), prefix: []byte("dir/file"), want: true},
		{name: "directory", path: []byte("dir/file"), prefix: []byte("dir"), want: true},
		{name: "component not string prefix", path: []byte("directory/file"), prefix: []byte("dir"), want: false},
		{name: "nonexistent", path: []byte("dir/file"), prefix: []byte("missing"), want: false},
		{name: "wildcard literal", path: []byte("*literal/file"), prefix: []byte("*literal"), want: true},
		{name: "backslash literal", path: []byte(`dir\\name/file`), prefix: []byte(`dir\\name`), want: true},
		{name: "arbitrary bytes", path: []byte{0xff, 'x', '/', 'y'}, prefix: []byte{0xff, 'x'}, want: true},
	}
	for _, test := range componentCases {
		t.Run(test.name, func(t *testing.T) {
			if got := pathHasComponentPrefix(test.path, test.prefix); got != test.want {
				t.Fatalf("pathHasComponentPrefix(%v,%v)=%v want=%v", test.path, test.prefix, got, test.want)
			}
		})
	}

	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	dirTree := strings.Repeat("3", 40)
	longBlob := strings.Repeat("4", 40)
	arbitraryBlob := strings.Repeat("5", 40)
	wildBlob := strings.Repeat("6", 40)
	backslashBlob := strings.Repeat("7", 40)
	fileBlob := strings.Repeat("8", 40)
	longPath := bytes.Repeat([]byte{'l'}, MaxInlinePathBytes+1)
	arbitraryPath := []byte{0xff, 'x'}
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: {
				{Name: []byte("dir"), Mode: "040000", ObjectType: "tree", ObjectOID: dirTree},
				{Name: []byte("*literal"), Mode: "100644", ObjectType: "blob", ObjectOID: wildBlob},
				{Name: []byte(`dir\\name`), Mode: "100644", ObjectType: "blob", ObjectOID: backslashBlob},
				{Name: arbitraryPath, Mode: "100644", ObjectType: "blob", ObjectOID: arbitraryBlob},
				{Name: longPath, Mode: "100644", ObjectType: "blob", ObjectOID: longBlob},
			},
			dirTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("file.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: fileBlob}},
		},
		blobs: map[string][]byte{
			longBlob:      []byte("needle"),
			arbitraryBlob: []byte("needle"),
			wildBlob:      []byte("needle"),
			backslashBlob: []byte("needle"),
			fileBlob:      []byte("needle"),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	selectors := &fidelitySelectorFake{values: map[string]workflowstore.SourcePathSelector{}}
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, selectors)
	identity, err := service.makePathIdentity(context.Background(), authority, longPath)
	if err != nil || identity.SelectorID == "" || identity.InlineBase64 != "" {
		t.Fatalf("long identity=%#v err=%v", identity, err)
	}

	cases := []struct {
		name      string
		prefixes  []PathReference
		wantPath  []byte
		wantCount int
	}{
		{name: "implicit root", wantPath: []byte("*literal"), wantCount: 5},
		{name: "explicit root", prefixes: []PathReference{PathReference{}}, wantPath: []byte("*literal"), wantCount: 5},
		{name: "exact file", prefixes: []PathReference{pathReference([]byte("dir/file.txt"))}, wantPath: []byte("dir/file.txt"), wantCount: 1},
		{name: "directory", prefixes: []PathReference{pathReference([]byte("dir"))}, wantPath: []byte("dir/file.txt"), wantCount: 1},
		{name: "nonexistent", prefixes: []PathReference{pathReference([]byte("missing"))}, wantCount: 0},
		{name: "wildcard literal", prefixes: []PathReference{pathReference([]byte("*literal"))}, wantPath: []byte("*literal"), wantCount: 1},
		{name: "backslash literal", prefixes: []PathReference{pathReference([]byte(`dir\\name`))}, wantPath: []byte(`dir\\name`), wantCount: 1},
		{name: "arbitrary byte", prefixes: []PathReference{pathReference(arbitraryPath)}, wantPath: arbitraryPath, wantCount: 1},
		{name: "long selector", prefixes: []PathReference{referenceFromIdentity(identity)}, wantPath: longPath, wantCount: 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := byteSearchRequest([]byte("needle"))
			request.Prefixes = test.prefixes
			result, err := service.Search(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Completion != SearchCompletionComplete || result.Cursor != "" {
				t.Fatalf("result=%#v", result)
			}
			if len(result.Matches) != test.wantCount {
				t.Fatalf("matches=%d want=%d %#v", len(result.Matches), test.wantCount, result.Matches)
			}
			if test.wantCount > 0 && result.Matches[0].Path.PathID != pathID(test.wantPath) {
				t.Fatalf("first match=%#v want path=%v", result.Matches[0], test.wantPath)
			}
		})
	}

	request := byteSearchRequest([]byte("needle"))
	request.Prefixes = []PathReference{PathReference{}, PathReference{}}
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidRequest {
		t.Fatalf("duplicate root prefix error=%v", err)
	}

	other := authority
	other.Summary.ProjectID = "other-project"
	otherService := newSearchService(t, searchAuthorityResolver{authority: other}, vault, selectors)
	request.Prefixes = []PathReference{referenceFromIdentity(identity)}
	if _, err := otherService.Search(context.Background(), request); ErrorCode(err) != CodeInvalidSelector {
		t.Fatalf("cross-authority selector error=%v", err)
	}
}

func TestSearchCorpusCoversBlobModesTypesPathsAndStableOrdering(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	deepTree := strings.Repeat("3", 40)
	longPath := bytes.Repeat([]byte{'z'}, MaxInlinePathBytes+1)
	arbitraryPath := []byte{0xfe, 'p'}
	entries := []sourcevault.RetainedTreeEntry{
		{Name: []byte("symlink"), Mode: "120000", ObjectType: "blob", ObjectOID: strings.Repeat("4", 40)},
		{Name: []byte("regular"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("5", 40)},
		{Name: []byte("gitlink"), Mode: "160000", ObjectType: "commit", ObjectOID: strings.Repeat("6", 40)},
		{Name: []byte("executable"), Mode: "100755", ObjectType: "blob", ObjectOID: strings.Repeat("7", 40)},
		{Name: []byte("binary"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("8", 40)},
		{Name: []byte("empty"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("9", 40)},
		{Name: []byte("one"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("a", 40)},
		{Name: []byte("lfs"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("b", 40)},
		{Name: []byte("nul-text"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("c", 40)},
		{Name: arbitraryPath, Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("d", 40)},
		{Name: longPath, Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("e", 40)},
		{Name: []byte("deep"), Mode: "040000", ObjectType: "tree", ObjectOID: deepTree},
	}
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{
			rootTree: entries,
			deepTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("nested"), Mode: "100644", ObjectType: "blob", ObjectOID: strings.Repeat("f", 40)}},
		},
		blobs: map[string][]byte{
			strings.Repeat("4", 40): []byte("needle"),
			strings.Repeat("5", 40): []byte("needle"),
			strings.Repeat("7", 40): []byte("needle"),
			strings.Repeat("8", 40): append([]byte("needle"), 0xff),
			strings.Repeat("9", 40): {},
			strings.Repeat("a", 40): []byte("x"),
			strings.Repeat("b", 40): []byte("version https://git-lfs.github.com/spec/v1\noid sha256:needle\n"),
			strings.Repeat("c", 40): []byte{'n', 'e', 'e', 'd', 'l', 'e', 0},
			strings.Repeat("d", 40): []byte("needle"),
			strings.Repeat("e", 40): []byte("needle"),
			strings.Repeat("f", 40): []byte("needle"),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	selectors := &fidelitySelectorFake{values: map[string]workflowstore.SourcePathSelector{}}
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, selectors)
	request := byteSearchRequest([]byte("needle"))
	result, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion != SearchCompletionComplete || result.ExaminedObjects != 11 {
		t.Fatalf("result=%#v", result)
	}
	wantPaths := [][]byte{[]byte("binary"), []byte("deep/nested"), []byte("executable"), []byte("lfs"), []byte("nul-text"), []byte("regular"), []byte("symlink"), arbitraryPath, longPath}
	sort.Slice(wantPaths, func(i, j int) bool { return bytes.Compare(wantPaths[i], wantPaths[j]) < 0 })
	if len(result.Matches) != len(wantPaths) {
		t.Fatalf("matches=%d want=%d %#v", len(result.Matches), len(wantPaths), result.Matches)
	}
	for index, want := range wantPaths {
		if result.Matches[index].Path.PathID != pathID(want) {
			t.Fatalf("match[%d]=%#v want path=%v", index, result.Matches[index], want)
		}
	}
	if result.Matches[0].Path.PathID == pathID([]byte("gitlink")) {
		t.Fatal("gitlink entered blob candidate set")
	}

	one := byteSearchRequest([]byte("x"))
	one.Prefixes = []PathReference{pathReference([]byte("one"))}
	oneResult, err := service.Search(context.Background(), one)
	if err != nil || len(oneResult.Matches) != 1 || oneResult.Matches[0].ByteOffset != 0 {
		t.Fatalf("one-byte result=%#v err=%v", oneResult, err)
	}

	text := textSearchRequest("needle")
	textResult, err := service.Search(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range textResult.Matches {
		if match.Path.PathID == pathID([]byte("binary")) || match.Path.PathID == pathID([]byte("nul-text")) {
			t.Fatal("text-ineligible blob entered text results")
		}
	}
}

func TestSearchLiteralBoundariesAndModeOffsetEquivalence(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	boundaryBlob := strings.Repeat("3", 40)
	largeQueryBlob := strings.Repeat("4", 40)
	lineBlob := strings.Repeat("5", 40)
	finalBlob := strings.Repeat("6", 40)
	boundaryPrefix := bytes.Repeat([]byte{'a'}, int(textValidationChunkBytes)-1)
	boundaryContent := append(append([]byte(nil), boundaryPrefix...), []byte("\u00e9needle")...)
	largeQuery := bytes.Repeat([]byte{'q'}, int(textValidationChunkBytes)+1)
	largeContent := append([]byte{'x'}, largeQuery...)
	lineContent := append(bytes.Repeat([]byte{'l'}, int(textValidationChunkBytes)+1024), []byte("needle")...)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("boundary.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: boundaryBlob},
			{Name: []byte("large.bin"), Mode: "100644", ObjectType: "blob", ObjectOID: largeQueryBlob},
			{Name: []byte("line.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: lineBlob},
			{Name: []byte("final.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: finalBlob},
		}},
		blobs: map[string][]byte{
			boundaryBlob:   boundaryContent,
			largeQueryBlob: largeContent,
			lineBlob:       lineContent,
			finalBlob:      []byte("zzneedle"),
		},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))

	text := textSearchRequest("\u00e9needle")
	text.Prefixes = []PathReference{pathReference([]byte("boundary.txt"))}
	text.Budget.ExaminedBytes = 2 << 20
	textResult, err := service.Search(context.Background(), text)
	if err != nil || len(textResult.Matches) != 1 || textResult.Matches[0].ByteOffset != int64(len(boundaryPrefix)) {
		t.Fatalf("boundary text=%#v err=%v", textResult, err)
	}
	queryBytes := []byte("\u00e9needle")
	validationBytes := int64(len(boundaryContent) + 1)
	scanBytes := int64(len(queryBytes))*int64(len(boundaryContent)-len(queryBytes)+1) + int64(len(queryBytes)-1)
	if textResult.ExaminedBytes != validationBytes+scanBytes {
		t.Fatalf("boundary examined bytes=%d want=%d", textResult.ExaminedBytes, validationBytes+scanBytes)
	}
	byteRequest := byteSearchRequest([]byte("\u00e9needle"))
	byteRequest.Prefixes = text.Prefixes
	byteRequest.Budget.ExaminedBytes = 2 << 20
	byteResult, err := service.Search(context.Background(), byteRequest)
	if err != nil || len(byteResult.Matches) != 1 || byteResult.Matches[0].ByteOffset != textResult.Matches[0].ByteOffset {
		t.Fatalf("boundary byte=%#v err=%v", byteResult, err)
	}

	large := byteSearchRequest(largeQuery)
	large.Prefixes = []PathReference{pathReference([]byte("large.bin"))}
	large.Budget.ExaminedBytes = 4 << 20
	largeResult, err := service.Search(context.Background(), large)
	if err != nil || len(largeResult.Matches) != 1 || largeResult.Matches[0].ByteOffset != 1 {
		t.Fatalf("large query=%#v err=%v", largeResult, err)
	}

	line := textSearchRequest("needle")
	line.Prefixes = []PathReference{pathReference([]byte("line.txt"))}
	line.Budget.ExaminedBytes = 4 << 20
	lineResult, err := service.Search(context.Background(), line)
	if err != nil || len(lineResult.Matches) != 1 || lineResult.Matches[0].ByteOffset != int64(len(lineContent)-len("needle")) {
		t.Fatalf("oversized line=%#v err=%v", lineResult, err)
	}

	final := byteSearchRequest([]byte("needle"))
	final.Prefixes = []PathReference{pathReference([]byte("final.txt"))}
	finalResult, err := service.Search(context.Background(), final)
	if err != nil || len(finalResult.Matches) != 1 || finalResult.Matches[0].ByteOffset != 2 {
		t.Fatalf("final match=%#v err=%v", finalResult, err)
	}
}

func TestSearchTextLiteralTreatsLineTerminatorsAsExactBytes(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	contents := map[string][]byte{
		"cr":       []byte("before\rneedle\rafter"),
		"crlf":     []byte("before\r\nneedle\r\nafter"),
		"lf":       []byte("before\nneedle\nafter"),
		"no-final": []byte("before needle"),
	}
	entries := make([]sourcevault.RetainedTreeEntry, 0, len(contents))
	blobs := map[string][]byte{}
	index := 3
	for name, content := range contents {
		oid := strings.Repeat(string(rune('0'+index)), 40)
		index++
		entries = append(entries, sourcevault.RetainedTreeEntry{Name: []byte(name), Mode: "100644", ObjectType: "blob", ObjectOID: oid})
		blobs[oid] = content
	}
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: entries}, blobs: blobs, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	for name, content := range contents {
		t.Run(name, func(t *testing.T) {
			request := textSearchRequest("needle")
			request.Prefixes = []PathReference{pathReference([]byte(name))}
			result, err := service.Search(context.Background(), request)
			wantOffset := int64(bytes.Index(content, []byte("needle")))
			if err != nil || len(result.Matches) != 1 || result.Matches[0].ByteOffset != wantOffset {
				t.Fatalf("result=%#v err=%v wantOffset=%d", result, err, wantOffset)
			}
		})
	}
}

func TestSearchExaminedWorkAccountingIsExact(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	validBlob := strings.Repeat("3", 40)
	invalidBlob := strings.Repeat("4", 40)
	emptyBlob := strings.Repeat("5", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("empty"), Mode: "100644", ObjectType: "blob", ObjectOID: emptyBlob},
			{Name: []byte("invalid"), Mode: "100644", ObjectType: "blob", ObjectOID: invalidBlob},
			{Name: []byte("valid"), Mode: "100644", ObjectType: "blob", ObjectOID: validBlob},
		}},
		blobs: map[string][]byte{validBlob: []byte("abcabc"), invalidBlob: {'a', 0xff, 'a'}, emptyBlob: {}},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))

	byteRequest := byteSearchRequest([]byte("bc"))
	byteRequest.Prefixes = []PathReference{pathReference([]byte("valid"))}
	byteResult, err := service.Search(context.Background(), byteRequest)
	if err != nil || byteResult.ExaminedObjects != 1 || byteResult.ExaminedBytes != 11 || len(byteResult.Matches) != 2 {
		t.Fatalf("byte accounting=%#v err=%v", byteResult, err)
	}

	textRequest := textSearchRequest("bc")
	textRequest.Prefixes = byteRequest.Prefixes
	textResult, err := service.Search(context.Background(), textRequest)
	if err != nil || textResult.ExaminedObjects != 1 || textResult.ExaminedBytes != 17 || len(textResult.Matches) != 2 {
		t.Fatalf("text accounting=%#v err=%v", textResult, err)
	}

	invalid := textSearchRequest("a")
	invalid.Prefixes = []PathReference{pathReference([]byte("invalid"))}
	invalidResult, err := service.Search(context.Background(), invalid)
	if err != nil || invalidResult.ExaminedObjects != 1 || invalidResult.ExaminedBytes != 3 || len(invalidResult.Matches) != 0 || invalidResult.Completion != SearchCompletionComplete {
		t.Fatalf("invalid accounting=%#v err=%v", invalidResult, err)
	}

	empty := byteSearchRequest([]byte("x"))
	empty.Prefixes = []PathReference{pathReference([]byte("empty"))}
	emptyResult, err := service.Search(context.Background(), empty)
	if err != nil || emptyResult.ExaminedObjects != 1 || emptyResult.ExaminedBytes != 0 || len(emptyResult.Matches) != 0 || emptyResult.Completion != SearchCompletionComplete {
		t.Fatalf("empty accounting=%#v err=%v", emptyResult, err)
	}
}

func TestSearchContinuationExhaustionIsCompletePageSizeIndependentAndDuplicateFree(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	firstBlob := strings.Repeat("3", 40)
	secondBlob := strings.Repeat("4", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("b.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: secondBlob},
			{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: firstBlob},
		}},
		blobs: map[string][]byte{firstBlob: []byte("aaaa"), secondBlob: []byte("baaa")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	base := byteSearchRequest([]byte("aa"))
	base.Budget = SearchBudget{ExaminedObjects: 8, ExaminedBytes: 256}
	var reference []string
	for _, limits := range [][]int{[]int{1}, []int{2, 1, 3}, []int{MaxSearchPageMatches}} {
		request := base
		matches, pages := exhaustSearch(t, service, request, limits)
		ids := make([]string, len(matches))
		seen := map[string]bool{}
		for index, match := range matches {
			ids[index] = match.MatchID
			if seen[match.MatchID] {
				t.Fatalf("duplicate match=%#v", match)
			}
			seen[match.MatchID] = true
		}
		if reference == nil {
			reference = ids
		} else if strings.Join(reference, ",") != strings.Join(ids, ",") {
			t.Fatalf("limits=%v ids=%v want=%v", limits, ids, reference)
		}
		if pages[len(pages)-1].Completion != SearchCompletionComplete || pages[len(pages)-1].Cursor != "" {
			t.Fatalf("terminal page=%#v", pages[len(pages)-1])
		}
		for _, page := range pages[:len(pages)-1] {
			if page.Cursor == "" || page.Completion == SearchCompletionComplete {
				t.Fatalf("incomplete page=%#v", page)
			}
		}
	}
}

func TestSearchBudgetContinuationCoversValidationScanAndCandidateBoundaries(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	firstBlob := strings.Repeat("3", 40)
	secondBlob := strings.Repeat("4", 40)
	vault := &fidelityVaultFake{
		trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
			{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: firstBlob},
			{Name: []byte("b.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: secondBlob},
		}},
		blobs: map[string][]byte{firstBlob: []byte("aaaa"), secondBlob: []byte("zzzzx")},
		nodes: map[string]sourcevault.RetainedCommitNode{},
	}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))

	scan := byteSearchRequest([]byte("aa"))
	scan.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 5}
	first, err := service.Search(context.Background(), scan)
	if err != nil || first.Completion != SearchCompletionBudgetIncomplete || !first.ByteBudgetExhausted || first.ObjectBudgetExhausted || len(first.Matches) != 2 || first.ExaminedBytes != 4 || first.Cursor == "" {
		t.Fatalf("scan first=%#v err=%v", first, err)
	}
	scan.Cursor = first.Cursor
	second, err := service.Search(context.Background(), scan)
	if err != nil || second.Completion != SearchCompletionBudgetIncomplete || !second.ObjectBudgetExhausted || second.ByteBudgetExhausted || second.Cursor == "" || second.ExaminedObjects != 1 || second.ExaminedBytes != 3 || len(second.Matches) != 1 || second.Matches[0].ByteOffset != 2 {
		t.Fatalf("scan second=%#v err=%v", second, err)
	}

	withinCandidate := byteSearchRequest([]byte("aa"))
	withinCandidate.Prefixes = []PathReference{pathReference([]byte("a.txt"))}
	withinCandidate.Limit = 1
	withinCandidate.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 32}
	_, withinPages := exhaustSearch(t, service, withinCandidate, []int{1})
	for index, page := range withinPages {
		if page.ExaminedObjects != 1 {
			t.Fatalf("within-candidate page %d examined objects=%d", index, page.ExaminedObjects)
		}
	}

	text := textSearchRequest("x")
	text.Prefixes = []PathReference{pathReference([]byte("b.txt"))}
	text.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 4}
	var textMatches []SearchMatch
	lastOffset := int64(-1)
	lastPhase := searchPhase("")
	for page := 0; page < 16; page++ {
		result, searchErr := service.Search(context.Background(), text)
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		textMatches = append(textMatches, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			if len(textMatches) != 1 || textMatches[0].ByteOffset != 4 {
				t.Fatalf("text matches=%#v", textMatches)
			}
			break
		}
		if result.Cursor == "" || !result.ByteBudgetExhausted {
			t.Fatalf("text page=%#v", result)
		}
		cursor, decodeErr := service.cursors.Decode(result.Cursor)
		if decodeErr != nil {
			t.Fatalf("cursor=%#v err=%v", cursor, decodeErr)
		}
		if cursor.SearchPhase == lastPhase && cursor.NextOffset <= lastOffset {
			t.Fatalf("cursor did not advance within phase: %#v last=%d", cursor, lastOffset)
		}
		if lastPhase == searchPhaseLiteralScan && cursor.SearchPhase != searchPhaseLiteralScan {
			t.Fatalf("cursor phase regressed: %#v", cursor)
		}
		lastPhase = cursor.SearchPhase
		lastOffset = cursor.NextOffset
		text.Cursor = result.Cursor
		if page == 15 {
			t.Fatal("text continuation did not terminate")
		}
	}

	object := byteSearchRequest([]byte("x"))
	object.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 16}
	objectFirst, err := service.Search(context.Background(), object)
	if err != nil || objectFirst.Completion != SearchCompletionBudgetIncomplete || !objectFirst.ObjectBudgetExhausted || objectFirst.ByteBudgetExhausted || len(objectFirst.Matches) != 0 {
		t.Fatalf("object first=%#v err=%v", objectFirst, err)
	}
	object.Cursor = objectFirst.Cursor
	objectSecond, err := service.Search(context.Background(), object)
	if err != nil || objectSecond.Completion != SearchCompletionComplete || len(objectSecond.Matches) != 1 || objectSecond.Matches[0].Path.PathID != pathID([]byte("b.txt")) {
		t.Fatalf("object second=%#v err=%v", objectSecond, err)
	}
}

func TestSearchCompleteEmptyIsDistinctFromIncompleteNoMatch(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("zzzz")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	complete := byteSearchRequest([]byte("x"))
	completeResult, err := service.Search(context.Background(), complete)
	if err != nil || completeResult.Completion != SearchCompletionComplete || completeResult.Cursor != "" || len(completeResult.Matches) != 0 {
		t.Fatalf("complete=%#v err=%v", completeResult, err)
	}
	incomplete := complete
	incomplete.Budget.ExaminedBytes = 4
	incompleteResult, err := service.Search(context.Background(), incomplete)
	if err != nil || incompleteResult.Completion != SearchCompletionBudgetIncomplete || incompleteResult.Cursor == "" || len(incompleteResult.Matches) != 0 || !incompleteResult.ByteBudgetExhausted {
		t.Fatalf("incomplete=%#v err=%v", incompleteResult, err)
	}
}

func TestSearchCursorBindsEveryAuthorityDimension(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	baseAuthority := fidelityAuthority(commitOID, rootTree, "", 1)
	baseService := newSearchService(t, searchAuthorityResolver{authority: baseAuthority}, vault, nil)
	baseRequest := byteSearchRequest([]byte("aa"))
	baseRequest.Limit = 1
	first, err := baseService.Search(context.Background(), baseRequest)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}

	cases := []struct {
		name   string
		mutate func(*operations.SourceReadAuthority, *SearchRequest)
	}{
		{name: "packet", mutate: func(a *operations.SourceReadAuthority, r *SearchRequest) {
			a.Summary.PacketID = "opkt-other"
			r.PacketID = "opkt-other"
		}},
		{name: "packet digest", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) {
			a.Summary.PacketSHA256 = strings.Repeat("b", 64)
		}},
		{name: "route", mutate: func(a *operations.SourceReadAuthority, r *SearchRequest) {
			a.Summary.SurfaceContract = "other-surface.v1"
			r.SurfaceContract = "other-surface.v1"
		}},
		{name: "operation", mutate: func(a *operations.SourceReadAuthority, r *SearchRequest) {
			a.Summary.OperationID = "planner.other"
			r.OperationID = "planner.other"
		}},
		{name: "project", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) { a.Summary.ProjectID = "other-project" }},
		{name: "repository", mutate: func(a *operations.SourceReadAuthority, r *SearchRequest) {
			a.RepositoryKey = "other"
			a.DependencyKey = "repository:other:primary"
			a.Relationship.DependencyKey = a.DependencyKey
			r.RepositoryKey = "other"
		}},
		{name: "anchor revision", mutate: func(a *operations.SourceReadAuthority, r *SearchRequest) {
			a.AnchorName = "baseline"
			a.DependencyKey = "repository:relay:anchor:baseline"
			a.Relationship.DependencyKey = a.DependencyKey
			r.Revision.AnchorName = "baseline"
		}},
		{name: "publication", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) {
			a.PublicationID = "publication-other"
			a.Relationship.PublicationID = a.PublicationID
		}},
		{name: "relationship", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) { a.Relationship.ID = 2 }},
		{name: "commit", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) {
			a.Relationship.CommitOID = strings.Repeat("4", 40)
		}},
		{name: "tree", mutate: func(a *operations.SourceReadAuthority, _ *SearchRequest) {
			a.Relationship.TreeOID = strings.Repeat("5", 40)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			authority := baseAuthority
			request := baseRequest
			request.Cursor = first.Cursor
			test.mutate(&authority, &request)
			service := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil)
			if _, searchErr := service.Search(context.Background(), request); ErrorCode(searchErr) != CodeInvalidCursor {
				t.Fatalf("error=%v", searchErr)
			}
		})
	}
}

func TestSearchCursorBindsQueryFiltersBudgetsOrderingAndNotPageSize(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil)
	request := byteSearchRequest([]byte("aa"))
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}

	pageSize := request
	pageSize.Cursor = first.Cursor
	pageSize.Limit = 2
	if _, err := service.Search(context.Background(), pageSize); err != nil {
		t.Fatalf("page-size-only resume failed: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*SearchRequest)
	}{
		{name: "query", mutate: func(r *SearchRequest) { r.ByteLiteral = []byte("ab") }},
		{name: "mode", mutate: func(r *SearchRequest) { r.Mode = SearchModeTextLiteral; r.TextLiteral = "aa"; r.ByteLiteral = nil }},
		{name: "prefix", mutate: func(r *SearchRequest) { r.Prefixes = []PathReference{pathReference([]byte("a"))} }},
		{name: "object budget", mutate: func(r *SearchRequest) { r.Budget.ExaminedObjects++ }},
		{name: "byte budget", mutate: func(r *SearchRequest) { r.Budget.ExaminedBytes++ }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := request
			changed.Cursor = first.Cursor
			test.mutate(&changed)
			if _, searchErr := service.Search(context.Background(), changed); ErrorCode(searchErr) != CodeInvalidCursor {
				t.Fatalf("error=%v", searchErr)
			}
		})
	}

	prefixes, err := service.canonicalSearchPrefixes(context.Background(), authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	literal := []byte("aa")
	current := searchFingerprint(authority, SearchModeByteLiteral, literal, prefixes, request.Budget)
	parts := append([]string{}, revisionFingerprint(authority)...)
	parts = append(parts, "search", "relay.source-search-order.v2", string(SearchModeByteLiteral), string(literal), "", "8", "256")
	if current == requestFingerprint(parts...) {
		t.Fatal("ordering version is absent from the search fingerprint")
	}
}

func TestSearchCursorRejectsTamperAndEveryInconsistentCoordinate(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	aBlob := strings.Repeat("3", 40)
	bBlob := strings.Repeat("4", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: {
		{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: aBlob},
		{Name: []byte("b"), Mode: "100644", ObjectType: "blob", ObjectOID: bBlob},
	}}, blobs: map[string][]byte{aBlob: []byte("aaaa"), bBlob: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	request := byteSearchRequest([]byte("aa"))
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	base, err := service.cursors.Decode(first.Cursor)
	if err != nil || !base.SearchObjectSizeKnown || base.SearchObjectSize != 4 {
		t.Fatalf("cursor=%#v err=%v", base, err)
	}
	cases := []struct {
		name   string
		mutate func(*cursorPayload)
	}{
		{name: "path id", mutate: func(v *cursorPayload) { v.PathID = strings.Repeat("f", 64) }},
		{name: "current path", mutate: func(v *cursorPayload) { v.AfterPath = pathReference([]byte("b")) }},
		{name: "blob", mutate: func(v *cursorPayload) { v.ObjectOID = bBlob }},
		{name: "phase", mutate: func(v *cursorPayload) { v.SearchPhase = "invalid" }},
		{name: "text ordinal", mutate: func(v *cursorPayload) { v.SearchPhase = searchPhaseTextValidation; v.NextIndex = 1 }},
		{name: "ordinal beyond offset", mutate: func(v *cursorPayload) { v.NextIndex = v.NextOffset + 1 }},
		{name: "negative offset", mutate: func(v *cursorPayload) { v.NextOffset = -1 }},
		{name: "offset beyond size", mutate: func(v *cursorPayload) { v.NextOffset = v.SearchObjectSize + 1 }},
		{name: "unknown size with value", mutate: func(v *cursorPayload) { v.SearchObjectSizeKnown = false; v.SearchObjectSize = 1 }},
		{name: "last commit", mutate: func(v *cursorPayload) { v.LastCommitOID = strings.Repeat("9", 40) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			token, encodeErr := service.cursors.Encode(value)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			changed := request
			changed.Cursor = token
			if _, searchErr := service.Search(context.Background(), changed); ErrorCode(searchErr) != CodeInvalidCursor {
				t.Fatalf("error=%v cursor=%#v", searchErr, value)
			}
		})
	}

	tampered := first.Cursor[:len(first.Cursor)-1] + "A"
	changed := request
	changed.Cursor = tampered
	if _, err := service.Search(context.Background(), changed); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("tamper error=%v", err)
	}
}

func TestSearchIdentityAndReturnedBuffersAreStableAndIsolated(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	service := newFidelityService(t, vault, fidelityAuthority(commitOID, rootTree, "", 1))
	literal := []byte("aa")
	prefix := pathReference([]byte("a"))
	request := byteSearchRequest(literal)
	request.Prefixes = []PathReference{prefix}
	first, err := service.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	literal[0] = 'z'
	request.Prefixes[0].PathID = strings.Repeat("f", 64)
	secondRequest := byteSearchRequest([]byte("aa"))
	secondRequest.Prefixes = []PathReference{prefix}
	second, err := service.Search(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.QueryID != second.QueryID || first.FilterID != second.FilterID || len(first.Matches) != len(second.Matches) {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	for index := range first.Matches {
		if first.Matches[index].MatchID != second.Matches[index].MatchID {
			t.Fatalf("match identity changed at %d", index)
		}
	}
	original := second.Matches[0]
	first.Matches[0].MatchID = "changed"
	first.Matches[0].Path.Display = "changed"
	first.Matches[0].Path.InlineBase64 = "changed"
	if second.Matches[0] != original {
		t.Fatalf("result buffers alias: %#v want %#v", second.Matches[0], original)
	}
}

func TestSearchLifecycleReadsRemainEquivalentWhileRetentionExists(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	base := fidelityAuthority(commitOID, rootTree, "", 1)
	request := byteSearchRequest([]byte("aa"))
	var matchIDs []string
	for _, state := range []string{"active", "superseded", "closed"} {
		authority := base
		authority.Summary.LifecycleState = state
		service := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil)
		result, err := service.Search(context.Background(), request)
		if err != nil || result.Source.LifecycleState != state {
			t.Fatalf("state=%s result=%#v err=%v", state, result, err)
		}
		ids := matchIDList(result.Matches)
		if matchIDs == nil {
			matchIDs = ids
		} else if strings.Join(matchIDs, ",") != strings.Join(ids, ",") {
			t.Fatalf("state=%s ids=%v want=%v", state, ids, matchIDs)
		}
	}

	active := base
	active.Summary.LifecycleState = "active"
	activeService := newSearchService(t, searchAuthorityResolver{authority: active}, vault, nil)
	page := request
	page.Limit = 1
	first, err := activeService.Search(context.Background(), page)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	closed := base
	closed.Summary.LifecycleState = "closed"
	closedService := newSearchService(t, searchAuthorityResolver{authority: closed}, vault, nil)
	page.Cursor = first.Cursor
	if _, err := closedService.Search(context.Background(), page); err != nil {
		t.Fatalf("closed retained continuation failed: %v", err)
	}
}

func TestSearchReleasedMissingDuplicateAndMismatchedAuthorityFailsClosed(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	baseVault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("x")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	request := byteSearchRequest([]byte("x"))

	missing := authority
	missing.Relationship.ID = 0
	if _, err := newSearchService(t, searchAuthorityResolver{authority: missing}, baseVault, nil).Search(context.Background(), request); ErrorCode(err) != CodeRetainedAuthorityUnavailable {
		t.Fatalf("missing relationship error=%v", err)
	}
	resolverFailure := &operations.Error{Code: operations.CodeRetainedAuthorityUnavailable}
	if _, err := newSearchService(t, searchAuthorityResolver{err: resolverFailure}, baseVault, nil).Search(context.Background(), request); ErrorCode(err) != CodeRetainedAuthorityUnavailable {
		t.Fatalf("duplicate/missing authority error=%v", err)
	}
	for _, name := range []string{"released", "mismatched"} {
		t.Run(name, func(t *testing.T) {
			vault := &searchVaultWrapper{base: baseVault, tree: func(context.Context, sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
				return sourcevault.ReadRetainedTreeResult{}, &sourcevault.Error{Code: sourcevault.CodeVaultUnavailable}
			}}
			if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeRetainedAuthorityUnavailable {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSearchRetainedReadFailuresAndIntegrityMismatchesFailClosed(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	baseVault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	request := byteSearchRequest([]byte("a"))

	t.Run("tree failure", func(t *testing.T) {
		vault := &searchVaultWrapper{base: baseVault, tree: func(context.Context, sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
			return sourcevault.ReadRetainedTreeResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeObjectUnavailable {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("blob failure", func(t *testing.T) {
		vault := &searchVaultWrapper{base: baseVault, blob: func(context.Context, sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
			return sourcevault.ReadRetainedBlobRangeResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeObjectUnavailable {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("wrong tree oid", func(t *testing.T) {
		vault := &searchVaultWrapper{base: baseVault, tree: func(ctx context.Context, r sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
			value, err := baseVault.ReadRetainedTree(ctx, r)
			value.TreeOID = strings.Repeat("9", 40)
			return value, err
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeObjectMismatch {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("wrong blob oid", func(t *testing.T) {
		vault := &searchVaultWrapper{base: baseVault, blob: func(ctx context.Context, r sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
			value, err := baseVault.ReadRetainedBlobRange(ctx, r)
			value.BlobOID = strings.Repeat("9", 40)
			return value, err
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeObjectMismatch {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("zero byte nonterminal", func(t *testing.T) {
		vault := &searchVaultWrapper{base: baseVault, blob: func(_ context.Context, r sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
			return sourcevault.ReadRetainedBlobRangeResult{BlobOID: r.BlobOID, Offset: r.Offset, TotalSize: 4}, nil
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeObjectMismatch {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("wrong object type", func(t *testing.T) {
		vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "tag", ObjectOID: blobOID}}}, blobs: map[string][]byte{}, nodes: map[string]sourcevault.RetainedCommitNode{}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(context.Background(), request); ErrorCode(err) != CodeIntegrityFailure {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		vault := &searchVaultWrapper{base: baseVault, tree: func(ctx context.Context, _ sourcevault.ReadRetainedTreeRequest) (sourcevault.ReadRetainedTreeResult, error) {
			if ctx.Err() == nil {
				t.Fatal("context was not cancelled")
			}
			return sourcevault.ReadRetainedTreeResult{}, &sourcevault.Error{Code: sourcevault.CodeOperationCancelled}
		}}
		if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, vault, nil).Search(ctx, request); err == nil {
			t.Fatal("cancelled search succeeded")
		}
	})
}

func TestSearchObjectSizeInconsistencyFailsWithinCallAndAcrossResume(t *testing.T) {
	commitOID := strings.Repeat("1", 40)
	rootTree := strings.Repeat("2", 40)
	blobOID := strings.Repeat("3", 40)
	authority := fidelityAuthority(commitOID, rootTree, "", 1)
	baseVault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{rootTree: []sourcevault.RetainedTreeEntry{sourcevault.RetainedTreeEntry{Name: []byte("a"), Mode: "100644", ObjectType: "blob", ObjectOID: blobOID}}}, blobs: map[string][]byte{blobOID: []byte("aaaa")}, nodes: map[string]sourcevault.RetainedCommitNode{}}

	calls := 0
	within := &searchVaultWrapper{base: baseVault, blob: func(ctx context.Context, r sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
		calls++
		value, err := baseVault.ReadRetainedBlobRange(ctx, r)
		if calls > 1 {
			value.TotalSize++
		}
		return value, err
	}}
	if _, err := newSearchService(t, searchAuthorityResolver{authority: authority}, within, nil).Search(context.Background(), byteSearchRequest([]byte("a"))); ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("within-call size error=%v", err)
	}

	calls = 0
	across := &searchVaultWrapper{base: baseVault, blob: func(ctx context.Context, r sourcevault.ReadRetainedBlobRangeRequest) (sourcevault.ReadRetainedBlobRangeResult, error) {
		calls++
		value, err := baseVault.ReadRetainedBlobRange(ctx, r)
		if calls > 1 {
			value.TotalSize++
		}
		return value, err
	}}
	service := newSearchService(t, searchAuthorityResolver{authority: authority}, across, nil)
	request := byteSearchRequest([]byte("a"))
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	request.Cursor = first.Cursor
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("resume size error=%v", err)
	}
}

func TestSearchRealRetainedGitCoversModesPathsAndNoSourceFallback(t *testing.T) {
	ctx := context.Background()
	repo := newSearchGitRepository(t)
	deepDir := filepath.Join(repo, "deep", "nested")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"regular.txt":          []byte("needle regular\n"),
		"executable.sh":        []byte("#!/bin/sh\necho needle\n"),
		"deep/nested/text.txt": []byte("deep needle\n"),
		"empty":                {},
		"one":                  []byte("x"),
		"raw.bin":              {0xff, 0, 0xff, 0},
		"nul.txt":              {'n', 'e', 'e', 'd', 'l', 'e', 0},
		"pointer.lfs":          []byte("version https://git-lfs.github.com/spec/v1\noid sha256:needle\nsize 1\n"),
	}
	for name, data := range files {
		full := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(repo, "executable.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target-needle", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	arbitraryName := string([]byte{'r', 'a', 'w', 0xfe})
	if err := os.WriteFile(filepath.Join(repo, arbitraryName), []byte("needle arbitrary"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "add", "-A")
	runSearchGit(t, repo, "commit", "-m", "fixture")
	gitlinkTarget := runSearchGit(t, repo, "rev-parse", "HEAD")
	runSearchGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+gitlinkTarget+",gitlink")
	runSearchGit(t, repo, "commit", "-m", "gitlink")
	commitOID := runSearchGit(t, repo, "rev-parse", "HEAD")
	treeOID := runSearchGit(t, repo, "rev-parse", "HEAD^{tree}")

	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.sqlite"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, createErr := tx.CreateRepositoryTargetWithConfiguration(ctx, workflowstore.CreateRepositoryTargetParams{RepoTarget: "relay", LocalPath: repo, ConfiguredBranchRef: sql.NullString{String: "refs/heads/main", Valid: true}})
		return createErr
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
	retention, err := manager.RetainClosure(ctx, sourcevault.RetainRequest{ClosureID: imported.Closure.ClosureID, OwnerClass: workflowstore.SourceVaultOwnerOperationPacket, OwnerIdentity: "packet-search-expanded"})
	if err != nil {
		t.Fatal(err)
	}
	relationship := workflowstore.OperationPacketVaultRelationship{ID: 1, PublicationID: "publication-fidelity", PacketRowID: 11, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: "repository:relay:primary", OwnerIdentity: retention.OwnerIdentity, RetentionRowID: retention.ID, ClosureRowID: imported.Closure.ID, VaultRowID: imported.Vault.ID, CommitOID: imported.CommitOID, TreeOID: imported.TreeOID}
	authority := fidelityAuthority(imported.CommitOID, imported.TreeOID, "", relationship.ID)
	authority.Relationship = relationship
	service := newFidelityService(t, manager, authority)

	runSearchGit(t, repo, "checkout", "--orphan", "replacement")
	runSearchGit(t, repo, "rm", "-rf", "--ignore-unmatch", ".")
	if err := os.WriteFile(filepath.Join(repo, "replacement.txt"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "add", "-A")
	runSearchGit(t, repo, "commit", "-m", "replacement")
	replacement := runSearchGit(t, repo, "rev-parse", "HEAD")
	runSearchGit(t, repo, "checkout", "--detach", replacement)
	runSearchGit(t, repo, "branch", "-D", "main")
	runSearchGit(t, repo, "branch", "-D", "replacement")
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	runSearchGit(t, repo, "reflog", "expire", "--expire=now", "--all")
	runSearchGit(t, repo, "gc", "--prune=now")
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	text, err := service.Search(ctx, textSearchRequest("needle"))
	if err != nil || text.Completion != SearchCompletionComplete {
		t.Fatalf("text=%#v err=%v", text, err)
	}
	for _, path := range [][]byte{[]byte("regular.txt"), []byte("executable.sh"), []byte("deep/nested/text.txt"), []byte("link"), []byte("pointer.lfs"), []byte(arbitraryName)} {
		if !containsMatchPath(text.Matches, path) {
			t.Fatalf("missing retained text path=%v matches=%#v", path, text.Matches)
		}
	}
	if containsMatchPath(text.Matches, []byte("gitlink")) || containsMatchPath(text.Matches, []byte("raw.bin")) || containsMatchPath(text.Matches, []byte("nul.txt")) {
		t.Fatalf("text-ineligible or non-blob path entered text matches=%#v", text.Matches)
	}
	nulBytes, err := service.Search(ctx, byteSearchRequest([]byte{'e', 0}))
	if err != nil || len(nulBytes.Matches) != 1 || !containsMatchPath(nulBytes.Matches, []byte("nul.txt")) || nulBytes.Matches[0].ByteOffset != 5 {
		t.Fatalf("NUL byte search=%#v err=%v", nulBytes, err)
	}
	modes := map[string]string{}
	for _, match := range text.Matches {
		modes[match.Path.PathID] = match.FileMode
	}
	if modes[pathID([]byte("executable.sh"))] != "100755" || modes[pathID([]byte("link"))] != "120000" {
		t.Fatalf("modes=%v", modes)
	}

	binary, err := service.Search(ctx, byteSearchRequest([]byte{0xff, 0}))
	if err != nil || len(binary.Matches) != 2 || !containsMatchPath(binary.Matches, []byte("raw.bin")) {
		t.Fatalf("binary=%#v err=%v", binary, err)
	}
	one := byteSearchRequest([]byte("x"))
	one.Prefixes = []PathReference{pathReference([]byte("one"))}
	oneResult, err := service.Search(ctx, one)
	if err != nil || len(oneResult.Matches) != 1 || oneResult.Matches[0].ByteOffset != 0 {
		t.Fatalf("one=%#v err=%v", oneResult, err)
	}

	pagedText := textSearchRequest("needle")
	pagedText.Limit = 1
	pagedText.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 1 << 20}
	pagedTextMatches, textPages := exhaustSearch(t, service, pagedText, []int{1, 3, 2})
	if len(textPages) < 2 || strings.Join(matchIDList(pagedTextMatches), ",") != strings.Join(matchIDList(text.Matches), ",") {
		t.Fatalf("paged text pages=%d matches=%#v want=%#v", len(textPages), pagedTextMatches, text.Matches)
	}
	pagedBinary := byteSearchRequest([]byte{0xff, 0})
	pagedBinary.Limit = 1
	pagedBinary.Budget = SearchBudget{ExaminedObjects: 1, ExaminedBytes: 8}
	pagedBinaryMatches, binaryPages := exhaustSearch(t, service, pagedBinary, []int{1, 2})
	if len(binaryPages) < 2 || strings.Join(matchIDList(pagedBinaryMatches), ",") != strings.Join(matchIDList(binary.Matches), ",") {
		t.Fatalf("paged binary pages=%d matches=%#v want=%#v", len(binaryPages), pagedBinaryMatches, binary.Matches)
	}

	if _, err := manager.ReleaseRetention(ctx, retention.RetentionID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(ctx, textSearchRequest("needle")); ErrorCode(err) != CodeRetainedAuthorityUnavailable {
		t.Fatalf("released retained search error=%v", err)
	}
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

func byteSearchRequest(literal []byte) SearchRequest {
	return SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeByteLiteral, ByteLiteral: append([]byte(nil), literal...), Limit: MaxSearchPageMatches, Budget: SearchBudget{ExaminedObjects: 64, ExaminedBytes: 8 << 20}}
}

func textSearchRequest(literal string) SearchRequest {
	return SearchRequest{PacketID: "opkt-fidelity", SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", RepositoryKey: "relay", Mode: SearchModeTextLiteral, TextLiteral: literal, Limit: MaxSearchPageMatches, Budget: SearchBudget{ExaminedObjects: 64, ExaminedBytes: 8 << 20}}
}

func pathReference(path []byte) PathReference {
	return PathReference{PathID: pathID(path), InlineBase64: canonicalInline(path)}
}

func exhaustSearch(t *testing.T, service *Service, request SearchRequest, limits []int) ([]SearchMatch, []SearchResult) {
	t.Helper()
	var matches []SearchMatch
	var pages []SearchResult
	for page := 0; page < 1024; page++ {
		request.Limit = limits[page%len(limits)]
		result, err := service.Search(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, result)
		matches = append(matches, result.Matches...)
		if result.Completion == SearchCompletionComplete {
			return matches, pages
		}
		if result.Cursor == "" {
			t.Fatalf("incomplete page omitted cursor: %#v", result)
		}
		request.Cursor = result.Cursor
	}
	t.Fatal("search continuation exceeded page guard")
	return nil, nil
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

func matchIDList(matches []SearchMatch) []string {
	result := make([]string, len(matches))
	for index, match := range matches {
		result[index] = match.MatchID
	}
	return result
}
