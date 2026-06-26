package drift

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appplans "relay/internal/app/plans"
)

type normalizedFinding struct {
	FindingID           string   `json:"finding_id"`
	Severity            string   `json:"severity"`
	Summary             string   `json:"summary"`
	Evidence            []string `json:"evidence"`
	SuggestedResolution string   `json:"suggested_resolution"`
}

type ModelMetadata struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	ModelTier   string  `json:"model_tier"`
	Temperature float64 `json:"temperature"`
}

type contractReview struct {
	IntentDriftReviewID    string              `json:"intent_drift_review_id"`
	ProjectID              string              `json:"project_id"`
	PlanAttemptID          string              `json:"plan_attempt_id"`
	IntentThreadID         string              `json:"intent_thread_id"`
	RootIntentPacketID     string              `json:"root_intent_packet_id"`
	ReviewedIntentPacketID string              `json:"reviewed_intent_packet_id"`
	ReviewPacketHash       string              `json:"review_packet_hash"`
	Provenance             contractProvenance  `json:"provenance"`
	OverallAlignment       string              `json:"overall_alignment"`
	Confidence             float64             `json:"confidence"`
	Findings               []normalizedFinding `json:"findings"`
	RecommendedAction      string              `json:"recommended_action"`
	ApprovalGateStatus     string              `json:"approval_gate_status"`
	ModelMetadata          ModelMetadata       `json:"model_metadata"`
	InputHash              string              `json:"input_hash"`
	OutputHash             string              `json:"output_hash"`
	CreatedAt              string              `json:"created_at"`
	Notes                  string              `json:"notes,omitempty"`
}

type contractProvenance struct {
	ReviewSource       string `json:"review_source"`
	SubmittedBy        string `json:"submitted_by"`
	SourceArtifactPath string `json:"source_artifact_path,omitempty"`
}

// ValidateModelOutput parses and validates the model-native structured output.
func ValidateModelOutput(raw []byte) (ModelOutput, error) {
	var out ModelOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return ModelOutput{}, fmt.Errorf("model output must be valid JSON: %w", err)
	}
	if !knownOverallAlignment(out.OverallAlignment) {
		return ModelOutput{}, fmt.Errorf("unknown overall_alignment %q", out.OverallAlignment)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		return ModelOutput{}, fmt.Errorf("confidence must be between 0 and 1")
	}
	if !knownRecommendedAction(out.RecommendedAction) {
		return ModelOutput{}, fmt.Errorf("unknown recommended_action %q", out.RecommendedAction)
	}
	if strings.TrimSpace(out.ApprovalGateStatus) == "" {
		return ModelOutput{}, fmt.Errorf("approval_gate_status is required")
	}
	if _, err := normalizeFindings(out); err != nil {
		return ModelOutput{}, err
	}
	return out, nil
}

// NormalizeModelOutput converts model-native output into the app-layer persistence input
// and a contract-shaped JSON document suitable for audit/schema validation.
func NormalizeModelOutput(
	packet appplans.PlanIntentReviewPacket,
	response ReviewModelResponse,
	out ModelOutput,
	submittedBy string,
	inputHash string,
	outputHash string,
	now time.Time,
) (appplans.DriftReviewInput, []byte, error) {
	findings, err := normalizeFindings(out)
	if err != nil {
		return appplans.DriftReviewInput{}, nil, err
	}
	if strings.TrimSpace(submittedBy) == "" {
		submittedBy = SubmittedByInternalReviewer
	}
	tier := strings.TrimSpace(response.FinalTier)
	if tier == "" {
		tier = appplans.ModelTierStandard
	}
	meta := ModelMetadata{
		Provider:    response.Provider,
		Model:       response.Model,
		ModelTier:   tier,
		Temperature: response.Temperature,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return appplans.DriftReviewInput{}, nil, fmt.Errorf("marshal model metadata: %w", err)
	}
	findingsBytes, err := json.Marshal(findings)
	if err != nil {
		return appplans.DriftReviewInput{}, nil, fmt.Errorf("marshal findings: %w", err)
	}
	createdAt := now.UTC().Format(time.RFC3339)
	reviewID := generateReviewID(now.UTC(), packet.PlanAttemptID)
	gate := normalizedGateStatus(out.RecommendedAction)
	provenance := contractProvenance{
		ReviewSource: appplans.ReviewSourceInternal,
		SubmittedBy:  submittedBy,
	}
	if path := strings.TrimSpace(packet.ReviewedIntentPacket.SourceArtifactPath); path != "" {
		provenance.SourceArtifactPath = path
	}

	input := appplans.DriftReviewInput{
		IntentDriftReviewID:    reviewID,
		PlanAttemptID:          packet.PlanAttemptID,
		IntentThreadID:         packet.IntentThreadID,
		RootIntentPacketID:     packet.RootIntentPacket.IntentPacketID,
		ReviewedIntentPacketID: packet.ReviewedIntentPacket.IntentPacketID,
		ReviewPacketHash:       packet.PacketHash,
		ReviewSource:           appplans.ReviewSourceInternal,
		SubmittedBy:            submittedBy,
		SourceArtifactPath:     packet.ReviewedIntentPacket.SourceArtifactPath,
		OverallAlignment:       out.OverallAlignment,
		Confidence:             out.Confidence,
		FindingsJSON:           findingsBytes,
		RecommendedAction:      out.RecommendedAction,
		ApprovalGateStatus:     gate,
		ModelMetadataJSON:      metaBytes,
		InputHash:              inputHash,
		OutputHash:             outputHash,
	}
	contract := contractReview{
		IntentDriftReviewID:    reviewID,
		ProjectID:              packet.ProjectID,
		PlanAttemptID:          packet.PlanAttemptID,
		IntentThreadID:         packet.IntentThreadID,
		RootIntentPacketID:     packet.RootIntentPacket.IntentPacketID,
		ReviewedIntentPacketID: packet.ReviewedIntentPacket.IntentPacketID,
		ReviewPacketHash:       packet.PacketHash,
		Provenance:             provenance,
		OverallAlignment:       out.OverallAlignment,
		Confidence:             out.Confidence,
		Findings:               findings,
		RecommendedAction:      out.RecommendedAction,
		ApprovalGateStatus:     gate,
		ModelMetadata:          meta,
		InputHash:              inputHash,
		OutputHash:             outputHash,
		CreatedAt:              createdAt,
		Notes:                  out.Notes,
	}
	contractBytes, err := json.Marshal(contract)
	if err != nil {
		return appplans.DriftReviewInput{}, nil, fmt.Errorf("marshal contract review: %w", err)
	}
	if err := ValidateIntentDriftReviewJSON(contractBytes); err != nil {
		return appplans.DriftReviewInput{}, nil, err
	}
	return input, contractBytes, nil
}

func normalizeFindings(out ModelOutput) ([]normalizedFinding, error) {
	if len(out.Findings) == 0 {
		out.Findings = json.RawMessage("[]")
	}
	var findings []normalizedFinding
	if err := json.Unmarshal(out.Findings, &findings); err != nil {
		return nil, fmt.Errorf("findings must be a JSON array with contract-shaped objects: %w", err)
	}
	if len(findings) == 0 {
		if out.OverallAlignment == appplans.OverallAlignmentAligned && out.RecommendedAction == appplans.RecommendedActionApprove {
			return findings, nil
		}
		return nil, fmt.Errorf("findings may be empty only when overall_alignment=aligned and recommended_action=approve")
	}
	for i := range findings {
		f := findings[i]
		if strings.TrimSpace(f.FindingID) == "" {
			return nil, fmt.Errorf("findings[%d].finding_id is required", i)
		}
		if !knownFindingSeverity(f.Severity) {
			return nil, fmt.Errorf("findings[%d].severity must be info, low, medium, or high", i)
		}
		if strings.TrimSpace(f.Summary) == "" {
			return nil, fmt.Errorf("findings[%d].summary is required", i)
		}
		if strings.TrimSpace(f.SuggestedResolution) == "" {
			return nil, fmt.Errorf("findings[%d].suggested_resolution is required", i)
		}
		nonEmptyEvidence := make([]string, 0, len(f.Evidence))
		for _, evidence := range f.Evidence {
			if strings.TrimSpace(evidence) != "" {
				nonEmptyEvidence = append(nonEmptyEvidence, evidence)
			}
		}
		if len(nonEmptyEvidence) == 0 {
			return nil, fmt.Errorf("findings[%d].evidence must contain at least one non-empty string", i)
		}
		findings[i].Evidence = nonEmptyEvidence
	}
	return findings, nil
}

func generateReviewID(t time.Time, seed string) string {
	slug := sanitizeReviewIDSlug(seed)
	if slug == "" {
		slug = fmt.Sprintf("%d", t.UnixNano())
	}
	return fmt.Sprintf("intent-drift-review-%s-%s-%d", t.Format("2006-01-02"), slug, t.UnixNano())
}

func sanitizeReviewIDSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func knownOverallAlignment(value string) bool {
	switch value {
	case appplans.OverallAlignmentAligned, appplans.OverallAlignmentMinorDrift, appplans.OverallAlignmentMajorDrift, appplans.OverallAlignmentUnclear:
		return true
	default:
		return false
	}
}

func knownRecommendedAction(value string) bool {
	switch value {
	case appplans.RecommendedActionApprove, appplans.RecommendedActionApproveWithAck, appplans.RecommendedActionRevise, appplans.RecommendedActionVoid, appplans.RecommendedActionManualReview:
		return true
	default:
		return false
	}
}

func knownFindingSeverity(value string) bool {
	switch value {
	case "info", "low", "medium", "high":
		return true
	default:
		return false
	}
}
