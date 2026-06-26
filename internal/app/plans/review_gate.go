package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"relay/internal/store"
	"relay/internal/store/generated"
)

const (
	WorkflowReviewNotRequired          = "review_not_required"
	WorkflowManualReviewAvailable      = "manual_review_available"
	WorkflowAutomaticReviewPending     = "automatic_review_pending_or_failed"
	WorkflowExternalReviewRequired     = "external_review_required"
	WorkflowApprovalReady              = "approval_ready"
	WorkflowDriftAcknowledgementNeeded = "drift_acknowledgement_required"
	WorkflowRevisionRequired           = "revision_required"
	WorkflowDriftReviewBlocked         = "drift_review_blocked"
	WorkflowReadyForSubmission         = "ready_for_submission"
	WorkflowSubmitted                  = "submitted"
	WorkflowVoided                     = "voided"
	WorkflowSuperseded                 = "superseded"
)

type PlanAttemptReviewGateRequest struct {
	ProjectID     string
	PlanAttemptID string
}

type RunPlanAttemptDriftReviewRequest struct {
	ProjectID          string
	PlanAttemptID      string
	AllowModelCall     bool
	RequestedTier      string
	ForceHighAssurance bool
	AutomaticFlow      bool
}

type PreparedPlanAttemptDriftReview struct {
	ProjectID          string
	PlanAttemptID      string
	RequestedTier      string
	AllowModelCall     bool
	ForceHighAssurance bool
	ReviewGate         *PlanAttemptReviewGate
}

type PlanAttemptReviewGate struct {
	WorkflowState              string
	DriftReviewMode            string
	ModelTier                  string
	ReviewRequired             bool
	ModelCallAllowed           bool
	ModelCallWarning           string
	AcceptedDriftReviewID      string
	LatestReview               *store.IntentDriftReview
	AllowedActions             []string
	Blockers                   []PlanAttemptBlocker
	ExternalReviewInstructions *ExternalReviewInstructions
}

type PlanAttemptBlocker struct {
	Code        PlanAttemptBlockerCode
	Message     string
	Recoverable bool
}

type ExternalReviewInstructions struct {
	ReviewPacketRoute string
	SubmitReviewRoute string
}

func (svc *Service) GetPlanAttemptReviewGate(ctx context.Context, req PlanAttemptReviewGateRequest) (*PlanAttemptReviewGate, *PlanAttemptResult, error) {
	project, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return nil, blocked, err
	}
	gate, err := svc.buildReviewGate(ctx, *project, attempt)
	if err != nil {
		return nil, nil, err
	}
	return gate, nil, nil
}

func (svc *Service) PreparePlanAttemptDriftReview(ctx context.Context, req RunPlanAttemptDriftReviewRequest) (*PreparedPlanAttemptDriftReview, *PlanAttemptResult, error) {
	gate, blocked, err := svc.GetPlanAttemptReviewGate(ctx, PlanAttemptReviewGateRequest{
		ProjectID:     req.ProjectID,
		PlanAttemptID: req.PlanAttemptID,
	})
	if blocked != nil || err != nil {
		return nil, blocked, err
	}
	requestedTier := strings.TrimSpace(req.RequestedTier)
	if requestedTier == "" {
		requestedTier = gate.ModelTier
	} else if _, ok := normalizeModelTier(requestedTier); !ok {
		return nil, &PlanAttemptResult{
			OK:          false,
			BlockerCode: BlockerDriftReviewBlocked,
			Message:     "invalid model_tier",
			ReviewGate:  gate,
		}, nil
	}
	switch gate.DriftReviewMode {
	case DriftReviewModeDisabled:
		return nil, &PlanAttemptResult{
			OK:          false,
			BlockerCode: BlockerDriftReviewBlocked,
			Message:     "disabled drift review mode does not run internal reviews",
			ReviewGate:  gate,
		}, nil
	case DriftReviewModeExternal:
		return nil, &PlanAttemptResult{
			OK:          false,
			BlockerCode: BlockerDriftReviewRequired,
			Message:     "external drift review mode requires review packet retrieval and external review submission",
			ReviewGate:  gate,
		}, nil
	case DriftReviewModeManual:
		if !req.AllowModelCall {
			return nil, &PlanAttemptResult{
				OK:          false,
				BlockerCode: BlockerDriftReviewBlocked,
				Message:     "model call is not explicitly allowed in the request",
				ReviewGate:  gate,
				ReviewAction: &PlanAttemptReviewAction{
					Action:      "run_drift_review",
					OK:          false,
					FailureCode: "model_call_not_allowed",
					Message:     "model call is not explicitly allowed in the request",
				},
			}, nil
		}
	case DriftReviewModeAutomatic:
		req.AllowModelCall = true
	default:
		return nil, &PlanAttemptResult{
			OK:          false,
			BlockerCode: BlockerDriftReviewBlocked,
			Message:     "unknown drift review mode",
			ReviewGate:  gate,
		}, nil
	}
	return &PreparedPlanAttemptDriftReview{
		ProjectID:          req.ProjectID,
		PlanAttemptID:      req.PlanAttemptID,
		RequestedTier:      requestedTier,
		AllowModelCall:     req.AllowModelCall,
		ForceHighAssurance: req.ForceHighAssurance,
		ReviewGate:         gate,
	}, nil, nil
}

func (svc *Service) buildReviewGate(ctx context.Context, project store.Project, attempt store.PlanAttempt) (*PlanAttemptReviewGate, error) {
	settings, _, err := svc.loadPlanReviewSettings(ctx, project)
	if err != nil {
		return nil, err
	}
	q := generated.New(svc.store.DB())
	reviews, err := q.ListIntentDriftReviewsByAttempt(ctx, attempt.ID)
	if err != nil {
		return nil, err
	}
	var latest *store.IntentDriftReview
	if len(reviews) > 0 {
		latest = &reviews[0]
	}
	gate := &PlanAttemptReviewGate{
		DriftReviewMode:       attempt.DriftReviewMode,
		ModelTier:             attempt.ModelTier,
		ModelCallWarning:      settings.ManualModelCallWarning,
		AcceptedDriftReviewID: attempt.AcceptedDriftReviewID.String,
		LatestReview:          latest,
	}
	switch attempt.Status {
	case PlanAttemptStatusApproved:
		gate.WorkflowState = WorkflowReadyForSubmission
		gate.AllowedActions = []string{"submit_plan_attempt"}
		return gate, nil
	case PlanAttemptStatusSubmitted:
		gate.WorkflowState = WorkflowSubmitted
		return gate, nil
	case PlanAttemptStatusVoided:
		gate.WorkflowState = WorkflowVoided
		return gate, nil
	case PlanAttemptStatusSuperseded:
		gate.WorkflowState = WorkflowSuperseded
		return gate, nil
	}
	gate.AllowedActions = []string{"get_plan_intent_review_packet", "revise_plan_attempt", "void_plan_attempt"}
	switch attempt.DriftReviewMode {
	case DriftReviewModeDisabled:
		gate.WorkflowState = WorkflowReviewNotRequired
		gate.ReviewRequired = false
		gate.AllowedActions = append(gate.AllowedActions, "approve_plan_attempt")
		return gate, nil
	case DriftReviewModeManual:
		gate.ReviewRequired = false
		gate.ModelCallAllowed = true
		if latest == nil {
			gate.WorkflowState = WorkflowManualReviewAvailable
			gate.AllowedActions = append(gate.AllowedActions, "run_drift_review", "approve_plan_attempt_with_no_review_acknowledgement")
			return gate, nil
		}
	case DriftReviewModeAutomatic:
		gate.ReviewRequired = true
		gate.ModelCallAllowed = true
		if latest == nil || latest.ReviewSource != ReviewSourceInternal {
			gate.WorkflowState = WorkflowAutomaticReviewPending
			gate.AllowedActions = append(gate.AllowedActions, "run_drift_review")
			gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftReviewRequired, Message: "automatic drift review mode requires an internal review", Recoverable: true})
			return gate, nil
		}
	case DriftReviewModeExternal:
		gate.ReviewRequired = true
		gate.ExternalReviewInstructions = externalReviewInstructions(project.ProjectID, attempt.PlanAttemptID)
		if latest == nil || latest.ReviewSource != ReviewSourceExternal {
			gate.WorkflowState = WorkflowExternalReviewRequired
			gate.AllowedActions = append(gate.AllowedActions, "submit_intent_drift_review")
			gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftReviewRequired, Message: "external drift review mode requires an external review", Recoverable: true})
			return gate, nil
		}
	default:
		gate.WorkflowState = WorkflowDriftReviewBlocked
		gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerApprovalRequired, Message: "unknown drift review mode", Recoverable: false})
		return gate, nil
	}
	status := effectiveApprovalGateStatus(latest)
	switch status {
	case ApprovalGateStatusReady:
		gate.WorkflowState = WorkflowApprovalReady
		gate.AllowedActions = append(gate.AllowedActions, "approve_plan_attempt")
	case ApprovalGateStatusAckRequired:
		gate.WorkflowState = WorkflowDriftAcknowledgementNeeded
		gate.AllowedActions = append(gate.AllowedActions, "approve_plan_attempt_with_drift_acknowledgement")
		gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftAcknowledgementReq, Message: "drift acknowledgement is required", Recoverable: true})
	case ApprovalGateStatusRevisionRequired:
		gate.WorkflowState = WorkflowRevisionRequired
		gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftRevisionRequired, Message: "drift review requires revision", Recoverable: true})
	case ApprovalGateStatusBlocked, ApprovalGateStatusNotRequired:
		gate.WorkflowState = WorkflowDriftReviewBlocked
		gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftReviewBlocked, Message: "drift review blocks approval", Recoverable: false})
	default:
		gate.WorkflowState = WorkflowDriftReviewBlocked
		gate.Blockers = append(gate.Blockers, PlanAttemptBlocker{Code: BlockerDriftReviewBlocked, Message: "unknown approval gate status", Recoverable: false})
	}
	return gate, nil
}

func (svc *Service) approvalGateBlocker(ctx context.Context, project store.Project, attempt store.PlanAttempt, review store.IntentDriftReview, hasReview bool, req ApprovePlanAttemptRequest) (*PlanAttemptResult, error) {
	gate, err := svc.buildReviewGate(ctx, project, attempt)
	if err != nil {
		return nil, err
	}
	switch attempt.DriftReviewMode {
	case DriftReviewModeDisabled:
		if hasReview || strings.TrimSpace(req.AcceptedDriftReviewID) != "" {
			return blockAttempt(BlockerApprovalRequired, "disabled drift review mode must not accept a review")
		}
		return nil, nil
	case DriftReviewModeManual:
		if !hasReview {
			if !req.NoDriftReviewAcknowledged {
				return blockAttempt(BlockerDriftAcknowledgementReq, "manual approval without drift review requires no-review acknowledgement")
			}
			return nil, nil
		}
	case DriftReviewModeAutomatic:
		if !hasReview || review.ReviewSource != ReviewSourceInternal {
			return blockAttempt(BlockerDriftReviewRequired, "automatic drift review mode requires an internal review")
		}
	case DriftReviewModeExternal:
		if !hasReview || review.ReviewSource != ReviewSourceExternal {
			return blockAttempt(BlockerDriftReviewRequired, "external drift review mode requires an external review")
		}
	default:
		return blockAttempt(BlockerApprovalRequired, "unknown drift review mode")
	}
	switch gate.WorkflowState {
	case WorkflowApprovalReady:
		return nil, nil
	case WorkflowDriftAcknowledgementNeeded:
		if !req.DriftAcknowledged {
			return blockAttempt(BlockerDriftAcknowledgementReq, "drift acknowledgement is required")
		}
		return nil, nil
	case WorkflowRevisionRequired:
		return blockAttempt(BlockerDriftRevisionRequired, "drift review requires revision")
	case WorkflowDriftReviewBlocked:
		return blockAttempt(BlockerDriftReviewBlocked, "drift review blocks approval")
	default:
		if len(gate.Blockers) > 0 {
			b := gate.Blockers[0]
			return blockAttempt(b.Code, b.Message)
		}
		return blockAttempt(BlockerDriftReviewBlocked, "drift review is not approval-ready")
	}
}

func effectiveApprovalGateStatus(review *store.IntentDriftReview) string {
	if review == nil {
		return ""
	}
	status := review.ApprovalGateStatus
	severity := maxFindingSeverity(review.FindingsJson)
	if severity == "high" && weakerGate(status, ApprovalGateStatusRevisionRequired) {
		return ApprovalGateStatusRevisionRequired
	}
	if severity == "medium" && weakerGate(status, ApprovalGateStatusAckRequired) {
		return ApprovalGateStatusAckRequired
	}
	return status
}

func weakerGate(status string, minimum string) bool {
	rank := map[string]int{
		ApprovalGateStatusReady:            1,
		ApprovalGateStatusAckRequired:      2,
		ApprovalGateStatusRevisionRequired: 3,
		ApprovalGateStatusBlocked:          4,
	}
	return rank[status] < rank[minimum]
}

func maxFindingSeverity(findingsJSON string) string {
	var raw any
	if err := json.Unmarshal([]byte(findingsJSON), &raw); err != nil {
		return ""
	}
	max := ""
	visitFinding := func(m map[string]any) {
		sev, _ := m["severity"].(string)
		switch strings.ToLower(strings.TrimSpace(sev)) {
		case "high":
			max = "high"
		case "medium":
			if max != "high" {
				max = "medium"
			}
		}
	}
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				visitFinding(m)
			}
		}
	case map[string]any:
		if arr, ok := v["findings"].([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					visitFinding(m)
				}
			}
		} else {
			visitFinding(v)
		}
	}
	return max
}

func externalReviewInstructions(projectID, planAttemptID string) *ExternalReviewInstructions {
	return &ExternalReviewInstructions{
		ReviewPacketRoute: "/api/projects/" + projectID + "/plan-attempts/" + planAttemptID + "/intent-review-packet",
		SubmitReviewRoute: "/api/projects/" + projectID + "/plan-attempts/" + planAttemptID + "/intent-drift-reviews",
	}
}

func latestReviewForAttempt(ctx context.Context, q *generated.Queries, attempt store.PlanAttempt) (store.IntentDriftReview, bool, error) {
	reviews, err := q.ListIntentDriftReviewsByAttempt(ctx, attempt.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.IntentDriftReview{}, false, nil
		}
		return store.IntentDriftReview{}, false, err
	}
	if len(reviews) == 0 {
		return store.IntentDriftReview{}, false, nil
	}
	return reviews[0], true, nil
}
