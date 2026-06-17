package validationrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"relay/internal/artifacts"
	"relay/internal/store"
)

type sealedCommandStore interface {
	GetRun(id int64) (*store.Run, error)
	GetRepo(id int64) (*store.Repo, error)
	CreateArtifact(runID int64, kind, path, mimeType string) (*store.Artifact, error)
	CreateEvent(runID int64, level, message string) (*store.Event, error)
	CreateCheck(runID int64, kind, status, summary, detailsJSON string) (*store.Check, error)
	UpdateRunStatus(id int64, status string) (*store.Run, error)
	ListArtifactsByRunKind(runID int64, kind string) ([]store.Artifact, error)
	DeleteArtifactsByRunKind(runID int64, kind string) error
}

type Service struct {
	store sealedCommandStore
}

func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

type canonicalPacketRaw struct {
	ExecutionPayload *executionPayloadRaw `json:"execution_payload"`
}

type executionPayloadRaw struct {
	ValidationCommands []validationCommandRaw `json:"validation_commands"`
}

type validationCommandRaw struct {
	ID              string `json:"id"`
	Command         string `json:"command"`
	Required        bool   `json:"required"`
	Purpose         string `json:"purpose"`
	SuccessSignal   string `json:"success_signal"`
	FailureHandling string `json:"failure_handling"`
}

func (svc *Service) RunValidation(ctx context.Context, runID int64) (*ValidationRun, error) {
	run, err := svc.store.GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}

	repo, err := svc.store.GetRepo(run.RepoID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repo.Path == "" {
		return nil, fmt.Errorf("repo path is empty")
	}
	if _, err := os.Stat(repo.Path); err != nil {
		return nil, fmt.Errorf("repo path does not exist: %s", repo.Path)
	}

	canonicalData, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		return nil, fmt.Errorf("read canonical_packet: %w", err)
	}

	var pkt canonicalPacketRaw
	if err := json.Unmarshal(canonicalData, &pkt); err != nil {
		return nil, fmt.Errorf("parse canonical_packet: %w", err)
	}

	cmdSpecs := pkt.ExecutionPayload.ValidationCommands
	if len(cmdSpecs) == 0 {
		result := &ValidationRun{
			RunID:      runID,
			Status:     StatusPassed,
			StartedAt:  nowUTC(),
			FinishedAt: nowUTC(),
			Commands:   []CommandResult{},
			RepoPath:   repo.Path,
		}
		_ = svc.persistArtifacts(runID, result, "", "")
		_, _ = svc.store.CreateEvent(runID, "info", "Validation skipped: no validation commands declared in packet")
		return result, nil
	}

	svc.store.DeleteArtifactsByRunKind(runID, ArtifactKindStdout)
	svc.store.DeleteArtifactsByRunKind(runID, ArtifactKindStderr)
	svc.store.DeleteArtifactsByRunKind(runID, ArtifactKindJSON)

	svc.store.UpdateRunStatus(runID, "local_validation_running")
	svc.store.CreateEvent(runID, "status_change", "Local validation started")

	startedAt := nowUTC()
	allCommadsStdout := strings.Builder{}
	allCommadsStderr := strings.Builder{}
	vr := &ValidationRun{
		RunID:     runID,
		Status:    StatusPassed,
		StartedAt: startedAt,
		Commands:  make([]CommandResult, 0, len(cmdSpecs)),
		RepoPath:  repo.Path,
	}

	overallFailed := false

	for _, spec := range cmdSpecs {
		cmd := sealedCommand{
			ID:              spec.ID,
			Command:         spec.Command,
			Required:        spec.Required,
			Purpose:         spec.Purpose,
			SuccessSignal:   spec.SuccessSignal,
			FailureHandling: spec.FailureHandling,
		}

		out := runAndCapture(ctx, cmd, repo.Path)

		cmdStatus := "pass"
		if out.exitCode != 0 || out.notRunReason != "" {
			cmdStatus = "fail"
		}

		cr := CommandResult{
			ID:         spec.ID,
			Command:    spec.Command,
			Required:   spec.Required,
			Status:     cmdStatus,
			ExitCode:   out.exitCode,
			DurationMs: out.durationMs,
			Workdir:    out.workdir,
			StartedAt:  out.startedAt,
			FinishedAt: out.finishedAt,
		}

		if out.notRunReason != "" {
			cr.NotRunReason = out.notRunReason
		}

		{
			stdoutFilename := fmt.Sprintf("validation_stdout_%s.txt", spec.ID)
			stdoutPath := filepath.Join(artifacts.Dir(runID), stdoutFilename)
			if err := writeArtifactFile(stdoutPath, out.stdout); err == nil {
				svc.store.CreateArtifact(runID, ArtifactKindStdout, stdoutPath, "text/plain")
				cr.StdoutKind = ArtifactKindStdout
			}
		}

		{
			stderrFilename := fmt.Sprintf("validation_stderr_%s.txt", spec.ID)
			stderrPath := filepath.Join(artifacts.Dir(runID), stderrFilename)
			if err := writeArtifactFile(stderrPath, out.stderr); err == nil {
				svc.store.CreateArtifact(runID, ArtifactKindStderr, stderrPath, "text/plain")
				cr.StderrKind = ArtifactKindStderr
			}
		}

		summary := fmt.Sprintf("Command: %s, Exit code: %d", spec.Command, out.exitCode)
		checkStatus := "pass"
		if cmdStatus == "fail" {
			checkStatus = "fail"
			if spec.Required {
				overallFailed = true
			}
		}
		svc.store.CreateCheck(runID, "validation", checkStatus, summary, "{}")

		if out.stdout != "" {
			allCommadsStdout.WriteString(fmt.Sprintf("=== %s (%s) ===\n", spec.ID, spec.Command))
			allCommadsStdout.WriteString(out.stdout)
			allCommadsStdout.WriteString("\n")
		}
		if out.stderr != "" {
			allCommadsStderr.WriteString(fmt.Sprintf("=== %s (%s) ===\n", spec.ID, spec.Command))
			allCommadsStderr.WriteString(out.stderr)
			allCommadsStderr.WriteString("\n")
		}

		vr.Commands = append(vr.Commands, cr)
	}

	if overallFailed {
		vr.Status = StatusFailed
	} else {
		vr.Status = StatusPassed
	}
	vr.FinishedAt = nowUTC()

	combinedStdout := allCommadsStdout.String()
	combinedStderr := allCommadsStderr.String()

	_ = svc.persistArtifacts(runID, vr, combinedStdout, combinedStderr)

	if overallFailed {
		_, _ = svc.store.CreateEvent(runID, "warn", "Validation failed: one or more required commands failed")
		_, _ = svc.store.UpdateRunStatus(runID, "validation_failed")
	} else {
		_, _ = svc.store.CreateEvent(runID, "info", "Validation passed")
		_, _ = svc.store.UpdateRunStatus(runID, "validation_passed")
	}

	return vr, nil
}

func (svc *Service) persistArtifacts(runID int64, vr *ValidationRun, stdout, stderr string) error {
	stdoutPath, err := artifacts.Write(runID, ArtifactKindStdout, validationStdoutFilename, []byte(stdout))
	if err == nil && stdoutPath != "" {
		svc.store.CreateArtifact(runID, ArtifactKindStdout, stdoutPath, "text/plain")
		vr.StdoutPath = stdoutPath
	}

	stderrPath, err := artifacts.Write(runID, ArtifactKindStderr, validationStderrFilename, []byte(stderr))
	if err == nil && stderrPath != "" {
		svc.store.CreateArtifact(runID, ArtifactKindStderr, stderrPath, "text/plain")
		vr.StderrPath = stderrPath
	}

	jsonBytes, err := json.MarshalIndent(vr, "", "  ")
	if err == nil {
		progPath, perr := artifacts.Write(runID, ArtifactKindJSON, validationRunFilename, jsonBytes)
		if perr == nil && progPath != "" {
			svc.store.CreateArtifact(runID, ArtifactKindJSON, progPath, "application/json")
			vr.ProgressPath = progPath
		}
	}

	return nil
}

func (svc *Service) HasValidationArtifacts(runID int64) bool {
	arts, err := svc.store.ListArtifactsByRunKind(runID, ArtifactKindJSON)
	if err != nil {
		return false
	}
	return len(arts) > 0
}

func (svc *Service) RequiredCommandsInPacket(runID int64) (bool, error) {
	data, err := artifacts.Read(runID, "canonical_packet", "canonical_packet.json")
	if err != nil {
		return false, nil
	}
	var pkt canonicalPacketRaw
	if err := json.Unmarshal(data, &pkt); err != nil {
		return false, nil
	}
	if pkt.ExecutionPayload == nil {
		return false, nil
	}
	for _, c := range pkt.ExecutionPayload.ValidationCommands {
		if c.Required {
			return true, nil
		}
	}
	return false, nil
}
