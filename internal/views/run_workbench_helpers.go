package views

import (
	"fmt"
	"strings"

	"relay/internal/pipeline"
	"relay/internal/store"
)

type SetupReviewChecklistItem struct {
	Label  string
	Detail string
	Done   bool
}

type PromptSummaryItem struct {
	Title  string
	Detail string
}

type PromptDetailRow struct {
	Label string
	Value string
}

func shouldShowRunSetupReview(hasRequestedStep bool, activeStep string, previews RunPreviews, artifacts []store.Artifact) bool {
	if hasRequestedStep || activeStep != "handoff" {
		return false
	}
	return artifactExists(artifacts, "agent_prompt") && artifactExists(artifacts, "opencode_handoff_packet")
}

func runSetupReviewChecklistItems(run *store.Run, artifacts []store.Artifact, previews RunPreviews, intakeReview *pipeline.IntakeReview) []SetupReviewChecklistItem {
	items := make([]SetupReviewChecklistItem, 0, 5)

	intakeDetail := "Relay reviewed the original handoff."
	intakeDone := false
	if intakeReview != nil {
		intakeDone = len(intakeReview.Blockers) == 0
		if len(intakeReview.Blockers) > 0 {
			intakeDetail = strings.Join(intakeReview.Blockers, " ")
		} else if len(intakeReview.Warnings) > 0 {
			intakeDetail = strings.Join(intakeReview.Warnings, " ")
		}
	}
	items = append(items, SetupReviewChecklistItem{
		Label:  "Intake review passed",
		Detail: intakeDetail,
		Done:   intakeDone,
	})

	items = append(items, SetupReviewChecklistItem{
		Label:  "Agent prompt generated",
		Detail: setupReviewArtifactDetail(previews.AgentPrompt, previews.AgentPromptEstimate, "Relay generated the compact repo-agent prompt."),
		Done:   artifactExists(artifacts, "agent_prompt"),
	})

	items = append(items, SetupReviewChecklistItem{
		Label:  "Agent packet generated",
		Detail: setupReviewArtifactDetail(previews.OpenCodePacket, "", "Relay prepared the OpenCode handoff packet."),
		Done:   artifactExists(artifacts, "opencode_handoff_packet"),
	})

	baselineDetail := "No git baseline was captured yet."
	if previews.GitBaselineAvailable {
		baselineDetail = "Git baseline captured for the run."
		if previews.GitBaselineBaselineSHA != "" {
			baselineDetail = "Git baseline captured at " + shortenText(previews.GitBaselineBaselineSHA, 12)
		}
	}
	items = append(items, SetupReviewChecklistItem{
		Label:  "Git baseline captured",
		Detail: baselineDetail,
		Done:   previews.GitBaselineAvailable,
	})

	selectedModel := ""
	if run != nil {
		selectedModel = strings.TrimSpace(run.SelectedModel)
	}
	items = append(items, SetupReviewChecklistItem{
		Label:  "Selected model set",
		Detail: modelSelectionDetail(run),
		Done:   selectedModel != "",
	})

	return items
}

func setupReviewArtifactDetail(content string, estimate string, fallback string) string {
	if strings.TrimSpace(content) == "" {
		return fallback
	}
	if strings.TrimSpace(estimate) != "" {
		return "Prompt estimate: " + estimate
	}
	return fallback
}

func modelSelectionDetail(run *store.Run) string {
	if run == nil {
		return "No model selection is recorded for this run."
	}
	selected := strings.TrimSpace(run.SelectedModel)
	recommended := strings.TrimSpace(run.RecommendedModel)
	switch {
	case selected != "" && recommended != "" && selected == recommended:
		return "Selected model: " + selected + " (matches handoff recommendation)"
	case selected != "" && recommended != "":
		return "Selected model: " + selected + " (recommended: " + recommended + ")"
	case selected != "":
		return "Selected model: " + selected
	case recommended != "":
		return "Recommended model: " + recommended
	default:
		return "No model selection is recorded for this run."
	}
}

func promptTransformationItems(run *store.Run, intakeReview *pipeline.IntakeReview, previews RunPreviews) []PromptSummaryItem {
	scopeCount := len(promptScopedFiles(intakeReview))
	scopeDetail := "No explicit scoped files were declared."
	if scopeCount > 0 {
		scopeDetail = fmt.Sprintf("%d scoped file%s were preserved.", scopeCount, pluralSuffix(scopeCount))
	}

	outputContractDetail := "Relay appends the final output contract to the generated prompt."
	if promptOutputContractPresent(intakeReview) {
		outputContractDetail = "Relay preserved the final output contract from the handoff."
	}

	return []PromptSummaryItem{
		{
			Title:  "Removed orchestration-only wrapper text",
			Detail: "Relay stripped wrapper prose, validation-command scaffolding, and other orchestration-only instructions from the generated prompt.",
		},
		{
			Title:  "Preserved the implementation goal",
			Detail: promptImplementationGoal(run, intakeReview),
		},
		{
			Title:  "Preserved scoped files",
			Detail: scopeDetail,
		},
		{
			Title:  "Added the repo-agent execution contract",
			Detail: outputContractDetail,
		},
		{
			Title:  "Normalized model and prompt metadata",
			Detail: promptMetadataDetail(run, previews),
		},
	}
}

func promptModelExecutionRows(run *store.Run, intakeReview *pipeline.IntakeReview, previews RunPreviews) []PromptDetailRow {
	rows := []PromptDetailRow{
		{Label: "Recommended model", Value: promptRecommendedModel(run)},
		{Label: "Selected model", Value: promptSelectedModel(run)},
		{Label: "Execution mode", Value: "Compact repo-agent prompt"},
		{Label: "Prompt size estimate", Value: promptSizeEstimate(previews)},
		{Label: "Output contract", Value: promptOutputContractState(intakeReview)},
		{Label: "Model rationale", Value: promptModelRationale(run)},
	}
	return rows
}

func promptImplementationGoal(run *store.Run, intakeReview *pipeline.IntakeReview) string {
	if run != nil && strings.TrimSpace(run.Title) != "" {
		return "Relay kept the implementation goal centered on " + run.Title + "."
	}
	if intakeReview != nil && strings.TrimSpace(intakeReview.Metadata.Title) != "" {
		return "Relay kept the implementation goal centered on " + intakeReview.Metadata.Title + "."
	}
	return "Relay kept the implementation goal centered on the original handoff."
}

func promptMetadataDetail(run *store.Run, previews RunPreviews) string {
	selected := promptSelectedModel(run)
	if selected == "" {
		selected = "No selected model recorded"
	}
	size := promptSizeEstimate(previews)
	if size == "" {
		size = "prompt size unavailable"
	}
	return selected + "; " + size + "."
}

func promptRecommendedModel(run *store.Run) string {
	if run == nil || strings.TrimSpace(run.RecommendedModel) == "" {
		return "Not declared"
	}
	return run.RecommendedModel
}

func promptSelectedModel(run *store.Run) string {
	if run == nil || strings.TrimSpace(run.SelectedModel) == "" {
		return "Not selected"
	}
	return run.SelectedModel
}

func promptSizeEstimate(previews RunPreviews) string {
	if strings.TrimSpace(previews.AgentPromptEstimate) == "" {
		return "Unavailable"
	}
	return previews.AgentPromptEstimate
}

func promptOutputContractState(intakeReview *pipeline.IntakeReview) string {
	if promptOutputContractPresent(intakeReview) {
		return "Present"
	}
	return "Missing"
}

func promptOutputContractPresent(intakeReview *pipeline.IntakeReview) bool {
	if intakeReview == nil {
		return false
	}
	return strings.TrimSpace(intakeReview.Metadata.FinalOutputContract) != ""
}

func promptModelRationale(run *store.Run) string {
	if run == nil {
		return "No model metadata is available."
	}
	selected := strings.TrimSpace(run.SelectedModel)
	recommended := strings.TrimSpace(run.RecommendedModel)
	switch {
	case selected == "" && recommended == "":
		return "No model metadata is available."
	case selected != "" && recommended != "" && selected == recommended:
		return "Selected model matches the handoff recommendation."
	case selected != "" && recommended != "":
		return "Relay kept the handoff recommendation in view while the selected model was set to " + selected + "."
	case selected != "":
		return "Selected model was set to " + selected + "."
	default:
		return "The handoff recommended " + recommended + ", but no explicit selection was recorded."
	}
}

func promptPreviewExcerpt(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxChars <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	excerpt := string(runes[:maxChars])
	if idx := strings.LastIndex(excerpt, "\n"); idx > 0 {
		excerpt = excerpt[:idx]
	}
	excerpt = strings.TrimSpace(excerpt)
	if excerpt == "" {
		excerpt = strings.TrimSpace(string(runes[:maxChars]))
	}
	return excerpt + "\n..."
}

func promptScopedFiles(intakeReview *pipeline.IntakeReview) []string {
	if intakeReview == nil || len(intakeReview.Metadata.ScopedFiles) == 0 {
		return nil
	}
	paths := make([]string, 0, len(intakeReview.Metadata.ScopedFiles))
	for _, sf := range intakeReview.Metadata.ScopedFiles {
		if path := strings.TrimSpace(sf.Path); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func setupChecklistCardClass(done bool) string {
	if done {
		return "border-green-800/50 bg-green-950/20"
	}
	return "border-gray-800 bg-gray-950/40"
}

func setupChecklistBadgeClass(done bool) string {
	if done {
		return "relay-chip-green shrink-0"
	}
	return "relay-chip-gray shrink-0"
}

func shortenText(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" || max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}
