package mcpingress

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/transport/transporttrace"
)

func TestProxyPreservesBytesInjectsBearerAndWritesMetadataOnlyTrace(t *testing.T) {
	advertised := "planner-authoring-v1__search_source"
	requestBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + advertised + `","arguments":{"operation_id":"planner.requirements","packet_id":"packet-1","project_id":"project-1","repository_key":"relay","cursor":"raw-cursor-secret","text_literal":"protected-source-literal"}}}`)
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"protected-source-response"}],"structuredContent":{"Source":{"RepositoryKey":"relay","CommitOID":"0123456789012345678901234567890123456789"},"Complete":false,"Cursor":"response-cursor-secret"},"isError":false}}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp/planner" {
			t.Fatalf("upstream path=%s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Fatalf("upstream authorization=%q", request.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(body, requestBody) {
			t.Fatalf("upstream body changed: %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(responseBody)
	}))
	defer upstream.Close()
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/mcp/planner"
	spec := testMappingSpec(MappingPlanner, parsed.Path, "planner", advertised, "/mcp/v1/planner/authoring", "planner-authoring.v1")
	spec.Listener = PrivateAddress{value: "127.0.0.1:18102"}
	spec.Upstream = UpstreamTarget{value: *parsed}
	traceRoot := t.TempDir()
	traces := &recoveringTraceStore{root: traceRoot, mappingID: string(spec.ID), policy: transporttrace.RetentionPolicy{MaxAge: transporttrace.DefaultMaxAge, MaxBytes: transporttrace.DefaultMaxBytes}}
	health := newHealthTracker(spec.ID, spec.RoutePath, time.Now)
	logBuffer := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logBuffer, nil))
	handler := newProxyHandler(spec, newProxyClient(), NewBearerInjector("upstream-secret"), traces, health, log, time.Now, strings.NewReader(strings.Repeat("r", 16)))
	request := httptest.NewRequest(http.MethodPost, spec.RoutePath, bytes.NewReader(requestBody))
	request.Header.Set("Authorization", "Bearer client-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), responseBody) {
		t.Fatalf("response status=%d body=%s", response.Code, response.Body.Bytes())
	}
	if err := traces.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(traceRoot, string(spec.ID)))
	if err != nil || len(entries) != 1 {
		t.Fatalf("trace entries=%d err=%v", len(entries), err)
	}
	line, err := os.ReadFile(filepath.Join(traceRoot, string(spec.ID), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{"client-secret", "upstream-secret", "raw-cursor-secret", "response-cursor-secret", "protected-source-literal", "protected-source-response"} {
		if bytes.Contains(line, []byte(protected)) || strings.Contains(logBuffer.String(), protected) {
			t.Fatalf("protected value leaked: %s", protected)
		}
	}
	var record transporttrace.Record
	if err := json.Unmarshal(bytes.TrimSpace(line), &record); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(responseBody)
	if record.MappingID != string(spec.ID) || record.PublicSurface != "planner" || record.PublicAdvertisedToolName != advertised || record.InternalToolName != "search_source" || record.InternalRoutePath != "/mcp/v1/planner/authoring" || record.SurfaceContract != "planner-authoring.v1" || record.StandingAuthorityPath != "agents/test.md" || record.PacketID != "packet-1" || record.ProjectID != "project-1" || record.ResponseSHA256 != hex.EncodeToString(digest[:]) || record.CompletionState != transporttrace.CompletionBounded || record.OutcomeClass != transporttrace.OutcomeSuccess || !record.DownstreamWrite.Complete {
		t.Fatalf("record=%#v", record)
	}
}

func TestProxyRejectsMethodAndPathWithoutForwarding(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls++ }))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	parsed.Path = "/mcp/auditor"
	spec := testMappingSpec(MappingAuditor, parsed.Path, "auditor", "auditor-audit-v1__get_audit_packet", "/mcp/v1/auditor/audit", "auditor-audit.v1")
	spec.Listener = PrivateAddress{value: "127.0.0.1:18103"}
	spec.Upstream = UpstreamTarget{value: *parsed}
	traces := &recoveringTraceStore{root: t.TempDir(), mappingID: string(spec.ID), policy: transporttrace.RetentionPolicy{MaxAge: transporttrace.DefaultMaxAge, MaxBytes: transporttrace.DefaultMaxBytes}}
	defer traces.Close()
	handler := newProxyHandler(spec, newProxyClient(), BearerInjector{}, traces, newHealthTracker(spec.ID, spec.RoutePath, time.Now), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now, strings.NewReader(strings.Repeat("r", 32)))
	for _, test := range []struct {
		method string
		path   string
		status int
	}{{http.MethodGet, spec.RoutePath, http.StatusMethodNotAllowed}, {http.MethodPost, "/mcp/wayfinder", http.StatusNotFound}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s %s status=%d", test.method, test.path, response.Code)
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls=%d", upstreamCalls)
	}
}

type boundedResponseWriter struct {
	header http.Header
	limit  int
	err    error
	body   bytes.Buffer
	status int
}

func (writer *boundedResponseWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = http.Header{}
	}
	return writer.header
}
func (writer *boundedResponseWriter) WriteHeader(status int) { writer.status = status }
func (writer *boundedResponseWriter) Write(value []byte) (int, error) {
	limit := writer.limit
	if limit > len(value) {
		limit = len(value)
	}
	if limit > 0 {
		_, _ = writer.body.Write(value[:limit])
	}
	return limit, writer.err
}

func TestCopyUpstreamResponseDistinguishesShortWriteAndDisconnect(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	for _, test := range []struct {
		name  string
		limit int
		err   error
		want  transporttrace.ErrorClass
	}{
		{name: "short write", limit: 5, want: transporttrace.ErrorDownstreamShortWrite},
		{name: "disconnect", limit: 0, err: io.ErrClosedPipe, want: transporttrace.ErrorDownstreamDisconnected},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &boundedResponseWriter{limit: test.limit, err: test.err}
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
			_, write, attempted, _, responseClass := copyUpstreamResponse(writer, response)
			if write.ErrorClass != test.want || write.Complete || attempted != int64(len(body)) || responseClass != transporttrace.ErrorNone {
				t.Fatalf("write=%#v attempted=%d responseClass=%s", write, attempted, responseClass)
			}
		})
	}
}

func TestTracePersistenceFailureDoesNotChangeAuthoritativeResponse(t *testing.T) {
	responseBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(responseBody)
	}))
	defer upstream.Close()
	parsed, _ := url.Parse(upstream.URL)
	parsed.Path = "/mcp/wayfinder"
	spec := testMappingSpec(MappingWayfinder, parsed.Path, "wayfinder", "wayfinder-workspace-v1__list_projects", "/mcp/v1/wayfinder/workspace", "wayfinder-workspace.v1")
	spec.Listener = PrivateAddress{value: "127.0.0.1:18101"}
	spec.Upstream = UpstreamTarget{value: *parsed}
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	traces := &recoveringTraceStore{root: blocked, mappingID: string(spec.ID), policy: transporttrace.RetentionPolicy{MaxAge: transporttrace.DefaultMaxAge, MaxBytes: transporttrace.DefaultMaxBytes}}
	health := newHealthTracker(spec.ID, spec.RoutePath, time.Now)
	handler := newProxyHandler(spec, newProxyClient(), BearerInjector{}, traces, health, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now, strings.NewReader(strings.Repeat("r", 16)))
	requestBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, spec.RoutePath, bytes.NewReader(requestBody)))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), responseBody) {
		t.Fatalf("response status=%d body=%s", response.Code, response.Body.Bytes())
	}
	snapshot := health.Snapshot()
	if snapshot.State != HealthUnhealthy || snapshot.LastErrorClass != transporttrace.ErrorInternalTransportFailure {
		t.Fatalf("health=%#v", snapshot)
	}
}

func testMappingSpec(id MappingID, publicPath, publicSurface, advertisedName, internalRoute, contract string) MappingSpec {
	identity := testToolIdentity(advertisedName, internalRoute)
	identity.SurfaceContract = contract
	if parts := strings.SplitN(advertisedName, "__", 2); len(parts) == 2 {
		identity.InternalToolName = parts[1]
	}
	return MappingSpec{
		ID: id, RoutePath: publicPath, PublicSurface: publicSurface, PublicSurfaceManifestSHA256: strings.Repeat("a", 64),
		ToolIdentities: []ToolIdentity{identity},
	}
}
