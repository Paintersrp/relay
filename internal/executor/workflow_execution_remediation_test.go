package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/pipeline"
	workflowstore "relay/internal/store/workflow"
)

type absentProcessController struct{}

func (absentProcessController) StartOwned(context.Context, pipeline.CommandSpec) (pipeline.OwnedProcess, error) {
	return nil, errors.New("unexpected process start")
}

func (absentProcessController) OpenOwned(pipeline.ProcessIdentity) (pipeline.OwnedProcess, error) {
	return nil, pipeline.ErrProcessNotRunning
}

type previewSecretAdapter struct {
	preview string
}

func (a previewSecretAdapter) ID() AdapterID { return AdapterOpenCodeGo }

func (a previewSecretAdapter) BuildInvocation(request ExecutorAdapterRequest) (ExecutorInvocation, error) {
	return ExecutorInvocation{
		Adapter:     AdapterOpenCodeGo,
		Binary:      "fake-agent",
		WorkDir:     request.RepoPath,
		Stdin:       request.BriefContent,
		StdinSource: request.BriefPath,
		Model:       request.SelectedModel,
		Agent:       string(AdapterOpenCodeGo),
		Preview:     a.preview,
	}, nil
}

func (a previewSecretAdapter) NormalizeResult(raw string) NormalizedExecutorResult {
	if strings.Contains(raw, "STATUS: DONE") {
		return NormalizedExecutorResult{Status: pipeline.AgentResultDone, ExecutorResultText: raw}
	}
	return NormalizedExecutorResult{Status: pipeline.AgentResultBlocked, ExecutorResultText: raw, BlockerText: "blocked"}
}

func TestWorkflowUnverifiedTerminationBlocksRetryUntilReconciled(t *testing.T) {
	tests := []struct {
		name          string
		result        pipeline.AgentCommandRunResult
		useCancel     bool
		wantAttempt   string
		wantRunStatus string
	}{
		{
			name: "normal completion",
			result: pipeline.AgentCommandRunResult{
				ExitCode:            0,
				Stdout:              "STATUS: DONE\n",
				LaunchStarted:       true,
				LaunchDisposition:   pipeline.AgentLaunchOwned,
				TerminationVerified: false,
			},
			wantAttempt:   workflowstore.AttemptStatusSucceeded,
			wantRunStatus: workflowstore.RunStatusValidating,
		},
		{
			name: "timeout",
			result: pipeline.AgentCommandRunResult{
				ExitCode:            -2,
				TimedOut:            true,
				LaunchStarted:       true,
				LaunchDisposition:   pipeline.AgentLaunchOwned,
				TerminationVerified: false,
			},
			wantAttempt:   workflowstore.AttemptStatusTimedOut,
			wantRunStatus: workflowstore.RunStatusExecutionFailed,
		},
		{
			name: "operator cancellation",
			result: pipeline.AgentCommandRunResult{
				ExitCode:            1,
				LaunchStarted:       true,
				LaunchDisposition:   pipeline.AgentLaunchOwned,
				TerminationVerified: false,
			},
			useCancel:     true,
			wantAttempt:   workflowstore.AttemptStatusCancelled,
			wantRunStatus: workflowstore.RunStatusCancelled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newWorkflowFixture(t)
			fixture.service.runner = func(_ context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
				identity := pipeline.ProcessIdentity{PID: 500, StartedAt: "1", Platform: "linux"}
				if callbacks.OnProcessStarted != nil {
					if err := callbacks.OnProcessStarted(identity); err != nil {
						t.Fatal(err)
					}
				}
				if callbacks.OnStdout != nil && tt.result.Stdout != "" {
					callbacks.OnStdout([]byte(tt.result.Stdout))
				}
				result := tt.result
				result.ProcessIdentity = identity
				result.IdentityAvailable = true
				result.StartedAt = time.Now()
				result.FinishedAt = time.Now()
				return result
			}
			started, err := fixture.service.Start(context.Background(), WorkflowStartInput{
				RunID:   fixture.run.RunID,
				Adapter: "opencode_go",
				Model:   "test-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			pending, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if pending.Status != workflowstore.AttemptStatusRunning || !strings.Contains(pending.ResultJSON, `"cleanup_pending":true`) {
				t.Fatalf("cleanup-pending attempt = %+v", pending)
			}
			if _, err := fixture.service.Start(context.Background(), WorkflowStartInput{
				RunID:   fixture.run.RunID,
				Adapter: "codex",
				Model:   "retry-model",
			}); err == nil {
				t.Fatal("retry started before process-tree absence was verified")
			}
			fixture.service.controller = absentProcessController{}
			var reconciled WorkflowCancelResult
			if tt.useCancel {
				reconciled, err = fixture.service.Cancel(context.Background(), fixture.run.RunID, pending.AttemptID)
			} else {
				reconciled, err = fixture.service.Reconcile(context.Background(), fixture.run.RunID, pending.AttemptID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if reconciled.Attempt.Status != tt.wantAttempt || reconciled.Run.Status != tt.wantRunStatus {
				t.Fatalf("reconciled result = %+v", reconciled)
			}
		})
	}
}

func TestWorkflowExecutionEvidenceRedactsCommandPreviewSecret(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "preview-secret-token")
	fixture := newWorkflowFixture(t)
	fixture.service.adapterFactory = func(string) (ExecutorAdapter, error) {
		return previewSecretAdapter{preview: "fake-agent --token preview-secret-token < brief.md"}, nil
	}
	fixture.service.runner = successfulRunner

	started, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var evidencePath string
	for _, artifact := range artifacts {
		if artifact.Kind == "execution_evidence" {
			evidencePath = filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))
		}
	}
	if evidencePath == "" {
		t.Fatal("execution evidence artifact was not persisted")
	}
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "preview-secret-token") {
		t.Fatal("execution evidence leaked configured command preview secret")
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Fatal("execution evidence is missing redaction marker")
	}
}

func TestWorkflowAttemptEvidenceIsFullRedactedAndLiveCaptureIsBounded(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "workflow-secret-token")
	fixture := newWorkflowFixture(t)
	largePrefix := strings.Repeat("x", WorkflowLiveOutputLimitBytes+4096)
	fixture.service.runner = func(_ context.Context, _ string, _ string, _ []string, _ string, _ time.Duration, callbacks pipeline.AgentCommandStreamCallbacks, _ pipeline.ProcessController) pipeline.AgentCommandRunResult {
		identity := pipeline.ProcessIdentity{PID: 501, StartedAt: "1", Platform: "linux"}
		if callbacks.OnProcessStarted != nil {
			if err := callbacks.OnProcessStarted(identity); err != nil {
				t.Fatal(err)
			}
		}
		if callbacks.OnStdout != nil {
			callbacks.OnStdout([]byte(largePrefix + " workflow-sec"))
			callbacks.OnStdout([]byte("ret-token STATUS: DONE\n"))
		}
		return pipeline.AgentCommandRunResult{
			ExitCode:            0,
			Stdout:              "STATUS: DONE\n",
			StartedAt:           time.Now(),
			FinishedAt:          time.Now(),
			LaunchStarted:       true,
			LaunchDisposition:   pipeline.AgentLaunchOwned,
			ProcessIdentity:     identity,
			IdentityAvailable:   true,
			TerminationVerified: true,
		}
	}
	started, err := fixture.service.Start(context.Background(), WorkflowStartInput{
		RunID:   fixture.run.RunID,
		Adapter: "opencode_go",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := fixture.store.GetExecutionAttemptByAttemptID(context.Background(), started.Attempt.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := fixture.store.ListArtifactsByExecutionAttempt(context.Background(), attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stdoutPath string
	for _, artifact := range artifacts {
		if artifact.Kind == "executor_stdout" {
			stdoutPath = filepath.Join(fixture.store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))
		}
	}
	if stdoutPath == "" {
		t.Fatal("stdout artifact was not persisted")
	}
	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= WorkflowLiveOutputLimitBytes {
		t.Fatalf("stdout artifact was truncated to %d bytes", len(data))
	}
	if strings.Contains(string(data), "workflow-secret-token") || !strings.Contains(string(data), "[REDACTED]") {
		t.Fatal("stdout artifact was not redacted")
	}
}
