package drift

import "context"

// DriftReviewer is a vendor-neutral, injectable interface for model-backed intent drift review.
//
// Implementations must:
//   - Accept a bounded ReviewModelRequest derived only from a PlanIntentReviewPacket.
//   - Return raw structured JSON in ReviewModelResponse.RawJSON that can be parsed as ModelOutput.
//   - Not perform any state mutation on Relay data.
//   - Not expose credentials, API keys, auth headers, or secrets in any returned value.
//
// A nil DriftReviewer causes the service to return FailureModelProviderUnavailable immediately,
// without attempting a model call. Tests should supply a fake implementation.
type DriftReviewer interface {
	ReviewIntentDrift(ctx context.Context, req ReviewModelRequest) (ReviewModelResponse, error)
}
