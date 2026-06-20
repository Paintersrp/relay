package executor

import (
	"fmt"
	"strings"
	"time"

	"relay/internal/pipeline"
)

type AdapterID string

const (
	AdapterOpenCodeGo  AdapterID = "opencode_go"
	AdapterCodex       AdapterID = "codex"
	AdapterAntigravity AdapterID = "antigravity"
)

func NormalizeKnownAdapterID(id string) (string, error) {
	id = strings.TrimSpace(id)
	id = strings.ToLower(id)
	switch id {
	case "opencode", "opencode_go", "":
		return string(AdapterOpenCodeGo), nil
	case "codex":
		return string(AdapterCodex), nil
	case "agy", "antigravity":
		return string(AdapterAntigravity), nil
	default:
		return "", fmt.Errorf("invalid executor adapter %q", id)
	}
}

func IsKnownAdapterID(id string) bool {
	_, err := NormalizeKnownAdapterID(id)
	return err == nil
}

func NormalizeAdapterID(id string) string {
	if norm, err := NormalizeKnownAdapterID(id); err == nil {
		return norm
	}
	return id
}

func NewAdapterFromID(id string) (ExecutorAdapter, error) {
	norm, err := NormalizeKnownAdapterID(id)
	if err != nil {
		return nil, fmt.Errorf("unknown executor adapter %q", id)
	}
	switch AdapterID(norm) {
	case AdapterOpenCodeGo:
		return NewOpenCodeAdapterFromEnv(), nil
	case AdapterCodex:
		return NewCodexAdapterFromEnv(), nil
	case AdapterAntigravity:
		return nil, fmt.Errorf("executor adapter %q is not implemented", norm)
	default:
		return nil, fmt.Errorf("unknown executor adapter %q", id)
	}
}

type ExecutorInvocation struct {
	Adapter     AdapterID
	Binary      string
	Args        []string
	WorkDir     string
	Stdin       string
	StdinSource string
	StdinBytes  int
	Model       string
	Agent       string
	Variant     string
	Preview     string
	ResultFile  string
}

type ExecutorAdapterRequest struct {
	RunID         int64
	RepoPath      string
	BriefContent  string
	BriefPath     string
	SelectedModel string
	Timeout       time.Duration
}

type NormalizedExecutorResult struct {
	Status             pipeline.AgentResultStatus
	AssistantText      string
	ExecutorResultText string
	BlockerText        string
	ParseError         string
}

type ExecutorAdapter interface {
	ID() AdapterID
	BuildInvocation(req ExecutorAdapterRequest) (ExecutorInvocation, error)
	NormalizeResult(raw string) NormalizedExecutorResult
}
