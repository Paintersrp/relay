package cutover

import (
	"testing"
)

func TestIntegratedGatewayConfigurationDigestIsStable(t *testing.T) {
	first, err := normalizeGatewayConfiguration(validGatewayConfigurationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeGatewayConfiguration(validGatewayConfigurationFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigurationSHA256 != second.ConfigurationSHA256 {
		t.Fatalf("configuration digest changed: %s != %s", first.ConfigurationSHA256, second.ConfigurationSHA256)
	}
}

func TestIntegratedGatewayConfigurationRejectsPublicMappingMismatch(t *testing.T) {
	configuration := validGatewayConfigurationFixture(t)
	configuration.AppSurfaceMappings[0].PublicPath = "/mcp/auditor"
	if _, err := normalizeGatewayConfiguration(configuration); err == nil {
		t.Fatal("mapping-to-public-surface mismatch was accepted")
	}
}
