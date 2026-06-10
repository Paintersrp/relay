package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
// The timeout context is created before the command, ensuring the timeout kills the child process.
func RunLocalAgentCommand(ctx context.Context, workDir string, command string, timeout time.Duration) AgentCommandRunResult {
	start := time.Now()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	}

	cmd.Dir = workDir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	finished := time.Now()

	if runCtx.Err() == context.DeadlineExceeded {
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

// RunLocalAgentCommandArgs executes a binary with args in workDir, piping optional stdin.
// The timeout context is created before the command, ensuring the timeout kills the child process.
func RunLocalAgentCommandArgs(
	ctx context.Context,
	workDir string,
	binary string,
	args []string,
	stdin string,
	timeout time.Duration,
) AgentCommandRunResult {
	start := time.Now()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, binary, args...)
	cmd.Dir = workDir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	finished := time.Now()

	commandPreview := binary
	if len(args) > 0 {
		commandPreview += " " + strings.Join(args, " ")
	}

	if runCtx.Err() == context.DeadlineExceeded {
		return AgentCommandRunResult{
			Command:    commandPreview,
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
		Command:    commandPreview,
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

// --- OpenCode adapter types ---

type OpenCodeRunConfig struct {
	Binary  string
	Agent   string
	Variant string
}

type OpenCodeRunInput struct {
	RepoPath        string
	BranchName      string
	SelectedModel   string
	AgentPromptPath string
	AgentPromptText string
	PacketPath      string
	ArtifactDir     string
}

type OpenCodeRunInvocation struct {
	Binary          string   `json:"binary"`
	Args            []string `json:"args"`
	WorkDir         string   `json:"work_dir"`
	Stdin           string   `json:"-"`
	StdinSource     string   `json:"stdin_source"`
	StdinBytes      int      `json:"stdin_bytes"`
	AgentPromptPath string   `json:"agent_prompt_path"`
	PacketPath      string   `json:"packet_path"`
	Model           string   `json:"model"`
	Agent           string   `json:"agent"`
	Variant         string   `json:"variant,omitempty"`
	Preview         string   `json:"preview"`
}

// OpenCodeRunConfigFromEnv reads configuration from environment variables.
func OpenCodeRunConfigFromEnv() OpenCodeRunConfig {
	binary := strings.TrimSpace(os.Getenv("RELAY_OPENCODE_BIN"))
	if binary == "" {
		binary = "opencode"
	}

	agent := strings.TrimSpace(os.Getenv("RELAY_OPENCODE_AGENT"))
	if agent == "" {
		agent = "build"
	}

	return OpenCodeRunConfig{
		Binary:  binary,
		Agent:   agent,
		Variant: strings.TrimSpace(os.Getenv("RELAY_OPENCODE_VARIANT")),
	}
}

// OpenCodeModelEnvSlug converts a model label to an environment variable slug.
// e.g. "DeepSeek V4 Flash" -> "DEEPSEEK_V4_FLASH"
func OpenCodeModelEnvSlug(label string) string {
	var b strings.Builder
	lastUnderscore := false

	for _, r := range strings.ToUpper(label) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(b.String(), "_")
}

// ResolveOpenCodeModel resolves a selected model to a provider/model string.
// If the model already contains "/", it is used directly.
// Otherwise, the model label is slugified and looked up in RELAY_OPENCODE_MODEL_<SLUG>.
// Human-friendly labels require RELAY_OPENCODE_MODEL_<SLUG> env mappings.
// Direct provider/model values pass through unchanged.
func ResolveOpenCodeModel(selectedModel string) (string, error) {
	selectedModel = strings.TrimSpace(selectedModel)
	if selectedModel == "" {
		return "", fmt.Errorf("selected model is empty")
	}

	if strings.Contains(selectedModel, "/") {
		return selectedModel, nil
	}

	slug := OpenCodeModelEnvSlug(selectedModel)
	key := "RELAY_OPENCODE_MODEL_" + slug
	mapped := strings.TrimSpace(os.Getenv(key))
	if mapped == "" {
		return "", fmt.Errorf("OpenCode model mapping required for selected model %q; set %s=<provider/model>", selectedModel, key)
	}
	if !strings.Contains(mapped, "/") {
		return "", fmt.Errorf("OpenCode model mapping %s must be in provider/model format", key)
	}
	return mapped, nil
}

// BuildOpenCodeRunInvocation builds the full invocation parameters for an OpenCode run.
func BuildOpenCodeRunInvocation(cfg OpenCodeRunConfig, input OpenCodeRunInput) (OpenCodeRunInvocation, error) {
	if strings.TrimSpace(cfg.Binary) == "" {
		return OpenCodeRunInvocation{}, fmt.Errorf("OpenCode binary is empty")
	}
	if strings.TrimSpace(input.RepoPath) == "" {
		return OpenCodeRunInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(input.AgentPromptPath) == "" {
		return OpenCodeRunInvocation{}, fmt.Errorf("agent prompt path is empty")
	}
	if strings.TrimSpace(input.AgentPromptText) == "" {
		return OpenCodeRunInvocation{}, fmt.Errorf("agent prompt text is empty")
	}
	if strings.TrimSpace(input.PacketPath) == "" {
		return OpenCodeRunInvocation{}, fmt.Errorf("OpenCode packet path is empty")
	}

	model, err := ResolveOpenCodeModel(input.SelectedModel)
	if err != nil {
		return OpenCodeRunInvocation{}, err
	}

	agent := strings.TrimSpace(cfg.Agent)
	if agent == "" {
		agent = "build"
	}

	args := []string{
		"run",
		"--format", "json",
		"--dir", input.RepoPath,
		"--agent", agent,
		"--model", model,
		"--thinking", "max",
	}
	if strings.TrimSpace(cfg.Variant) != "" {
		args = append(args, "--variant", strings.TrimSpace(cfg.Variant))
	}

	preview := ShellPreview(cfg.Binary, args)
	preview += " < " + quotePreview(input.AgentPromptPath)

	return OpenCodeRunInvocation{
		Binary:          cfg.Binary,
		Args:            args,
		WorkDir:         input.RepoPath,
		Stdin:           input.AgentPromptText,
		StdinSource:     input.AgentPromptPath,
		StdinBytes:      len([]byte(input.AgentPromptText)),
		AgentPromptPath: input.AgentPromptPath,
		PacketPath:      input.PacketPath,
		Model:           model,
		Agent:           agent,
		Variant:         strings.TrimSpace(cfg.Variant),
		Preview:         preview,
	}, nil
}

// ShellPreview builds a display-only shell command preview string.
func ShellPreview(binary string, args []string) string {
	parts := []string{quotePreview(binary)}
	for _, arg := range args {
		parts = append(parts, quotePreview(arg))
	}
	return strings.Join(parts, " ")
}

func quotePreview(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\n\"'") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// ExtractOpenCodeAssistantText extracts assistant text from OpenCode JSONL stdout.
// It concatenates "text" type event parts. Falls back to raw stdout if no JSON events found.
func ExtractOpenCodeAssistantText(stdout string) string {
	type event struct {
		Type string `json:"type"`
		Part struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"part"`
	}

	var texts []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == "text" && ev.Part.Text != "" {
			texts = append(texts, ev.Part.Text)
		}
	}

	if len(texts) == 0 {
		return stdout
	}
	return strings.Join(texts, "\n")
}
