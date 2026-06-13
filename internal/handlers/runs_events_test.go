package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/events"

	"github.com/go-chi/chi/v5"
)

type nopFlusher struct{}

func (nopFlusher) Flush() {}

type sseCaptureWriter struct {
	mu     sync.Mutex
	header http.Header
	status int
	body   bytes.Buffer
}

func newSSECaptureWriter() *sseCaptureWriter {
	return &sseCaptureWriter{header: make(http.Header)}
}

func (w *sseCaptureWriter) Header() http.Header {
	return w.header
}

func (w *sseCaptureWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *sseCaptureWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func (w *sseCaptureWriter) Flush() {}

func (w *sseCaptureWriter) Body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func (w *sseCaptureWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func waitForSSEBody(t *testing.T, writer *sseCaptureWriter, want string) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		body := writer.Body()
		if strings.Contains(body, want) {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %q; body=%q", want, writer.Body())
	return ""
}

func TestWriteRunEventSSEFormatsNamedEvent(t *testing.T) {
	var buf bytes.Buffer

	event := events.RunEvent{
		RunID:  42,
		Kind:   events.KindRunConnected,
		Source: "events",
		Status: "connected",
		At:     "2026-06-13T12:34:56Z",
	}

	if err := writeRunEventSSE(&buf, nopFlusher{}, event); err != nil {
		t.Fatalf("writeRunEventSSE: %v", err)
	}

	got := buf.String()
	want := "event: run.connected\ndata: {\"run_id\":42,\"kind\":\"run.connected\",\"source\":\"events\",\"status\":\"connected\",\"at\":\"2026-06-13T12:34:56Z\"}\n\n"
	if got != want {
		t.Fatalf("unexpected SSE output:\nwant %q\ngot  %q", want, got)
	}
}

func TestRunsHandlerEventsEmitsHandshakeAndForwardsPublishedEvents(t *testing.T) {
	s := setupTestStore(t)
	hub := events.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewRunsHandler(s, slog.New(slog.NewTextHandler(io.Discard, nil)), hub)
	runID := newTestHandoff(t, s, validHandoff())

	req := httptest.NewRequest(http.MethodGet, "/runs/"+strconv.FormatInt(runID, 10)+"/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", strconv.FormatInt(runID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	writer := newSSECaptureWriter()
	done := make(chan struct{})
	go func() {
		h.Events(writer, req)
		close(done)
	}()

	body := waitForSSEBody(t, writer, "event: run.connected")
	if ct := writer.Header().Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", ct)
	}

	firstBlock := strings.SplitN(body, "\n\n", 2)[0]
	for _, forbidden := range []string{
		"event: run.summary",
		"event: step.agent",
		"event: step.validation",
		"event: step.audit",
		"event: step.commit",
		"event: step.artifacts",
		"event: toast",
	} {
		if strings.Contains(firstBlock, forbidden) {
			t.Fatalf("handshake block unexpectedly contained refresh event %q: %q", forbidden, firstBlock)
		}
	}

	hub.Publish(events.RunEvent{
		RunID:  runID,
		Kind:   events.KindStepValidation,
		Source: "validation",
		Status: "running",
		At:     "2026-06-13T12:35:00Z",
	})

	waitForSSEBody(t, writer, "event: step.validation")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Events handler to exit")
	}
}
