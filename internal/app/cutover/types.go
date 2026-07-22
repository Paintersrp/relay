package cutover

import (
	"errors"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrCutoverNotFound              = errors.New("cutover activation not found")
	ErrCutoverAlreadyActive         = errors.New("a cutover activation is already active")
	ErrCutoverNotReady              = errors.New("cutover activation is not ready")
	ErrCutoverNotActive             = errors.New("cutover activation is not active")
	ErrCutoverRollbackBlocked       = errors.New("cutover rollback is blocked after first new execution")
	ErrCutoverBoundaryCrossed       = errors.New("cutover execution boundary is already crossed")
	ErrCutoverBoundaryQualification = errors.New("Run does not qualify for cutover boundary crossing")
	ErrCutoverConfigurationInvalid  = errors.New("cutover gateway configuration is invalid")
	ErrCutoverConfigurationMismatch = errors.New("persisted cutover gateway configuration does not match its digest")
	ErrLegacyAdmissionClosed        = errors.New("legacy admission is closed after cutover activation")
)

type State struct {
	ActivationID             string                        `json:"activationId"`
	Status                   string                        `json:"status"`
	BoundaryStatus           string                        `json:"boundaryStatus"`
	RollbackStatus           string                        `json:"rollbackStatus"`
	RollForwardStatus        string                        `json:"rollForwardStatus"`
	ActivatedAt              *string                       `json:"activatedAt,omitempty"`
	AggregateAdmissionClosed bool                          `json:"aggregateAdmissionClosed"`
	GatewayConfiguration     *GatewayConfigurationIdentity `json:"gatewayConfiguration,omitempty"`
}

func stateFrom(activation workflowstore.CutoverActivation) State {
	result := State{
		ActivationID:             activation.CutoverActivationID,
		Status:                   activation.ActivationStatus,
		BoundaryStatus:           activation.ExecutionBoundaryStatus,
		RollbackStatus:           activation.RollbackStatus,
		RollForwardStatus:        activation.RollForwardStatus,
		AggregateAdmissionClosed: activation.ActivationStatus == "active",
	}
	if activation.ActivatedAt.Valid {
		result.ActivatedAt = &activation.ActivatedAt.String
	}
	return result
}

type Readiness struct {
	Ready                    bool                          `json:"ready"`
	Prepared                 bool                          `json:"prepared"`
	Active                   bool                          `json:"active"`
	BoundaryCrossed          bool                          `json:"boundaryCrossed"`
	AggregateAdmissionClosed bool                          `json:"aggregateAdmissionClosed"`
	Prerequisites            []string                      `json:"prerequisites"`
	Obligations              []string                      `json:"obligations"`
	RollForwardCriteria      []string                      `json:"rollForwardCriteria"`
	Evidence                 []PrerequisiteEvidence        `json:"evidence"`
	ActivationEvidence       []ObligationEvidence          `json:"activationEvidence"`
	GatewayConfiguration     *GatewayConfigurationIdentity `json:"gatewayConfiguration,omitempty"`
	ConfigurationErrors      []string                      `json:"configurationErrors"`
}

type PrerequisiteEvidence struct {
	Prerequisite string `json:"prerequisite"`
	Evidence     string `json:"evidence"`
}

type ObligationEvidence struct {
	Kind       string `json:"kind"`
	Obligation string `json:"obligation"`
	Evidence   string `json:"evidence"`
}

type RouteIdentity struct {
	Sequence           int64  `json:"sequence"`
	RoutePath          string `json:"routePath"`
	Role               string `json:"role"`
	SurfaceContractID  string `json:"surfaceContractId"`
	ManifestSHA256     string `json:"manifestSha256"`
	AuthorityCommitOID string `json:"authorityCommitOid"`
	AuthorityBlobOID   string `json:"authorityBlobOid"`
}

type MappingIdentity struct {
	Sequence             int64  `json:"sequence"`
	MappingID            string `json:"mappingId"`
	RoutePath            string `json:"routePath"`
	ListenerIdentity     string `json:"listenerIdentity"`
	UpstreamIdentity     string `json:"upstreamIdentity"`
	HealthEvidenceSHA256 string `json:"healthEvidenceSha256"`
	TraceEvidenceSHA256  string `json:"traceEvidenceSha256"`
}

type AppSurfaceIdentity struct {
	Sequence       int64  `json:"sequence"`
	Surface        string `json:"surface"`
	PublicPath     string `json:"publicPath"`
	ManifestSHA256 string `json:"manifestSha256"`
}

type RouteMembershipIdentity struct {
	RoutePath     string `json:"routePath"`
	PublicSurface string `json:"publicSurface"`
}

type AppSurfaceMappingIdentity struct {
	Sequence             int64  `json:"sequence"`
	MappingID            string `json:"mappingId"`
	PublicSurface        string `json:"publicSurface"`
	PublicPath           string `json:"publicPath"`
	ListenerIdentity     string `json:"listenerIdentity"`
	UpstreamIdentity     string `json:"upstreamIdentity"`
	HealthEvidenceSHA256 string `json:"healthEvidenceSha256"`
	TraceEvidenceSHA256  string `json:"traceEvidenceSha256"`
}

type StandingAuthorityIdentity struct {
	Role          string `json:"role"`
	Repository    string `json:"repository"`
	CommitOID     string `json:"commitOid"`
	Path          string `json:"path"`
	BlobOID       string `json:"blobOid"`
	ContentSHA256 string `json:"contentSha256"`
}

type DependencyOutcomeIdentity struct {
	Sequence       int64  `json:"sequence"`
	TicketID       string `json:"ticketId"`
	TicketRevision int64  `json:"ticketRevision"`
	Outcome        string `json:"outcome"`
	EvidenceSHA256 string `json:"evidenceSha256"`
}

type GatewayConfigurationIdentity struct {
	ConfigurationSHA256 string                      `json:"configurationSha256"`
	RelayRepository     string                      `json:"relayRepository"`
	RelayCommitOID      string                      `json:"relayCommitOid"`
	StandingRepository  string                      `json:"standingRepository"`
	StandingCommitOID   string                      `json:"standingCommitOid"`
	Routes              []RouteIdentity             `json:"routes"`
	Mappings            []MappingIdentity           `json:"mappings,omitempty"`
	StandingAuthorities []StandingAuthorityIdentity `json:"standingAuthorities"`
	DependencyOutcomes  []DependencyOutcomeIdentity `json:"dependencyOutcomes"`
	TopologyVersion     string                      `json:"topologyVersion,omitempty"`
	AppSurfaces         []AppSurfaceIdentity        `json:"appSurfaces,omitempty"`
	RouteMemberships    []RouteMembershipIdentity   `json:"routeMemberships,omitempty"`
	AppSurfaceMappings  []AppSurfaceMappingIdentity `json:"appSurfaceMappings,omitempty"`
}

type PrepareRequest struct {
	ActivationID                      string                       `json:"activationId"`
	WorkspaceRowID                    int64                        `json:"workspaceRowId"`
	TransitionPlanTicketRevisionRowID int64                        `json:"transitionPlanTicketRevisionRowId"`
	TransitionPlanTicketID            string                       `json:"transitionPlanTicketId"`
	TransitionPlanTicketRevision      int64                        `json:"transitionPlanTicketRevision"`
	TransitionPlanAuthorityLayerRowID int64                        `json:"transitionPlanAuthorityLayerRowId"`
	TransitionPlanSHA256              string                       `json:"transitionPlanSha256"`
	AuthorityRevisionRowID            int64                        `json:"authorityRevisionRowId"`
	AuthorityRevisionID               string                       `json:"authorityRevisionId"`
	AuthorityRevisionNumber           int64                        `json:"authorityRevisionNumber"`
	AuthoritySHA256                   string                       `json:"authoritySha256"`
	RollbackEligibility               string                       `json:"rollbackEligibility"`
	GatewayConfiguration              GatewayConfigurationIdentity `json:"gatewayConfiguration"`
	Prerequisites                     []PrerequisiteEvidence       `json:"prerequisites"`
	ActivationEvidence                []ObligationEvidence         `json:"activationEvidence"`
	RollbackEvidence                  []ObligationEvidence         `json:"rollbackEvidence"`
	RollForwardCriteria               []string                     `json:"rollForwardCriteria"`
}

type ActivationRequest struct {
	ActivationID string
	ActivatedAt  string
}

type RollbackRequest struct {
	ActivationID string
	RolledBackAt string
}

type BoundaryRequest struct {
	ActivationID string
	RunID        string
	RunRowID     int64
	CrossedAt    string
}

type RollForwardEvidenceRequest struct {
	ActivationID      string
	CriterionSequence int64
	Evidence          string
}

type LegacyGateDecision struct {
	Allowed        bool
	Reason         string
	IsRead         bool
	IsRemediation  bool
	IsContinuation bool
}
