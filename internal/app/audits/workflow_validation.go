package audits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"relay/internal/executor"
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
