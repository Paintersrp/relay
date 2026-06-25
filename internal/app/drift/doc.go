// Package drift provides the optional Relay-internal LLM intent drift reviewer service.
//
// The drift service orchestrates bounded model-backed intent drift reviews for plan attempts.
// It retrieves review packets from the plan attempt service, builds a bounded prompt/input
// payload, calls an injected model provider, validates the structured output against the
// intent_drift_review schema, and persists the result as an internal drift review through
// the plan attempt service.
//
// # Design Constraints
//
// The following constraints are enforced by this package:
//
//   - A model call is only performed when AllowModelCall=true is explicitly set in the request.
//   - Model input is derived solely from the PlanIntentReviewPacket. No live chat history,
//     arbitrary filesystem reads, GitHub state, or environment secrets are included.
//   - Model output cannot approve, submit, revise, void, create runs, dispatch executors,
//     mutate git, or directly change plan-attempt state. Persistence is always routed through
//     the PlanAttemptService.SubmitIntentDriftReview seam with review_source=internal.
//   - Structured output is validated against the intent_drift_review schema before persistence.
//     Invalid output does not persist.
//   - Auto-escalation is deterministic policy (tier upgrade on low confidence / unclear alignment)
//     and is not a semantic proof of alignment.
package drift
