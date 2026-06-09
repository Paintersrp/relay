package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
	"unicode"
)

const DefaultValidationCommandTimeout = 2 * time.Minute

type CommandRunResult struct {
	Label      string `json:"label"`
	Command    string `json:"command"`
	Source     string `json:"source"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	TimedOut   bool   `json:"timed_out"`
	DurationMS int64  `json:"duration_ms"`
}

func RunValidationCommand(ctx context.Context, repoPath string, cmd ValidationCommand, timeout time.Duration) CommandRunResult {
	start := time.Now()

	args, err := splitCommand(cmd.Command)
	if err != nil {
		return CommandRunResult{
			Label:      cmd.Label,
			Command:    cmd.Command,
			Source:     cmd.Source,
			ExitCode:   -1,
			Stdout:     "",
			Stderr:     fmt.Sprintf("failed to parse command: %v", err),
			TimedOut:   false,
			DurationMS: time.Since(start).Milliseconds(),
		}
	}

	if len(args) == 0 {
		return CommandRunResult{
			Label:      cmd.Label,
			Command:    cmd.Command,
			Source:     cmd.Source,
			ExitCode:   -1,
			Stdout:     "",
			Stderr:     "empty command",
			TimedOut:   false,
			DurationMS: time.Since(start).Milliseconds(),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdoutBuf, stderrBuf bytes.Buffer

	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = repoPath
	c.Stdout = &stdoutBuf
	c.Stderr = &stderrBuf

	err = c.Run()

	elapsed := time.Since(start).Milliseconds()

	if ctx.Err() == context.DeadlineExceeded {
		return CommandRunResult{
			Label:      cmd.Label,
			Command:    cmd.Command,
			Source:     cmd.Source,
			ExitCode:   -2,
			Stdout:     stdoutBuf.String(),
			Stderr:     stderrBuf.String(),
			TimedOut:   true,
			DurationMS: elapsed,
		}
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return CommandRunResult{
		Label:      cmd.Label,
		Command:    cmd.Command,
		Source:     cmd.Source,
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		TimedOut:   false,
		DurationMS: elapsed,
	}
}

func splitCommand(input string) ([]string, error) {
	var args []string
	var current bytes.Buffer
	inSingle := false
	inDouble := false
	escaped := false

	for i, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' && inDouble {
			escaped = true
			continue
		}

		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if !inSingle && !inDouble && unicode.IsSpace(r) {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)

		// Reject unclosed quotes at end of input
		if i == len(input)-1 {
			if inSingle || inDouble {
				return nil, fmt.Errorf("unclosed quote in command: %s", input)
			}
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
