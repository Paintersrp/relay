package sourcevaultpolicy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const GitDiagnosticLimit = 64 << 10

// ControlledGitEnvironment removes inherited Git authority and disables
// configuration and prompts that could alter repository interpretation.
func ControlledGitEnvironment() []string {
	values := make([]string, 0, len(os.Environ())+5)
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if !ok || strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		values = append(values, value)
	}
	return append(values, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_ATTR_NOSYSTEM=1")
}

func NewGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = ControlledGitEnvironment()
	return cmd
}

func ResolveGitDirectories(ctx context.Context, source string) (string, string, error) {
	cmd := NewGitCommand(ctx, "--no-replace-objects", "-C", source, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	stdout := &limitedBuffer{limit: GitDiagnosticLimit}
	stderr := &limitedBuffer{limit: GitDiagnosticLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("resolve Git directories: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || strings.TrimSpace(lines[0]) == "" || strings.TrimSpace(lines[1]) == "" {
		return "", "", fmt.Errorf("resolve Git directories")
	}
	gitDir, err := canonicalExistingPath(strings.TrimSpace(lines[0]))
	if err != nil {
		return "", "", err
	}
	commonDir, err := canonicalExistingPath(strings.TrimSpace(lines[1]))
	if err != nil {
		return "", "", err
	}
	return gitDir, commonDir, nil
}

type limitedBuffer struct {
	limit int
	buf   bytes.Buffer
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buf.Write(value)
	}
	return original, nil
}
func (b *limitedBuffer) String() string { return strings.TrimSpace(b.buf.String()) }
