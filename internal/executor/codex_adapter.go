package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/pipeline"
)

type CodexAdapterConfig struct {
	Binary  string
	Model   string
	Sandbox string
	Profile string
}

func NewCodexAdapterFromEnv() *CodexAdapter {
	bin := strings.TrimSpace(os.Getenv("RELAY_CODEX_BIN"))
	if bin == "" {
		bin = "codex"
	}
	sandbox := strings.TrimSpace(os.Getenv("RELAY_CODEX_SANDBOX"))
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	return &CodexAdapter{
		Config: CodexAdapterConfig{
			Binary:  bin,
			Model:   strings.TrimSpace(os.Getenv("RELAY_CODEX_MODEL")),
			Sandbox: sandbox,
			Profile: strings.TrimSpace(os.Getenv("RELAY_CODEX_PROFILE")),
		},
	}
}

type CodexAdapter struct {
	Config CodexAdapterConfig
}

func (a *CodexAdapter) ID() AdapterID {
	return AdapterCodex
}

func (a *CodexAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	if strings.TrimSpace(a.Config.Binary) == "" {
		return ExecutorInvocation{}, fmt.Errorf("Codex binary is empty")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return ExecutorInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(req.BriefContent) == "" {
		return ExecutorInvocation{}, fmt.Errorf("executor brief content is empty")
	}

	if a.Config.Sandbox != "workspace-write" && a.Config.Sandbox != "read-only" {
		return ExecutorInvocation{}, fmt.Errorf("invalid sandbox value %q, allowed are 'workspace-write' or 'read-only'", a.Config.Sandbox)
	}

	lastMessagePath := filepath.Join(artifacts.Dir(req.RunID), "codex_last_message.txt")

	args := []string{
		"exec",
		"--cd", req.RepoPath,
		"--ask-for-approval", "never",
		"--sandbox", a.Config.Sandbox,
		"--json",
		"--output-last-message", lastMessagePath,
	}

	model := a.Config.Model
	if model == "" {
		// Do not pass --model, allow Codex to use configured default.
	} else {
		args = append(args, "--model", model)
	}

	if a.Config.Profile != "" {
		args = append(args, "--profile", a.Config.Profile)
	}

	args = append(args, "-")

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
		Agent:       "codex",
		Preview:     preview,
		ResultFile:  lastMessagePath,
	}, nil
}

func (a *CodexAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	parsed := pipeline.ParseAgentResult(raw)

	res := NormalizedExecutorResult{
		Status:        parsed.Status,
		AssistantText: raw,
	}

	if parsed.Status == pipeline.AgentResultUnknown || parsed.Status == "" {
		res.Status = pipeline.AgentResultUnknown
		res.ParseError = "executor result parse failed: missing or invalid STATUS line"
		res.ExecutorResultText = fmt.Sprintf("STATUS: UNKNOWN\n\nRaw output:\n%s\n", raw)
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
