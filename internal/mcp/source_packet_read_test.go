package mcp

import (
	"strconv"
	"strings"
	"testing"

	"relay/internal/operations/registry"
)

// TestSourceToolsUnderPacketAuthority proves the required source-read sequence
// against one published Wayfinder packet whose repository has since advanced.
// The fixture is immutable after construction and every subtest is read only,
// so one snapshot serves the whole contract.
func TestSourceToolsUnderPacketAuthority(t *testing.T) {
	fixture := openSourcePacketFixture(t)

	t.Run("exact_snapshot_authority", func(t *testing.T) {
		var tree listSourceTreeView
		fixture.callSource(t, "list_source_tree", fixture.sourceRequest(map[string]any{"recursive": true, "limit": 512}), &tree)
		if !tree.Complete || tree.Cursor != "" {
			t.Fatalf("tree continuation = %v/%q", tree.Complete, tree.Cursor)
		}
		assertSourceCommitA(t, fixture, tree.Source, "list_source_tree")
		assertSourcePaths(t, sourceTreePaths(tree), sourceSnapshotCommitAPaths)

		var newer searchSourceView
		fixture.callSource(t, "search_source", fixture.sourceRequest(sourceSearchMembers(sourceSnapshotCommitBMarker, 8)), &newer)
		if newer.Completion != "complete" || len(newer.Matches) != 0 {
			t.Fatalf("commit B literal search = %s/%d matches", newer.Completion, len(newer.Matches))
		}
		assertSourceCommitA(t, fixture, newer.Source, "search_source")

		var readme readSourceTextView
		fixture.callSource(t, "read_source_text", fixture.sourceRequest(map[string]any{"path": sourcePathArgument("README.md"), "limit": 4096}), &readme)
		assertSourceCommitA(t, fixture, readme.Source, "read_source_text")
		if text := string(sourceTextBytes(readme)); text != sourceSnapshotReadmeA {
			t.Fatalf("README bytes = %q want %q", text, sourceSnapshotReadmeA)
		}
		if !readme.Complete || readme.TotalSize != int64(len(sourceSnapshotReadmeA)) {
			t.Fatalf("README page = %#v", readme)
		}

		rejected, reason := sourceRejection(t, coldStartCall(t, fixture.server, "read_source_text", fixture.sourceRequest(map[string]any{"path": sourcePathArgument("b-only.txt"), "limit": 4096})))
		if !rejected {
			t.Fatal("a commit B only path was readable from the commit A packet")
		}
		assertBoundedSourceReason(t, fixture, reason)
	})

	t.Run("broad_path_access", func(t *testing.T) {
		var nested listSourceTreeView
		fixture.callSource(t, "list_source_tree", fixture.sourceRequest(map[string]any{"directory": sourcePathArgument("internal/deep/nested"), "limit": 8}), &nested)
		if nested.Directory.Display != "internal/deep/nested" {
			t.Fatalf("directory identity = %#v", nested.Directory)
		}
		assertSourcePaths(t, sourceTreePaths(nested), []string{sourceSnapshotNestedPath})

		var found searchSourceView
		fixture.callSource(t, "search_source", fixture.sourceRequest(sourceSearchMembers(sourceSnapshotNestedMarker, 8)), &found)
		if found.Completion != "complete" || len(found.Matches) != 1 {
			t.Fatalf("nested marker search = %s/%d matches", found.Completion, len(found.Matches))
		}
		match := found.Matches[0]
		if match.Path.Display != sourceSnapshotNestedPath || match.MatchLength != int64(len(sourceSnapshotNestedMarker)) {
			t.Fatalf("nested marker match = %#v", match)
		}
		if match.ByteOffset != int64(strings.Index(sourceSnapshotNestedBytes, sourceSnapshotNestedMarker)) {
			t.Fatalf("nested marker offset = %d", match.ByteOffset)
		}

		var text readSourceTextView
		fixture.callSource(t, "read_source_text", fixture.sourceRequest(map[string]any{"path": sourcePathArgument(sourceSnapshotNestedPath), "limit": 4096}), &text)
		if value := string(sourceTextBytes(text)); value != sourceSnapshotNestedBytes {
			t.Fatalf("nested bytes = %q want %q", value, sourceSnapshotNestedBytes)
		}
		if text.Path.Display != sourceSnapshotNestedPath || text.TotalSize != int64(len(sourceSnapshotNestedBytes)) {
			t.Fatalf("nested text identity = %#v", text)
		}
	})

	t.Run("authorization_failures", func(t *testing.T) {
		for _, tool := range sourceToolCases(fixture) {
			t.Run(tool.tool, func(t *testing.T) {
				cases := []struct {
					name      string
					arguments map[string]any
				}{
					{"unknown_packet", tool.with(map[string]any{"packet_id": "opkt-source-snapshot-absent"})},
					{"unauthorized_repository", tool.with(map[string]any{"repository_key": fixture.unbound})},
					{"foreign_surface", tool.with(map[string]any{"surface_contract": "wayfinder-workspace.v1"})},
					{"foreign_operation", tool.with(map[string]any{"operation_id": "wayfinder.workspace"})},
					{"absolute_path", tool.withPath("/etc/passwd")},
					{"traversal_path", tool.withPath("../escape.txt")},
				}
				for _, test := range cases {
					t.Run(test.name, func(t *testing.T) {
						rejected, reason := sourceRejection(t, coldStartCall(t, fixture.server, tool.tool, test.arguments))
						if !rejected {
							t.Fatalf("%s accepted an unauthorized request", tool.tool)
						}
						assertBoundedSourceReason(t, fixture, reason)
					})
				}
				// The dispatcher keeps the mounted route authoritative even
				// when the legacy pre-validator is not in the path.
				if result := fixture.dispatch(t, tool.tool, tool.with(map[string]any{"surface_contract": "wayfinder-workspace.v1"})); !result.IsError {
					t.Fatalf("%s dispatcher accepted a foreign surface", tool.tool)
				}
			})
		}
	})

	t.Run("bounds_and_determinism", func(t *testing.T) {
		// The unpaginated tree is asserted by exact_snapshot_authority; a
		// bounded page size must reproduce the identical order.
		assertSourcePaths(t, paginateSourceTree(t, fixture, 2), sourceSnapshotCommitAPaths)
		assertSourcePaths(t, paginateSourceSearch(t, fixture, 1), expectedSourceAlphaMatches())

		chunked := readSourceTextInChunks(t, fixture, sourceSnapshotNestedPath, 32)
		if chunked != sourceSnapshotNestedBytes {
			t.Fatalf("chunked text = %q want %q", chunked, sourceSnapshotNestedBytes)
		}

		for _, tool := range sourceToolCases(fixture) {
			t.Run(tool.tool, func(t *testing.T) {
				rejected, reason := sourceRejection(t, coldStartCall(t, fixture.server, tool.tool, tool.with(map[string]any{"cursor": "not-a-relay-source-cursor"})))
				if !rejected {
					t.Fatalf("%s accepted a malformed cursor", tool.tool)
				}
				assertBoundedSourceReason(t, fixture, reason)
				if result := fixture.dispatch(t, tool.tool, tool.with(map[string]any{"obsolete_member": true})); !result.IsError {
					t.Fatalf("%s strict decoder accepted an unknown member", tool.tool)
				}
			})
		}
	})

	t.Run("runtime_output_matches_published_schema", func(t *testing.T) {
		// Each representative response carries a continuation so the published
		// output schema is proven against a bounded page, not only a final one.
		responses := map[string]map[string]any{
			"list_source_tree": fixture.sourceRequest(map[string]any{"recursive": true, "limit": 1}),
			"search_source":    fixture.sourceRequest(map[string]any{"mode": "text_literal", "text_literal": "alpha", "limit": 1, "examined_objects": 1, "examined_bytes": 8}),
			"read_source_text": fixture.sourceRequest(map[string]any{"path": sourcePathArgument(sourceSnapshotNestedPath), "limit": 8}),
		}
		for tool, arguments := range responses {
			t.Run(tool, func(t *testing.T) {
				schema := sourceToolManifest(t, fixture.manifest, tool).OutputSchema
				document := fixture.callSourceText(t, tool, arguments)
				if err := registry.ValidateSchemaInstance(schema, []byte(document)); err != nil {
					t.Fatalf("%s runtime response violates its published output schema: %v\n%s", tool, err, document)
				}
				if !strings.Contains(document, `"Cursor": "`) {
					t.Fatalf("%s representative response carried no continuation:\n%s", tool, document)
				}
			})
		}
	})
}

type sourceToolCase struct {
	tool     string
	fixture  sourcePacketFixture
	members  map[string]any
	pathName string
	pathList bool
}

func sourceToolCases(fixture sourcePacketFixture) []sourceToolCase {
	return []sourceToolCase{
		{tool: "list_source_tree", fixture: fixture, members: map[string]any{"limit": 8}, pathName: "directory"},
		{tool: "search_source", fixture: fixture, members: sourceSearchMembers("alpha", 8), pathName: "prefixes", pathList: true},
		{tool: "read_source_text", fixture: fixture, members: map[string]any{"limit": 4096, "path": sourcePathArgument("README.md")}, pathName: "path"},
	}
}

func (c sourceToolCase) with(overrides map[string]any) map[string]any {
	request := c.fixture.sourceRequest(c.members)
	for name, value := range overrides {
		request[name] = value
	}
	return request
}

func (c sourceToolCase) withPath(path string) map[string]any {
	reference := sourcePathArgument(path)
	if c.pathList {
		return c.with(map[string]any{c.pathName: []any{reference}})
	}
	return c.with(map[string]any{c.pathName: reference})
}

func sourceSearchMembers(literal string, limit int) map[string]any {
	return map[string]any{
		"mode":             "text_literal",
		"text_literal":     literal,
		"limit":            limit,
		"examined_objects": 64,
		"examined_bytes":   1 << 20,
	}
}

func paginateSourceTree(t *testing.T, fixture sourcePacketFixture, limit int) []string {
	t.Helper()
	members := map[string]any{"recursive": true, "limit": limit}
	paths := []string{}
	for page := 0; page < 64; page++ {
		var view listSourceTreeView
		fixture.callSource(t, "list_source_tree", fixture.sourceRequest(members), &view)
		paths = append(paths, sourceTreePaths(view)...)
		if view.Complete {
			if view.Cursor != "" {
				t.Fatal("a complete tree page returned a continuation")
			}
			return paths
		}
		if view.Cursor == "" {
			t.Fatal("an incomplete tree page omitted its continuation")
		}
		members["cursor"] = view.Cursor
	}
	t.Fatal("tree continuation exceeded the page guard")
	return nil
}

// expectedSourceAlphaMatches is the exact ordered literal match sequence of the
// commit A corpus in full path byte order.
func expectedSourceAlphaMatches() []string {
	values := []string{}
	for _, file := range []struct{ path, content string }{
		{"docs/notes.md", sourceSnapshotNotes},
		{sourceSnapshotNestedPath, sourceSnapshotNestedBytes},
	} {
		for offset := 0; offset+len("alpha") <= len(file.content); offset++ {
			if file.content[offset:offset+len("alpha")] == "alpha" {
				values = append(values, file.path+"@"+strconv.Itoa(offset))
			}
		}
	}
	return values
}

func paginateSourceSearch(t *testing.T, fixture sourcePacketFixture, limit int) []string {
	t.Helper()
	members := sourceSearchMembers("alpha", limit)
	identities := []string{}
	seen := map[string]struct{}{}
	for page := 0; page < 64; page++ {
		var view searchSourceView
		fixture.callSource(t, "search_source", fixture.sourceRequest(members), &view)
		for _, match := range view.Matches {
			if _, duplicate := seen[match.MatchID]; duplicate {
				t.Fatalf("search returned a duplicate match identity: %s", match.MatchID)
			}
			seen[match.MatchID] = struct{}{}
			identities = append(identities, match.Path.Display+"@"+strconv.FormatInt(match.ByteOffset, 10))
		}
		if view.Completion == "complete" {
			return identities
		}
		if view.Cursor == "" {
			t.Fatalf("an incomplete search page omitted its continuation: %s", view.Completion)
		}
		members["cursor"] = view.Cursor
	}
	t.Fatal("search continuation exceeded the page guard")
	return nil
}

func readSourceTextInChunks(t *testing.T, fixture sourcePacketFixture, path string, limit int) string {
	t.Helper()
	members := map[string]any{"path": sourcePathArgument(path), "limit": limit}
	data := []byte{}
	for page := 0; page < 512; page++ {
		var view readSourceTextView
		fixture.callSource(t, "read_source_text", fixture.sourceRequest(members), &view)
		data = append(data, sourceTextBytes(view)...)
		if view.Complete {
			if view.Cursor != "" {
				t.Fatal("a complete text page returned a continuation")
			}
			return string(data)
		}
		if view.Cursor == "" {
			t.Fatal("an incomplete text page omitted its continuation")
		}
		members["cursor"] = view.Cursor
	}
	t.Fatal("text continuation exceeded the page guard")
	return ""
}

func assertSourceCommitA(t *testing.T, fixture sourcePacketFixture, identity sourceIdentityView, tool string) {
	t.Helper()
	if identity.CommitOID != fixture.commitA {
		t.Fatalf("%s commit = %s want %s", tool, identity.CommitOID, fixture.commitA)
	}
	if identity.CommitOID == fixture.commitB {
		t.Fatalf("%s resolved the advanced working repository commit", tool)
	}
	if identity.PacketID != fixture.packetID || identity.RepositoryKey != fixture.repository || identity.DependencyKey != "repository:"+fixture.repository+":primary" {
		t.Fatalf("%s source authority = %#v", tool, identity)
	}
	if identity.SurfaceContract != sourceSnapshotSurface || identity.OperationID != sourceSnapshotOperation || identity.AnchorName != "" {
		t.Fatalf("%s route authority = %#v", tool, identity)
	}
}

func assertSourcePaths(t *testing.T, actual, expected []string) {
	t.Helper()
	if strings.Join(actual, "|") != strings.Join(expected, "|") {
		t.Fatalf("paths = %v want %v", actual, expected)
	}
}

func assertBoundedSourceReason(t *testing.T, fixture sourcePacketFixture, reason string) {
	t.Helper()
	if strings.TrimSpace(reason) == "" {
		t.Fatal("a rejected source request returned no bounded reason")
	}
	if len(reason) > 300 {
		t.Fatalf("rejection reason is not bounded: %d bytes", len(reason))
	}
	lowered := strings.ToLower(reason)
	for _, forbidden := range []string{"select ", "sqlite", "workflow.db", ".git", "source-vault"} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("rejection reason leaked %q: %s", forbidden, reason)
		}
	}
	for _, path := range fixture.secretPaths {
		for _, value := range []string{path, strings.ReplaceAll(path, "\\", "/")} {
			if strings.Contains(lowered, strings.ToLower(value)) {
				t.Fatalf("rejection reason leaked a local path: %s", reason)
			}
		}
	}
}
