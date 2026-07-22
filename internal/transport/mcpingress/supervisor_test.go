package mcpingress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSupervisorIsolatesBindFailureAndRecoversMapping(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"health","result":{}}`))
	}))
	defer upstream.Close()
	addresses := make([]string, len(mappingCatalog))
	var blocker net.Listener
	for index := range addresses {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addresses[index] = listener.Addr().String()
		if index == 0 {
			blocker = listener
		} else {
			_ = listener.Close()
		}
	}
	environment := map[string]string{traceDirectoryEnv: t.TempDir()}
	for index, entry := range mappingCatalog {
		environment[entry.ListenerEnv] = addresses[index]
	}
	config, err := LoadConfig(func(name string) string { return environment[name] }, upstream.URL, testRouteDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewSupervisor(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := supervisor.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitForHTTP(t, "http://"+addresses[1]+"/healthz")
	first := supervisor.Snapshots()[0]
	if first.State != HealthUnhealthy || first.ListenerReady {
		t.Fatalf("blocked mapping health=%#v", first)
	}
	second := supervisor.Snapshots()[1]
	if !second.ListenerReady {
		t.Fatalf("independent mapping did not start: %#v", second)
	}
	_ = blocker.Close()
	waitForHTTP(t, "http://"+addresses[0]+"/healthz")
	if err := supervisor.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func waitForHTTP(t *testing.T, target string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(target)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("endpoint did not become ready: %s", target)
}

func TestCatalogHasNoDynamicOrAggregateMapping(t *testing.T) {
	for _, entry := range mappingCatalog {
		if strings.Contains(entry.RoutePath, "{") || entry.RoutePath == "/mcp" || entry.ID == "" || entry.ListenerEnv == "" {
			t.Fatalf("invalid catalog entry: %#v", entry)
		}
	}
	if len(mappingCatalog) != 7 {
		t.Fatalf("catalog entries=%d", len(mappingCatalog))
	}
	_ = fmt.Sprint(mappingCatalog)
}
