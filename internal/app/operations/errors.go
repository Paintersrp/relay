package operations

import "errors"

type Error struct {
	Code                   string
	Replacement            *ReplacementPacketIdentity
	MissingDependencyClass string
}

func (e *Error) Error() string {
	if e == nil {
		return "operation packet failure"
	}
	switch e.Code {
	case CodePacketNotFound:
		return "operation packet was not found"
	case CodePacketNotReady:
		return "operation packet is not ready"
	case CodePacketSuperseded:
		return "operation packet is superseded"
	case CodePacketClosed:
		return "operation packet is closed"
	case CodePacketRouteMismatch:
		return "operation packet route does not match"
	case CodePacketActionNotAllowed:
		return "operation packet action is not allowed"
	case CodePacketRefreshConflict:
		return "operation packet refresh conflict"
	case CodeRetainedAuthorityUnavailable:
		return "operation packet retained authority is unavailable"
	case CodeInvalidPacketDocument:
		return "operation packet document is invalid"
	case CodePacketArtifactMismatch:
		return "operation packet artifact identity does not match"
	case CodeAuthorityPublicationConflict:
		return "operation packet authority publication conflicts with committed authority"
	case CodeAuthorityPublicationFailure:
		return "operation packet authority publication failed"
	default:
		return "operation packet persistence failed"
	}
}
func ErrorCode(err error) string {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}
