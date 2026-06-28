package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	appplans "relay/internal/app/plans"
	"relay/internal/artifacts"
	legacyintake "relay/internal/intake"
	"relay/internal/store"
	"relay/internal/store/generated"
)

// Service is the app-layer intake facade. It owns the planner-handoff run
// creation/update workflow that the intake HTTP endpoint exposes.
type Service struct {
	store     *store.Store
	lifecycle *appplans.RunLifecycleService
}

// NewService constructs an intake app service.
func NewService(st *store.Store) *Service {
	return &Service{store: st, lifecycle: appplans.NewRunLifecycleService(st)}
}

// IntakePlannerHandoff preserves POST /api/intake/planner-handoff behavior. It
// returns a typed *Error for client-facing failures.
func (s *Service) IntakePlannerHandoff(ctx context.Context, input IntakeInput) (*IntakeResult, error) {
	markdown := input.PlannerHandoffMarkdown
	if strings.TrimSpace(markdown) == "" && input.HandoffPath != "" {
		if data, err := os.ReadFile(input.HandoffPath); err == nil {
			markdown = string(data)
		}
	}
	if strings.TrimSpace(markdown) == "" {
		return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "Planner handoff markdown is empty or missing"}
	}

	metadata, _, _, _ := legacyintake.ParseFrontmatter(markdown)
	warnings, blockers := legacyintake.ValidateHandoffText(markdown)
	if len(blockers) > 0 {
		return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: strings.Join(blockers, "; ")}
	}

	repoTarget := input.Repo
	if repoTarget == "" {
		repoTarget = input.RepoTarget
	}
	if repoTarget == "" {
		repoTarget = metadata["repo"]
	}
	if repoTarget == "" {
		repoTarget = metadata["repo_target"]
	}
	if repoTarget == "" {
		return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "No repository target found in request or frontmatter"}
	}

	repo, err := s.resolveRepo(repoTarget)
	if err != nil {
		return nil, &Error{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to resolve repository: " + err.Error()}
	}

	branchContext := input.Branch
	if branchContext == "" {
		branchContext = input.BranchContext
	}
	if branchContext == "" {
		branchContext = metadata["branch"]
	}
	if branchContext == "" {
		branchContext = metadata["branch_context"]
	}
	if branchContext == "" {
		branchContext = "main"
	}

	title := input.Name
	if title == "" {
		title = input.PacketID
	}
	if title == "" {
		title = metadata["title"]
	}
	if title == "" {
		title = deriveRunTitleFromMarkdown(markdown)
	}

	executorAdapter, explicitAdapter, err := resolveIntakeExecutorAdapter(input, metadata)
	if err != nil {
		return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: err.Error()}
	}
	recommendedModel := resolveIntakeRecommendedModel(input, metadata, executorAdapter)
	selectedModel := recommendedModel
	planID, passID := resolveIntakePlanInputs(input)

	var run *generated.Run
	var association legacyintake.RunPlanAssociation
	var sourceContextIDs struct {
		ContextPacketID  string
		SourceSnapshotID string
	}
	isNew := false

	if input.RunID != "" {
		if planID != "" || passID != "" {
			return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "planId/passId may only be set when creating a new run"}
		}
		runIDInt, err := strconv.ParseInt(input.RunID, 10, 64)
		if err != nil {
			return nil, &Error{HTTPStatus: http.StatusBadRequest, Code: "BAD_REQUEST", Message: "Invalid run_id format"}
		}
		run, err = s.store.GetRun(runIDInt)
		if err != nil || run == nil {
			return nil, &Error{HTTPStatus: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Run with ID %s not found", input.RunID)}
		}
	} else {
		isNew = true
		status := "intake_received"
		if len(warnings) > 0 {
			status = "intake_needs_review"
		}
		association, err = legacyintake.ResolveRunPlanAssociation(ctx, s.store, planID, passID)
		if err != nil {
			return nil, mapInputError(err, "Failed to resolve plan association: ")
		}
		contextPacketID, sourceSnapshotID := resolveIntakeSourceContextInputs(input)
		validatedSourceContext, err := legacyintake.NewService(s.store).ResolveRunSourceContextProvenance(association, metadata, legacyintake.CreateRunInput{
			ContextPacketID:  contextPacketID,
			SourceSnapshotID: sourceSnapshotID,
		})
		if err != nil {
			return nil, mapInputError(err, "Failed to validate source context: ")
		}
		sourceContextIDs.ContextPacketID = validatedSourceContext.ContextPacketID
		sourceContextIDs.SourceSnapshotID = validatedSourceContext.SourceSnapshotID
		if err := legacyintake.ValidateManagedRunSourceContextRequirement(association, validatedSourceContext.ContextPacketID, validatedSourceContext.SourceSnapshotID); err != nil {
			return nil, mapInputError(err, "Failed to validate source context requirement: ")
		}
		r, err := s.store.CreateRunWithAssociation(
			repo.ID,
			title,
			status,
			recommendedModel,
			selectedModel,
			executorAdapter,
			branchContext,
			association.PlanRowID,
			association.PlanPassRowID,
		)
		if err != nil {
			return nil, &Error{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to create run: " + err.Error()}
		}
		if err := s.lifecycle.MarkAssociatedPassRunCreated(r); err != nil {
			return nil, &Error{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update associated pass status: " + err.Error()}
		}
		run = r
		planID = association.PlanID
		passID = association.PassID
	}

	if !isNew {
		status := "intake_received"
		if len(warnings) > 0 {
			status = "intake_needs_review"
		}
		updatedRun, err := s.store.UpdateRunStatus(run.ID, status)
		if err != nil {
			return nil, &Error{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "Failed to update run status: " + err.Error()}
		}
		run = updatedRun
		if recommendedModel != "" {
			if modelRun, err := s.store.UpdateRunModel(run.ID, recommendedModel, selectedModel); err == nil {
				run = modelRun
			}
		}
		if branchContext != "" {
			if branchRun, err := s.store.UpdateRunBranch(run.ID, branchContext, "", ""); err == nil {
				run = branchRun
			}
		}
		if explicitAdapter {
			if adapterRun, err := s.store.UpdateRunExecutorAdapter(run.ID, executorAdapter); err == nil {
				run = adapterRun
			}
		}
	}

	s.writeIntakeArtifacts(run, markdown, metadata, branchContext, repo, planID, passID, input.Source)

	if isNew {
		s.recordProvenance(run, markdown, metadata, repoTarget, branchContext, planID, passID, sourceContextIDs.ContextPacketID, sourceContextIDs.SourceSnapshotID, association, input.Source)
	}

	_, _ = s.store.CreateEvent(run.ID, "info", "Handoff intake receipt: planner handoff registered")

	return &IntakeResult{RunID: run.ID, PlanID: planID, PassID: passID}, nil
}

func resolveIntakeRecommendedModel(input IntakeInput, metadata map[string]string, executorAdapter string) string {
	for _, v := range []string{input.ExecutorModelProfile, input.ExecutorModelProfile2, input.RecommendedModel, input.Model} {
		if v := strings.TrimSpace(v); v != "" {
			return v
		}
	}
	for _, key := range []string{"recommended_model", "executor_model_profile", "model"} {
		if v := strings.TrimSpace(metadata[key]); v != "" {
			return v
		}
	}
	if executorAdapter == "kiro_cli" {
		return "auto"
	}
	targetExec := strings.TrimSpace(metadata["target_executor"])
	switch targetExec {
	case "deepseek", "deepseek-v4-pro":
		return "deepseek-v4-pro"
	case "deepseek-v4-flash":
		return "deepseek-v4-flash"
	default:
		return "deepseek-v4-pro"
	}
}

func mapInputError(err error, internalPrefix string) error {
	var inputErr *legacyintake.InputError
	if errors.As(err, &inputErr) {
		statusCode := http.StatusBadRequest
		errorCode := "BAD_REQUEST"
		if inputErr.Code == legacyintake.ErrCodeNotFound {
			statusCode = http.StatusNotFound
			errorCode = "NOT_FOUND"
		}
		return &Error{HTTPStatus: statusCode, Code: errorCode, Message: inputErr.Message}
	}
	return &Error{HTTPStatus: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: internalPrefix + err.Error()}
}

func (s *Service) writeIntakeArtifacts(run *generated.Run, markdown string, metadata map[string]string, branchContext string, repo *store.Repo, planID, passID, source string) {
	_ = s.store.DeleteChecksByRunKind(run.ID, "validation")
	warnings, _ := legacyintake.ValidateHandoffText(markdown)
	if len(warnings) > 0 {
		for _, w := range warnings {
			_, _ = s.store.CreateCheck(run.ID, "validation", "warning", w, "{}")
		}
	} else {
		_, _ = s.store.CreateCheck(run.ID, "validation", "pass", "Intake validation successful", "{}")
	}

	_ = s.store.DeleteArtifactsByRunKind(run.ID, "planner_handoff")
	_ = s.store.DeleteArtifactsByRunKind(run.ID, "parsed_frontmatter")
	_ = s.store.DeleteArtifactsByRunKind(run.ID, "run_config")
	_ = s.store.DeleteArtifactsByRunKind(run.ID, "intake_validation_report")

	if path, err := artifacts.Write(run.ID, "planner_handoff", "planner_handoff.md", []byte(markdown)); err == nil {
		_, _ = s.store.CreateArtifact(run.ID, "planner_handoff", path, "text/markdown")
	}

	fmJSON, _ := json.MarshalIndent(metadata, "", "  ")
	if path, err := artifacts.Write(run.ID, "parsed_frontmatter", "parsed_frontmatter.json", fmJSON); err == nil {
		_, _ = s.store.CreateArtifact(run.ID, "parsed_frontmatter", path, "application/json")
	}

	sourceStr := source
	if sourceStr == "" {
		sourceStr = "api"
	}
	configMap := map[string]string{
		"repo_target":      repo.Path,
		"branch_context":   branchContext,
		"source":           sourceStr,
		"created_from":     "intake_endpoint",
		"executor_adapter": run.ExecutorAdapter,
		"selected_model":   run.SelectedModel,
	}
	if planID != "" {
		configMap["plan_id"] = planID
	}
	if passID != "" {
		configMap["pass_id"] = passID
	}
	configJSON, _ := json.MarshalIndent(configMap, "", "  ")
	if path, err := artifacts.Write(run.ID, "run_config", "run_config.json", configJSON); err == nil {
		_, _ = s.store.CreateArtifact(run.ID, "run_config", path, "application/json")
	}

	dbChecks, _ := s.store.ListChecksByRun(run.ID)
	errorsCount, warnCount, passedCount, issues := summarizeChecks(dbChecks)
	report := map[string]interface{}{
		"status":   run.Status,
		"errors":   errorsCount,
		"warnings": warnCount,
		"passed":   passedCount,
		"issues":   issues,
	}
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if path, err := artifacts.Write(run.ID, "intake_validation_report", "intake_validation_report.json", reportJSON); err == nil {
		_, _ = s.store.CreateArtifact(run.ID, "intake_validation_report", path, "application/json")
	}
}

func (s *Service) recordProvenance(run *generated.Run, markdown string, metadata map[string]string, repoTarget, branchContext, planID, passID, contextPacketID, sourceSnapshotID string, association legacyintake.RunPlanAssociation, source string) {
	sourceStr := source
	if sourceStr == "" {
		sourceStr = "api"
	}
	handoffHash := sha256.Sum256([]byte(markdown))
	handoffSHA := hex.EncodeToString(handoffHash[:])
	sourceArtifactPath := firstNonEmptyString(metadata["source_artifact_path"], metadata["intended_handoff_path"])
	handoffMetadataJSON, _ := json.Marshal(metadata)
	submissionArgsJSON, _ := json.Marshal(map[string]interface{}{
		"has_plan_id":            planID != "",
		"has_pass_id":            passID != "",
		"has_context_packet_id":  contextPacketID != "",
		"has_source_snapshot_id": sourceSnapshotID != "",
		"source":                 sourceStr,
	})
	_, _ = s.store.CreateRunSubmissionProvenance(store.CreateRunSubmissionProvenanceParams{
		RunID:                run.ID,
		PlannerHandoffSha256: handoffSHA,
		PlannerHandoffBytes:  int64(len([]byte(markdown))),
		Source:               sourceStr,
		SourceArtifactPath:   sourceArtifactPath,
		RepoTarget:           repoTarget,
		BranchContext:        branchContext,
		PlanID:               planID,
		PassID:               passID,
		PlanRowID:            association.PlanRowID,
		PlanPassRowID:        association.PlanPassRowID,
		ManagedPlanPass:      metadata["managed_plan_pass"],
		ManagedPlanPassName:  metadata["managed_plan_pass_name"],
		ContextPacketID:      contextPacketID,
		SourceSnapshotID:     sourceSnapshotID,
		HandoffMetadataJSON:  string(handoffMetadataJSON),
		SubmissionArgsJSON:   string(submissionArgsJSON),
	})
}

type intakeValidationIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

func summarizeChecks(checks []store.Check) (errorsCount, warnCount, passedCount int, issues []intakeValidationIssue) {
	issues = make([]intakeValidationIssue, 0)
	for _, c := range checks {
		switch c.Status {
		case "fail", "error", "block":
			errorsCount++
		case "warn", "warning":
			warnCount++
		case "pass", "passed":
			passedCount++
		}
		if c.Kind == "validation" || c.Kind == "validation_run" {
			severity := "info"
			switch c.Status {
			case "fail", "error", "block":
				severity = "error"
			case "warn", "warning":
				severity = "warning"
			}
			issues = append(issues, intakeValidationIssue{
				Severity: severity,
				Code:     c.Kind,
				Message:  c.Summary,
			})
		}
	}
	return
}
