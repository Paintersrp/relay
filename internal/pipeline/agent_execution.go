package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// AgentCommandContext holds values for command template rendering.
type AgentCommandContext struct {
	RepoPath         string
	BranchName       string
	SelectedModel    string
	RecommendedModel string
	AgentPromptPath  string
	PacketPath       string
	ArtifactDir      string
}

// RenderAgentCommandTemplate replaces placeholders in a command template.
// Empty template returns error.
// Unknown placeholder returns error.
// Missing required value returns error when its placeholder is used.
func RenderAgentCommandTemplate(template string, ctx AgentCommandContext) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("command template is empty")
	}

	placeholders := map[string]string{
		"{{repo_path}}":         ctx.RepoPath,
		"{{branch_name}}":       ctx.BranchName,
		"{{selected_model}}":    ctx.SelectedModel,
		"{{recommended_model}}": ctx.RecommendedModel,
		"{{agent_prompt_path}}": ctx.AgentPromptPath,
		"{{packet_path}}":       ctx.PacketPath,
		"{{artifact_dir}}":      ctx.ArtifactDir,
	}

	result := template
	for placeholder, value := range placeholders {
		if strings.Contains(result, placeholder) {
			if value == "" {
				return "", fmt.Errorf("placeholder %s requires a value but it is empty", placeholder)
			}
			result = strings.ReplaceAll(result, placeholder, value)
		}
	}

	// Check for unknown placeholders (anything matching {{...}})
	remaining := result
	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(remaining[start:], "}}")
		if end < 0 {
			break
		}
		unknown := remaining[start : start+end+2]
		return "", fmt.Errorf("unknown placeholder: %s", unknown)
	}

	return result, nil
}

// AgentCommandRunResult holds the result of running a local agent command.
type AgentCommandRunResult struct {
	Command    string    `json:"command"`
	WorkDir    string    `json:"work_dir"`
	ExitCode   int       `json:"exit_code"`
	Stdout     string    `json:"stdout"`
	Stderr     string    `json:"stderr"`
	TimedOut   bool      `json:"timed_out"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

const DefaultAgentCommandTimeout = 30 * time.Minute

// RunLocalAgentCommand executes a command string in workDir via the platform shell.
func RunLocalAgentCommand(ctx context.Context, workDir string, command string, timeout time.Duration) AgentCommandRunResult {
	start := time.Now()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}

	cmd.Dir = workDir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := cmd.Run()
	finished := time.Now()

	if ctx.Err() == context.DeadlineExceeded {
		return AgentCommandRunResult{
			Command:    command,
			WorkDir:    workDir,
			ExitCode:   -2,
			Stdout:     stdoutBuf.String(),
			Stderr:     stderrBuf.String(),
			TimedOut:   true,
			StartedAt:  start,
			FinishedAt: finished,
		}
	}

	exitCode := 0
	errMsg := ""
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			errMsg = err.Error()
		}
	}

	return AgentCommandRunResult{
		Command:    command,
		WorkDir:    workDir,
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		TimedOut:   false,
		Error:      errMsg,
		StartedAt:  start,
		FinishedAt: finished,
	}
}