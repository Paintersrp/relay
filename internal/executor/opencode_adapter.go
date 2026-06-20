package executor

import (
	"fmt"
	"strings"

	"relay/internal/pipeline"
)

type OpenCodeAdapter struct {
	Config pipeline.OpenCodeRunConfig
}

func NewOpenCodeAdapterFromEnv() *OpenCodeAdapter {
	cfg := pipeline.OpenCodeRunConfigFromEnv()
	if cfg.Binary == "" {
		cfg.Binary = "opencode"
	}
	return &OpenCodeAdapter{Config: cfg}
}

func (a *OpenCodeAdapter) ID() AdapterID {
	return AdapterOpenCodeGo
}

func (a *OpenCodeAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	if strings.TrimSpace(a.Config.Binary) == "" {
		return ExecutorInvocation{}, fmt.Errorf("OpenCode binary is empty; set RELAY_OPENCODE_BIN")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return ExecutorInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(req.BriefContent) == "" {
		return ExecutorInvocation{}, fmt.Errorf("executor brief content is empty")
	}
	if strings.TrimSpace(req.SelectedModel) == "" {
		return ExecutorInvocation{}, fmt.Errorf("selected model is empty")
	}

	model, err := pipeline.ResolveOpenCodeModel(req.SelectedModel)
	if err != nil {
		return ExecutorInvocation{}, err
	}

	agent := strings.TrimSpace(a.Config.Agent)
	if agent == "" {
		agent = "build"
	}

	args := []string{
		"run",
		"--format", "json",
		"--dir", req.RepoPath,
		"--agent", agent,
		"--model", model,
		"--thinking", "max",
	}

	variant := strings.TrimSpace(a.Config.Variant)
	if variant != "" {
		args = append(args, "--variant", variant)
	}

	preview := pipeline.ShellPreview(a.Config.Binary, args)
	preview += " < " + quotePreview(req.BriefPath)

	return ExecutorInvocation{
		Adapter:     a.ID(),
		Binary:      a.Config.Binary,
		Args:        args,
		WorkDir:     req.RepoPath,
		Stdin:       req.BriefContent,
		StdinSource: req.BriefPath,
		StdinBytes:  len([]byte(req.BriefContent)),
		Model:       model,
		Agent:       agent,
		Variant:     variant,
		Preview:     preview,
	}, nil
}

func (a *OpenCodeAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	assistantText := pipeline.ExtractOpenCodeAssistantText(raw)
	parsed := pipeline.ParseAgentResult(assistantText)

	res := NormalizedExecutorResult{
		Status:        parsed.Status,
		AssistantText: assistantText,
	}

	if parsed.Status == pipeline.AgentResultUnknown || parsed.Status == "" {
		res.Status = pipeline.AgentResultUnknown
		res.ParseError = "executor result parse failed: missing or invalid STATUS line"
		res.ExecutorResultText = fmt.Sprintf("STATUS: UNKNOWN\n\nRaw output:\n%s\n", assistantText)
		return res
	}

	executorResult := fmt.Sprintf("STATUS: %s\n\nBuild status: %s\nTest status: %s\nCount of LOC changed: %s\n",
		string(parsed.Status), parsed.BuildStatus, parsed.TestStatus, parsed.LOCChanged)

	blockerText := parsed.BlockerError
	if blockerText != "" {
		executorResult += fmt.Sprintf("Blocker/error only if blocked: %s\n", blockerText)
	} else if parsed.Status == pipeline.AgentResultBlocked {
		blockerText = "executor reported BLOCKED"
	}

	res.ExecutorResultText = executorResult
	res.BlockerText = blockerText

	return res
}

// quotePreview escapes strings for the preview command line
func quotePreview(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"'") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
