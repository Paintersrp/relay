package pipeline

import "time"

type ValidationProgressCommand struct {
	Label      string `json:"label"`
	Command    string `json:"command"`
	Source     string `json:"source"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	DurationMs int64  `json:"duration_ms"`
	HasStdout  bool   `json:"has_stdout"`
	HasStderr  bool   `json:"has_stderr"`
}

type ValidationProgress struct {
	Status         string                      `json:"status"`
	RepoPath       string                      `json:"repo_path"`
	StartedAt      string                      `json:"started_at"`
	UpdatedAt      string                      `json:"updated_at"`
	FinishedAt     string                      `json:"finished_at"`
	CurrentIndex   int                         `json:"current_index"`
	CurrentCommand string                      `json:"current_command"`
	TotalCommands  int                         `json:"total_commands"`
	Commands       []ValidationProgressCommand `json:"commands"`
	Error          string                      `json:"error"`
}

func NewValidationProgress(repoPath string, total int) ValidationProgress {
	now := time.Now().UTC().Format(time.RFC3339)
	return ValidationProgress{
		Status:         "starting",
		RepoPath:       repoPath,
		StartedAt:      now,
		UpdatedAt:      now,
		FinishedAt:     "",
		CurrentIndex:   0,
		CurrentCommand: "",
		TotalCommands:  total,
		Commands:       make([]ValidationProgressCommand, 0, total),
		Error:          "",
	}
}

func (vp *ValidationProgress) MarkRunning() {
	vp.Status = "running"
	vp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func (vp *ValidationProgress) MarkCommandRunning(index int, command string) {
	vp.CurrentIndex = index
	vp.CurrentCommand = command
	vp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func (vp *ValidationProgress) AppendCommandResult(cmd ValidationProgressCommand) {
	vp.Commands = append(vp.Commands, cmd)
	vp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func (vp *ValidationProgress) MarkFinished(status string) {
	vp.Status = status
	vp.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	vp.UpdatedAt = vp.FinishedAt
	vp.CurrentIndex = 0
	vp.CurrentCommand = ""
}

func (vp *ValidationProgress) MarkError(errMsg string) {
	vp.Status = "error"
	vp.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	vp.UpdatedAt = vp.FinishedAt
	vp.Error = errMsg
	vp.CurrentIndex = 0
	vp.CurrentCommand = ""
}

func (vp *ValidationProgress) IsRunning() bool {
	return vp.Status == "starting" || vp.Status == "running"
}
