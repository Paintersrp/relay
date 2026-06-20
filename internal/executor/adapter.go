package executor

import (
	"time"

	"relay/internal/pipeline"
)

type AdapterID string

const (
	AdapterOpenCodeGo AdapterID = "opencode_go"
)

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
