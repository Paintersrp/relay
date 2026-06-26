package drift

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	appplans "relay/internal/app/plans"
)

// PlanAttemptService interface matches the plans.Service seams we consume.
type PlanAttemptService interface {
	GetPlanIntentReviewPacket(ctx context.Context, req appplans.GetPlanIntentReviewPacketRequest) (*appplans.PlanAttemptResult, error)
	SubmitIntentDriftReview(ctx context.Context, req appplans.SubmitIntentDriftReviewRequest) (*appplans.PlanAttemptResult, error)
}

// Service orchestrates Relay-internal LLM intent drift reviews.
type Service struct {
	plans    PlanAttemptService
	reviewer DriftReviewer
	log      *slog.Logger
	now      func() time.Time
}

// NewService creates a new drift review service.
func NewService(plans PlanAttemptService, reviewer DriftReviewer, loggers ...*slog.Logger) *Service {
	var log *slog.Logger
	if len(loggers) > 0 {
		log = loggers[0]
	}
	return &Service{
		plans:    plans,
		reviewer: reviewer,
		log:      log,
		now:      time.Now,
	}
}

// NewReviewerFromEnv returns the configured production reviewer.
//
// No networked provider is configured in this pass, so the safe default is nil.
// RunInternalReview maps a nil reviewer to FailureModelProviderUnavailable
// before any packet retrieval, model call, or persistence can occur.
func NewReviewerFromEnv(log *slog.Logger) DriftReviewer {
	return nil
}

// RunInternalReview runs the internal drift review workflow.
func (svc *Service) RunInternalReview(ctx context.Context, req InternalReviewRequest) (*InternalReviewResult, error) {
	if !req.AllowModelCall {
		return fail(FailureModelCallNotAllowed, "model call is not explicitly allowed in the request"), nil
	}
	if svc == nil || svc.reviewer == nil {
		return fail(FailureModelProviderUnavailable, "no model reviewer provider is configured"), nil
	}
	if svc.plans == nil {
		return fail(FailurePacketRetrievalFailed, "plan attempt service is unavailable"), nil
	}

	res, err := svc.plans.GetPlanIntentReviewPacket(ctx, appplans.GetPlanIntentReviewPacketRequest{
		ProjectID:     req.ProjectID,
		PlanAttemptID: req.PlanAttemptID,
	})
	if err != nil {
		return fail(FailurePacketRetrievalFailed, fmt.Sprintf("failed to retrieve review packet: %v", err)), nil
	}
	if res == nil || !res.OK || res.ReviewPacket == nil {
		msg := "packet retrieval failed"
		if res != nil && res.Message != "" {
			msg = res.Message
		}
		return fail(FailurePacketRetrievalFailed, msg), nil
	}
	packet := res.ReviewPacket

	if !packet.RetrievalSemantics.RetrievalOnly || packet.RetrievalSemantics.ModelCallPerformed || packet.RetrievalSemantics.StateMutated {
		return fail(FailureUnsafeRetrievalSemantics, "review packet retrieval semantics are unsafe or incorrect"), nil
	}
	if packet.PlanAttempt.Status != appplans.PlanAttemptStatusDraft {
		return fail(FailureAttemptNotDraft, fmt.Sprintf("plan attempt is not in draft status (current: %s)", packet.PlanAttempt.Status)), nil
	}
	if packetBlockedSensitive(*packet) {
		return fail(FailurePacketBlockedSensitive, "packet contains blocked sensitive content and cannot be sent to the model"), nil
	}

	promptText, inputPayload, err := BuildPromptInput(*packet)
	if err != nil {
		return fail(FailureReviewGenerationFailed, fmt.Sprintf("failed to build model prompt input: %v", err)), nil
	}
	if containsSecretLikeContent([]byte(promptText)) || containsSecretLikeContent(inputPayload) {
		return fail(FailureSecretDetectedInPacket, "secret-like content detected in review packet input"), nil
	}

	currentTier := resolveStartTier(req)
	allowEscalation := strings.TrimSpace(req.RequestedTier) == appplans.ModelTierAutoEscalate && !req.ForceHighAssurance
	inputHash := sha256Bytes(inputPayload)
	submittedBy := strings.TrimSpace(req.SubmittedBy)
	if submittedBy == "" {
		submittedBy = SubmittedByInternalReviewer
	}

	var (
		providerRes ReviewModelResponse
		modelOut    ModelOutput
		outputHash  string
		callErr     error
		finalErr    error
		schemaErr   bool
	)
	callAndNormalize := func(tier string) error {
		providerRes, callErr = svc.reviewer.ReviewIntentDrift(ctx, ReviewModelRequest{
			Tier:         tier,
			PromptText:   promptText,
			InputPayload: inputPayload,
			SchemaHint:   intentDriftReviewSchemaBytes,
			Temperature:  0.0,
		})
		if callErr != nil {
			schemaErr = false
			return callErr
		}
		if providerRes.FinalTier == "" {
			providerRes.FinalTier = tier
		}
		outputHash = sha256Bytes(providerRes.RawJSON)

		modelOut, finalErr = ValidateModelOutput(providerRes.RawJSON)
		if finalErr != nil {
			schemaErr = true
			return finalErr
		}
		_, _, finalErr = NormalizeModelOutput(*packet, providerRes, modelOut, submittedBy, inputHash, outputHash, svc.clock().UTC())
		if finalErr != nil {
			schemaErr = false
			return finalErr
		}
		schemaErr = false
		return nil
	}

	finalErr = callAndNormalize(currentTier)
	escalated := false
	escReason := ""
	if allowEscalation && currentTier != appplans.ModelTierHighAssurance && (finalErr != nil || escalationRequired(modelOut, finalErr, false)) {
		escalated = true
		escReason = escalationReason(modelOut, finalErr, false)
		if escReason == "" {
			escReason = "escalation policy triggered"
		}
		currentTier = appplans.ModelTierHighAssurance
		finalErr = callAndNormalize(currentTier)
	}
	if finalErr != nil {
		code := FailureReviewGenerationFailed
		if schemaErr {
			code = FailureSchemaValidationFailed
		}
		return &InternalReviewResult{
			OK:               false,
			FailureCode:      code,
			Message:          fmt.Sprintf("review generation failed: %v", finalErr),
			Escalated:        escalated,
			EscalationReason: escReason,
			FinalTier:        currentTier,
		}, nil
	}

	driftInput, _, err := NormalizeModelOutput(*packet, providerRes, modelOut, submittedBy, inputHash, outputHash, svc.clock().UTC())
	if err != nil {
		return fail(FailureReviewGenerationFailed, fmt.Sprintf("failed to normalize drift review: %v", err)), nil
	}
	submitRes, err := svc.plans.SubmitIntentDriftReview(ctx, appplans.SubmitIntentDriftReviewRequest{
		ProjectID:     req.ProjectID,
		PlanAttemptID: req.PlanAttemptID,
		DriftReview:   driftInput,
	})
	if err != nil {
		return failWithContext(FailureReviewGenerationFailed, fmt.Sprintf("failed to submit drift review: %v", err), escalated, escReason, currentTier), nil
	}
	if submitRes == nil || !submitRes.OK {
		msg := "drift review submission rejected by plan service"
		if submitRes != nil && submitRes.Message != "" {
			msg = submitRes.Message
		}
		return failWithContext(FailureReviewGenerationFailed, msg, escalated, escReason, currentTier), nil
	}

	return &InternalReviewResult{
		OK:               true,
		Escalated:        escalated,
		EscalationReason: escReason,
		FinalTier:        currentTier,
		DriftReview:      submitRes.DriftReview,
	}, nil
}

func (svc *Service) clock() time.Time {
	if svc != nil && svc.now != nil {
		return svc.now()
	}
	return time.Now()
}

func fail(code ReviewFailureCode, msg string) *InternalReviewResult {
	return &InternalReviewResult{OK: false, FailureCode: code, Message: msg}
}

func failWithContext(code ReviewFailureCode, msg string, escalated bool, reason string, tier string) *InternalReviewResult {
	return &InternalReviewResult{OK: false, FailureCode: code, Message: msg, Escalated: escalated, EscalationReason: reason, FinalTier: tier}
}

func packetBlockedSensitive(packet appplans.PlanIntentReviewPacket) bool {
	return packet.RedactionStatus == appplans.RedactionStatusBlockedSensitive ||
		packet.RootIntentPacket.RedactionStatus == appplans.RedactionStatusBlockedSensitive ||
		packet.ReviewedIntentPacket.RedactionStatus == appplans.RedactionStatusBlockedSensitive
}

var secretLikePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|auth[_-]?header|private[_-]?key|session[_-]?cookie|password)\s*[:=]`),
	regexp.MustCompile(`-----BEGIN (RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]{20,}`),
}

func containsSecretLikeContent(b []byte) bool {
	for _, pattern := range secretLikePatterns {
		if pattern.Match(b) {
			return true
		}
	}
	return false
}

func sha256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func resolveStartTier(req InternalReviewRequest) string {
	if req.ForceHighAssurance {
		return appplans.ModelTierHighAssurance
	}
	switch strings.TrimSpace(req.RequestedTier) {
	case appplans.ModelTierEconomy, appplans.ModelTierStandard, appplans.ModelTierHighAssurance:
		return strings.TrimSpace(req.RequestedTier)
	case appplans.ModelTierAutoEscalate:
		return appplans.ModelTierStandard
	default:
		return appplans.ModelTierStandard
	}
}
