package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	appplans "relay/internal/app/plans"
	"relay/internal/artifacts"
	"relay/internal/compiler"
	"relay/internal/executor"
	"relay/internal/renderer"
	"relay/internal/repairer"
	"relay/internal/store"
	"relay/internal/validation"
	"relay/internal/validationrunner"
)

// ApproveIntake preserves POST /api/runs/{id}/approve-intake behavior.
func (s *Service) ApproveIntake(ctx context.Context, runID int64, req ApproveIntakeRequest, lifecycle *appplans.RunLifecycleService) (ApproveIntakeResult, error) {
	_ = ctx
	run, err := s.store.GetRun(runID)
	if err != nil {
		return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %d not found", runID)}
	}

	if run.Status != "intake_received" && run.Status != "intake_needs_review" {
		return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: fmt.Sprintf("Run status is %q, cannot approve/review in this state", run.Status)}
	}

	if req.Action != "approve" && req.Action != "needs_revision" && req.Action != "blocked" {
		return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: fmt.Sprintf("Invalid decision action %q", req.Action)}
	}

	var updatedRun *store.Run = run

	if req.Overrides.Repo != "" {
		newRepo, err := s.resolveRepo(req.Overrides.Repo)
		if err != nil {
			return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to resolve repository: " + err.Error()}
		}
		if newRepo.ID != run.RepoID {
			updatedRun, err = s.store.UpdateRunRepo(run.ID, newRepo.ID)
			if err != nil {
				return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run repository: " + err.Error()}
			}
		}
	}

	if req.Overrides.Model != "" && req.Overrides.Model != run.SelectedModel {
		updatedRun, err = s.store.UpdateRunModel(run.ID, run.RecommendedModel, req.Overrides.Model)
		if err != nil {
			return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run model: " + err.Error()}
		}
	}

	if req.Overrides.Branch != "" && req.Overrides.Branch != run.BranchName {
		updatedRun, err = s.store.UpdateRunBranch(run.ID, req.Overrides.Branch, run.BaseCommit, run.HeadCommit)
		if err != nil {
			return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run branch: " + err.Error()}
		}
	}

	if req.Overrides.ExecutorAdapter != "" {
		normalizedAdapter, err := executor.NormalizeKnownAdapterID(req.Overrides.ExecutorAdapter)
		if err != nil {
			return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: err.Error()}
		}
		if normalizedAdapter != run.ExecutorAdapter {
			updatedRun, err = s.store.UpdateRunExecutorAdapter(run.ID, normalizedAdapter)
			if err != nil {
				return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run executor adapter: " + err.Error()}
			}
		}
	}

	configMap := make(map[string]interface{})
	if data, err := artifacts.Read(run.ID, "run_config", "run_config.json"); err == nil {
		_ = json.Unmarshal(data, &configMap)
	}
	if req.Overrides.Repo != "" {
		configMap["repo_target"] = req.Overrides.Repo
	}
	if req.Overrides.Branch != "" {
		configMap["branch_context"] = req.Overrides.Branch
	}
	if req.Overrides.Worktree != "" {
		configMap["worktree"] = req.Overrides.Worktree
	}
	if req.Overrides.Model != "" {
		configMap["model"] = req.Overrides.Model
	}
	if req.Overrides.ValidationCommands != "" {
		configMap["validation_commands"] = req.Overrides.ValidationCommands
	}
	if req.Overrides.ExecutorAdapter != "" {
		normalizedAdapter, _ := executor.NormalizeKnownAdapterID(req.Overrides.ExecutorAdapter)
		configMap["executor_adapter"] = normalizedAdapter
	}
	configMap["notes"] = req.Notes
	configMap["decision"] = req.Action
	configMap["reviewed_at"] = time.Now().UTC().Format(time.RFC3339)

	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	_ = s.store.DeleteArtifactsByRunKind(run.ID, "run_config")
	if path, err := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON); err == nil {
		_, _ = s.store.CreateArtifact(run.ID, "run_config", path, "application/json")
	}

	nextStatus := "intake_needs_review"
	eventMessage := "Intake needs revision"
	if req.Action == "approve" {
		nextStatus = "approved_for_prepare"
		eventMessage = "Intake approved"
	} else if req.Action == "blocked" {
		nextStatus = "blocked"
		eventMessage = "Intake blocked"
	}

	if req.Notes != "" {
		eventMessage = fmt.Sprintf("%s: %s", eventMessage, req.Notes)
	}

	updatedRun, err = s.store.UpdateRunStatus(run.ID, nextStatus)
	if err != nil {
		return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run status: " + err.Error()}
	}

	if err := lifecycle.SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return ApproveIntakeResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update associated pass status: " + err.Error()}
	}

	_, _ = s.store.CreateEvent(run.ID, "status_change", eventMessage)

	repoName := "Unknown Repo"
	if repo, err := s.store.GetRepo(updatedRun.RepoID); err == nil && repo != nil {
		repoName = repo.Name
	}

	return ApproveIntakeResult{Run: *updatedRun, RepoName: repoName}, nil
}

// PrepareRun preserves POST /api/runs/{id}/prepare behavior.
func (s *Service) PrepareRun(ctx context.Context, runID int64) (PrepareResult, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return PrepareResult{}, &RunError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %d not found", runID)}
	}

	if run.Status != "approved_for_prepare" && run.Status != "packet_validation_failed" {
		return PrepareResult{}, &RunError{
			HTTPStatus: http.StatusConflict,
			Body: map[string]interface{}{
				"error":            "CONFLICT",
				"message":          fmt.Sprintf("Run status is %q, must be approved_for_prepare or packet_validation_failed to compile", run.Status),
				"currentStatus":    run.Status,
				"requiredStatuses": []string{"approved_for_prepare", "packet_validation_failed"},
			},
		}
	}

	comp := compiler.New(s.store)
	res, err := comp.CompileApprovedRun(ctx, runID)
	if err != nil {
		return PrepareResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	if !res.Success {
		return PrepareResult{
			Run:              *run,
			Success:          false,
			PacketID:         res.PacketID,
			ValidationReport: res.ValidationReport,
			Issues:           res.Issues,
		}, nil
	}

	return PrepareResult{
		Run:              *run,
		Success:          true,
		PacketID:         res.PacketID,
		ValidationReport: res.ValidationReport,
	}, nil
}

// RenderBrief preserves POST /api/runs/{id}/render-brief behavior.
func (s *Service) RenderBrief(ctx context.Context, runID int64) (BriefResult, error) {
	rend := renderer.New(s.store)
	res, err := rend.RenderExecutorBrief(ctx, runID)
	if err != nil {
		return BriefResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	if !res.Success {
		return BriefResult{Success: false, Issues: res.Issues}, nil
	}
	run, err := s.store.GetRun(runID)
	if err == nil && run != nil {
		return BriefResult{Run: *run, RunLoaded: true, Success: true}, nil
	}
	return BriefResult{Success: true, RunLoaded: false}, nil
}

// ApproveBrief preserves POST /api/runs/{id}/approve-brief behavior.
func (s *Service) ApproveBrief(ctx context.Context, runID int64) (BriefResult, error) {
	rend := renderer.New(s.store)
	res, err := rend.ApproveExecutorBrief(ctx, runID)
	if err != nil {
		return BriefResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	if !res.Success {
		return BriefResult{Success: false, Issues: res.Issues}, nil
	}
	run, err := s.store.GetRun(runID)
	if err == nil && run != nil {
		return BriefResult{Run: *run, RunLoaded: true, Success: true}, nil
	}
	return BriefResult{Success: true, RunLoaded: false}, nil
}

// ExecuteRun preserves the "start" action of POST /api/runs/{id}/execute.
func (s *Service) ExecuteRun(ctx context.Context, runID int64) (ExecuteResult, error) {
	_ = ctx
	params := &executor.DispatchParams{
		Store:    s.store,
		Log:      s.log,
		EventHub: s.eventHub,
		RunID:    runID,
	}

	if _, err := executor.DispatchBrief(params); err != nil {
		return ExecuteResult{}, &RunError{
			HTTPStatus: http.StatusUnprocessableEntity,
			Body: map[string]interface{}{
				"success": false,
				"runId":   fmt.Sprintf("%d", runID),
				"error":   err.Error(),
			},
		}
	}

	run, _ := s.store.GetRun(runID)
	if run == nil {
		return ExecuteResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "run not found after dispatch"}
	}
	return ExecuteResult{Run: *run}, nil
}

// ValidateRun preserves POST /api/runs/{id}/validate behavior.
func (s *Service) ValidateRun(ctx context.Context, runID int64) (ValidateResult, error) {
	run, err := s.store.GetRun(runID)
	if err != nil {
		return ValidateResult{}, &RunError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %d not found", runID)}
	}

	if run.Status != executor.StatusExecutorDone && run.Status != executor.StatusExecutorBlocked &&
		run.Status != "validation_passed" && run.Status != "validation_failed" {
		return ValidateResult{}, &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT",
			Message: fmt.Sprintf("Validation requires executor_done, executor_blocked, validation_passed, or validation_failed status, got %q", run.Status)}
	}

	svc := validationrunner.NewService(s.store)
	vr, err := svc.RunValidation(ctx, runID)
	if err != nil {
		return ValidateResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: err.Error()}
	}

	updatedRun, _ := s.store.GetRun(runID)
	postStatus := "validation_passed"
	if updatedRun != nil {
		postStatus = updatedRun.Status
	}

	return ValidateResult{
		ValidationStatus: string(vr.Status),
		RunStatus:        postStatus,
		Commands:         vr.Commands,
		Stdout:           vr.StdoutPath,
		Stderr:           vr.StderrPath,
		Progress:         vr.ProgressPath,
	}, nil
}

// AcceptFailedValidation preserves POST /api/runs/{id}/validate/accept-failure
// behavior. reason must be non-empty (validated by the caller).
func (s *Service) AcceptFailedValidation(ctx context.Context, runID int64, reason string, lifecycle *appplans.RunLifecycleService) error {
	_ = ctx
	run, err := s.store.GetRun(runID)
	if err != nil {
		return &RunError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %d not found", runID)}
	}

	if run.Status != "validation_failed" {
		return &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT",
			Message: fmt.Sprintf("Accepting validation failure requires validation_failed status, got %q", run.Status)}
	}

	valSvc := validationrunner.NewService(s.store)
	if !valSvc.HasValidationArtifacts(runID) {
		return &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: "Cannot accept validation failure without final validation evidence (validation_run.json missing)"}
	}

	acceptanceData := map[string]interface{}{
		"runId":      runID,
		"acceptedAt": time.Now().UTC().Format(time.RFC3339),
		"reason":     reason,
	}
	acceptanceBytes, err := json.MarshalIndent(acceptanceData, "", "  ")
	if err != nil {
		return &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to format acceptance data"}
	}

	progPath, err := artifacts.Write(runID, "validation_failure_acceptance_json", "validation_failure_acceptance.json", acceptanceBytes)
	if err != nil {
		return &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: fmt.Sprintf("Failed to write artifact: %s", err.Error())}
	}

	if _, err = s.store.CreateArtifact(runID, "validation_failure_acceptance_json", progPath, "application/json"); err != nil {
		return &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: fmt.Sprintf("Failed to save artifact: %s", err.Error())}
	}

	_, _ = s.store.CreateEvent(runID, "info", fmt.Sprintf("Validation failure accepted. Reason: %s", reason))

	updatedRun, err := s.store.UpdateRunStatus(runID, "validation_failed_accepted")
	if err != nil {
		return &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run status"}
	}

	if err := lifecycle.SyncAssociatedPassForRunStatus(updatedRun); err != nil {
		return &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update associated pass status: " + err.Error()}
	}

	return nil
}

// RepairValidation preserves POST /api/runs/{id}/repair/validation behavior.
func (s *Service) RepairValidation(ctx context.Context, runID int64) (RepairResult, error) {
	_ = ctx
	run, err := s.store.GetRun(runID)
	if err != nil {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %d not found", runID)}
	}

	if run.Status != "packet_validation_failed" {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT",
			Message: fmt.Sprintf("Repair requires packet_validation_failed status, got %q", run.Status)}
	}

	arts, err := s.store.ListArtifactsByRunKind(runID, "packet_validation_report")
	if err != nil || len(arts) == 0 {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: "No packet validation report found. Run prepare first."}
	}

	reportArt := arts[len(arts)-1]
	reportBytes, err := os.ReadFile(reportArt.Path)
	if err != nil {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to read validation report artifact"}
	}

	var report validation.ValidationReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to parse validation report"}
	}

	idStr := fmt.Sprintf("%d", runID)

	eligible, reason := repairer.CheckEligibility(&report)
	if !eligible {
		return RepairResult{
			HTTPStatus: http.StatusUnprocessableEntity,
			Body: map[string]interface{}{
				"success":          false,
				"runId":            idStr,
				"eligible":         false,
				"ineligibleReason": reason,
			},
		}, nil
	}

	packetArts, err := s.store.ListArtifactsByRunKind(runID, "canonical_packet")
	if err != nil || len(packetArts) == 0 {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusConflict, Code: "CONFLICT", Message: "No canonical packet found. Run prepare first."}
	}
	packetArt := packetArts[len(packetArts)-1]
	packetJSON, err := os.ReadFile(packetArt.Path)
	if err != nil {
		return RepairResult{}, &RunError{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to read canonical packet artifact"}
	}

	svc := repairer.NewService(s.store)
	result := svc.RepairValidation(runID, packetJSON, &report)

	body := map[string]interface{}{
		"success":            result.Success,
		"runId":              idStr,
		"eligible":           result.Eligible,
		"repairAttempted":    result.RepairAttempted,
		"blockedReason":      result.BlockedReason,
		"ineligibleReason":   result.IneligibleReason,
		"reValidationValid":  result.ReValidationValid,
		"reValidationReport": result.ReValidationReport,
		"reValidationError":  result.ReValidationError,
		"error":              result.Error,
		"repairArtifacts":    result.RepairArtifacts,
	}

	if !result.Eligible {
		return RepairResult{HTTPStatus: http.StatusUnprocessableEntity, Body: body}, nil
	}
	if !result.RepairAttempted {
		return RepairResult{HTTPStatus: http.StatusConflict, Body: body}, nil
	}
	if !result.Success {
		return RepairResult{HTTPStatus: http.StatusUnprocessableEntity, Body: body}, nil
	}
	return RepairResult{HTTPStatus: http.StatusOK, Body: body}, nil
}
