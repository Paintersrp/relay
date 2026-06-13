package pipeline

import "testing"

func TestNewValidationProgressFromCommandsInitializesPlannedRows(t *testing.T) {
	commands := []ValidationCommand{
		{Label: "templ generate", Command: "templ generate", Source: "handoff"},
		{Label: "go fmt", Command: "go fmt ./...", Source: "repo-default"},
	}

	vp := NewValidationProgressFromCommands("D:/Code/relay", commands)
	if vp.Status != "starting" {
		t.Fatalf("expected starting status, got %s", vp.Status)
	}
	if vp.TotalCommands != len(commands) {
		t.Fatalf("expected %d total commands, got %d", len(commands), vp.TotalCommands)
	}
	if len(vp.Commands) != len(commands) {
		t.Fatalf("expected %d command rows, got %d", len(commands), len(vp.Commands))
	}

	for i, cmd := range vp.Commands {
		if cmd.Index != i+1 {
			t.Errorf("row %d index = %d, want %d", i, cmd.Index, i+1)
		}
		if cmd.Status != "pending" {
			t.Errorf("row %d status = %s, want pending", i, cmd.Status)
		}
		if cmd.Command != commands[i].Command {
			t.Errorf("row %d command = %q, want %q", i, cmd.Command, commands[i].Command)
		}
	}
}

func TestValidationProgressUpdatesRowsInPlace(t *testing.T) {
	commands := []ValidationCommand{
		{Label: "templ generate", Command: "templ generate", Source: "handoff"},
		{Label: "go fmt", Command: "go fmt ./...", Source: "repo-default"},
	}

	vp := NewValidationProgressFromCommands("D:/Code/relay", commands)
	vp.MarkRunning()
	vp.MarkCommandRunning(1)
	if vp.CurrentIndex != 1 {
		t.Fatalf("expected current index 1, got %d", vp.CurrentIndex)
	}
	if vp.CurrentCommand != commands[0].Command {
		t.Fatalf("expected current command %q, got %q", commands[0].Command, vp.CurrentCommand)
	}
	if vp.Commands[0].Status != "running" {
		t.Fatalf("expected first row running, got %s", vp.Commands[0].Status)
	}
	if vp.Commands[1].Status != "pending" {
		t.Fatalf("expected second row pending, got %s", vp.Commands[1].Status)
	}

	result := CommandRunResult{
		Label:      commands[0].Label,
		Command:    commands[0].Command,
		Source:     commands[0].Source,
		ExitCode:   0,
		Stdout:     "ok",
		Stderr:     "",
		TimedOut:   false,
		DurationMS: 42,
	}
	vp.MarkCommandResult(1, result)

	if vp.Commands[0].Status != "pass" {
		t.Fatalf("expected first row pass, got %s", vp.Commands[0].Status)
	}
	if vp.Commands[0].ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", vp.Commands[0].ExitCode)
	}
	if !vp.Commands[0].HasStdout {
		t.Fatal("expected stdout flag on completed row")
	}
	if len(vp.Commands) != 2 {
		t.Fatalf("expected 2 rows after update, got %d", len(vp.Commands))
	}

	vp.MarkCommandRunning(2)
	result = CommandRunResult{
		Label:      commands[1].Label,
		Command:    commands[1].Command,
		Source:     commands[1].Source,
		ExitCode:   -2,
		Stdout:     "",
		Stderr:     "",
		TimedOut:   true,
		DurationMS: 99,
	}
	vp.MarkCommandResult(2, result)
	if vp.Commands[1].Status != "timed_out" {
		t.Fatalf("expected second row timed_out, got %s", vp.Commands[1].Status)
	}

	vp.MarkRemainingSkipped()
	for i, cmd := range vp.Commands {
		if cmd.Status == "pending" {
			t.Fatalf("row %d unexpectedly remained pending", i)
		}
	}
}
