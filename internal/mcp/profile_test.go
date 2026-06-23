package mcp

import (
	"testing"
)

func TestToolProfileFromEnvDefaultsAndPrecedence(t *testing.T) {
	t.Run("NoEnvDefaultsToLocalOperator", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "")
		t.Setenv(EnvLegacyContextBrokerEnabled, "")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected %s, got %s", ToolProfileLocalOperator, got)
		}
	})

	t.Run("CanonicalProfileRestricted", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "restricted")
		t.Setenv(EnvLegacyContextBrokerEnabled, "true") // profile takes precedence
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileRestricted {
			t.Errorf("expected %s, got %s", ToolProfileRestricted, got)
		}
	})

	t.Run("CanonicalProfileLocalOperator", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "local-operator")
		t.Setenv(EnvLegacyContextBrokerEnabled, "false") // profile takes precedence
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected %s, got %s", ToolProfileLocalOperator, got)
		}
	})

	t.Run("LegacyContextBrokerEnabledFalse", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "")
		t.Setenv(EnvLegacyContextBrokerEnabled, "false")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileRestricted {
			t.Errorf("expected %s, got %s", ToolProfileRestricted, got)
		}
	})

	t.Run("LegacyContextBrokerEnabledTrue", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "")
		t.Setenv(EnvLegacyContextBrokerEnabled, "true")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected %s, got %s", ToolProfileLocalOperator, got)
		}
	})

	t.Run("LegacyContextBrokerEnabledInvalid", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "")
		t.Setenv(EnvLegacyContextBrokerEnabled, "not-a-bool")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected default %s on invalid legacy env, got %s", ToolProfileLocalOperator, got)
		}
	})

	t.Run("CanonicalProfileInvalid", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "invalid-profile-name")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected default %s on invalid profile, got %s", ToolProfileLocalOperator, got)
		}
	})
}

func TestNewDepsFromEnvUsesCanonicalProfileForAllLaunchers(t *testing.T) {
	t.Setenv(EnvMCPProfile, "restricted")
	deps := NewDepsFromEnv(nil, nil)
	if deps.ToolProfile != ToolProfileRestricted {
		t.Errorf("expected deps to carry ToolProfileRestricted, got %s", deps.ToolProfile)
	}
}
