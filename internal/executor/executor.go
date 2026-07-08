package executor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// NewOwnerInstanceID generates a process-owner identity used to tag runtime
// state written by this Relay server instance.
func NewOwnerInstanceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "relay-owner"
	}
	return "relay-" + hex.EncodeToString(b[:])
}

var knownSecrets = []string{
	"RELAY_OPENCODE_BIN", "RELAY_OPENCODE_AGENT", "RELAY_OPENCODE_VARIANT",
	"RELAY_CODEX_BIN", "RELAY_CODEX_MODEL", "RELAY_CODEX_PROFILE", "OPENAI_API_KEY",
	"RELAY_ANTIGRAVITY_BIN", "RELAY_ANTIGRAVITY_MODEL", "RELAY_ANTIGRAVITY_APPROVE_FLAG", "ANTIGRAVITY_API_KEY",
	"RELAY_KIRO_BIN", "RELAY_KIRO_MODEL", "RELAY_KIRO_EFFORT", "RELAY_KIRO_TRUST_TOOLS",
	"RELAY_KIRO_AGENT", "RELAY_KIRO_AGENT_ENGINE", "KIRO_API_KEY",
}

func redactSensitive(input string) string {
	result := input
	for _, key := range knownSecrets {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			result = strings.ReplaceAll(result, value, "[REDACTED]")
		}
	}
	return result
}

func RedactSensitiveText(value string) string { return redactSensitive(value) }

func RedactSensitiveBytes(value []byte) []byte { return redactSensitiveBytes(value) }

func redactSensitiveBytes(value []byte) []byte { return []byte(redactSensitive(string(value))) }

func defaultExecutorPreflight(inv ExecutorInvocation) ExecutorPreflightResult {
	result := ExecutorPreflightResult{OK: true, Adapter: inv.Adapter, Binary: inv.Binary, WorkDir: inv.WorkDir, CommandPreview: inv.Preview, Checks: []ExecutorPreflightCheck{}}
	add := func(name string, ok bool, detail string) {
		result.Checks = append(result.Checks, ExecutorPreflightCheck{Name: name, OK: ok, Detail: detail})
		if !ok && result.OK {
			result.OK = false
			result.BlockerText = detail
		}
	}
	if inv.Binary == "" {
		add("binary_configured", false, "executor binary is not configured")
	} else if filepath.IsAbs(inv.Binary) || strings.ContainsAny(inv.Binary, `/\`) {
		info, err := os.Stat(inv.Binary)
		if err != nil {
			add("binary_available", false, fmt.Sprintf("executor binary not found at %s", inv.Binary))
		} else if info.IsDir() {
			add("binary_available", false, fmt.Sprintf("executor binary is a directory at %s", inv.Binary))
		} else {
			add("binary_available", true, "binary found")
		}
	} else if _, err := exec.LookPath(inv.Binary); err != nil {
		add("binary_available", false, fmt.Sprintf("executor binary %s not found in PATH", inv.Binary))
	} else {
		add("binary_available", true, "binary found in PATH")
	}

	if inv.WorkDir == "" {
		add("workdir_configured", false, "workdir is not configured")
	} else if info, err := os.Stat(inv.WorkDir); err != nil {
		add("workdir_available", false, fmt.Sprintf("workdir not found: %s", inv.WorkDir))
	} else if !info.IsDir() {
		add("workdir_available", false, fmt.Sprintf("workdir is not a directory: %s", inv.WorkDir))
	} else {
		add("workdir_available", true, "workdir exists")
	}

	if inv.StdinSource == "" || inv.StdinSource == "/dev/null" {
		add("stdin_source", true, "no stdin source required")
	} else if info, err := os.Stat(inv.StdinSource); err != nil {
		add("stdin_source", false, fmt.Sprintf("stdin source not found: %s", inv.StdinSource))
	} else if info.IsDir() {
		add("stdin_source", false, fmt.Sprintf("stdin source is a directory: %s", inv.StdinSource))
	} else {
		add("stdin_source", true, "stdin source found")
	}

	if inv.Preview == "" {
		add("command_preview", false, "command preview is empty")
	} else {
		add("command_preview", true, "command preview present")
	}
	return result
}