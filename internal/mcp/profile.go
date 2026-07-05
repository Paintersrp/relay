package mcp

import (
	"log/slog"
	"os"
	"strings"
)

type ToolProfile string

const (
	ToolProfilePlanner       ToolProfile = "planner"
	ToolProfileAuditor       ToolProfile = "auditor"
	ToolProfileLocalOperator ToolProfile = "local_operator"

	EnvMCPProfile = "RELAY_MCP_PROFILE"
)

func NormalizeToolProfile(raw string) (ToolProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ToolProfilePlanner):
		return ToolProfilePlanner, true
	case string(ToolProfileAuditor):
		return ToolProfileAuditor, true
	case string(ToolProfileLocalOperator):
		return ToolProfileLocalOperator, true
	default:
		return ToolProfilePlanner, false
	}
}

func ToolProfileFromEnv(log *slog.Logger) ToolProfile {
	raw := strings.TrimSpace(os.Getenv(EnvMCPProfile))
	profile, ok := NormalizeToolProfile(raw)
	if !ok && log != nil {
		log.Warn("invalid RELAY_MCP_PROFILE value; defaulting to planner", "value", raw)
	}
	return profile
}
