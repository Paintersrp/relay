package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"relay/internal/pipeline"
)

type AntigravityAdapterConfig struct {
	Binary      string
	Model       string
	ApproveFlag string
}

type AntigravityAdapter struct {
	Config AntigravityAdapterConfig
}

func NewAntigravityAdapterFromEnv() *AntigravityAdapter {
	bin := strings.TrimSpace(os.Getenv("RELAY_ANTIGRAVITY_BIN"))
	if bin == "" {
		bin = "antigravity"
	}
	approveFlag := strings.TrimSpace(os.Getenv("RELAY_ANTIGRAVITY_APPROVE_FLAG"))
	if approveFlag == "" {
		approveFlag = "--yes"
	}
	return &AntigravityAdapter{
		Config: AntigravityAdapterConfig{
			Binary:      bin,
			Model:       strings.TrimSpace(os.Getenv("RELAY_ANTIGRAVITY_MODEL")),
			ApproveFlag: approveFlag,
		},
	}
}

func (a *AntigravityAdapter) ID() AdapterID {
	return AdapterAntigravity
}

func (a *AntigravityAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	if a.Config.Binary == "" {
		return ExecutorInvocation{}, fmt.Errorf("antigravity adapter requires a binary")
	}
	if req.RepoPath == "" {
		return ExecutorInvocation{}, fmt.Errorf("repo path cannot be empty")
	}
	if req.BriefPath == "" {
		return ExecutorInvocation{}, fmt.Errorf("brief path cannot be empty")
	}
	if req.BriefContent == "" {
		return ExecutorInvocation{}, fmt.Errorf("brief content cannot be empty")
	}

	args := []string{
		"run",
		"--prompt-file", req.BriefPath,
	}

	if a.Config.ApproveFlag != "" && !strings.EqualFold(a.Config.ApproveFlag, "none") {
		args = append(args, a.Config.ApproveFlag)
	}

	args = append(args, "--no-color", "--output", "json")

	model := a.Config.Model
	if model != "" {
		args = append(args, "--model", model)
	} else {
		model = "antigravity-config-default"
	}

	preview := pipeline.ShellPreview(a.Config.Binary, args) + " < /dev/null"

	return ExecutorInvocation{
		Adapter:         a.ID(),
		Binary:          a.Config.Binary,
		Args:            args,
		WorkDir:         req.RepoPath,
		Stdin:           "",
		StdinSource:     "/dev/null",
		StdinBytes:      0,
		Model:           model,
		Agent:           "antigravity",
		Preview:         preview,
		RequireZeroExit: true,
	}, nil
}

func (a *AntigravityAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	// 1. Try pipeline canonical status first
	if parsed := pipeline.ParseAgentResult(raw); parsed.Status == pipeline.AgentResultDone || parsed.Status == pipeline.AgentResultBlocked {
		return NormalizedExecutorResult{
			Status:             parsed.Status,
			ExecutorResultText: parsed.Raw,
		}
	}

	// 2. Parse JSON object
	var payload struct {
		Status  string `json:"status"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return NormalizedExecutorResult{
			Status:     pipeline.AgentResultUnknown,
			ParseError: fmt.Sprintf("invalid JSON output: %v", err),
		}
	}

	if strings.ToLower(payload.Status) == "success" {
		return NormalizedExecutorResult{
			Status:             pipeline.AgentResultDone,
			ExecutorResultText: "STATUS: DONE\n\nBuild status: antigravity-json-success\nTest status: not_reported\nCount of LOC changed: not_reported\n",
		}
	}

	// Non-success JSON
	blockerText := payload.Error
	if blockerText == "" {
		blockerText = payload.Message
	}
	if blockerText == "" {
		blockerText = payload.Status
	}

	return NormalizedExecutorResult{
		Status:      pipeline.AgentResultBlocked,
		BlockerText: blockerText,
	}
}
