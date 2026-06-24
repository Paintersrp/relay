package validationrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type sealedCommand struct {
	ID              string
	Command         string
	Required        bool
	Purpose         string
	SuccessSignal   string
	FailureHandling string
}

type commandOutput struct {
	stdout       string
	stderr       string
	exitCode     int
	startedAt    string
	finishedAt   string
	durationMs   int64
	workdir      string
	notRunReason string
}

var envSecretPatterns = []string{
	"RELAY_OPENCODE_BIN",
	"RELAY_OPENCODE_AGENT",
	"RELAY_OPENCODE_VARIANT",
	"RELAY_OPENCODE_API_KEY",
	"RELAY_OPENCODE_SECRET",
	"OPENAI_API_KEY",
	"ANTHROPIC_API_KEY",
}

// commonRedactTokens are literal strings that should always be redacted from output.
var commonRedactTokens = []string{
	"sk-",
}

func redactSensitiveFromOutput(data string) string {
	result := data
	for _, key := range envSecretPatterns {
		val := strings.TrimSpace(os.Getenv(key))
		if val == "" {
			continue
		}
		result = strings.ReplaceAll(result, val, "[REDACTED]")
	}
	for _, token := range commonRedactTokens {
		result = strings.ReplaceAll(result, token, "[REDACTED]")
	}
	return result
}

func runCommand(ctx context.Context, cmd sealedCommand, workdir string) commandOutput {
	out := commandOutput{
		workdir: workdir,
	}

	startedAt := nowUTC()
	out.startedAt = startedAt

	if strings.TrimSpace(cmd.Command) == "" {
		out.exitCode = -1
		out.notRunReason = "empty command"
		out.finishedAt = nowUTC()
		return out
	}

	cmdCtx, cancel := context.WithTimeout(ctx, DefaultCommandTimeout)
	defer cancel()

	startTime := time.Now()

	var stdoutBuf, stderrBuf bytes.Buffer

	shell := shellCommand(cmd.Command)
	c := exec.CommandContext(cmdCtx, shell[0], shell[1:]...)
	c.Dir = workdir
	c.Stdout = &stdoutBuf
	c.Stderr = &stderrBuf

	err := c.Run()

	elapsed := time.Since(startTime)
	out.durationMs = elapsed.Milliseconds()
	out.finishedAt = nowUTC()

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			out.exitCode = -1
			out.notRunReason = "timeout"
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			out.exitCode = exitErr.ExitCode()
		} else {
			out.exitCode = -1
			out.notRunReason = err.Error()
		}
	} else {
		out.exitCode = 0
	}

	out.stdout = redactSensitiveFromOutput(stdoutBuf.String())
	out.stderr = redactSensitiveFromOutput(stderrBuf.String())

	return out
}

func runAndCapture(ctx context.Context, cmd sealedCommand, workdir string) commandOutput {
	return runCommand(ctx, cmd, workdir)
}

// shellCommand returns the argv used to execute a validation command through
// the host's default shell. Windows uses cmd /C; every other platform uses
// the POSIX shell so commands run consistently in CI and on developer machines.
func shellCommand(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/C", command}
	}
	return []string{"sh", "-c", command}
}

func writeArtifactFile(path, content string) error {
	if content == "" {
		content = "[empty output]"
	}
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

func buildCommandOutput(out commandOutput) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Status: %s\n", statusFromExit(out.exitCode, out.notRunReason)))
	b.WriteString(fmt.Sprintf("Exit code: %d\n", out.exitCode))
	if out.durationMs >= 0 {
		b.WriteString(fmt.Sprintf("Duration: %dms\n", out.durationMs))
	}
	b.WriteString(fmt.Sprintf("Workdir: %s\n", out.workdir))
	if out.notRunReason != "" {
		b.WriteString(fmt.Sprintf("Not run reason: %s\n", out.notRunReason))
	}
	return b.String()
}

func statusFromExit(code int, reason string) string {
	if reason != "" {
		return "fail"
	}
	if code == 0 {
		return "pass"
	}
	return "fail"
}
