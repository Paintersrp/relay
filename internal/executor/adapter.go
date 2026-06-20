package executor

import (
	"fmt"
	"time"

	"relay/internal/pipeline"
)

type AdapterID string

const (
	AdapterOpenCodeGo AdapterID = "opencode_go"
	AdapterCodex      AdapterID = "codex"
	AdapterAntigravity AdapterID = "antigravity"
)

func NormalizeAdapterID(id string) string {
	switch id {
	case "opencode", "opencode_go", "":
		return string(AdapterOpenCodeGo)
	case "codex":
		return string(AdapterCodex)
	case "agy", "antigravity":
		return string(AdapterAntigravity)
	default:
		return id
	}
}

func NewAdapterFromID(id string) (ExecutorAdapter, error) {
	norm := NormalizeAdapterID(id)
	switch AdapterID(norm) {
	case AdapterOpenCodeGo:
		return NewOpenCodeAdapterFromEnv(), nil
	case AdapterCodex:
		return nil, fmt.Errorf("executor adapter %q is not implemented", norm)
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
