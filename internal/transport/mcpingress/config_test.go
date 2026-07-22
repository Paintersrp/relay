package mcpingress

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"relay/internal/transport/transporttrace"
)

func TestLoadConfigBuildsExactSevenPrivateMappings(t *testing.T) {
	environment := map[string]string{}
	config, err := LoadConfig(func(name string) string { return environment[name] }, "http://127.0.0.1:8080", testRouteDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Mappings) != 7 || config.Retention.MaxAge != 14*24*time.Hour || config.Retention.MaxBytes != 100<<20 {
		t.Fatalf("config=%#v", config)
	}
	for index, entry := range mappingCatalog {
		mapping := config.Mappings[index]
		if mapping.ID != entry.ID || mapping.RoutePath != entry.RoutePath || mapping.Listener.String() != entry.DefaultAddress || mapping.Upstream.URL().Path != entry.RoutePath {
			t.Fatalf("mapping[%d]=%#v", index, mapping)
		}
	}
}

func TestLoadConfigRejectsUnsafeListenersAndWeakenedRetention(t *testing.T) {
	for _, value := range []string{"0.0.0.0:18101", "example.com:18101", "8.8.8.8:18101", "169.254.1.1:18101", "127.0.0.1:0", ":18101"} {
		t.Run(strings.ReplaceAll(value, ":", "_"), func(t *testing.T) {
			environment := map[string]string{mappingCatalog[0].ListenerEnv: value}
			if _, err := LoadConfig(func(name string) string { return environment[name] }, "http://127.0.0.1:8080", testRouteDescriptors()); err == nil {
				t.Fatalf("unsafe listener %q accepted", value)
			}
		})
	}
	environment := map[string]string{traceMaxAgeEnv: "337h"}
	if _, err := LoadConfig(func(name string) string { return environment[name] }, "http://127.0.0.1:8080", testRouteDescriptors()); err == nil {
		t.Fatal("retention age above the ceiling was accepted")
	}
	environment = map[string]string{traceMaxBytesEnv: fmt.Sprint(transporttrace.DefaultMaxBytes + 1)}
	if _, err := LoadConfig(func(name string) string { return environment[name] }, "http://127.0.0.1:8080", testRouteDescriptors()); err == nil {
		t.Fatal("retention bytes above the ceiling were accepted")
	}
}

func TestBearerInjectorDoesNotRenderSecret(t *testing.T) {
	injector := NewBearerInjector("top-secret-value")
	if !injector.Configured() {
		t.Fatal("configured bearer was not reported")
	}
	for _, rendered := range []string{fmt.Sprint(injector), fmt.Sprintf("%+v", injector), fmt.Sprintf("%#v", injector)} {
		if strings.Contains(rendered, "top-secret-value") {
			t.Fatalf("secret rendered: %s", rendered)
		}
	}
}

func testRouteDescriptors() []RouteDescriptor {
	result := make([]RouteDescriptor, len(mappingCatalog))
	for index, entry := range mappingCatalog {
		result[index] = RouteDescriptor{
			MappingID:           entry.ID,
			RoutePath:           entry.RoutePath,
			SurfaceContract:     fmt.Sprintf("surface-%d.v1", index+1),
			RouteManifestSHA256: strings.Repeat(fmt.Sprintf("%x", index+1), 64),
		}
	}
	return result
}
