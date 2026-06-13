package pipeline

import "time"

type ValidationProgressCommand struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Command     string `json:"command"`
	Source      string `json:"source"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ExitCode    int    `json:"exit_code"`
	TimedOut    bool   `json:"timed_out"`
	DurationMs  int64  `json:"duration_ms"`
	HasStdout   bool   `json:"has_stdout"`
	HasStderr   bool   `json:"has_stderr"`
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
	vp := ValidationProgress{
		Status:         "starting",
		RepoPath:       repoPath,
		StartedAt:      now,
		UpdatedAt:      now,
		FinishedAt:     "",
		CurrentIndex:   0,
		CurrentCommand: "",
		TotalCommands:  total,
		Commands:       make([]ValidationProgressCommand, total),
		Error:          "",
	}
	for i := range vp.Commands {
		vp.Commands[i].Index = i + 1
		vp.Commands[i].Status = "pending"
	}
	return vp
}

func NewValidationProgressFromCommands(repoPath string, commands []ValidationCommand) ValidationProgress {
	vp := NewValidationProgress(repoPath, len(commands))
	for i, cmd := range commands {
		vp.Commands[i].Label = cmd.Label
		vp.Commands[i].Command = cmd.Command
		vp.Commands[i].Source = cmd.Source
	}
	return vp
}

func (vp *ValidationProgress) touch() {
	vp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func (vp *ValidationProgress) commandAt(index int) *ValidationProgressCommand {
	if index < 1 || index > len(vp.Commands) {
		return nil
	}
	return &vp.Commands[index-1]
}

func (vp *ValidationProgress) MarkRunning() {
	vp.Status = "running"
	vp.touch()
}

func (vp *ValidationProgress) MarkCommandRunning(index int) {
	cmd := vp.commandAt(index)
	if cmd == nil {
		vp.touch()
		return
	}
	now := time.Now().UTC()
	cmd.Index = index
	cmd.Status = "running"
	if cmd.StartedAt == "" {
		cmd.StartedAt = now.Format(time.RFC3339)
	}
	cmd.CompletedAt = ""
	vp.CurrentIndex = index
	vp.CurrentCommand = cmd.Command
	vp.Status = "running"
	vp.touch()
}

func (vp *ValidationProgress) MarkCommandResult(index int, result CommandRunResult) {
	cmd := vp.commandAt(index)
	if cmd == nil {
		vp.touch()
		return
	}
	now := time.Now().UTC()
	cmd.Index = index
	if cmd.StartedAt == "" {
		startedAt := now.Add(-time.Duration(result.DurationMS) * time.Millisecond)
		cmd.StartedAt = startedAt.UTC().Format(time.RFC3339)
	}
	cmd.CompletedAt = now.Format(time.RFC3339)
	cmd.ExitCode = result.ExitCode
	cmd.TimedOut = result.TimedOut
	cmd.DurationMs = result.DurationMS
	cmd.HasStdout = result.Stdout != ""
	cmd.HasStderr = result.Stderr != ""
	switch {
	case result.TimedOut:
		cmd.Status = "timed_out"
	case result.ExitCode != 0:
		cmd.Status = "fail"
	default:
		cmd.Status = "pass"
	}
	vp.CurrentIndex = index
	vp.CurrentCommand = cmd.Command
	vp.Status = "running"
	vp.touch()
}

func (vp *ValidationProgress) MarkCommandError(index int, errMsg string) {
	cmd := vp.commandAt(index)
	if cmd == nil {
		vp.touch()
		return
	}
	now := time.Now().UTC()
	cmd.Index = index
	if cmd.StartedAt == "" {
		cmd.StartedAt = now.Format(time.RFC3339)
	}
	cmd.CompletedAt = now.Format(time.RFC3339)
	cmd.Status = "error"
	cmd.ExitCode = -1
	cmd.DurationMs = 0
	if errMsg != "" {
		cmd.HasStderr = true
	}
	vp.CurrentIndex = index
	vp.CurrentCommand = cmd.Command
	vp.touch()
}

func (vp *ValidationProgress) MarkRemainingSkipped() {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range vp.Commands {
		if vp.Commands[i].Status == "pending" {
			vp.Commands[i].Status = "skipped"
			vp.Commands[i].CompletedAt = now
		}
	}
	vp.touch()
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
