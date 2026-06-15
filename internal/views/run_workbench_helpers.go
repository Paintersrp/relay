package views

import (
	"strings"

	"relay/internal/pipeline"
	"relay/internal/store"
)

type SetupReviewChecklistItem struct {
	Label  string
	Detail string
	Done   bool
}

type PromptDetailRow struct {
	Label string
	Value string
}

type PromptChangeEvidence struct {
	RemovedCount   int
	AddedCount     int
	RemovedSamples []string
	AddedSamples   []string
	ScopedFiles    []string
	HasDiff        bool
}

type artifactGroupView struct {
	Label string
	Kinds []string
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

func promptChangeEvidence(previews RunPreviews, intakeReview *pipeline.IntakeReview) PromptChangeEvidence {
	evidence := PromptChangeEvidence{
		ScopedFiles: promptScopedFiles(intakeReview),
	}
	if len(previews.AgentPromptDiff.Lines) == 0 {
		return evidence
	}

	evidence.HasDiff = true
	for _, line := range previews.AgentPromptDiff.Lines {
		switch line.Kind {
		case "remove":
			evidence.RemovedCount++
			if sample := promptChangeEvidenceSample(line.Text); sample != "" {
				evidence.RemovedSamples = appendPromptChangeEvidenceSample(evidence.RemovedSamples, sample, 3)
			}
		case "add":
			evidence.AddedCount++
			if sample := promptChangeEvidenceSample(line.Text); sample != "" {
				evidence.AddedSamples = appendPromptChangeEvidenceSample(evidence.AddedSamples, sample, 3)
			}
		}
	}

	return evidence
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

func artifactGroupsForDisplay(artifacts []store.Artifact, groups []artifactGroupDef) []artifactGroupView {
	if len(groups) == 0 {
		return nil
	}
	rendered := make([]artifactGroupView, 0, len(groups))
	for _, group := range groups {
		if kinds := filterArtifactKinds(artifacts, group.Kinds); len(kinds) > 0 {
			rendered = append(rendered, artifactGroupView{
				Label: group.Label,
				Kinds: kinds,
			})
		}
	}
	return rendered
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

func promptChangeEvidenceSample(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "```") {
		return ""
	}
	if strings.Trim(text, "-=_*`~# ") == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	return shortenText(text, 120)
}

func appendPromptChangeEvidenceSample(samples []string, sample string, limit int) []string {
	for _, existing := range samples {
		if existing == sample {
			return samples
		}
	}
	if limit > 0 && len(samples) >= limit {
		return samples
	}
	return append(samples, sample)
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
