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

func TestToolProfileAuditNormalization(t *testing.T) {
	t.Run("ExactAudit", func(t *testing.T) {
		profile, ok := NormalizeToolProfile("audit")
		if profile != ToolProfileAudit {
			t.Errorf("expected ToolProfileAudit, got %s", profile)
		}
		if !ok {
			t.Error("expected ok=true for valid audit profile")
		}
	})
	t.Run("CaseInsensitiveAudit", func(t *testing.T) {
		profile, ok := NormalizeToolProfile("AUDIT")
		if profile != ToolProfileAudit {
			t.Errorf("expected ToolProfileAudit, got %s", profile)
		}
		if !ok {
			t.Error("expected ok=true for AUDIT")
		}
	})
	t.Run("WhitespaceTrimmedAudit", func(t *testing.T) {
		profile, ok := NormalizeToolProfile("  audit  ")
		if profile != ToolProfileAudit {
			t.Errorf("expected ToolProfileAudit, got %s", profile)
		}
		if !ok {
			t.Error("expected ok=true for trimmed audit")
		}
	})
}

func TestToolProfileAuditFromEnv(t *testing.T) {
	t.Run("RELAY_MCP_PROFILE_audit", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "audit")
		t.Setenv(EnvLegacyContextBrokerEnabled, "true") // profile takes precedence
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileAudit {
			t.Errorf("expected ToolProfileAudit, got %s", got)
		}
	})
	t.Run("AuditProfilePrecedenceOverContextBroker", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "audit")
		t.Setenv(EnvLegacyContextBrokerEnabled, "true")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileAudit {
			t.Errorf("expected audit profile to take precedence over context broker env, got %s", got)
		}
	})
}

func TestToolProfileAuditContextBrokerDisabled(t *testing.T) {
	if ToolProfileAudit.ContextBrokerEnabled() {
		t.Error("expected audit profile to disable context broker")
	}
	if ToolProfileRestricted.ContextBrokerEnabled() {
		t.Error("expected restricted profile to disable context broker")
	}
	if !ToolProfileLocalOperator.ContextBrokerEnabled() {
		t.Error("expected local-operator profile to enable context broker")
	}
}

func TestInvalidProfileDefaultsToLocalOperator(t *testing.T) {
	profile, ok := NormalizeToolProfile("invalid-profile-name")
	if profile != ToolProfileLocalOperator {
		t.Errorf("expected default ToolProfileLocalOperator, got %s", profile)
	}
	if ok {
		t.Error("expected ok=false for unknown profile")
	}
	t.Run("UnknownEnvDefaults", func(t *testing.T) {
		t.Setenv(EnvMCPProfile, "invalid-profile-name")
		got := ToolProfileFromEnv(nil)
		if got != ToolProfileLocalOperator {
			t.Errorf("expected default %s on invalid profile, got %s", ToolProfileLocalOperator, got)
		}
	})
}
