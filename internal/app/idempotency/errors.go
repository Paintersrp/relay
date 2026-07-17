package idempotency

import "errors"

type ErrorCode string

const (
	ErrorInvalidMutationID       ErrorCode = "invalid_mutation_id"
	ErrorUnknownSurfaceContract  ErrorCode = "unknown_surface_contract"
	ErrorUnknownMutationTool     ErrorCode = "unknown_state_changing_tool"
	ErrorInvalidSemanticIdentity ErrorCode = "invalid_semantic_identity"
	ErrorInvalidResultIdentity   ErrorCode = "invalid_result_identity"
	ErrorMutationConflict        ErrorCode = "mutation_id_conflict"
	ErrorConcurrentWinner        ErrorCode = "mutation_concurrent_winner"
	ErrorCorruptStoredResult     ErrorCode = "corrupt_stored_result"
	ErrorStoreUnavailable        ErrorCode = "mutation_store_unavailable"
	ErrorTransactionIntegration  ErrorCode = "mutation_transaction_failed"
)

type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string {
	if e == nil || e.Code == "" {
		return "mutation request failed"
	}
	return "mutation request failed: " + string(e.Code)
}

func appError(code ErrorCode) error {
	return &Error{Code: code}
}

func HasCode(err error, code ErrorCode) bool {
	var value *Error
	return errors.As(err, &value) && value.Code == code
}

func IsConcurrentWinner(err error) bool {
	return HasCode(err, ErrorConcurrentWinner)
}
