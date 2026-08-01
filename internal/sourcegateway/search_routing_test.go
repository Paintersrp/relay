package sourcegateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourceindex/reader"
	"relay/internal/sourcevault"
)

func publicRoutingService(t *testing.T) (*Service, *fidelityVaultFake, SearchRequest) {
	t.Helper()
	commit, tree, blob := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	vault := &fidelityVaultFake{trees: map[string][]sourcevault.RetainedTreeEntry{tree: {{Name: []byte("a.txt"), Mode: "100644", ObjectType: "blob", ObjectOID: blob}}}, blobs: map[string][]byte{blob: []byte("needle needle")}, nodes: map[string]sourcevault.RetainedCommitNode{}}
	request := textSearchRequest("needle")
	request.Limit = 1
	return newFidelityService(t, vault, fidelityAuthority(commit, tree, "", 1)), vault, request
}

func requireScannerCursor(t *testing.T, service *Service, token string) {
	t.Helper()
	payload, err := service.cursors.Decode(token)
	if err != nil || payload.Version != SearchCursorVersion || payload.SearchBackend != string(searchBackendScanner) || payload.SearchGenerationID != "" || payload.SearchGenerationManifestSHA256 != "" || payload.SearchCoverageManifestSHA256 != "" || payload.SearchArtifactManifestSHA256 != "" {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}
}

func TestPublicSearchInitialScannerRouting(t *testing.T) {
	service, _, request := publicRoutingService(t)
	provider := &searchIndexProviderFake{}
	service.searchIndex = provider
	for _, test := range []struct {
		name    string
		request SearchRequest
	}{
		{"byte", byteSearchRequest([]byte("needle"))},
		{"one rune", textSearchRequest("a")},
		{"two runes", textSearchRequest("ab")},
		{"non utf8 prefix", func() SearchRequest {
			r := textSearchRequest("needle")
			r.Prefixes = []PathReference{pathReference([]byte{'x', 0xff})}
			return r
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.request.Limit = 1
			result, err := service.Search(context.Background(), test.request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Completion != SearchCompletionComplete && result.Cursor != "" {
				requireScannerCursor(t, service, result.Cursor)
			}
		})
	}
	if provider.opens != 0 {
		t.Fatalf("provider opens=%d", provider.opens)
	}

	service.searchIndex = nil
	if _, err := service.Search(context.Background(), request); err != nil {
		t.Fatal(err)
	}
}

func TestPublicSearchInitialStableIndexMissScans(t *testing.T) {
	for _, openErr := range []error{reader.ErrGenerationUnavailable, reader.ErrGenerationIntegrity, reader.ErrUnsupportedPlatform, reader.ErrInvalidConfiguration} {
		t.Run(openErr.Error(), func(t *testing.T) {
			service, _, request := publicRoutingService(t)
			provider := &searchIndexProviderFake{err: openErr}
			service.searchIndex = provider
			result, err := service.Search(context.Background(), request)
			if err != nil || len(result.Matches) != 1 || provider.opens != 1 {
				t.Fatalf("result=%#v err=%v opens=%d", result, err, provider.opens)
			}
			requireScannerCursor(t, service, result.Cursor)
		})
	}
}

func TestPublicSearchIndexOpenErrorsAndInvalidDescriptor(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{"canceled", context.Canceled}, {"deadline", context.DeadlineExceeded}, {"unexpected", errors.New("unexpected provider failure")},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, request := publicRoutingService(t)
			service.searchIndex = &searchIndexProviderFake{err: test.err}
			result, err := service.Search(context.Background(), request)
			if !errors.Is(err, test.err) || !reflect.DeepEqual(result, SearchResult{}) {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
	t.Run("handle plus error", func(t *testing.T) {
		service, _, request := publicRoutingService(t)
		handle := &hybridReaderFake{descriptor: hybridTestDescriptor(t)}
		want := errors.New("primary")
		service.searchIndex = &searchIndexProviderFake{handle: handle, err: want}
		result, err := service.Search(context.Background(), request)
		if !errors.Is(err, want) || !reflect.DeepEqual(result, SearchResult{}) || handle.closes != 1 || handle.queries != 0 || handle.fallbacks != 0 {
			t.Fatalf("result=%#v err=%v handle=%#v", result, err, handle)
		}
	})
	t.Run("invalid descriptor falls back after close", func(t *testing.T) {
		service, _, request := publicRoutingService(t)
		bad := hybridTestDescriptor(t)
		bad.GenerationID = "bad"
		handle := &hybridReaderFake{descriptor: bad}
		service.searchIndex = &searchIndexProviderFake{handle: handle}
		result, err := service.Search(context.Background(), request)
		if err != nil || len(result.Matches) != 1 || handle.closes != 1 || handle.queries != 0 {
			t.Fatalf("result=%#v err=%v handle=%#v", result, err, handle)
		}
		requireScannerCursor(t, service, result.Cursor)
	})
}

func TestPublicSearchIndexedFailuresDoNotScanAndCloseOnce(t *testing.T) {
	for _, queryErr := range []error{reader.ErrQueryIncomplete, reader.ErrGenerationIntegrity, reader.ErrClosed, context.Canceled, context.DeadlineExceeded, errors.New("query failure")} {
		t.Run(queryErr.Error(), func(t *testing.T) {
			service, vault, request := publicRoutingService(t)
			handle := &hybridReaderFake{descriptor: hybridTestDescriptor(t), err: queryErr}
			service.searchIndex = &searchIndexProviderFake{handle: handle}
			result, err := service.Search(context.Background(), request)
			if !errors.Is(err, queryErr) || !reflect.DeepEqual(result, SearchResult{}) || handle.closes != 1 || handle.queries != 1 || handle.fallbacks != 0 {
				t.Fatalf("result=%#v err=%v handle=%#v", result, err, handle)
			}
			if len(vault.trees) != 1 {
				t.Fatal("unexpected scanner mutation")
			}
		})
	}
}

func TestPublicSearchScannerContinuationNeverOpensIndex(t *testing.T) {
	service, _, request := publicRoutingService(t)
	request.Mode, request.TextLiteral, request.ByteLiteral = SearchModeByteLiteral, "", []byte("needle")
	request.Limit = 1
	first, err := service.Search(context.Background(), request)
	if err != nil || first.Cursor == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	provider := &searchIndexProviderFake{handle: &hybridReaderFake{descriptor: hybridTestDescriptor(t)}}
	service.searchIndex = provider
	request.Cursor = first.Cursor
	second, err := service.Search(context.Background(), request)
	if err != nil || len(second.Matches) != 1 || second.Matches[0].ByteOffset != 7 || provider.opens != 0 {
		t.Fatalf("second=%#v err=%v opens=%d", second, err, provider.opens)
	}
	if second.Cursor != "" {
		requireScannerCursor(t, service, second.Cursor)
	}
}

func TestPublicSearchCloseFailurePrecedence(t *testing.T) {
	service, _, request := publicRoutingService(t)
	handle := &hybridReaderFake{descriptor: hybridTestDescriptor(t), indexed: []reader.Candidate{{Path: []byte("a.txt")}}, closeErr: errors.New("close")}
	service.searchIndex = &searchIndexProviderFake{handle: handle}
	result, err := service.Search(context.Background(), request)
	if ErrorCode(err) != CodeInternalFailure || !reflect.DeepEqual(result, SearchResult{}) || handle.closes != 1 {
		t.Fatalf("result=%#v err=%v closes=%d", result, err, handle.closes)
	}
}
