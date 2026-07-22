package mcpingress

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"relay/internal/mcp"
	"relay/internal/transport/transporttrace"
)

type proxyHandler struct {
	spec      MappingSpec
	client    *http.Client
	bearer    BearerInjector
	traces    *recoveringTraceStore
	health    *healthTracker
	log       *slog.Logger
	now       func() time.Time
	random    io.Reader
	logMu     sync.Mutex
	lastLogAt time.Time
}

type recoveringTraceStore struct {
	mu        sync.Mutex
	root      string
	mappingID string
	policy    transporttrace.RetentionPolicy
	store     *transporttrace.Store
}

func (store *recoveringTraceStore) Append(record transporttrace.Record) (int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.store == nil {
		value, err := transporttrace.NewStore(store.root, store.mappingID, store.policy)
		if err != nil {
			return 0, err
		}
		store.store = value
	}
	removed, err := store.store.Append(record)
	if err != nil {
		_ = store.store.Close()
		store.store = nil
	}
	return removed, err
}

func (store *recoveringTraceStore) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.store == nil {
		return nil
	}
	err := store.store.Close()
	store.store = nil
	return err
}

func newProxyHandler(spec MappingSpec, client *http.Client, bearer BearerInjector, traces *recoveringTraceStore, health *healthTracker, log *slog.Logger, now func() time.Time, random io.Reader) *proxyHandler {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	if random == nil {
		random = rand.Reader
	}
	return &proxyHandler{spec: spec, client: client, bearer: bearer, traces: traces, health: health, log: log, now: now, random: random}
}

func (handler *proxyHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
		handler.serveHealth(writer)
		return
	}
	if request.URL.Path != handler.spec.RoutePath {
		handler.reject(writer, request, http.StatusNotFound, transporttrace.ErrorPathNotFound)
		return
	}
	if request.Method != http.MethodPost {
		handler.reject(writer, request, http.StatusMethodNotAllowed, transporttrace.ErrorMethodNotAllowed)
		return
	}
	handler.forward(writer, request)
}

func (handler *proxyHandler) serveHealth(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(handler.health.Snapshot())
}

func (handler *proxyHandler) reject(writer http.ResponseWriter, request *http.Request, status int, class transporttrace.ErrorClass) {
	started := handler.now().UTC()
	requestID, idErr := handler.requestID()
	if idErr != nil {
		requestID = strings.Repeat("0", 32)
		class = transporttrace.ErrorInternalTransportFailure
	}
	counted := &countingReader{reader: request.Body}
	identity, _ := mcp.ObserveTraceRequest(counted)
	write, digest := writeBoundedError(writer, status)
	classification := transporttrace.Classify(identity, mcp.TraceResponseOutcome{}, status, write)
	classification.Error = class
	handler.appendRecord(started, requestID, identity, counted.Count(), write.AttemptedBytes, digest, classification, write)
}

func (handler *proxyHandler) forward(writer http.ResponseWriter, request *http.Request) {
	started := handler.now().UTC()
	requestID, err := handler.requestID()
	if err != nil {
		write, digest := writeBoundedError(writer, http.StatusInternalServerError)
		handler.appendRecord(started, strings.Repeat("0", 32), mcp.TraceRequestIdentity{}, 0, write.AttemptedBytes, digest, transporttrace.Classification{Completion: transporttrace.CompletionUnknown, Outcome: transporttrace.OutcomeApplicationFailure, Error: transporttrace.ErrorInternalTransportFailure}, write)
		return
	}
	observation := newObservedRequestBody(request.Body)
	upstreamURL := handler.spec.Upstream.URL()
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, upstreamURL.String(), observation)
	if err != nil {
		observation.Close()
		identity := observation.Result()
		write, digest := writeBoundedError(writer, http.StatusBadGateway)
		handler.appendRecord(started, requestID, identity, observation.Count(), write.AttemptedBytes, digest, transporttrace.Classification{Completion: transporttrace.CompletionUnknown, Outcome: transporttrace.OutcomeApplicationFailure, Error: transporttrace.ErrorInternalTransportFailure, Source: transporttrace.Classify(identity, mcp.TraceResponseOutcome{}, http.StatusBadGateway, write).Source}, write)
		return
	}
	upstream.ContentLength = request.ContentLength
	copyRequestHeaders(upstream.Header, request.Header)
	upstream.Host = upstreamURL.Host
	upstream.Header.Del("Authorization")
	handler.bearer.Apply(upstream)
	response, requestErr := handler.client.Do(upstream)
	identity := observation.Result()
	if requestErr != nil {
		status := http.StatusBadGateway
		class := transporttrace.ErrorUpstreamUnavailable
		if errors.Is(requestErr, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
			class = transporttrace.ErrorUpstreamTimeout
		}
		write, digest := writeBoundedError(writer, status)
		classification := transporttrace.Classify(identity, mcp.TraceResponseOutcome{}, status, write)
		classification.Completion = sourceCompletion(identity.ToolName)
		classification.Error = class
		if observation.ReadError() != nil {
			classification.Error = transporttrace.ErrorRequestReadFailed
		}
		handler.appendRecord(started, requestID, identity, observation.Count(), write.AttemptedBytes, digest, classification, write)
		return
	}
	defer response.Body.Close()
	outcome, write, responseBytes, digest, responseErr := copyUpstreamResponse(writer, response)
	classification := transporttrace.Classify(identity, outcome, response.StatusCode, write)
	if responseErr != transporttrace.ErrorNone && write.Complete {
		classification.Outcome = transporttrace.OutcomeApplicationFailure
		classification.Error = responseErr
	}
	handler.appendRecord(started, requestID, identity, observation.Count(), responseBytes, digest, classification, write)
}

func (handler *proxyHandler) appendRecord(started time.Time, requestID string, request mcp.TraceRequestIdentity, requestBytes, responseBytes int64, digest string, classification transporttrace.Classification, write transporttrace.DownstreamWrite) {
	record := transporttrace.Record{
		SchemaVersion:       transporttrace.SchemaVersion,
		RequestID:           requestID,
		StartedAt:           started.Format(time.RFC3339Nano),
		DurationMS:          maxInt64(0, handler.now().UTC().Sub(started).Milliseconds()),
		MappingID:           string(handler.spec.ID),
		RoutePath:           handler.spec.RoutePath,
		SurfaceContract:     handler.spec.SurfaceContract,
		RouteManifestSHA256: handler.spec.RouteManifestSHA256,
		JSONRPCMethod:       request.JSONRPCMethod,
		ToolName:            request.ToolName,
		OperationID:         request.OperationID,
		PacketID:            request.PacketID,
		ProjectID:           request.ProjectID,
		SourceIdentity:      classification.Source,
		RequestSizeBytes:    requestBytes,
		ResponseSizeBytes:   responseBytes,
		ResponseSHA256:      digest,
		CompletionState:     classification.Completion,
		OutcomeClass:        classification.Outcome,
		ErrorClass:          classification.Error,
		DownstreamWrite:     write,
	}
	removed, err := handler.traces.Append(record)
	if err != nil {
		handler.health.traceFailure()
		handler.logTraceFailure()
		return
	}
	handler.health.traceSuccess()
	if removed > 0 {
		handler.log.Info("MCP trace retention pruned segments", "mapping_id", handler.spec.ID, "route_path", handler.spec.RoutePath, "retention_action_count", removed)
	}
}

func (handler *proxyHandler) logTraceFailure() {
	handler.logMu.Lock()
	defer handler.logMu.Unlock()
	now := handler.now().UTC()
	if !handler.lastLogAt.IsZero() && now.Sub(handler.lastLogAt) < time.Minute {
		return
	}
	handler.lastLogAt = now
	handler.log.Warn("MCP trace persistence failed", "mapping_id", handler.spec.ID, "route_path", handler.spec.RoutePath, "error_class", transporttrace.ErrorInternalTransportFailure)
}

func (handler *proxyHandler) requestID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(handler.random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func copyUpstreamResponse(writer http.ResponseWriter, response *http.Response) (mcp.TraceResponseOutcome, transporttrace.DownstreamWrite, int64, string, transporttrace.ErrorClass) {
	copyResponseHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	reader, observer := newResponseObserver()
	defer reader.Close()
	hasher := sha256.New()
	buffer := make([]byte, 32<<10)
	var attempted int64
	var written int64
	var writeClass transporttrace.ErrorClass
	var responseClass transporttrace.ErrorClass
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			attempted += int64(count)
			_, _ = hasher.Write(chunk)
			_, _ = observer.Write(chunk)
			output, writeErr := writer.Write(chunk)
			written += int64(output)
			if writeErr != nil {
				writeClass = transporttrace.ErrorDownstreamDisconnected
				break
			}
			if output != count {
				writeClass = transporttrace.ErrorDownstreamShortWrite
				break
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				responseClass = transporttrace.ErrorInternalTransportFailure
			}
			break
		}
	}
	_ = observer.Close()
	outcome, observeErr := reader.Result()
	if observeErr != nil && responseClass == transporttrace.ErrorNone {
		responseClass = transporttrace.ErrorProtocolRejected
	}
	write := transporttrace.DownstreamWrite{AttemptedBytes: attempted, WrittenBytes: written, ErrorClass: writeClass}
	write.Complete = write.AttemptedBytes == write.WrittenBytes && write.ErrorClass == transporttrace.ErrorNone
	return outcome, write, attempted, hex.EncodeToString(hasher.Sum(nil)), responseClass
}

func writeBoundedError(writer http.ResponseWriter, status int) (transporttrace.DownstreamWrite, string) {
	message := http.StatusText(status) + "\n"
	body := []byte(message)
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	written, err := writer.Write(body)
	class := transporttrace.ErrorNone
	if err != nil {
		class = transporttrace.ErrorDownstreamDisconnected
	} else if written != len(body) {
		class = transporttrace.ErrorDownstreamShortWrite
	}
	write := transporttrace.DownstreamWrite{AttemptedBytes: int64(len(body)), WrittenBytes: int64(written), ErrorClass: class}
	write.Complete = write.AttemptedBytes == write.WrittenBytes && class == transporttrace.ErrorNone
	digest := sha256.Sum256(body)
	return write, hex.EncodeToString(digest[:])
}

func copyRequestHeaders(target, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			target.Add(name, value)
		}
	}
	removeHopHeaders(target)
	target.Del("Authorization")
}

func copyResponseHeaders(target, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			target.Add(name, value)
		}
	}
	removeHopHeaders(target)
}

func removeHopHeaders(headers http.Header) {
	if connection := headers.Get("Connection"); connection != "" {
		for _, name := range strings.Split(connection, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(name)
	}
}

func sourceCompletion(toolName string) transporttrace.CompletionState {
	switch toolName {
	case "list_source_tree", "search_source", "read_source_text", "read_source_blob", "get_source_commit", "list_source_history", "compare_source", "read_source_diff":
		return transporttrace.CompletionUnknown
	default:
		return transporttrace.CompletionNotApplicable
	}
}

type countingReader struct {
	reader io.Reader
	count  atomic.Int64
}

func (reader *countingReader) Read(value []byte) (int, error) {
	if reader.reader == nil {
		return 0, io.EOF
	}
	count, err := reader.reader.Read(value)
	reader.count.Add(int64(count))
	return count, err
}

func (reader *countingReader) Count() int64 { return reader.count.Load() }

type observedRequestBody struct {
	source  io.ReadCloser
	pipe    *io.PipeWriter
	count   atomic.Int64
	close   sync.Once
	errMu   sync.Mutex
	readErr error
	result  chan requestObservation
}

type requestObservation struct {
	identity mcp.TraceRequestIdentity
	err      error
}

func newObservedRequestBody(source io.ReadCloser) *observedRequestBody {
	if source == nil {
		source = io.NopCloser(strings.NewReader(""))
	}
	reader, writer := io.Pipe()
	result := make(chan requestObservation, 1)
	go func() {
		defer reader.Close()
		identity, err := mcp.ObserveTraceRequest(reader)
		result <- requestObservation{identity: identity, err: err}
		close(result)
	}()
	return &observedRequestBody{source: source, pipe: writer, result: result}
}

func (body *observedRequestBody) Read(value []byte) (int, error) {
	count, err := body.source.Read(value)
	if count > 0 {
		body.count.Add(int64(count))
		_, _ = body.pipe.Write(value[:count])
	}
	if err != nil {
		if err != io.EOF {
			body.errMu.Lock()
			body.readErr = err
			body.errMu.Unlock()
		}
		body.closePipe()
	}
	return count, err
}

func (body *observedRequestBody) Close() error {
	body.closePipe()
	return body.source.Close()
}

func (body *observedRequestBody) closePipe() {
	body.close.Do(func() { _ = body.pipe.Close() })
}

func (body *observedRequestBody) Result() mcp.TraceRequestIdentity {
	body.closePipe()
	result := <-body.result
	return result.identity
}

func (body *observedRequestBody) Count() int64 { return body.count.Load() }
func (body *observedRequestBody) ReadError() error {
	body.errMu.Lock()
	defer body.errMu.Unlock()
	return body.readErr
}

type responseObserver struct {
	pipe   *io.PipeWriter
	close  sync.Once
	result chan responseObservation
}

type responseObservation struct {
	outcome mcp.TraceResponseOutcome
	err     error
}

type responseObservationReader struct {
	observer *responseObserver
}

func newResponseObserver() (*responseObservationReader, *responseObserver) {
	reader, writer := io.Pipe()
	observer := &responseObserver{pipe: writer, result: make(chan responseObservation, 1)}
	go func() {
		defer reader.Close()
		outcome, err := mcp.ObserveTraceResponse(reader)
		observer.result <- responseObservation{outcome: outcome, err: err}
		close(observer.result)
	}()
	return &responseObservationReader{observer: observer}, observer
}

func (observer *responseObserver) Write(value []byte) (int, error) { return observer.pipe.Write(value) }
func (observer *responseObserver) Close() error {
	observer.close.Do(func() { _ = observer.pipe.Close() })
	return nil
}
func (reader *responseObservationReader) Result() (mcp.TraceResponseOutcome, error) {
	result := <-reader.observer.result
	return result.outcome, result.err
}
func (reader *responseObservationReader) Close() error { return reader.observer.Close() }

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
