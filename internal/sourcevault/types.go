package sourcevault

import (
	"errors"

	workflowrepos "relay/internal/repos/workflow"
	workflowstore "relay/internal/store/workflow"
)

const (
	CodeInvalidRequest           = "invalid_request"
	CodeRepositoryMismatch       = "repository_mismatch"
	CodeStaleConfiguredAuthority = "stale_configured_authority"
	CodeImportInProgress         = "import_in_progress"
	CodeReleaseInProgress        = "release_in_progress"
	CodeSourceObjectUnavailable  = "source_object_unavailable"
	CodeVaultUnavailable         = "vault_unavailable"
	CodeObjectMismatch           = "object_mismatch"
	CodeRetentionConflict        = "retention_conflict"
	CodeCleanupBlocked           = "cleanup_blocked"
	CodeStateConflict            = "state_conflict"
	CodeDatabaseFailure          = "database_failure"
	CodeUnsafeVaultRoot          = "unsafe_vault_root"
	CodeOperationCancelled       = "operation_cancelled"
	CodeObjectLimitExceeded      = "object_limit_exceeded"
	CodeObjectUnavailable        = "object_unavailable"
	CodeInternal                 = "internal"
)

const MaxObjectReadBytes int64 = 64 << 20

type Error struct {
	Code          string
	FailureReason string
}

func (e *Error) Error() string {
	if e == nil {
		return "source vault failure"
	}
	switch e.Code {
	case CodeInvalidRequest:
		return "source vault request is invalid"
	case CodeRepositoryMismatch:
		return "resolved repository authority does not match the registered repository"
	case CodeStaleConfiguredAuthority:
		return "configured repository authority is stale"
	case CodeImportInProgress:
		return "source vault import is already in progress"
	case CodeReleaseInProgress:
		return "source vault release is already in progress"
	case CodeSourceObjectUnavailable:
		return "required source Git object is unavailable"
	case CodeVaultUnavailable:
		return "source vault retained authority is unavailable"
	case CodeObjectMismatch:
		return "source vault Git object identity does not match"
	case CodeRetentionConflict:
		return "source vault retention identity conflicts with existing authority"
	case CodeCleanupBlocked:
		return "source vault cleanup is blocked by retained authority"
	case CodeStateConflict:
		return "source vault state changed concurrently"
	case CodeDatabaseFailure:
		return "source vault persistence is unavailable"
	case CodeUnsafeVaultRoot:
		return "source vault storage overlaps registered source authority"
	case CodeOperationCancelled:
		return "source vault operation was cancelled"
	case CodeObjectLimitExceeded:
		return "source vault object exceeds the requested byte limit"
	case CodeObjectUnavailable:
		return "requested source vault object is unavailable"
	default:
		return "source vault operation failed"
	}
}

func ErrorCode(err error) string {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

func FailureReason(err error) string {
	var value *Error
	if errors.As(err, &value) {
		return value.FailureReason
	}
	return ""
}

type ImportRequest struct {
	Revision workflowrepos.ResolvedRevision
}

type ImportResult struct {
	Vault     workflowstore.SourceVault
	Closure   workflowstore.SourceVaultClosure
	CommitOID string
	TreeOID   string
	RefName   string
	Ready     bool
}

type RetainRequest struct {
	ClosureID       string
	OwnerClass      string
	OwnerIdentity   string
	PacketID        string
	DependencyClass string
	DependencyKey   string
}

// PreparedInvestigationRetention is verified source authority that can be
// retained with an investigation row in one workflow transaction.
type PreparedInvestigationRetention struct {
	OwnerIdentity string
	Vault         workflowstore.SourceVault
	Closure       workflowstore.SourceVaultClosure
}

type ReadObjectRequest struct {
	ClosureID    string
	ObjectOID    string
	ExpectedType string
	MaxBytes     int64
}

type ReadObjectResult struct {
	ObjectOID  string
	ObjectType string
	Bytes      []byte
}

type gitFailure struct {
	reason string
	code   string
	err    error
}

func (e *gitFailure) Error() string {
	return "git operation failed"
}

func (e *gitFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}
