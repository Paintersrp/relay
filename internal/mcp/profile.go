package mcp

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type ToolProfile string

const (
	ToolProfileLocalOperator ToolProfile = "local-operator"
	ToolProfileRestricted    ToolProfile = "restricted"
	ToolProfileAudit         ToolProfile = "audit"

	EnvMCPProfile                 = "RELAY_MCP_PROFILE"
	EnvLegacyContextBrokerEnabled = "RELAY_MCP_CONTEXT_BROKER_ENABLED"
)

func NormalizeToolProfile(raw string) (ToolProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ToolProfileLocalOperator):
		return ToolProfileLocalOperator, true
	case string(ToolProfileRestricted):
		return ToolProfileRestricted, true
	case string(ToolProfileAudit):
		return ToolProfileAudit, true
	default:
		return ToolProfileLocalOperator, false
	}
}

func ToolProfileFromEnv(log *slog.Logger) ToolProfile {
	if raw := strings.TrimSpace(os.Getenv(EnvMCPProfile)); raw != "" {
		profile, ok := NormalizeToolProfile(raw)
		if !ok && log != nil {
			log.Warn("invalid RELAY_MCP_PROFILE value; defaulting to local-operator", "value", raw)
		}
		return profile
	}

	if raw := strings.TrimSpace(os.Getenv(EnvLegacyContextBrokerEnabled)); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			if log != nil {
				log.Warn("invalid RELAY_MCP_CONTEXT_BROKER_ENABLED value; defaulting to local-operator", "value", raw, "error", err)
			}
			return ToolProfileLocalOperator
		}
		if enabled {
			return ToolProfileLocalOperator
		}
		return ToolProfileRestricted
	}

	return ToolProfileLocalOperator
}

func (p ToolProfile) ContextBrokerEnabled() bool {
	profile, ok := NormalizeToolProfile(string(p))
	if !ok {
		profile = ToolProfileLocalOperator
	}
	return profile == ToolProfileLocalOperator
}
