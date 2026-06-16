package auditor

import (
	"fmt"
	"os"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
)

type Collector struct {
	store *store.Store
}

func NewCollector(s *store.Store) *Collector {
	return &Collector{store: s}
}

func boundedPreview(data []byte, max int) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) > max {
		return string(data[:max]) + "..."
	}
	return string(data)
}

func (c *Collector) Collect(runID int64) (*Evidence, error) {
	run, err := c.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	ev := &Evidence{
		RunID:     runID,
		RunTitle:  run.Title,
		RunStatus: run.Status,
		Warnings:  nil,
	}

	c.collectPacketScope(runID, ev)
	c.collectExecutorResult(runID, ev)
	c.collectValidationOutput(runID, ev)
	c.collectChangedFiles(runID, ev)
	c.collectGitDiff(runID, ev)

	return ev, nil
}

func (c *Collector) collectPacketScope(runID int64, ev *Evidence) {
	data, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		ev.Warnings = append(ev.Warnings, "canonical_packet.json not found — packet scope metadata unavailable")
		return
	}

	ev.Packet = PacketScope{
		PacketID: fmt.Sprintf("packet-%d", runID),
	}
	if len(data) > 0 {
		ev.Packet.AuditSeed = boundedPreview(data, 2000)
	}
}

func (c *Collector) collectExecutorResult(runID int64, ev *Evidence) {
	data, err := artifacts.Read(runID, "executor_result", "executor_result.txt")
	if err != nil {
		ev.Warnings = append(ev.Warnings, "executor_result.txt not found — executor result evidence unavailable")
		return
	}

	ev.ExecutorResult = ExecutorResultEvidence{
		Present: true,
		Content: boundedPreview(data, MaxPreviewBytes),
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status:") || strings.HasPrefix(line, "Status:") || strings.HasPrefix(line, "exit_code:") || strings.HasPrefix(line, "ExitCode:") {
			ev.ExecutorResult.Summary += line + "\n"
		}
	}
	ev.ExecutorResult.Summary = strings.TrimSpace(ev.ExecutorResult.Summary)
}

func (c *Collector) collectValidationOutput(runID int64, ev *Evidence) {
	kinds := []string{"validation_stdout", "validation_run_json", "validation_stderr"}
	var best []byte
	var bestKind string
	for _, k := range kinds {
		paths := c.listArtifactPaths(runID, k)
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err == nil && len(data) > len(best) {
				best = data
				bestKind = k
			}
		}
	}

	if len(best) == 0 {
		ev.Warnings = append(ev.Warnings, "validation output artifact not found — validation evidence unavailable")
		return
	}

	ev.ValidationOutput = ValidationOutputEvidence{
		Present: true,
		Content: boundedPreview(best, MaxPreviewBytes),
		Summary: fmt.Sprintf("sourced from %s artifact (%d bytes)", bestKind, len(best)),
	}
}

func (c *Collector) collectChangedFiles(runID int64, ev *Evidence) {
	kinds := []string{"git_diff_name_status", "git_status_text", "git_diff_stat"}
	var bestFiles []string
	for _, k := range kinds {
		paths := c.listArtifactPaths(runID, k)
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err == nil {
				lines := strings.Split(string(data), "\n")
				for _, l := range lines {
					l = strings.TrimSpace(l)
					if l != "" {
						bestFiles = append(bestFiles, l)
					}
				}
				if len(bestFiles) > 0 {
					break
				}
			}
		}
		if len(bestFiles) > 0 {
			break
		}
	}

	if len(bestFiles) == 0 {
		ev.Warnings = append(ev.Warnings, "changed files artifact not found — changed files evidence unavailable")
		return
	}

	if len(bestFiles) > 100 {
		bestFiles = bestFiles[:100]
	}

	ev.ChangedFiles = ChangedFilesEvidence{
		Present: true,
		Files:   bestFiles,
		Preview: strings.Join(bestFiles, "\n"),
	}
}

func (c *Collector) collectGitDiff(runID int64, ev *Evidence) {
	data, err := artifacts.Read(runID, "git_diff_patch", "git_diff.patch")
	if err != nil {
		ev.Warnings = append(ev.Warnings, "git_diff.patch not found — diff evidence unavailable")
		return
	}

	ev.GitDiff = DiffEvidence{
		Present: true,
		Content: boundedPreview(data, MaxPreviewBytes),
		Preview: boundedPreview(data, 2000),
	}
}

func (c *Collector) listArtifactPaths(runID int64, kind string) []string {
	arts, err := c.store.ListArtifactsByRunKind(runID, kind)
	if err != nil {
		return nil
	}
	var paths []string
	for _, a := range arts {
		paths = append(paths, a.Path)
	}
	return paths
}
