package sourcegateway

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"relay/internal/sourceindex/reader"
)

func publicIndexedCursor(t *testing.T) (*Service, SearchRequest, cursorPayload) {
	t.Helper()
	service, _, request := publicRoutingService(t)
	handle := &hybridReaderFake{descriptor: hybridTestDescriptor(t), indexed: []reader.Candidate{{Path: []byte("a.txt")}}}
	service.searchIndex = &searchIndexProviderFake{handle: handle}
	result, err := service.Search(context.Background(), request)
	if err != nil || result.Cursor == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	payload, err := service.cursors.Decode(result.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = result.Cursor
	return service, request, payload
}

func TestSearchBackendBindingCursorValidation(t *testing.T) {
	service, request, payload := publicIndexedCursor(t)
	for _, test := range []struct {
		name   string
		change func(*cursorPayload)
	}{
		{"empty backend", func(p *cursorPayload) { p.SearchBackend = "" }},
		{"unknown backend", func(p *cursorPayload) { p.SearchBackend = "other" }},
		{"scanner generation", func(p *cursorPayload) { p.SearchBackend = "scanner" }},
		{"indexed no generation", func(p *cursorPayload) { p.SearchGenerationID = "" }},
		{"indexed no generation manifest", func(p *cursorPayload) { p.SearchGenerationManifestSHA256 = "" }},
		{"indexed no coverage manifest", func(p *cursorPayload) { p.SearchCoverageManifestSHA256 = "" }},
		{"indexed no artifact manifest", func(p *cursorPayload) { p.SearchArtifactManifestSHA256 = "" }},
		{"uppercase digest", func(p *cursorPayload) { p.SearchGenerationID = strings.ToUpper(p.SearchGenerationID) }},
		{"non hex digest", func(p *cursorPayload) { p.SearchArtifactManifestSHA256 = strings.Repeat("g", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := payload
			test.change(&changed)
			token, err := service.cursors.Encode(changed)
			if err != nil {
				t.Fatal(err)
			}
			request.Cursor = token
			if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSearchCursorTamperFieldsReject(t *testing.T) {
	service, request, payload := publicIndexedCursor(t)
	for _, test := range []struct {
		name string
		old  string
		new  string
	}{
		{"backend", `"search_backend":"indexed"`, `"search_backend":"scanner"`},
		{"generation", payload.SearchGenerationID, strings.Repeat("f", 64)},
		{"generation manifest", payload.SearchGenerationManifestSHA256, strings.Repeat("f", 64)},
		{"coverage manifest", payload.SearchCoverageManifestSHA256, strings.Repeat("f", 64)},
		{"artifact manifest", payload.SearchArtifactManifestSHA256, strings.Repeat("f", 64)},
		{"current path", payload.AfterPath.InlineBase64, "Yg"},
		{"path id", payload.PathID, strings.Repeat("f", 64)},
		{"blob", payload.ObjectOID, strings.Repeat("f", 40)},
		{"phase", `"search_phase":"literal_scan"`, `"search_phase":"bad_phase___"`},
		{"offset", `"next_offset":1`, `"next_offset":2`},
		{"ordinal", `"next_index":1`, `"next_index":2`},
		{"known size", `"search_object_size":13`, `"search_object_size":14`},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := tamperSignedSearchCursor(t, request.Cursor, test.old, test.new)
			request.Cursor = token
			if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func tamperSignedSearchCursor(t *testing.T, token, old, new string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(payload), old, new, 1)
	if changed == string(payload) {
		t.Fatalf("missing signed field %q", old)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(changed)) + "." + parts[1]
}

func TestSearchCursorVersionIsolation(t *testing.T) {
	service, request, payload := publicIndexedCursor(t)
	if payload.Version != SearchCursorVersion {
		t.Fatalf("version=%q", payload.Version)
	}
	payload.Version = CursorVersion
	// The codec refuses to mint a legacy search cursor; the canonical signed
	// payload is therefore rejected before it can become a continuation.
	if _, err := service.cursors.Encode(payload); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("error=%v", err)
	}

	tree := fidelityCursorBase(fidelityAuthority(strings.Repeat("1", 40), strings.Repeat("2", 40), "", 1), "tree", strings.Repeat("a", 64))
	token, err := service.cursors.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = token
	if _, err := service.Search(context.Background(), request); ErrorCode(err) != CodeInvalidCursor {
		t.Fatalf("tree cursor error=%v", err)
	}
}
