package cutover

import (
	"testing"
)

func TestIntegratedGatewayConfigurationDigestIsStable(t *testing.T) {
	first, err := normalizeGatewayConfiguration(validGatewayConfigurationFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeGatewayConfiguration(validGatewayConfigurationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigurationSHA256 != second.ConfigurationSHA256 {
		t.Fatalf("configuration digest changed: %s != %s", first.ConfigurationSHA256, second.ConfigurationSHA256)
	}
}

func TestIntegratedGatewayConfigurationRejectsMappingRouteMismatch(t *testing.T) {
	configuration := validGatewayConfigurationFixture()
	configuration.Mappings[0].RoutePath = "/mcp/v1/auditor/audit"
	if _, err := normalizeGatewayConfiguration(configuration); err == nil {
		t.Fatal("mapping-to-route mismatch was accepted")
	}
}
