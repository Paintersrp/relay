package plans

import appplans "relay/internal/app/plans"

type PlanReviewSettingsAPI struct {
	ProjectID               string `json:"projectId"`
	DriftReviewMode         string `json:"driftReviewMode"`
	ModelTier               string `json:"modelTier"`
	ManualModelCallWarning  string `json:"manualModelCallWarning"`
	AutomaticReviewEnabled  bool   `json:"automaticReviewEnabled"`
	ExternalReviewSupported bool   `json:"externalReviewSupported"`
	CreatedAt               string `json:"createdAt,omitempty"`
	UpdatedAt               string `json:"updatedAt,omitempty"`
}

type UpdatePlanReviewSettingsAPIRequest struct {
	DriftReviewMode string `json:"driftReviewMode"`
	ModelTier       string `json:"modelTier"`
}

type PlanReviewSettingsAPIResponse struct {
	Success     bool                   `json:"success"`
	BlockerCode string                 `json:"blockerCode,omitempty"`
	Message     string                 `json:"message,omitempty"`
	Settings    *PlanReviewSettingsAPI `json:"settings,omitempty"`
}

type RunPlanAttemptDriftReviewAPIRequest struct {
	AllowModelCall     bool   `json:"allowModelCall"`
	RequestedTier      string `json:"requestedTier,omitempty"`
	ForceHighAssurance bool   `json:"forceHighAssurance,omitempty"`
}

type PlanReviewPolicyAPI struct {
	ProjectID              string `json:"projectId"`
	DriftReviewMode        string `json:"driftReviewMode"`
	ModelTier              string `json:"modelTier"`
	ManualModelCallWarning string `json:"manualModelCallWarning"`
	Source                 string `json:"source"`
}

type PlanAttemptReviewActionAPI struct {
	Action           string `json:"action"`
	OK               bool   `json:"ok"`
	FailureCode      string `json:"failureCode,omitempty"`
	Message          string `json:"message,omitempty"`
	Escalated        bool   `json:"escalated,omitempty"`
	EscalationReason string `json:"escalationReason,omitempty"`
	FinalTier        string `json:"finalTier,omitempty"`
}

type PlanAttemptReviewGateAPI struct {
	WorkflowState              string                         `json:"workflowState"`
	DriftReviewMode            string                         `json:"driftReviewMode"`
	ModelTier                  string                         `json:"modelTier"`
	ReviewRequired             bool                           `json:"reviewRequired"`
	ModelCallAllowed           bool                           `json:"modelCallAllowed"`
	ModelCallWarning           string                         `json:"modelCallWarning,omitempty"`
	AcceptedDriftReviewID      string                         `json:"acceptedDriftReviewId,omitempty"`
	LatestReview               *IntentDriftReviewAPI          `json:"latestReview,omitempty"`
	AllowedActions             []string                       `json:"allowedActions"`
	Blockers                   []PlanAttemptBlockerAPI        `json:"blockers,omitempty"`
	ExternalReviewInstructions *ExternalReviewInstructionsAPI `json:"externalReviewInstructions,omitempty"`
}

type PlanAttemptReviewGateAPIResponse struct {
	Success     bool                      `json:"success"`
	BlockerCode string                    `json:"blockerCode,omitempty"`
	Message     string                    `json:"message,omitempty"`
	ReviewGate  *PlanAttemptReviewGateAPI `json:"reviewGate,omitempty"`
}

type PlanAttemptBlockerAPI struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable"`
}

type ExternalReviewInstructionsAPI struct {
	ReviewPacketRoute string `json:"reviewPacketRoute"`
	SubmitReviewRoute string `json:"submitReviewRoute"`
}

func mapPlanReviewSettingsToAPI(settings appplans.PlanReviewSettings) PlanReviewSettingsAPI {
	return PlanReviewSettingsAPI{
		ProjectID:               settings.ProjectID,
		DriftReviewMode:         settings.DriftReviewMode,
		ModelTier:               settings.ModelTier,
		ManualModelCallWarning:  settings.ManualModelCallWarning,
		AutomaticReviewEnabled:  settings.DriftReviewMode == appplans.DriftReviewModeAutomatic,
		ExternalReviewSupported: true,
		CreatedAt:               settings.CreatedAt,
		UpdatedAt:               settings.UpdatedAt,
	}
}

func mapReviewPolicyToAPI(policy appplans.EffectivePlanReviewPolicy) PlanReviewPolicyAPI {
	return PlanReviewPolicyAPI{
		ProjectID:              policy.ProjectID,
		DriftReviewMode:        policy.DriftReviewMode,
		ModelTier:              policy.ModelTier,
		ManualModelCallWarning: policy.ManualModelCallWarning,
		Source:                 policy.Source,
	}
}

func mapReviewActionToAPI(action appplans.PlanAttemptReviewAction) PlanAttemptReviewActionAPI {
	return PlanAttemptReviewActionAPI{
		Action:           action.Action,
		OK:               action.OK,
		FailureCode:      action.FailureCode,
		Message:          action.Message,
		Escalated:        action.Escalated,
		EscalationReason: action.EscalationReason,
		FinalTier:        action.FinalTier,
	}
}

func mapReviewGateToAPI(gate appplans.PlanAttemptReviewGate) PlanAttemptReviewGateAPI {
	api := PlanAttemptReviewGateAPI{
		WorkflowState:         gate.WorkflowState,
		DriftReviewMode:       gate.DriftReviewMode,
		ModelTier:             gate.ModelTier,
		ReviewRequired:        gate.ReviewRequired,
		ModelCallAllowed:      gate.ModelCallAllowed,
		ModelCallWarning:      gate.ModelCallWarning,
		AcceptedDriftReviewID: gate.AcceptedDriftReviewID,
		AllowedActions:        gate.AllowedActions,
	}
	if gate.LatestReview != nil {
		v := mapIntentDriftReviewToAPI(*gate.LatestReview)
		api.LatestReview = &v
	}
	if len(gate.Blockers) > 0 {
		api.Blockers = make([]PlanAttemptBlockerAPI, 0, len(gate.Blockers))
		for _, blocker := range gate.Blockers {
			api.Blockers = append(api.Blockers, PlanAttemptBlockerAPI{
				Code:        string(blocker.Code),
				Message:     blocker.Message,
				Recoverable: blocker.Recoverable,
			})
		}
	}
	if gate.ExternalReviewInstructions != nil {
		api.ExternalReviewInstructions = &ExternalReviewInstructionsAPI{
			ReviewPacketRoute: gate.ExternalReviewInstructions.ReviewPacketRoute,
			SubmitReviewRoute: gate.ExternalReviewInstructions.SubmitReviewRoute,
		}
	}
	return api
}
