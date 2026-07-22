package cutover

import (
	"context"
	"errors"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestStateInertPrepared(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := svc.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no current activation in fresh store")
	}
	closed, err := svc.IsLegacyAdmissionClosed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("legacy admission must be open before activation")
	}
}

func TestNormalizeGatewayConfigurationRequiresExactRouteAndStandingSets(t *testing.T) {
	configuration := validGatewayConfigurationFixture()
	normalized, err := normalizeGatewayConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if !validLowerHex(normalized.ConfigurationSHA256, 64) {
		t.Fatalf("configuration digest = %q", normalized.ConfigurationSHA256)
	}
	configuration.Routes = configuration.Routes[:6]
	if _, err := normalizeGatewayConfiguration(configuration); !errors.Is(err, ErrCutoverConfigurationInvalid) {
		t.Fatalf("six-route configuration error = %v", err)
	}
}

func validGatewayConfigurationFixture() GatewayConfigurationIdentity {
	routes := []RouteIdentity{
		{1, "/mcp/v1/wayfinder/workspace", "wayfinder", "wayfinder-workspace.v1", strings.Repeat("a", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{2, "/mcp/v1/wayfinder/discovery", "wayfinder", "wayfinder-discovery.v1", strings.Repeat("d", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{3, "/mcp/v1/wayfinder/investigation", "wayfinder", "wayfinder-investigation.v1", strings.Repeat("e", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{4, "/mcp/v1/planner/authoring", "planner", "planner-authoring.v1", strings.Repeat("f", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{5, "/mcp/v1/planner/frontier", "planner", "planner-ticket-frontier.v1", strings.Repeat("1", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{6, "/mcp/v1/auditor/review", "auditor", "auditor-review.v1", strings.Repeat("2", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
		{7, "/mcp/v1/auditor/audit", "auditor", "auditor-audit.v1", strings.Repeat("3", 64), strings.Repeat("b", 40), strings.Repeat("c", 40)},
	}
	mappingIDs := []string{"wayfinder-workspace", "wayfinder-discovery", "wayfinder-investigation", "planner-authoring", "planner-frontier", "auditor-review", "auditor-audit"}
	mappings := make([]MappingIdentity, 0, len(routes))
	for index, route := range routes {
		mappings = append(mappings, MappingIdentity{
			Sequence: int64(index + 1), MappingID: mappingIDs[index], RoutePath: route.RoutePath,
			ListenerIdentity:     "127.0.0.1:1810" + string(rune('1'+index)),
			UpstreamIdentity:     "http://127.0.0.1:8080" + route.RoutePath,
			HealthEvidenceSHA256: strings.Repeat("4", 64), TraceEvidenceSHA256: strings.Repeat("5", 64),
		})
	}
	return GatewayConfigurationIdentity{
		RelayRepository: "Paintersrp/relay", RelayCommitOID: strings.Repeat("6", 40),
		StandingRepository: "Paintersrp/relay-specs", StandingCommitOID: strings.Repeat("7", 40),
		Routes: routes, Mappings: mappings,
		StandingAuthorities: []StandingAuthorityIdentity{
			{"auditor", "Paintersrp/relay-specs", strings.Repeat("7", 40), "agents/auditor.md", strings.Repeat("8", 40), strings.Repeat("9", 64)},
			{"planner", "Paintersrp/relay-specs", strings.Repeat("7", 40), "agents/planner.md", strings.Repeat("a", 40), strings.Repeat("b", 64)},
			{"wayfinder", "Paintersrp/relay-specs", strings.Repeat("7", 40), "agents/wayfinder.md", strings.Repeat("c", 40), strings.Repeat("d", 64)},
		},
		DependencyOutcomes: []DependencyOutcomeIdentity{
			{1, "CURRENT-MCP-SURFACES", 2, "completed_accepted", strings.Repeat("e", 64)},
			{2, "PRIVATE-TRANSPORT-TRACE", 2, "completed_accepted", strings.Repeat("f", 64)},
			{3, "STANDING-AUTHORITY", 2, "completed_accepted", strings.Repeat("1", 64)},
		},
	}
}

func TestLegacyGateAllowsBeforeActivation(t *testing.T) {
	store, teardown := testStore(t)
	defer teardown()
	svc, _ := NewService(store)
	gate := NewLegacyGate(svc)
	decision, err := gate.AllowNewPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("expected legacy gate to allow before activation")
	}
}

func testStore(t *testing.T) (*workflowstore.Store, func()) {
	t.Helper()
	store, err := workflowstore.Open("file::memory:?cache=shared", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, func() {
		store.Close()
	}
}
