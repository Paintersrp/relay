package canonical

import (
	"errors"

	"relay/internal/speccompiler"
)

type ErrorCode string

const (
	ErrorInvalidArtifactKind    ErrorCode = "invalid_artifact_kind"
	ErrorInvalidExpectedHash    ErrorCode = "invalid_expected_hash"
	ErrorExpectedHashMismatch   ErrorCode = "expected_hash_mismatch"
	ErrorCompilerRejected       ErrorCode = "compiler_rejected"
	ErrorProjectNotFound        ErrorCode = "project_not_found"
	ErrorUnknownResource        ErrorCode = "unknown_resource"
	ErrorProjectArchived        ErrorCode = "project_archived"
	ErrorRepositoryNotFound     ErrorCode = "repository_not_found"
	ErrorPlanPassAssociation    ErrorCode = "plan_pass_association"
	ErrorSelectedPassFilename   ErrorCode = "selected_pass_filename"
	ErrorRemediationAssociation ErrorCode = "remediation_association"
	ErrorPersistence            ErrorCode = "persistence_failed"
)

type ApplicationError struct {
	Code        ErrorCode
	Message     string
	Ref         string
	Recoverable bool
	Diagnostics []speccompiler.Diagnostic
	Notices     []speccompiler.Diagnostic
	Cause       error
}

func (e *ApplicationError) Error() string {
	return e.Message
}

func (e *ApplicationError) Unwrap() error {
	return e.Cause
}

func AsApplicationError(err error) (*ApplicationError, bool) {
	var target *ApplicationError
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}

func applicationError(code ErrorCode, message, ref string, recoverable bool, cause error) *ApplicationError {
	return &ApplicationError{
		Code:        code,
		Message:     message,
		Ref:         ref,
		Recoverable: recoverable,
		Diagnostics: []speccompiler.Diagnostic{},
		Notices:     []speccompiler.Diagnostic{},
		Cause:       cause,
	}
}

func compilerError(message, ref string, diagnostics, notices []speccompiler.Diagnostic) *ApplicationError {
	return &ApplicationError{
		Code:        ErrorCompilerRejected,
		Message:     message,
		Ref:         ref,
		Recoverable: true,
		Diagnostics: boundedDiagnostics(diagnostics),
		Notices:     boundedDiagnostics(notices),
	}
}
