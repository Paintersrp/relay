package executor

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"relay/internal/pipeline"
)

type KiroCLIAdapterConfig struct {
	Binary            string
	Model             string
	Effort            string
	TrustTools        string
	RequireMCPStartup bool
	Agent             string
	AgentEngine       string
}

const defaultKiroTrustTools = "fs_read,fs_write,grep,execute_cmd"

var supportedKiroModels = map[string]struct{}{
	"auto":              {},
	"claude-opus-4.8":   {},
	"claude-opus-4.7":   {},
	"claude-opus-4.6":   {},
	"claude-sonnet-4.6": {},
	"claude-opus-4.5":   {},
	"claude-sonnet-4.5": {},
	"claude-sonnet-4":   {},
	"claude-haiku-4.5":  {},
	"deepseek-3.2":      {},
	"minimax-m2.5":      {},
	"minimax-m2.1":      {},
	"glm-5":             {},
	"qwen3-coder-next":  {},
}

type KiroCLIAdapter struct {
	Config KiroCLIAdapterConfig
}

func NewKiroCLIAdapterFromEnv() *KiroCLIAdapter {
	bin := strings.TrimSpace(os.Getenv("RELAY_KIRO_BIN"))
	if bin == "" {
		bin = "kiro-cli"
	}

	requireMCP := false
	if strings.ToLower(strings.TrimSpace(os.Getenv("RELAY_KIRO_REQUIRE_MCP_STARTUP"))) == "true" {
		requireMCP = true
	}

	return &KiroCLIAdapter{
		Config: KiroCLIAdapterConfig{
			Binary:            bin,
			Model:             stringOr(os.Getenv("RELAY_KIRO_DEFAULT_MODEL"), strings.TrimSpace(os.Getenv("RELAY_KIRO_MODEL"))),
			Effort:            stringOr(os.Getenv("RELAY_KIRO_EFFORT"), "high"),
			TrustTools:        stringOr(os.Getenv("RELAY_KIRO_TRUST_TOOLS"), defaultKiroTrustTools),
			RequireMCPStartup: requireMCP,
			Agent:             strings.TrimSpace(os.Getenv("RELAY_KIRO_AGENT")),
			AgentEngine:       strings.TrimSpace(os.Getenv("RELAY_KIRO_AGENT_ENGINE")),
		},
	}
}

func (a *KiroCLIAdapter) ID() AdapterID {
	return AdapterKiroCLI
}

func (a *KiroCLIAdapter) BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error) {
	if strings.TrimSpace(a.Config.Binary) == "" {
		return ExecutorInvocation{}, fmt.Errorf("Kiro CLI binary is empty")
	}
	if strings.TrimSpace(req.RepoPath) == "" {
		return ExecutorInvocation{}, fmt.Errorf("repo path is empty")
	}
	if strings.TrimSpace(req.BriefContent) == "" {
		return ExecutorInvocation{}, fmt.Errorf("executor brief content is empty")
	}

	model := strings.TrimSpace(req.SelectedModel)
	if model == "" {
		model = strings.TrimSpace(a.Config.Model)
	}
	if model == "" {
		model = "auto"
	}
	if _, ok := supportedKiroModels[model]; !ok {
		return ExecutorInvocation{}, fmt.Errorf("unsupported Kiro model %q", model)
	}

	args := []string{
		"chat",
		"--no-interactive",
		"--wrap", "never",
		"--model", model,
		"--effort", a.Config.Effort,
		"--trust-tools=" + a.Config.TrustTools,
	}

	if a.Config.RequireMCPStartup {
		args = append(args, "--require-mcp-startup")
	}

	if a.Config.Agent != "" {
		args = append(args, "--agent", a.Config.Agent)
	}

	if a.Config.AgentEngine != "" {
		args = append(args, "--agent-engine", a.Config.AgentEngine)
	}

	preview := pipeline.ShellPreview(a.Config.Binary, args)
	preview += " < " + quotePreview(req.BriefPath)

	return ExecutorInvocation{
		Adapter:         a.ID(),
		Binary:          a.Config.Binary,
		Args:            args,
		WorkDir:         req.RepoPath,
		Stdin:           req.BriefContent,
		StdinSource:     req.BriefPath,
		StdinBytes:      len([]byte(req.BriefContent)),
		Model:           model,
		Agent:           "kiro-cli",
		Preview:         preview,
		RequireZeroExit: true,
	}, nil
}

func (a *KiroCLIAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	normalizedRaw := normalizeKiroHeadlessOutput(raw)
	parsed := pipeline.ParseAgentResult(normalizedRaw)

	res := NormalizedExecutorResult{
		Status:        parsed.Status,
		AssistantText: raw,
	}

	if parsed.Status == pipeline.AgentResultUnknown || parsed.Status == "" {
		res.Status = pipeline.AgentResultUnknown
		res.ParseError = "executor result parse failed: missing or invalid STATUS line"
		res.ExecutorResultText = fmt.Sprintf("STATUS: UNKNOWN\n\nRaw output:\n%s\n", boundedRaw(raw))
		return res
	}

	executorResult := fmt.Sprintf("STATUS: %s\n\nBuild status: %s\nTest status: %s\nCount of LOC changed: %s\n",
		string(parsed.Status), parsed.BuildStatus, parsed.TestStatus, parsed.LOCChanged)

	blockerText := parsed.BlockerError
	if blockerText != "" {
		executorResult += fmt.Sprintf("Blocker/error only if blocked: %s\n", blockerText)
	} else if parsed.Status == pipeline.AgentResultBlocked {
		blockerText = "kiro executor reported BLOCKED"
	}

	res.ExecutorResultText = executorResult
	res.BlockerText = blockerText

	return res
}

func normalizeKiroHeadlessOutput(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		// First strip ANSI/terminal control sequences
		line = stripANSIText(line)
		// Then remove prompt prefix
		trimmedLeft := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmedLeft, ">") {
			lines[i] = strings.TrimLeft(strings.TrimPrefix(trimmedLeft, ">"), " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// stripANSIText is a local copy of pipeline.stripANSI for use in the adapter
// before it calls into pipeline.ParseAgentResult (which also strips ANSI).
func stripANSIText(text string) string {
	// CSI sequence: ESC [ followed by optional params and a final letter
	text = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(text, "")
	// OSC sequence: ESC ] (0-9 or :) followed by content and BEL or ESC \
	text = regexp.MustCompile(`\x1b\][0-9:;]*[^\x07\x1b]`).ReplaceAllString(text, "")
	// Remove any remaining ESC bytes that aren't part of valid sequences
	text = strings.ReplaceAll(text, "\x1b", "")
	return text
}

func boundedRaw(raw string) string {
	const maxLen = 4096
	if len(raw) <= maxLen {
		return raw
	}
	return raw[:maxLen] + "\n... (truncated)"
}

func stringOr(val, defaultVal string) string {
	v := strings.TrimSpace(val)
	if v == "" {
		return defaultVal
	}
	return v
}

// ExecutorUsageTelemetry captures executor usage telemetry (Kiro-only for now).
type ExecutorUsageTelemetry struct {
	Provider     string   `json:"provider"`
	Adapter      string   `json:"adapter"`
	CreditsText  string   `json:"creditsText"`
	Credits      *float64 `json:"credits,omitempty"`
	SourceStream string   `json:"sourceStream,omitempty"`
	Model        string   `json:"model,omitempty"`
	RawLine      string   `json:"rawLine,omitempty"`
}

// extractKiroUsageTelemetry parses Kiro credit usage from normalized stdout/stderr.
// It prefers stdout first, then stderr, and returns no telemetry if no safe usage line is found.
func extractKiroUsageTelemetry(stdout, stderr, model string) (ExecutorUsageTelemetry, bool) {
	// Try stdout first
	if tel, ok := extractKiroCreditsFromStream(stdout, "stdout", model); ok {
		return tel, true
	}
	// Fallback to stderr
	if tel, ok := extractKiroCreditsFromStream(stderr, "stderr", model); ok {
		return tel, true
	}
	return ExecutorUsageTelemetry{}, false
}

// extractKiroCreditsFromStream searches for credit usage in a single stream.
func extractKiroCreditsFromStream(text, sourceStream, model string) (ExecutorUsageTelemetry, bool) {
	if text == "" {
		return ExecutorUsageTelemetry{}, false
	}

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		// Normalize: strip ANSI and prompt prefixes
		line = stripANSIText(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Remove prompt prefix
		trimmed = strings.TrimLeft(trimmed, " \t")
		if strings.HasPrefix(trimmed, ">") {
			trimmed = strings.TrimLeft(strings.TrimPrefix(trimmed, ">"), " \t")
		}

		// Look for credit-related lines (case-insensitive)
		lower := strings.ToLower(trimmed)
		if !strings.Contains(lower, "credit") {
			continue
		}

		// Try to extract a numeric value
		creditsText, credits := extractCreditValue(trimmed)
		if creditsText == "" {
			continue
		}

		return ExecutorUsageTelemetry{
			Provider:     "kiro",
			Adapter:      "kiro_cli",
			CreditsText:  creditsText,
			Credits:      credits,
			SourceStream: sourceStream,
			Model:        model,
			RawLine:      trimmed,
		}, true
	}

	return ExecutorUsageTelemetry{}, false
}

// extractCreditValue extracts the credit display text and numeric value from a line.
// Examples: "Credit cost: 0.05" -> ("0.05", 0.05)
func extractCreditValue(line string) (string, *float64) {
	// Find numbers in the line (decimal or integer)
	// Match patterns like "0.05", "1.23", "42"
	numPattern := regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	matches := numPattern.FindAllString(line, -1)

	if len(matches) == 0 {
		return "", nil
	}

	// Use the last number in the line (typically the credit value)
	lastMatch := matches[len(matches)-1]
	val, err := strconv.ParseFloat(lastMatch, 64)
	if err != nil {
		return lastMatch, nil
	}

	return lastMatch, &val
}
