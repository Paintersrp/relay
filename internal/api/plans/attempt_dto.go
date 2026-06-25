package plans

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appplans "relay/internal/app/plans"
)

type PlanArtifactRefAPI struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	ArtifactKind string `json:"artifactKind"`
}

type RawPlanJSONAPI struct {
	Content     json.RawMessage `json:"content"`
	ContentHash string          `json:"contentHash,omitempty"`
}

type IntentSourceAPI struct {
	CapturedFrom       string `json:"capturedFrom"`
	CapturedBy         string `json:"capturedBy"`
	SourceArtifactPath string `json:"sourceArtifactPath"`
}

type IntentPacketInputAPI struct {
	Summary            string          `json:"summary"`
	LiteralUserRequest string          `json:"literalUserRequest"`
	Constraints        []string        `json:"constraints"`
	Source             IntentSourceAPI `json:"source"`
	RedactionStatus    string          `json:"redactionStatus"`
	ContentHash        string          `json:"contentHash,omitempty"`
}

type DriftReviewInputAPI struct {
	IntentDriftReviewID    string          `json:"intentDriftReviewId,omitempty"`
	PlanAttemptID          string          `json:"planAttemptId,omitempty"`
	IntentThreadID         string          `json:"intentThreadId,omitempty"`
	RootIntentPacketID     string          `json:"rootIntentPacketId,omitempty"`
	ReviewedIntentPacketID string          `json:"reviewedIntentPacketId,omitempty"`
	ReviewPacketHash       string          `json:"reviewPacketHash"`
	ReviewSource           string          `json:"reviewSource"`
	SubmittedBy            string          `json:"submittedBy,omitempty"`
	SourceArtifactPath     string          `json:"sourceArtifactPath,omitempty"`
	OverallAlignment       string          `json:"overallAlignment"`
	Confidence             float64         `json:"confidence"`
	FindingsJSON           json.RawMessage `json:"findingsJson"`
	RecommendedAction      string          `json:"recommendedAction"`
	ApprovalGateStatus     string          `json:"approvalGateStatus"`
	ModelMetadataJSON      json.RawMessage `json:"modelMetadataJson,omitempty"`
	InputHash              string          `json:"inputHash"`
	OutputHash             string          `json:"outputHash"`
}

type CreatePlanAttemptWithIntentAPIRequest struct {
	PlanAttemptID       string               `json:"planAttemptId,omitempty"`
	IntentPacketID      string               `json:"intentPacketId,omitempty"`
	IntentThreadID      string               `json:"intentThreadId,omitempty"`
	PlanArtifactRef     PlanArtifactRefAPI   `json:"planArtifactRef"`
	OptionalMarkdownRef *PlanArtifactRefAPI  `json:"optionalMarkdownRef,omitempty"`
	RawPlanJSON         RawPlanJSONAPI       `json:"rawPlanJson"`
	DriftReviewMode     string               `json:"driftReviewMode,omitempty"`
	ModelTier           string               `json:"modelTier,omitempty"`
	IntentPacket        IntentPacketInputAPI `json:"intentPacket"`
}

type SubmitIntentDriftReviewAPIRequest struct {
	DriftReview DriftReviewInputAPI `json:"driftReview"`
}

type RevisePlanAttemptAPIRequest struct {
	NewPlanAttemptID    string               `json:"newPlanAttemptId,omitempty"`
	NewIntentPacketID   string               `json:"newIntentPacketId,omitempty"`
	PlanArtifactRef     PlanArtifactRefAPI   `json:"planArtifactRef"`
	OptionalMarkdownRef *PlanArtifactRefAPI  `json:"optionalMarkdownRef,omitempty"`
	RawPlanJSON         RawPlanJSONAPI       `json:"rawPlanJson"`
	NewIntentPacket     IntentPacketInputAPI `json:"newIntentPacket"`
}

type VoidPlanAttemptAPIRequest struct{}

type ApprovePlanAttemptAPIRequest struct {
	Approved                  bool   `json:"approved"`
	AcceptedDriftReviewID     string `json:"acceptedDriftReviewId,omitempty"`
	DriftAcknowledged         bool   `json:"driftAcknowledged"`
	NoDriftReviewAcknowledged bool   `json:"noDriftReviewAcknowledged"`
}

type SubmitPlanAttemptAPIRequest struct {
	SubmissionConfirmed            bool   `json:"submissionConfirmed"`
	ReviewedPlanJSONArtifactSHA256 string `json:"reviewedPlanJsonArtifactSha256"`
	AcceptedDriftReviewID          string `json:"acceptedDriftReviewId,omitempty"`
}

type PlanAttemptAPIResponse struct {
	Success      bool                             `json:"success"`
	BlockerCode  string                           `json:"blockerCode,omitempty"`
	Message      string                           `json:"message,omitempty"`
	IntentPacket *IntentPacketAPI                 `json:"intentPacket,omitempty"`
	PlanAttempt  *PlanAttemptAPI                  `json:"planAttempt,omitempty"`
	DriftReview  *IntentDriftReviewAPI            `json:"driftReview,omitempty"`
	Plan         *PlanAPIPlan                     `json:"plan,omitempty"`
	Passes       []PlanAPIPass                    `json:"passes,omitempty"`
	ReviewPacket *appplans.PlanIntentReviewPacket `json:"reviewPacket,omitempty"`
}

type IntentPacketAPI struct {
	ID                      string `json:"id"`
	IntentPacketID          string `json:"intentPacketId"`
	ProjectID               string `json:"projectId"`
	IntentThreadID          string `json:"intentThreadId"`
	RootIntentPacketID      string `json:"rootIntentPacketId"`
	ParentIntentPacketID    string `json:"parentIntentPacketId,omitempty"`
	RevisionOfPlanAttemptID string `json:"revisionOfPlanAttemptId,omitempty"`
	Kind                    string `json:"kind"`
	CapturedFrom            string `json:"capturedFrom"`
	CapturedBy              string `json:"capturedBy"`
	SourceArtifactPath      string `json:"sourceArtifactPath"`
	Summary                 string `json:"summary"`
	LiteralUserRequest      string `json:"literalUserRequest"`
	ConstraintsJSON         string `json:"constraintsJson"`
	RedactionStatus         string `json:"redactionStatus"`
	ContentHash             string `json:"contentHash"`
	CreatedAt               string `json:"createdAt"`
}

type PlanAttemptAPI struct {
	ID                         string `json:"id"`
	PlanAttemptID              string `json:"planAttemptId"`
	ProjectID                  string `json:"projectId"`
	IntentThreadID             string `json:"intentThreadId"`
	RootIntentPacketID         string `json:"rootIntentPacketId"`
	CurrentIntentPacketID      string `json:"currentIntentPacketId"`
	SupersedesPlanAttemptID    string `json:"supersedesPlanAttemptId,omitempty"`
	ReplacementPlanAttemptID   string `json:"replacementPlanAttemptId,omitempty"`
	Status                     string `json:"status"`
	ReviewState                string `json:"reviewState"`
	WorkflowState              string `json:"workflowState"`
	DriftReviewMode            string `json:"driftReviewMode"`
	ModelTier                  string `json:"modelTier"`
	PlanJsonArtifactPath       string `json:"planJsonArtifactPath"`
	PlanJsonArtifactSHA256     string `json:"planJsonArtifactSha256"`
	RawPlanJsonHash            string `json:"rawPlanJsonHash"`
	PlanMarkdownArtifactPath   string `json:"planMarkdownArtifactPath,omitempty"`
	PlanMarkdownArtifactSHA256 string `json:"planMarkdownArtifactSha256,omitempty"`
	AcceptedDriftReviewID      string `json:"acceptedDriftReviewId,omitempty"`
	SubmittedPlanID            string `json:"submittedPlanId,omitempty"`
	CreatedAt                  string `json:"createdAt"`
	UpdatedAt                  string `json:"updatedAt"`
}

type IntentDriftReviewAPI struct {
	ID                     string  `json:"id"`
	IntentDriftReviewID    string  `json:"intentDriftReviewId"`
	ProjectID              string  `json:"projectId"`
	PlanAttemptID          string  `json:"planAttemptId"`
	IntentThreadID         string  `json:"intentThreadId"`
	RootIntentPacketID     string  `json:"rootIntentPacketId"`
	ReviewedIntentPacketID string  `json:"reviewedIntentPacketId"`
	ReviewPacketHash       string  `json:"reviewPacketHash"`
	ReviewSource           string  `json:"reviewSource"`
	SubmittedBy            string  `json:"submittedBy"`
	SourceArtifactPath     string  `json:"sourceArtifactPath"`
	OverallAlignment       string  `json:"overallAlignment"`
	Confidence             float64 `json:"confidence"`
	FindingsJSON           string  `json:"findingsJson"`
	RecommendedAction      string  `json:"recommendedAction"`
	ApprovalGateStatus     string  `json:"approvalGateStatus"`
	InputHash              string  `json:"inputHash"`
	OutputHash             string  `json:"outputHash"`
	CreatedAt              string  `json:"createdAt"`
}

func (req CreatePlanAttemptWithIntentAPIRequest) toApp(projectID string) (appplans.CreatePlanAttemptWithIntentRequest, *appplans.PlanAttemptResult, error) {
	raw, blocked, err := rawPlanFromAttemptAPI(req.RawPlanJSON)
	if blocked != nil || err != nil {
		return appplans.CreatePlanAttemptWithIntentRequest{}, blocked, err
	}
	return appplans.CreatePlanAttemptWithIntentRequest{
		ProjectID:           strings.TrimSpace(projectID),
		PlanAttemptID:       req.PlanAttemptID,
		IntentPacketID:      req.IntentPacketID,
		IntentThreadID:      req.IntentThreadID,
		PlanArtifactRef:     artifactRefAPIToApp(req.PlanArtifactRef),
		OptionalMarkdownRef: artifactRefAPIToAppPtr(req.OptionalMarkdownRef),
		RawPlanJSON:         raw,
		DriftReviewMode:     req.DriftReviewMode,
		ModelTier:           req.ModelTier,
		IntentPacket:        intentPacketInputAPIToApp(req.IntentPacket),
	}, nil, nil
}

func (req RevisePlanAttemptAPIRequest) toApp(projectID, planAttemptID string) (appplans.RevisePlanAttemptRequest, *appplans.PlanAttemptResult, error) {
	raw, blocked, err := rawPlanFromAttemptAPI(req.RawPlanJSON)
	if blocked != nil || err != nil {
		return appplans.RevisePlanAttemptRequest{}, blocked, err
	}
	return appplans.RevisePlanAttemptRequest{
		ProjectID:           strings.TrimSpace(projectID),
		PlanAttemptID:       strings.TrimSpace(planAttemptID),
		NewPlanAttemptID:    req.NewPlanAttemptID,
		NewIntentPacketID:   req.NewIntentPacketID,
		PlanArtifactRef:     artifactRefAPIToApp(req.PlanArtifactRef),
		OptionalMarkdownRef: artifactRefAPIToAppPtr(req.OptionalMarkdownRef),
		RawPlanJSON:         raw,
		NewIntentPacket:     intentPacketInputAPIToApp(req.NewIntentPacket),
	}, nil, nil
}

func driftReviewAPIToApp(in DriftReviewInputAPI, planAttemptID string) appplans.DriftReviewInput {
	if strings.TrimSpace(in.PlanAttemptID) == "" {
		in.PlanAttemptID = planAttemptID
	}
	return appplans.DriftReviewInput{
		IntentDriftReviewID:    in.IntentDriftReviewID,
		PlanAttemptID:          in.PlanAttemptID,
		IntentThreadID:         in.IntentThreadID,
		RootIntentPacketID:     in.RootIntentPacketID,
		ReviewedIntentPacketID: in.ReviewedIntentPacketID,
		ReviewPacketHash:       in.ReviewPacketHash,
		ReviewSource:           in.ReviewSource,
		SubmittedBy:            in.SubmittedBy,
		SourceArtifactPath:     in.SourceArtifactPath,
		OverallAlignment:       in.OverallAlignment,
		Confidence:             in.Confidence,
		FindingsJSON:           in.FindingsJSON,
		RecommendedAction:      in.RecommendedAction,
		ApprovalGateStatus:     in.ApprovalGateStatus,
		ModelMetadataJSON:      in.ModelMetadataJSON,
		InputHash:              in.InputHash,
		OutputHash:             in.OutputHash,
	}
}

func rawPlanFromAttemptAPI(raw RawPlanJSONAPI) (json.RawMessage, *appplans.PlanAttemptResult, error) {
	if len(raw.Content) == 0 {
		return nil, &appplans.PlanAttemptResult{OK: false, BlockerCode: appplans.BlockerMissingPlanArtifact, Message: "rawPlanJson.content is required"}, nil
	}
	var doc any
	if err := json.Unmarshal(raw.Content, &doc); err != nil {
		return nil, &appplans.PlanAttemptResult{OK: false, BlockerCode: appplans.BlockerMissingPlanArtifact, Message: "rawPlanJson.content must be valid JSON"}, nil
	}
	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize raw plan JSON: %w", err)
	}
	if strings.TrimSpace(raw.ContentHash) != "" && raw.ContentHash != sha256BytesAPI(canonical) {
		return nil, &appplans.PlanAttemptResult{OK: false, BlockerCode: appplans.BlockerArtifactHashMismatch, Message: "rawPlanJson.contentHash does not match canonical raw plan JSON"}, nil
	}
	return json.RawMessage(canonical), nil, nil
}

func artifactRefAPIToApp(ref PlanArtifactRefAPI) appplans.PlanArtifactRef {
	return appplans.PlanArtifactRef{
		Path:         ref.Path,
		SHA256:       ref.SHA256,
		ArtifactKind: ref.ArtifactKind,
	}
}

func artifactRefAPIToAppPtr(ref *PlanArtifactRefAPI) *appplans.PlanArtifactRef {
	if ref == nil {
		return nil
	}
	appRef := artifactRefAPIToApp(*ref)
	return &appRef
}

func intentPacketInputAPIToApp(in IntentPacketInputAPI) appplans.IntentPacketInput {
	return appplans.IntentPacketInput{
		Summary:            in.Summary,
		LiteralUserRequest: in.LiteralUserRequest,
		Constraints:        in.Constraints,
		Source: appplans.IntentSource{
			CapturedFrom:       in.Source.CapturedFrom,
			CapturedBy:         in.Source.CapturedBy,
			SourceArtifactPath: in.Source.SourceArtifactPath,
		},
		RedactionStatus: in.RedactionStatus,
		ContentHash:     in.ContentHash,
	}
}

func mapPlanAttemptResultToAPI(result *appplans.PlanAttemptResult) PlanAttemptAPIResponse {
	if result == nil {
		return PlanAttemptAPIResponse{Success: false, Message: "plan attempt action returned no result"}
	}
	resp := PlanAttemptAPIResponse{
		Success:     result.OK,
		BlockerCode: string(result.BlockerCode),
		Message:     result.Message,
	}
	if result.IntentPacket != nil {
		v := mapIntentPacketToAPI(*result.IntentPacket)
		resp.IntentPacket = &v
	}
	if result.PlanAttempt != nil {
		v := mapPlanAttemptToAPI(*result.PlanAttempt)
		resp.PlanAttempt = &v
	}
	if result.DriftReview != nil {
		v := mapIntentDriftReviewToAPI(*result.DriftReview)
		resp.DriftReview = &v
	}
	if result.Plan != nil {
		v := mapPlanToAPI(*result.Plan)
		resp.Plan = &v
	}
	if len(result.Passes) > 0 {
		resp.Passes = make([]PlanAPIPass, 0, len(result.Passes))
		for _, pass := range result.Passes {
			resp.Passes = append(resp.Passes, mapPlanPassToAPI(pass, nil))
		}
	}
	resp.ReviewPacket = result.ReviewPacket
	return resp
}

func mapIntentPacketToAPI(packet appplans.IntentPacket) IntentPacketAPI {
	return IntentPacketAPI{
		ID:                      fmt.Sprintf("%d", packet.ID),
		IntentPacketID:          packet.IntentPacketID,
		ProjectID:               packet.ProjectID,
		IntentThreadID:          packet.IntentThreadID,
		RootIntentPacketID:      packet.RootIntentPacketID,
		ParentIntentPacketID:    packet.ParentIntentPacketID.String,
		RevisionOfPlanAttemptID: packet.RevisionOfPlanAttemptID.String,
		Kind:                    packet.Kind,
		CapturedFrom:            packet.CapturedFrom,
		CapturedBy:              packet.CapturedBy,
		SourceArtifactPath:      packet.SourceArtifactPath,
		Summary:                 packet.Summary,
		LiteralUserRequest:      packet.LiteralUserRequest,
		ConstraintsJSON:         packet.ConstraintsJson,
		RedactionStatus:         packet.RedactionStatus,
		ContentHash:             packet.ContentHash,
		CreatedAt:               packet.CreatedAt,
	}
}

func mapPlanAttemptToAPI(attempt appplans.PlanAttempt) PlanAttemptAPI {
	return PlanAttemptAPI{
		ID:                         fmt.Sprintf("%d", attempt.ID),
		PlanAttemptID:              attempt.PlanAttemptID,
		ProjectID:                  attempt.ProjectID,
		IntentThreadID:             attempt.IntentThreadID,
		RootIntentPacketID:         attempt.RootIntentPacketID,
		CurrentIntentPacketID:      attempt.CurrentIntentPacketID,
		SupersedesPlanAttemptID:    attempt.SupersedesPlanAttemptID.String,
		ReplacementPlanAttemptID:   attempt.ReplacementPlanAttemptID.String,
		Status:                     attempt.Status,
		ReviewState:                attempt.ReviewState,
		WorkflowState:              workflowState(attempt.Status, attempt.ReviewState),
		DriftReviewMode:            attempt.DriftReviewMode,
		ModelTier:                  attempt.ModelTier,
		PlanJsonArtifactPath:       attempt.PlanJsonArtifactPath,
		PlanJsonArtifactSHA256:     attempt.PlanJsonArtifactSha256,
		RawPlanJsonHash:            attempt.RawPlanJsonHash,
		PlanMarkdownArtifactPath:   attempt.PlanMarkdownArtifactPath.String,
		PlanMarkdownArtifactSHA256: attempt.PlanMarkdownArtifactSha256.String,
		AcceptedDriftReviewID:      attempt.AcceptedDriftReviewID.String,
		SubmittedPlanID:            attempt.SubmittedPlanID.String,
		CreatedAt:                  attempt.CreatedAt,
		UpdatedAt:                  attempt.UpdatedAt,
	}
}

func mapIntentDriftReviewToAPI(review appplans.IntentDriftReview) IntentDriftReviewAPI {
	return IntentDriftReviewAPI{
		ID:                     fmt.Sprintf("%d", review.ID),
		IntentDriftReviewID:    review.IntentDriftReviewID,
		ProjectID:              review.ProjectID,
		PlanAttemptID:          review.PlanAttemptID,
		IntentThreadID:         review.IntentThreadID,
		RootIntentPacketID:     review.RootIntentPacketID,
		ReviewedIntentPacketID: review.ReviewedIntentPacketID,
		ReviewPacketHash:       review.ReviewPacketHash,
		ReviewSource:           review.ReviewSource,
		SubmittedBy:            review.SubmittedBy,
		SourceArtifactPath:     review.SourceArtifactPath,
		OverallAlignment:       review.OverallAlignment,
		Confidence:             review.Confidence,
		FindingsJSON:           review.FindingsJson,
		RecommendedAction:      review.RecommendedAction,
		ApprovalGateStatus:     review.ApprovalGateStatus,
		InputHash:              review.InputHash,
		OutputHash:             review.OutputHash,
		CreatedAt:              review.CreatedAt,
	}
}

func workflowState(status, reviewState string) string {
	switch status {
	case appplans.PlanAttemptStatusApproved:
		return "approved_attempt_ready_to_submit"
	case appplans.PlanAttemptStatusSubmitted:
		return "managed_plan_created"
	case appplans.PlanAttemptStatusVoided:
		return "attempt_voided"
	case appplans.PlanAttemptStatusSuperseded:
		return "attempt_superseded_by_revision"
	}
	switch reviewState {
	case appplans.PlanAttemptReviewPacketReady:
		return "review_packet_available"
	case appplans.PlanAttemptReviewExternalSubmitted:
		return "external_review_recorded"
	case appplans.PlanAttemptReviewInternalGenerated:
		return "internal_review_recorded"
	default:
		return "draft_created"
	}
}

func attemptBlockerHTTPStatus(code appplans.PlanAttemptBlockerCode) int {
	switch code {
	case appplans.BlockerUnknownProject, appplans.BlockerUnknownAttempt:
		return 404
	case appplans.BlockerMissingPlanArtifact, appplans.BlockerMissingIntentPacket, appplans.BlockerUnsafeRetrieval:
		return 400
	case appplans.BlockerArtifactHashMismatch, appplans.BlockerStaleAttempt:
		return 409
	default:
		return 422
	}
}

func sha256BytesAPI(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
