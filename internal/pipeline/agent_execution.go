package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
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
	Command       string    `json:"command"`
	WorkDir       string    `json:"work_dir"`
	ExitCode      int       `json:"exit_code"`
	Stdout        string    `json:"stdout"`
	Stderr        string    `json:"stderr"`
	TimedOut      bool      `json:"timed_out"`
	Error         string    `json:"error,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	LaunchStarted bool      `json:"launch_started"`

	LaunchDisposition AgentLaunchDisposition `json:"launch_disposition"`
	ProcessIdentity   ProcessIdentity        `json:"process_identity,omitempty"`
	IdentityAvailable bool                   `json:"identity_available"`

	TerminationVerified bool   `json:"termination_verified"`
	TerminationError    string `json:"termination_error,omitempty"`
}

type AgentCommandWaitResult struct {
	Err          error
	ExitCode     int
	ProcessState string
}

func waitForCommandBounded(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("wait timed out after %s", timeout)
	}
}

type AgentCommandStreamCallbacks struct {
	OnStartCalled         func()
	OnStartReturned       func(pid int)
	OnProcessStarted      func(identity ProcessIdentity) error
	OnStartError          func(err error)
	OnLaunchSettled       func(disposition AgentLaunchDisposition)
	OnStdoutReaderStarted func()
	OnStdoutReaderDone    func(err error)
	OnStderrReaderStarted func()
	OnStderrReaderDone    func(err error)
	OnWaitStarted         func()
	OnWaitReturned        func(result AgentCommandWaitResult)
	OnStdout              func(chunk []byte)
	OnStderr              func(chunk []byte)
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
	return RunLocalAgentCommandArgsStreaming(ctx, workDir, binary, args, stdin, timeout, AgentCommandStreamCallbacks{})
}

// RunLocalAgentCommandArgsStreaming executes a binary with args in workDir and streams stdout/stderr chunks.
// The timeout context is created before the command, ensuring the timeout kills the child process.
func RunLocalAgentCommandArgsStreaming(
	ctx context.Context,
	workDir string,
	binary string,
	args []string,
	stdin string,
	timeout time.Duration,
	callbacks AgentCommandStreamCallbacks,
) AgentCommandRunResult {
	return RunLocalAgentCommandArgsStreamingWithController(ctx, workDir, binary, args, stdin, timeout, callbacks, DefaultProcessController())
}

func RunLocalAgentCommandArgsStreamingWithController(
	ctx context.Context,
	workDir string,
	binary string,
	args []string,
	stdin string,
	timeout time.Duration,
	callbacks AgentCommandStreamCallbacks,
	controller ProcessController,
) (runResult AgentCommandRunResult) {
	start := time.Now()
	commandPreview := binary
	if len(args) > 0 {
		commandPreview += " " + strings.Join(args, " ")
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if controller == nil {
		controller = DefaultProcessController()
	}

	if err := runCtx.Err(); err != nil {
		finished := time.Now()
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(AgentLaunchNotStarted)
		}
		return AgentCommandRunResult{
			Command:    commandPreview,
			WorkDir:    workDir,
			ExitCode:   -1,
			Error:      err.Error(),
			StartedAt:  start,
			FinishedAt: finished,
		}
	}

	if callbacks.OnStartCalled != nil {
		callbacks.OnStartCalled()
	}
	owned, err := controller.StartOwned(runCtx, CommandSpec{
		WorkDir: workDir,
		Binary:  binary,
		Args:    args,
		Stdin:   stdin,
		Timeout: timeout,
	})
	if owned == nil && err != nil {
		if callbacks.OnStartError != nil {
			callbacks.OnStartError(err)
		}
		finished := time.Now()
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(AgentLaunchNotStarted)
		}
		return AgentCommandRunResult{
			Command:           commandPreview,
			WorkDir:           workDir,
			ExitCode:          -1,
			Error:             err.Error(),
			StartedAt:         start,
			FinishedAt:        finished,
			LaunchDisposition: AgentLaunchNotStarted,
		}
	}
	if owned == nil {
		finished := time.Now()
		errMsg := "process controller returned no owned process"
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(AgentLaunchNotStarted)
		}
		return AgentCommandRunResult{
			Command:           commandPreview,
			WorkDir:           workDir,
			ExitCode:          -1,
			Error:             errMsg,
			StartedAt:         start,
			FinishedAt:        finished,
			LaunchDisposition: AgentLaunchNotStarted,
		}
	}
	defer func() {
		if owned != nil {
			if releaseErr := owned.Release(); releaseErr != nil {
				runResult.Error = appendErrorText(runResult.Error, "release owned process: "+releaseErr.Error())
				if runResult.ExitCode == 0 {
					runResult.ExitCode = -1
				}
			}
		}
	}()
	launchStarted := true
	identity := owned.Identity()
	identityAvailable := ValidateProcessIdentity(identity) == nil
	if callbacks.OnStartReturned != nil {
		callbacks.OnStartReturned(identity.PID)
	}
	if err != nil {
		if callbacks.OnStartError != nil {
			callbacks.OnStartError(err)
		}
		registerErr := error(nil)
		if callbacks.OnProcessStarted != nil && identityAvailable {
			registerErr = callbacks.OnProcessStarted(identity)
		}
		var ownedStartErr *OwnedStartError
		cleanup := ProcessTerminationResult{}
		cleanupErr := error(nil)
		if errors.As(err, &ownedStartErr) {
			cleanup = ownedStartErr.Cleanup
			cleanupErr = ownedStartErr.CleanupError
		} else {
			termination, termErr := owned.Terminate(2 * time.Second)
			cleanup = termination
			cleanupErr = termErr
		}
		finished := time.Now()
		errMsg := err.Error()
		if registerErr != nil {
			errMsg = appendErrorText(errMsg, "register post-create process identity: "+registerErr.Error())
		}
		if cleanupErr != nil && !errors.Is(cleanupErr, ErrProcessNotRunning) {
			errMsg = appendErrorText(errMsg, "cleanup post-create process tree: "+cleanupErr.Error())
		} else if !cleanup.VerifiedAbsent {
			errMsg = appendErrorText(errMsg, "cleanup post-create process tree: absence was not verified")
		}
		disposition := AgentLaunchCleanupPending
		if cleanup.VerifiedAbsent && (cleanupErr == nil || errors.Is(cleanupErr, ErrProcessNotRunning)) {
			disposition = AgentLaunchCleanupVerified
		}
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(disposition)
		}
		return AgentCommandRunResult{
			Command:             commandPreview,
			WorkDir:             workDir,
			ExitCode:            -1,
			Error:               errMsg,
			StartedAt:           start,
			FinishedAt:          finished,
			LaunchStarted:       launchStarted,
			LaunchDisposition:   disposition,
			ProcessIdentity:     identity,
			IdentityAvailable:   identityAvailable,
			TerminationVerified: cleanup.VerifiedAbsent,
			TerminationError:    errMsg,
		}
	}
	if callbacks.OnProcessStarted != nil {
		if err := callbacks.OnProcessStarted(identity); err != nil {
			if callbacks.OnStartError != nil {
				callbacks.OnStartError(err)
			}
			termination, terminateErr := owned.Terminate(2 * time.Second)
			finished := time.Now()
			errMsg := err.Error()
			if terminateErr != nil && !errors.Is(terminateErr, ErrProcessNotRunning) {
				errMsg += "; terminate unregistered process tree: " + terminateErr.Error()
			} else if !termination.VerifiedAbsent {
				errMsg += "; terminate unregistered process tree: absence was not verified"
			}
			if callbacks.OnLaunchSettled != nil {
				callbacks.OnLaunchSettled(dispositionForTermination(termination.VerifiedAbsent))
			}
			return AgentCommandRunResult{
				Command:             commandPreview,
				WorkDir:             workDir,
				ExitCode:            -1,
				Error:               errMsg,
				StartedAt:           start,
				FinishedAt:          finished,
				LaunchStarted:       launchStarted,
				LaunchDisposition:   dispositionForTermination(termination.VerifiedAbsent),
				ProcessIdentity:     identity,
				IdentityAvailable:   identityAvailable,
				TerminationVerified: termination.VerifiedAbsent,
				TerminationError:    errMsg,
			}
		}
	}
	stdoutPipe := owned.Stdout()
	stderrPipe := owned.Stderr()
	if stdoutPipe == nil || stderrPipe == nil {
		finished := time.Now()
		errMsg := "owned process did not provide stdout/stderr pipes"
		termination, terminateErr := owned.Terminate(2 * time.Second)
		if terminateErr != nil {
			errMsg += "; terminate pipe-less process tree: " + terminateErr.Error()
		}
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(dispositionForTermination(termination.VerifiedAbsent))
		}
		return AgentCommandRunResult{
			Command:             commandPreview,
			WorkDir:             workDir,
			ExitCode:            -1,
			Error:               errMsg,
			StartedAt:           start,
			FinishedAt:          finished,
			LaunchStarted:       launchStarted,
			LaunchDisposition:   dispositionForTermination(termination.VerifiedAbsent),
			ProcessIdentity:     identity,
			IdentityAvailable:   identityAvailable,
			TerminationVerified: termination.VerifiedAbsent,
			TerminationError:    errMsg,
		}
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	type streamReadResult struct {
		err error
	}

	streamResults := make(chan streamReadResult, 2)
	var wg sync.WaitGroup

	readStream := func(pipe io.Reader, buf *bytes.Buffer, callback func([]byte), done func(error)) {
		defer wg.Done()

		reader := bufio.NewReader(pipe)
		for {
			chunk, err := reader.ReadBytes('\n')
			if len(chunk) > 0 {
				buf.Write(chunk)
				if callback != nil {
					callback(append([]byte(nil), chunk...))
				}
			}
			if err != nil {
				if done != nil {
					done(err)
				}
				if err == io.EOF {
					streamResults <- streamReadResult{}
				} else {
					streamResults <- streamReadResult{err: err}
				}
				return
			}
		}
	}

	wg.Add(2)
	go func() {
		if callbacks.OnStdoutReaderStarted != nil {
			callbacks.OnStdoutReaderStarted()
		}
		readStream(stdoutPipe, &stdoutBuf, callbacks.OnStdout, callbacks.OnStdoutReaderDone)
	}()
	go func() {
		if callbacks.OnStderrReaderStarted != nil {
			callbacks.OnStderrReaderStarted()
		}
		readStream(stderrPipe, &stderrBuf, callbacks.OnStderr, callbacks.OnStderrReaderDone)
	}()

	if callbacks.OnWaitStarted != nil {
		callbacks.OnWaitStarted()
	}
	waitDone := make(chan struct{})
	terminationCh := make(chan struct {
		result ProcessTerminationResult
		err    error
	}, 1)
	go func() {
		select {
		case <-runCtx.Done():
		case <-waitDone:
			return
		}
		result, err := owned.Terminate(2 * time.Second)
		terminationCh <- struct {
			result ProcessTerminationResult
			err    error
		}{result: result, err: err}
	}()
	waitErr := owned.Wait()
	close(waitDone)
	var terminationErr error
	terminationVerified := true
	if runCtx.Err() != nil {
		termination := <-terminationCh
		terminationErr = termination.err
		terminationVerified = termination.result.VerifiedAbsent
	} else {
		running, treeErr := owned.TreeRunning()
		if treeErr != nil {
			terminationErr = treeErr
			terminationVerified = false
		} else if running {
			termination := struct {
				result ProcessTerminationResult
				err    error
			}{}
			termination.result, termination.err = owned.Terminate(2 * time.Second)
			terminationErr = termination.err
			terminationVerified = termination.result.VerifiedAbsent
			if !terminationVerified && terminationErr == nil {
				terminationErr = fmt.Errorf("%w: process tree still running after root exit", ErrProcessUnverifiable)
			}
		} else {
			terminationVerified = true
		}
	}
	cancel()
	exitCode := 0
	errMsg := ""
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if exitErr, ok := waitErr.(interface{ ExitCode() int }); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			errMsg = waitErr.Error()
		}
	}
	if callbacks.OnWaitReturned != nil {
		callbacks.OnWaitReturned(AgentCommandWaitResult{
			Err:          waitErr,
			ExitCode:     exitCode,
			ProcessState: "",
		})
	}
	wg.Wait()
	close(streamResults)

	var readErrors []string
	for result := range streamResults {
		if result.err != nil {
			readErrors = append(readErrors, result.err.Error())
		}
	}

	finished := time.Now()

	if runCtx.Err() == context.DeadlineExceeded {
		errText := ""
		if terminationErr != nil && !errors.Is(terminationErr, ErrProcessNotRunning) {
			errText = "terminate timed out process tree: " + terminationErr.Error()
		} else if !terminationVerified {
			errText = "terminate timed out process tree: absence was not verified"
		}
		if callbacks.OnLaunchSettled != nil {
			callbacks.OnLaunchSettled(dispositionForTermination(terminationVerified))
		}
		return AgentCommandRunResult{
			Command:             commandPreview,
			WorkDir:             workDir,
			ExitCode:            -2,
			Stdout:              stdoutBuf.String(),
			Stderr:              stderrBuf.String(),
			TimedOut:            true,
			Error:               errText,
			StartedAt:           start,
			FinishedAt:          finished,
			LaunchStarted:       launchStarted,
			LaunchDisposition:   dispositionForTermination(terminationVerified),
			ProcessIdentity:     identity,
			IdentityAvailable:   identityAvailable,
			TerminationVerified: terminationVerified,
			TerminationError:    errText,
		}
	}

	if terminationErr != nil && !errors.Is(terminationErr, ErrProcessNotRunning) {
		if errMsg != "" {
			errMsg += "; "
		}
		errMsg += "terminate canceled process tree: " + terminationErr.Error()
		if exitCode == 0 {
			exitCode = -1
		}
	}
	if !terminationVerified {
		if errMsg != "" {
			errMsg += "; "
		}
		errMsg += "terminate canceled process tree: absence was not verified"
		if exitCode == 0 {
			exitCode = -1
		}
	}

	if len(readErrors) > 0 {
		readErrMsg := "stream read errors: " + strings.Join(readErrors, "; ")
		if errMsg != "" {
			errMsg += "; " + readErrMsg
		} else {
			errMsg = readErrMsg
			if exitCode == 0 {
				exitCode = -1
			}
		}
	}

	if callbacks.OnLaunchSettled != nil {
		callbacks.OnLaunchSettled(AgentLaunchOwned)
	}
	return AgentCommandRunResult{
		Command:             commandPreview,
		WorkDir:             workDir,
		ExitCode:            exitCode,
		Stdout:              stdoutBuf.String(),
		Stderr:              stderrBuf.String(),
		TimedOut:            false,
		Error:               errMsg,
		StartedAt:           start,
		FinishedAt:          finished,
		LaunchStarted:       launchStarted,
		LaunchDisposition:   AgentLaunchOwned,
		ProcessIdentity:     identity,
		IdentityAvailable:   identityAvailable,
		TerminationVerified: terminationVerified,
	}
}

func dispositionForTermination(verified bool) AgentLaunchDisposition {
	if verified {
		return AgentLaunchCleanupVerified
	}
	return AgentLaunchCleanupPending
}

func appendErrorText(base, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return extra
	}
	return base + "; " + extra
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

// OpenCodeFailureHint returns a user-facing hint string based on a failed run result.
// It inspects stderr and stdout for common failure patterns and returns actionable messages.
// Returns empty string if no specific hint applies.
func OpenCodeFailureHint(result AgentCommandRunResult, invocation OpenCodeRunInvocation) string {
	stderr := strings.ToLower(result.Stderr)
	stdout := strings.ToLower(result.Stdout)
	combined := stderr + " " + stdout

	if result.TimedOut {
		return "OpenCode timed out. The command may be taking longer than expected, or the model may be unavailable."
	}

	if result.Error != "" {
		errLower := strings.ToLower(result.Error)
		if strings.Contains(errLower, "executable file not found") ||
			strings.Contains(errLower, "not recognized") ||
			strings.Contains(errLower, "no such file") {
			return "OpenCode binary not found. Check RELAY_OPENCODE_BIN or install opencode."
		}
	}

	if strings.Contains(combined, "executable file not found") ||
		strings.Contains(combined, "not recognized") ||
		strings.Contains(combined, "no such file") {
		return "OpenCode binary not found. Check RELAY_OPENCODE_BIN or install opencode."
	}

	if strings.Contains(combined, "auth") ||
		strings.Contains(combined, "unauthorized") ||
		strings.Contains(combined, "401") ||
		strings.Contains(combined, "api key") ||
		strings.Contains(combined, "connect") {
		return "OpenCode auth may be missing or expired. Run `opencode`, then `/connect`, then `opencode models`."
	}

	if strings.Contains(combined, "model") &&
		(strings.Contains(combined, "not found") ||
			strings.Contains(combined, "unknown model") ||
			strings.Contains(combined, "404")) {
		return "OpenCode model may be unavailable. Run `opencode models` and confirm the resolved model appears."
	}

	if result.ExitCode > 0 {
		return "OpenCode exited with code " + fmt.Sprintf("%d", result.ExitCode) + ". Review stderr and combined log artifacts."
	}

	return ""
}

// StreamProgress tracks the live streaming activity from stdout/stderr callbacks.
type StreamProgress struct {
	StdoutChunks  int64     `json:"stdout_chunks"`
	StderrChunks  int64     `json:"stderr_chunks"`
	StdoutBytes   int64     `json:"stdout_bytes"`
	StderrBytes   int64     `json:"stderr_bytes"`
	LastStdoutAt  string    `json:"last_stdout_at,omitempty"`
	LastStderrAt  string    `json:"last_stderr_at,omitempty"`
	LastChunkAt   string    `json:"last_chunk_at,omitempty"`
	lastChunkTime time.Time `json:"-"`
}

// UpdateStreamProgressFromStdout records a stdout chunk in the progress tracker.
func (sp *StreamProgress) UpdateStreamProgressFromStdout(chunk []byte) {
	sp.StdoutChunks++
	sp.StdoutBytes += int64(len(chunk))
	sp.lastChunkTime = time.Now()
	now := sp.lastChunkTime.Format(time.RFC3339Nano)
	sp.LastStdoutAt = now
	sp.LastChunkAt = now
}

// UpdateStreamProgressFromStderr records a stderr chunk in the progress tracker.
func (sp *StreamProgress) UpdateStreamProgressFromStderr(chunk []byte) {
	sp.StderrChunks++
	sp.StderrBytes += int64(len(chunk))
	sp.lastChunkTime = time.Now()
	now := sp.lastChunkTime.Format(time.RFC3339Nano)
	sp.LastStderrAt = now
	sp.LastChunkAt = now
}

// StreamProgressLastChunkAge returns the duration since the last chunk was received.
// Returns -1 if no chunks have been received.
func (sp *StreamProgress) StreamProgressLastChunkAge() time.Duration {
	if sp.lastChunkTime.IsZero() {
		return -1
	}
	return time.Since(sp.lastChunkTime)
}

func OpenCodePermissionWarning(stderr string) string {
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "permission requested:") ||
		strings.Contains(lower, "auto-rejecting") ||
		strings.Contains(lower, "external_directory") ||
		strings.Contains(lower, "permission denied") {
		return "OpenCode requested a permission that was denied. Review stderr or the combined log."
	}
	return ""
}

// ExtractOpenCodeAssistantText extracts assistant text from OpenCode JSONL stdout.
// It concatenates "text" type event parts. Falls back to raw stdout if no JSON events found.
// OpenCodeTranscriptEvent represents a single parsed line from OpenCode JSONL output.
type OpenCodeTranscriptEvent struct {
	Kind string
	Text string
}

// BuildOpenCodeTranscript parses OpenCode JSONL stdout into display events.
// Known event types: reasoning, tool_use, tool, text, step_start, step_finish.
// Invalid JSON lines become Kind "raw". Stderr lines become Kind "stderr".
// If maxEvents > 0, returns the last maxEvents events.
func BuildOpenCodeTranscript(stdout string, stderr string, maxEvents int) []OpenCodeTranscriptEvent {
	var events []OpenCodeTranscriptEvent

	// Parse stderr lines
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		events = append(events, OpenCodeTranscriptEvent{Kind: "stderr", Text: line})
	}

	// Parse stdout JSONL
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var raw struct {
			Type string `json:"type"`
			Part struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Tool  string `json:"tool"`
				Name  string `json:"name"`
				State struct {
					Status string `json:"status"`
					Input  struct {
						FilePath string `json:"filePath"`
					} `json:"input"`
				} `json:"state"`
				Reason string `json:"reason"`
			} `json:"part"`
		}

		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			events = append(events, OpenCodeTranscriptEvent{Kind: "raw", Text: line})
			continue
		}

		switch raw.Type {
		case "reasoning":
			events = append(events, OpenCodeTranscriptEvent{Kind: "reasoning", Text: raw.Part.Text})
		case "text":
			events = append(events, OpenCodeTranscriptEvent{Kind: "text", Text: raw.Part.Text})
		case "tool_use", "tool":
			toolName := raw.Part.Tool
			if toolName == "" {
				toolName = raw.Part.Name
			}
			status := raw.Part.State.Status
			filePath := raw.Part.State.Input.FilePath
			var displayText string
			if toolName != "" {
				displayText = toolName
				if filePath != "" {
					displayText += " " + filePath
				}
				if status != "" {
					displayText += " " + status
				}
			} else {
				displayText = line
			}
			events = append(events, OpenCodeTranscriptEvent{Kind: "tool", Text: displayText})
		case "step_start":
			events = append(events, OpenCodeTranscriptEvent{Kind: "step", Text: "started"})
		case "step_finish":
			reason := raw.Part.Reason
			text := "finished"
			if reason != "" {
				text += ": " + reason
			}
			events = append(events, OpenCodeTranscriptEvent{Kind: "step", Text: text})
		default:
			// Show unknown event types with as much context as possible
			extra := ""
			if raw.Part.Tool != "" {
				extra = " tool=" + raw.Part.Tool
			}
			if raw.Part.Name != "" {
				extra += " name=" + raw.Part.Name
			}
			if raw.Part.Reason != "" {
				extra += " reason=" + raw.Part.Reason
			}
			if raw.Part.Text != "" {
				text := raw.Part.Text
				if len(text) > 120 {
					text = text[:120] + "..."
				}
				extra += " text=" + text
			}
			if raw.Part.State.Status != "" {
				extra += " status=" + raw.Part.State.Status
			}
			if extra != "" {
				events = append(events, OpenCodeTranscriptEvent{Kind: "event", Text: raw.Type + extra})
			} else {
				events = append(events, OpenCodeTranscriptEvent{Kind: "event", Text: raw.Type})
			}
		}
	}

	if maxEvents > 0 && len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}
	return events
}

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
