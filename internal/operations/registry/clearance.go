package registry

import (
	"errors"
	"regexp"
)

const SensitiveDataClearancePolicyVersion = "relay.canonical-artifact-sensitive-data.v1"

type SensitiveDataClearance struct {
	PolicyVersion string                   `json:"policy_version"`
	SubjectSHA256 string                   `json:"subject_sha256"`
	Declaration   SensitiveDataDeclaration `json:"declaration"`
	Confirmed     bool                     `json:"confirmed"`
}

type SensitiveDataDeclaration struct {
	Password                             bool `json:"password"`
	APIKeyOrAccessToken                  bool `json:"api_key_or_access_token"`
	RefreshTokenOrSessionMaterial        bool `json:"refresh_token_or_session_material"`
	CookieOrAuthorizationHeader          bool `json:"cookie_or_authorization_header"`
	PrivateOrSSHKey                      bool `json:"private_or_ssh_key"`
	Credential                           bool `json:"credential"`
	CompleteSecretBearingEnvironmentFile bool `json:"complete_secret_bearing_environment_file"`
	AvoidableSignedSecretBearingURL      bool `json:"avoidable_signed_secret_bearing_url"`
}

var (
	ErrSensitiveDataClearance   = errors.New("invalid sensitive data clearance")
	ErrSensitiveDataDeclaration = errors.New("invalid sensitive data declaration")
	ErrMutationID               = errors.New("invalid mutation id")
)

func ValidateSensitiveDataClearance(value SensitiveDataClearance) error {
	if value.PolicyVersion != SensitiveDataClearancePolicyVersion || !validLowerSHA256(value.SubjectSHA256) || !value.Confirmed {
		return ErrSensitiveDataClearance
	}
	declaration := value.Declaration
	if declaration.Password || declaration.APIKeyOrAccessToken || declaration.RefreshTokenOrSessionMaterial || declaration.CookieOrAuthorizationHeader || declaration.PrivateOrSSHKey || declaration.Credential || declaration.CompleteSecretBearingEnvironmentFile || declaration.AvoidableSignedSecretBearingURL {
		return ErrSensitiveDataDeclaration
	}
	return nil
}

type MutationTool string

const (
	MutationToolCreateOperationPacket  MutationTool = "create_operation_packet"
	MutationToolRefreshOperationPacket MutationTool = "refresh_operation_packet"
	MutationToolCloseOperationPacket   MutationTool = "close_operation_packet"
	MutationToolSubmitPlan             MutationTool = "submit_plan"
	MutationToolCreateRun              MutationTool = "create_run"
	MutationToolRecordAuditDecision    MutationTool = "record_audit_decision"
)

var stateChangingTools = [...]MutationTool{
	MutationToolCreateOperationPacket,
	MutationToolRefreshOperationPacket,
	MutationToolCloseOperationPacket,
	MutationToolSubmitPlan,
	MutationToolCreateRun,
	MutationToolRecordAuditDecision,
}

var mutationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func StateChangingTools() []MutationTool {
	return append([]MutationTool(nil), stateChangingTools[:]...)
}

func IsStateChangingTool(tool string) bool {
	for _, candidate := range stateChangingTools {
		if string(candidate) == tool {
			return true
		}
	}
	return false
}

func IsStateChangingToolForSurface(surface SurfaceContractID, tool string) bool {
	if !IsStateChangingTool(tool) {
		return false
	}
	load()
	if loadErr != nil {
		return false
	}
	tools, ok := loaded.SurfaceTools[surface]
	if !ok {
		return false
	}
	_, ok = tools[tool]
	return ok
}

func ValidateMutationID(value string) error {
	if !mutationIDPattern.MatchString(value) {
		return ErrMutationID
	}
	return nil
}

func validLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
