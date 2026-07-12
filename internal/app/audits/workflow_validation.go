package audits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/executor"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const maxWorkflowAuditValidationConciseRunes = 1024

type workflowExecutionEvidencePayload struct {
	EffectiveBriefArtifactID string                            `json:"effective_brief_artifact_id"`
	EffectiveBriefSHA256     string                            `json:"effective_brief_sha256"`
	EffectiveBriefMode       string                            `json:"effective_brief_mode"`
	ValidationResults        []workflowAuditValidationEvidence `json:"-"`
}

type workflowAuditValidationEvidence struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Expected         string `json:"expected"`
	Status           string `json:"status"`
	ConciseResult    string `json:"concise_result"`
}

func resolveWorkflowExecutionEvidence(
	store *workflowstore.Store,
	run workflowstore.Run,
	attempt workflowstore.ExecutionAttempt,
	runArtifacts []workflowstore.Artifact,
	attemptArtifacts []workflowstore.Artifact,
) (workflowstore.Artifact, workflowExecutionEvidencePayload, workflowstore.Artifact, WorkflowAuditAttemptResult, error) {
	var evidenceArtifact workflowstore.Artifact
	count := 0
	for _, artifact := range attemptArtifacts {
		if artifact.Kind == "execution_evidence" {
			evidenceArtifact = artifact
			count++
		}
	}
	if count != 1 {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("expected exactly one execution_evidence artifact for selected attempt, found %d", count)
	}
	if evidenceArtifact.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !evidenceArtifact.ExecutionAttemptRowID.Valid || evidenceArtifact.ExecutionAttemptRowID.Int64 != attempt.ID {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("selected execution_evidence artifact ownership is invalid")
	}
	if evidenceArtifact.MediaType != "application/json" {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("selected execution_evidence artifact media type is unsupported")
	}
	data, err := readWorkflowArtifact(store, evidenceArtifact, MaxWorkflowAuditEvidenceBytes)
	if err != nil {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("read selected execution evidence: %w", err)
	}
	payload, err := decodeWorkflowExecutionEvidence(data)
	if err != nil {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, err
	}
	attemptResult := workflowAuditAttemptResult(attempt.ResultJSON)
	if payload.EffectiveBriefArtifactID != attemptResult.EffectiveBriefArtifactID || payload.EffectiveBriefSHA256 != attemptResult.EffectiveBriefSHA256 || payload.EffectiveBriefMode != attemptResult.EffectiveBriefMode {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("execution evidence effective brief identity does not match selected attempt runtime")
	}

	var effective workflowstore.Artifact
	switch payload.EffectiveBriefMode {
	case "full":
		for _, artifact := range runArtifacts {
			if artifact.ArtifactID == payload.EffectiveBriefArtifactID {
				if effective.ID != 0 {
					return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("full effective brief artifact ID is duplicated")
				}
				effective = artifact
			}
		}
		if effective.ID == 0 || effective.Kind != "executor_brief" || effective.OwnerType != workflowstore.ArtifactOwnerRun || !effective.RunRowID.Valid || effective.RunRowID.Int64 != run.ID {
			return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("full effective brief identity or ownership is invalid")
		}
	case "residual":
		for _, artifact := range attemptArtifacts {
			if artifact.ArtifactID == payload.EffectiveBriefArtifactID {
				if effective.ID != 0 {
					return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("residual effective brief artifact ID is duplicated")
				}
				effective = artifact
			}
		}
		if effective.ID == 0 || effective.Kind != "executor_residual_brief" || effective.OwnerType != workflowstore.ArtifactOwnerExecutionAttempt || !effective.ExecutionAttemptRowID.Valid || effective.ExecutionAttemptRowID.Int64 != attempt.ID {
			return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("residual effective brief identity or ownership is invalid")
		}
	default:
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("unsupported effective brief mode %q", payload.EffectiveBriefMode)
	}
	if effective.MediaType != "text/markdown" || effective.SHA256 != payload.EffectiveBriefSHA256 {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("effective brief artifact identity is invalid")
	}
	if _, err := readWorkflowArtifact(store, effective, MaxWorkflowAuditSourceBytes); err != nil {
		return workflowstore.Artifact{}, workflowExecutionEvidencePayload{}, workflowstore.Artifact{}, WorkflowAuditAttemptResult{}, fmt.Errorf("read effective brief: %w", err)
	}
	return evidenceArtifact, payload, effective, attemptResult, nil
}

func decodeWorkflowExecutionEvidence(data []byte) (workflowExecutionEvidencePayload, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("decode selected execution evidence: %w", err)
	}
	var payload workflowExecutionEvidencePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("decode selected execution evidence: %w", err)
	}
	if strings.TrimSpace(payload.EffectiveBriefArtifactID) == "" || payload.EffectiveBriefArtifactID != strings.TrimSpace(payload.EffectiveBriefArtifactID) ||
		strings.TrimSpace(payload.EffectiveBriefSHA256) == "" || payload.EffectiveBriefSHA256 != strings.TrimSpace(payload.EffectiveBriefSHA256) ||
		strings.TrimSpace(payload.EffectiveBriefMode) == "" || payload.EffectiveBriefMode != strings.TrimSpace(payload.EffectiveBriefMode) {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence effective brief identity is incomplete or noncanonical")
	}
	raw, present := root["validation_results"]
	if !present {
		return payload, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation_results must be omitted rather than null")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("decode selected execution evidence validation_results: %w", err)
	}
	if len(entries) == 0 {
		return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation_results must be omitted rather than empty")
	}
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		decoder := json.NewDecoder(bytes.NewReader(entry))
		decoder.DisallowUnknownFields()
		var result workflowAuditValidationEvidence
		if err := decoder.Decode(&result); err != nil {
			return workflowExecutionEvidencePayload{}, fmt.Errorf("decode selected execution evidence validation result %d: %w", index+1, err)
		}
		if strings.TrimSpace(result.Command) == "" || result.Command != strings.TrimSpace(result.Command) ||
			strings.TrimSpace(result.Expected) == "" || result.Expected != strings.TrimSpace(result.Expected) ||
			strings.TrimSpace(result.ConciseResult) == "" || result.ConciseResult != strings.TrimSpace(result.ConciseResult) {
			return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation result %d is incomplete or noncanonical", index+1)
		}
		if len([]rune(result.ConciseResult)) > maxWorkflowAuditValidationConciseRunes {
			return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation result %d exceeds the concise-result bound", index+1)
		}
		if executor.RedactSensitiveText(result.ConciseResult) != result.ConciseResult {
			return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation result %d contains unredacted sensitive content", index+1)
		}
		switch result.Status {
		case "passed", "failed", "not_run":
		default:
			return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence validation result %d has unsupported status %q", index+1, result.Status)
		}
		if _, duplicate := seen[result.Command]; duplicate {
			return workflowExecutionEvidencePayload{}, fmt.Errorf("selected execution evidence reports validation command %q more than once", result.Command)
		}
		seen[result.Command] = struct{}{}
		payload.ValidationResults = append(payload.ValidationResults, result)
	}
	return payload, nil
}

func mapWorkflowAuditValidation(implementation WorkflowImplementationEvidence, commands []speccompiler.ProjectedValidationCommand) ([]WorkflowAuditValidationResult, error) {
	if len(commands) == 0 {
		if implementation.Executor != nil && len(implementation.Executor.ExecutionEvidence.ValidationResults) != 0 {
			return nil, fmt.Errorf("selected execution evidence contains validation results but the canonical Execution Spec declares no validation commands")
		}
		return []WorkflowAuditValidationResult{}, nil
	}
	if implementation.Executor == nil {
		out := make([]WorkflowAuditValidationResult, 0, len(commands))
		for _, command := range commands {
			out = append(out, unavailableWorkflowAuditValidation(command))
		}
		return out, nil
	}

	type canonicalValidation struct {
		Command speccompiler.ProjectedValidationCommand
		Index   int
	}
	canonical := make(map[string]canonicalValidation, len(commands))
	for index, command := range commands {
		canonical[command.Command] = canonicalValidation{Command: command, Index: index}
	}
	matched := make(map[string]workflowAuditValidationEvidence, len(implementation.Executor.ExecutionEvidence.ValidationResults))
	lastIndex := -1
	for _, result := range implementation.Executor.ExecutionEvidence.ValidationResults {
		entry, ok := canonical[result.Command]
		if !ok {
			return nil, fmt.Errorf("selected execution evidence reports noncanonical validation command %q", result.Command)
		}
		if entry.Index <= lastIndex {
			return nil, fmt.Errorf("selected execution evidence validation results are not in canonical order")
		}
		lastIndex = entry.Index
		if result.WorkingDirectory != entry.Command.WorkingDirectory || result.Expected != projectedAuditExpected(entry.Command) {
			return nil, fmt.Errorf("selected execution evidence validation metadata does not match canonical command %q", result.Command)
		}
		matched[result.Command] = result
	}
	out := make([]WorkflowAuditValidationResult, 0, len(commands))
	for _, command := range commands {
		result, ok := matched[command.Command]
		if !ok {
			out = append(out, unavailableWorkflowAuditValidation(command))
			continue
		}
		out = append(out, WorkflowAuditValidationResult{
			Command:           command.Command,
			WorkingDirectory:  command.WorkingDirectory,
			Expected:          projectedAuditExpected(command),
			Status:            result.Status,
			ConciseResult:     result.ConciseResult,
			ArtifactReference: implementation.Executor.ExecutionEvidenceArtifact.ArtifactID,
		})
	}
	return out, nil
}

func unavailableWorkflowAuditValidation(command speccompiler.ProjectedValidationCommand) WorkflowAuditValidationResult {
	return WorkflowAuditValidationResult{
		Command:          command.Command,
		WorkingDirectory: command.WorkingDirectory,
		Expected:         projectedAuditExpected(command),
		Status:           "not_run",
		ConciseResult:    "No trustworthy structured result was available for this canonical validation command.",
	}
}

func projectedAuditExpected(command speccompiler.ProjectedValidationCommand) string {
	if strings.TrimSpace(command.Expected) != "" {
		return strings.TrimSpace(command.Expected)
	}
	if strings.TrimSpace(command.SuccessSignal) != "" {
		return strings.TrimSpace(command.SuccessSignal)
	}
	return "Command execution succeeds."
}
