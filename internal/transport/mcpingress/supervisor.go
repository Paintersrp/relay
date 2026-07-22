package mcpingress

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"relay/internal/mcp"
	"relay/internal/transport/transporttrace"
)

var restartDelays = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

const (
	probeInterval      = 5 * time.Second
	probeTimeout       = 2 * time.Second
	serveResetInterval = 60 * time.Second
	shutdownTimeout    = 5 * time.Second
)

type Supervisor struct {
	mu       sync.Mutex
	mappings []*mappingRuntime
	started  bool
	cancel   context.CancelFunc
}

type supervisorDependencies struct {
	listen func(string, string) (net.Listener, error)
	now    func() time.Time
	random io.Reader
}

func NewSupervisor(config Config, log *slog.Logger) (*Supervisor, error) {
	return newSupervisor(config, log, supervisorDependencies{})
}

func newSupervisor(config Config, log *slog.Logger, dependencies supervisorDependencies) (*Supervisor, error) {
	if len(config.Mappings) != len(mappingCatalog) {
		return nil, &ConfigError{Code: "MCP_INGRESS_MAPPING_SET_INVALID", Field: "mappings"}
	}
	if err := config.Retention.Validate(); err != nil {
		return nil, &ConfigError{Code: "MCP_INGRESS_RETENTION_INVALID", Field: "retention"}
	}
	if log == nil {
		log = slog.Default()
	}
	if dependencies.listen == nil {
		dependencies.listen = net.Listen
	}
	if dependencies.now == nil {
		dependencies.now = time.Now
	}
	if dependencies.random == nil {
		dependencies.random = rand.Reader
	}
	mappings := make([]*mappingRuntime, 0, len(config.Mappings))
	for _, spec := range config.Mappings {
		traces := &recoveringTraceStore{root: config.TraceDirectory, mappingID: string(spec.ID), policy: config.Retention}
		health := newHealthTracker(spec.ID, spec.RoutePath, dependencies.now)
		mapping := &mappingRuntime{
			spec:        spec,
			bearer:      config.Bearer,
			traces:      traces,
			health:      health,
			log:         log,
			listen:      dependencies.listen,
			now:         dependencies.now,
			random:      dependencies.random,
			proxyClient: newProxyClient(),
			probeClient: newProbeClient(),
			done:        make(chan struct{}),
		}
		mapping.proxy = newProxyHandler(spec, mapping.proxyClient, config.Bearer, traces, health, log, dependencies.now, dependencies.random)
		mappings = append(mappings, mapping)
	}
	return &Supervisor{mappings: mappings}, nil
}

func (supervisor *Supervisor) Start(parent context.Context) error {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if supervisor.started {
		return fmt.Errorf("MCP ingress supervisor already started")
	}
	ctx, cancel := context.WithCancel(parent)
	supervisor.cancel = cancel
	supervisor.started = true
	for _, mapping := range supervisor.mappings {
		go mapping.run(ctx)
	}
	return nil
}

func (supervisor *Supervisor) Shutdown(ctx context.Context) error {
	supervisor.mu.Lock()
	if !supervisor.started {
		supervisor.mu.Unlock()
		return nil
	}
	cancel := supervisor.cancel
	mappings := append([]*mappingRuntime(nil), supervisor.mappings...)
	supervisor.started = false
	supervisor.cancel = nil
	supervisor.mu.Unlock()
	cancel()
	errorsByMapping := make(chan error, len(mappings))
	var wait sync.WaitGroup
	for _, mapping := range mappings {
		wait.Add(1)
		go func(value *mappingRuntime) {
			defer wait.Done()
			deadline, cancelMapping := context.WithTimeout(ctx, shutdownTimeout)
			defer cancelMapping()
			errorsByMapping <- value.shutdown(deadline)
		}(mapping)
	}
	wait.Wait()
	close(errorsByMapping)
	var combined []error
	for err := range errorsByMapping {
		if err != nil {
			combined = append(combined, err)
		}
	}
	return errors.Join(combined...)
}

func (supervisor *Supervisor) Snapshots() []HealthSnapshot {
	supervisor.mu.Lock()
	mappings := append([]*mappingRuntime(nil), supervisor.mappings...)
	supervisor.mu.Unlock()
	result := make([]HealthSnapshot, len(mappings))
	for index, mapping := range mappings {
		result[index] = mapping.health.Snapshot()
	}
	return result
}

type mappingRuntime struct {
	spec        MappingSpec
	bearer      BearerInjector
	traces      *recoveringTraceStore
	health      *healthTracker
	proxy       *proxyHandler
	proxyClient *http.Client
	probeClient *http.Client
	log         *slog.Logger
	listen      func(string, string) (net.Listener, error)
	now         func() time.Time
	random      io.Reader

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	cancel   context.CancelFunc
	done     chan struct{}
}

func (mapping *mappingRuntime) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	mapping.mu.Lock()
	mapping.cancel = cancel
	mapping.mu.Unlock()
	defer close(mapping.done)
	defer mapping.health.stopped()
	defer mapping.traces.Close()
	go mapping.probeLoop(ctx)
	delayIndex := 0
	for {
		if ctx.Err() != nil {
			return
		}
		listener, err := mapping.listen("tcp", mapping.spec.Listener.String())
		if err != nil {
			mapping.health.listener(false, transporttrace.ErrorInternalTransportFailure)
			mapping.log.Warn("MCP ingress listener unavailable", "mapping_id", mapping.spec.ID, "route_path", mapping.spec.RoutePath, "listener_address", mapping.spec.Listener.String(), "error_class", transporttrace.ErrorInternalTransportFailure)
			if !waitForDelay(ctx, restartDelays[delayIndex]) {
				return
			}
			if delayIndex < len(restartDelays)-1 {
				delayIndex++
			}
			continue
		}
		started := mapping.now()
		server := &http.Server{
			Handler:           mapping.proxy,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		mapping.mu.Lock()
		mapping.listener = listener
		mapping.server = server
		mapping.mu.Unlock()
		mapping.health.listener(true, transporttrace.ErrorNone)
		serveErr := server.Serve(listener)
		mapping.mu.Lock()
		mapping.listener = nil
		mapping.server = nil
		mapping.mu.Unlock()
		mapping.health.listener(false, transporttrace.ErrorUpstreamUnavailable)
		if ctx.Err() != nil || errors.Is(serveErr, http.ErrServerClosed) {
			return
		}
		if mapping.now().Sub(started) >= serveResetInterval {
			delayIndex = 0
		}
		mapping.log.Warn("MCP ingress listener stopped", "mapping_id", mapping.spec.ID, "route_path", mapping.spec.RoutePath, "listener_address", mapping.spec.Listener.String(), "error_class", transporttrace.ErrorUpstreamUnavailable)
		if !waitForDelay(ctx, restartDelays[delayIndex]) {
			return
		}
		if delayIndex < len(restartDelays)-1 {
			delayIndex++
		}
	}
}

func (mapping *mappingRuntime) shutdown(ctx context.Context) error {
	mapping.health.stopping()
	mapping.mu.Lock()
	cancel := mapping.cancel
	server := mapping.server
	listener := mapping.listener
	mapping.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var shutdownErr error
	if server != nil {
		shutdownErr = server.Shutdown(ctx)
	} else if listener != nil {
		shutdownErr = listener.Close()
	}
	select {
	case <-mapping.done:
	case <-ctx.Done():
		return errors.Join(shutdownErr, ctx.Err())
	}
	return shutdownErr
}

func (mapping *mappingRuntime) probeLoop(ctx context.Context) {
	mapping.probe(ctx)
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mapping.probe(ctx)
		}
	}
}

func (mapping *mappingRuntime) probe(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	body := stringsReader(`{"jsonrpc":"2.0","id":"health","method":"ping"}`)
	upstream := mapping.spec.Upstream.URL()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.String(), body)
	if err != nil {
		mapping.health.probeFailure(transporttrace.ErrorInternalTransportFailure)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	mapping.bearer.Apply(request)
	response, err := mapping.probeClient.Do(request)
	if err != nil {
		class := transporttrace.ErrorUpstreamUnavailable
		if errors.Is(err, context.DeadlineExceeded) {
			class = transporttrace.ErrorUpstreamTimeout
		}
		mapping.health.probeFailure(class)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		mapping.health.probeFailure(transporttrace.ErrorUpstreamUnavailable)
		return
	}
	var value mcp.Response
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&value); err != nil || value.Error != nil {
		mapping.health.probeFailure(transporttrace.ErrorProtocolRejected)
		return
	}
	mapping.health.probeSuccess()
}

func newProxyClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func newProbeClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	return &http.Client{
		Transport:     transport,
		Timeout:       probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func waitForDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type stringReadCloser struct{ io.Reader }

func (stringReadCloser) Close() error { return nil }
func stringsReader(value string) io.ReadCloser {
	return stringReadCloser{Reader: strings.NewReader(value)}
}
