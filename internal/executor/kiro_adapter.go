package executor

import (
	"fmt"
	"os"
	"strings"

	"relay/internal/pipeline"
)

type KiroCLIAdapterConfig struct {
	Binary            string
	Model             string
	Effort            string
	TrustTools        string
	RequireMCPStartup bool
	Agent             string
	AgentEngine       string
}

type KiroCLIAdapter struct {
	Config KiroCLIAdapterConfig
}

func NewKiroCLIAdapterFromEnv() *KiroCLIAdapter {
	bin := strings.TrimSpace(os.Getenv("RELAY_KIRO_BIN"))
	if bin == "" {
		bin = "kiro-cli"
	}

	requireMCP := false
	if strings.ToLower(strings.TrimSpace(os.Getenv("RELAY_KIRO_REQUIRE_MCP_STARTUP"))) == "true" {
		requireMCP = true
	}

	return &KiroCLIAdapter{
		Config: KiroCLIAdapterConfig{
			Binary:            bin,
			Model:             strings.TrimSpace(os.Getenv("RELAY_KIRO_MODEL")),
			Effort:            stringOr(os.Getenv("RELAY_KIRO_EFFORT"), "high"),
			TrustTools:        stringOr(os.Getenv("RELAY_KIRO_TRUST_TOOLS"), "fs_read,fs_write,grep"),
			RequireMCPStartup: requireMCP,
			Agent:             strings.TrimSpace(os.Getenv("RELAY_KIRO_AGENT")),
			AgentEngine:       strings.TrimSpace(os.Getenv("RELAY_KIRO_AGENT_ENGINE")),
		},
	}
}

func (a *KiroCLIAdapter) ID() AdapterID {
	return AdapterKiroCLI
}

func (a *KiroCLIAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	if strings.TrimSpace(a.Config.Binary) == "" {
		return ExecutorInvocation{}, fmt.Errorf("Kiro CLI binary is empty")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return ExecutorInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(req.BriefContent) == "" {
		return ExecutorInvocation{}, fmt.Errorf("executor brief content is empty")
	}

	model := strings.TrimSpace(a.Config.Model)
	if model != "" {
		// env override takes priority
	} else if strings.TrimSpace(req.SelectedModel) != "" {
		model = req.SelectedModel
	} else {
		model = "auto"
	}

	args := []string{
		"chat",
		"--no-interactive",
		"--wrap", "never",
		"--model", model,
		"--effort", a.Config.Effort,
		"--trust-tools=" + a.Config.TrustTools,
	}

	if a.Config.RequireMCPStartup {
		args = append(args, "--require-mcp-startup")
	}

	if a.Config.Agent != "" {
		args = append(args, "--agent", a.Config.Agent)
	}

	if a.Config.AgentEngine != "" {
		args = append(args, "--agent-engine", a.Config.AgentEngine)
	}

	preview := pipeline.ShellPreview(a.Config.Binary, args)
	preview += " < " + quotePreview(req.BriefPath)

	return ExecutorInvocation{
		Adapter:         a.ID(),
		Binary:          a.Config.Binary,
		Args:            args,
		WorkDir:         req.RepoPath,
		Stdin:           req.BriefContent,
		StdinSource:     req.BriefPath,
		StdinBytes:      len([]byte(req.BriefContent)),
		Model:           model,
		Agent:           "kiro-cli",
		Preview:         preview,
		RequireZeroExit: true,
	}, nil
}

func (a *KiroCLIAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	parsed := pipeline.ParseAgentResult(raw)

	res := NormalizedExecutorResult{
		Status:        parsed.Status,
		AssistantText: raw,
	}

	if parsed.Status == pipeline.AgentResultUnknown || parsed.Status == "" {
		res.Status = pipeline.AgentResultUnknown
		res.ParseError = "executor result parse failed: missing or invalid STATUS line"
		res.ExecutorResultText = fmt.Sprintf("STATUS: UNKNOWN\n\nRaw output:\n%s\n", boundedRaw(raw))
		return res
	}

	executorResult := fmt.Sprintf("STATUS: %s\n\nBuild status: %s\nTest status: %s\nCount of LOC changed: %s\n",
		string(parsed.Status), parsed.BuildStatus, parsed.TestStatus, parsed.LOCChanged)

	blockerText := parsed.BlockerError
	if blockerText != "" {
		executorResult += fmt.Sprintf("Blocker/error only if blocked: %s\n", blockerText)
	} else if parsed.Status == pipeline.AgentResultBlocked {
		blockerText = "kiro executor reported BLOCKED"
	}

	res.ExecutorResultText = executorResult
	res.BlockerText = blockerText

	return res
}

func boundedRaw(raw string) string {
	const maxLen = 4096
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "\n... (truncated)"
}

func stringOr(val, defaultVal string) string {
	v := strings.TrimSpace(val)
	if v == "" {
		return defaultVal
	}
	return v
}
