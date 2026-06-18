package validationrunner

import "time"

const DefaultCommandTimeout = 5 * time.Minute

type RunStatus string

const (
	StatusPassed  RunStatus = "pass"
	StatusFailed  RunStatus = "fail"
	StatusSkipped RunStatus = "skipped"
	StatusError   RunStatus = "error"
)

type CommandResult struct {
	ID           string `json:"id"`
	Command      string `json:"command"`
	Required     bool   `json:"required"`
	Status       string `json:"status"`
	ExitCode     int    `json:"exitCode"`
	DurationMs   int64  `json:"durationMs"`
	Workdir      string `json:"workdir"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	StdoutKind   string `json:"stdoutKind"`
	StderrKind   string `json:"stderrKind"`
	NotRunReason string `json:"notRunReason,omitempty"`
}

type ValidationRun struct {
	RunID        int64           `json:"runId"`
	Status       RunStatus       `json:"status"`
	StartedAt    string          `json:"startedAt"`
	FinishedAt   string          `json:"finishedAt"`
	Commands     []CommandResult `json:"commands"`
	RepoPath     string          `json:"repoPath"`
	StdoutPath   string          `json:"stdoutPath"`
	StderrPath   string          `json:"stderrPath"`
	ProgressPath string          `json:"progressPath"`
	PassedCount  int             `json:"passedCount"`
	FailedCount  int             `json:"failedCount"`
	SkippedCount int             `json:"skippedCount"`
}

const validationStdoutFilename = "validation_stdout.txt"
const validationStderrFilename = "validation_stderr.txt"
const validationRunFilename = "validation_run.json"

const ArtifactKindStdout = "validation_stdout"
const ArtifactKindStderr = "validation_stderr"
const ArtifactKindJSON = "validation_run_json"

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
