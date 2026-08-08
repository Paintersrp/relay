// Package features exposes bounded operator HTTP surfaces for feature workspaces.
package features

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	featureapp "relay/internal/app/features"
	appoperations "relay/internal/app/operations"
	wayfinder "relay/internal/app/wayfinder"
	"relay/internal/prototypeexecution"

	"github.com/go-chi/chi/v5"
)

type WayfinderService interface {
	CreateWorkspace(context.Context, wayfinder.CreateWorkspaceInput) (Workspace, error)
	ReadWorkspace(context.Context, string) (wayfinder.WorkspaceDetail, error)
	AdmitInput(context.Context, wayfinder.AdmitInputInput) (AdmittedInput, Workspace, error)
	AddDestination(context.Context, wayfinder.AddDestinationInput) (Destination, Workspace, error)
	CreateDiscoveryTicket(context.Context, wayfinder.CreateDiscoveryTicketInput) (DiscoveryTicket, Workspace, error)
	ResolveDiscoveryTicket(context.Context, wayfinder.ResolveDiscoveryTicketInput) (Resolution, DiscoveryTicket, Workspace, error)
	RouteWorkspace(context.Context, wayfinder.RouteWorkspaceInput) (RouteState, Workspace, error)
}

type AuthorityService interface {
	ReadAuthority(context.Context, string) ([]featureapp.AuthorityRevisionDetail, error)
	PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, Workspace, error)
	RecordAuthorityApproval(context.Context, featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error)
	LaunchApprovedPrototype(context.Context, prototypeexecution.LaunchRequest) (prototypeexecution.Result, error)
	ReconcilePrototypeLaunch(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error)
	CancelPrototypeExecution(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error)
	SettlePrototypeTimeout(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error)
	ReconcilePrototypeCleanup(context.Context, prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error)
	PrepareAnotherPrototypeExecution(context.Context, featureapp.PrepareAnotherPrototypeExecutionInput) (featureapp.PrototypeExecutionDetail, error)
	PrepareQADiscoveryPacket(context.Context, featureapp.PrepareQADiscoveryPacketInput) (featureapp.PrototypeQAPacketDetail, error)
	AdmitOperatorQAEvidence(context.Context, featureapp.AdmitOperatorQAEvidenceInput) (featureapp.PrototypeQAPacketDetail, error)
	ReadPrototypeEvidenceForWayfinder(context.Context, string, string) (featureapp.PrototypeWayfinderEvidenceView, error)
}

type CompletionService interface {
	Evaluate(context.Context, string) (appoperations.FeatureCompletionStatus, error)
	Complete(context.Context, featureapp.CompletionInput) (appoperations.FeatureCompletionResult, error)
}

// GuidedService is the narrow Feature-owned mutation/read boundary used by
// the operator journey. The handler deliberately resolves internal revision
// identifiers from the current assessment instead of accepting them from the
// guided client.
type GuidedService interface {
	AssessDiscoveryDestination(context.Context, string) (GuidedAssessment, error)
	Currentness(context.Context, string) (featureapp.FeatureCurrentnessDecision, error)
	RecordDiscoveryDestinationAssessment(context.Context, featureapp.RecordDiscoveryDestinationAssessmentInput) error
	CloseFeatureDiscovery(context.Context, featureapp.CloseFeatureDiscoveryInput) error
}

// GuidedProjectionService and GuidedActionService are optional richer
// application-owned boundaries. The legacy GuidedService remains supported for
// raw compatibility and focused transport fakes.
type GuidedProjectionService interface {
	ReadGuidedProjection(context.Context, string) (featureapp.GuidedFeatureProjection, error)
}
type GuidedActionService interface {
	ExecuteGuidedAction(context.Context, featureapp.GuidedActionInput) (featureapp.GuidedActionResult, error)
}

type GuidedAssessment struct {
	State               featureapp.DiscoveryState
	Destination         featureapp.DiscoveryDestination
	Rationale           string
	Blockers            []string
	RestorationActions  []string
	PendingIntegrations []string
	ActiveOperations    []string
	RouteMaterialOpen   []string
	RequiredEvidence    []string
	Continuation        string
	Currentness         featureapp.DiscoveryCurrentness
	CurrentRevisionID   string
}

type Workspace struct {
	WorkspaceID string
	FeatureSlug string
	State       string
	Version     int64
	CreatedAt   string
	UpdatedAt   string
}

type WorkspaceProject struct {
	ProjectID string
	Name      string
}

type AdmittedInput struct {
	AdmittedInputID       string
	Sequence              int64
	InputName             string
	InputRole             string
	SourceKind            string
	ArtifactRowID         sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	SourceClosureRowID    sql.NullInt64
	ArtifactSha256        sql.NullString
	SourceReference       string
	CreatedAt             string
}

type Destination struct {
	DestinationID      string
	Sequence           int64
	DestinationKind    string
	DestinationKey     string
	RepoTarget         sql.NullString
	SourceClosureRowID sql.NullInt64
	CreatedAt          string
}

type DiscoveryTicket struct {
	DiscoveryTicketID string
	TicketKey         string
	Subject           string
	State             string
	Version           int64
	CreatedAt         string
	UpdatedAt         string
}

type Resolution struct {
	ResolutionID          string
	Sequence              int64
	ResolutionKind        string
	ArtifactRowID         sql.NullInt64
	RetainedArtifactRowID sql.NullInt64
	ArtifactSha256        string
	SourceClosureRowID    sql.NullInt64
	CreatedAt             string
}

type RouteState struct {
	RouteStateID     string
	Sequence         int64
	WorkspaceVersion int64
	State            string
	CreatedAt        string
}

type TicketDependency struct {
	DependsOnTicketRowID int64
	DependencyKind       string
}

type TicketDetail struct {
	Ticket       DiscoveryTicket
	Dependencies []TicketDependency
	Resolutions  []Resolution
}

type WorkspaceHandler struct {
	wayfinder  WayfinderService
	authority  AuthorityService
	completion CompletionService
	guided     GuidedService
}

func NewWorkspaceHandler(wayfinderService WayfinderService, authorityService AuthorityService, completionService CompletionService) *WorkspaceHandler {
	return NewWorkspaceHandlerWithGuided(wayfinderService, authorityService, completionService, nil)
}

func NewWorkspaceHandlerWithGuided(wayfinderService WayfinderService, authorityService AuthorityService, completionService CompletionService, guidedService GuidedService) *WorkspaceHandler {
	return &WorkspaceHandler{wayfinder: wayfinderService, authority: authorityService, completion: completionService, guided: guidedService}
}

// NewWorkspaceHandlerFromServices binds the application owners to the HTTP
// projection boundary without exposing persistence models from this package.
func NewWorkspaceHandlerFromServices(wayfinderService *wayfinder.Service, authorityService *featureapp.Service, completionService *appoperations.FeatureCompletionWorkflowService) *WorkspaceHandler {
	adapter := appAuthorityAdapter{service: authorityService}
	return NewWorkspaceHandlerWithGuided(appWayfinderAdapter{service: wayfinderService}, adapter, completionService, adapter)
}

type appWayfinderAdapter struct{ service *wayfinder.Service }

func (a appWayfinderAdapter) CreateWorkspace(ctx context.Context, input wayfinder.CreateWorkspaceInput) (Workspace, error) {
	value, err := a.service.CreateWorkspace(ctx, input)
	return Workspace{WorkspaceID: value.WorkspaceID, FeatureSlug: value.FeatureSlug, State: value.State, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, err
}

func (a appWayfinderAdapter) ReadWorkspace(ctx context.Context, workspaceID string) (wayfinder.WorkspaceDetail, error) {
	return a.service.ReadWorkspace(ctx, workspaceID)
}

func (a appWayfinderAdapter) AdmitInput(ctx context.Context, input wayfinder.AdmitInputInput) (AdmittedInput, Workspace, error) {
	value, workspace, err := a.service.AdmitInput(ctx, input)
	return AdmittedInput{AdmittedInputID: value.AdmittedInputID, Sequence: value.Sequence, InputName: value.InputName, InputRole: value.InputRole, SourceKind: value.SourceKind, ArtifactRowID: value.ArtifactRowID, RetainedArtifactRowID: value.RetainedArtifactRowID, SourceClosureRowID: value.SourceClosureRowID, ArtifactSha256: value.ArtifactSha256, SourceReference: value.SourceReference, CreatedAt: value.CreatedAt}, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}

func (a appWayfinderAdapter) AddDestination(ctx context.Context, input wayfinder.AddDestinationInput) (Destination, Workspace, error) {
	value, workspace, err := a.service.AddDestination(ctx, input)
	return Destination{DestinationID: value.DestinationID, Sequence: value.Sequence, DestinationKind: value.DestinationKind, DestinationKey: value.DestinationKey, RepoTarget: value.RepoTarget, SourceClosureRowID: value.SourceClosureRowID, CreatedAt: value.CreatedAt}, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}

func (a appWayfinderAdapter) CreateDiscoveryTicket(ctx context.Context, input wayfinder.CreateDiscoveryTicketInput) (DiscoveryTicket, Workspace, error) {
	value, workspace, err := a.service.CreateDiscoveryTicket(ctx, input)
	return DiscoveryTicket{DiscoveryTicketID: value.DiscoveryTicketID, TicketKey: value.TicketKey, Subject: value.Subject, State: value.State, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}

func (a appWayfinderAdapter) ResolveDiscoveryTicket(ctx context.Context, input wayfinder.ResolveDiscoveryTicketInput) (Resolution, DiscoveryTicket, Workspace, error) {
	value, ticket, workspace, err := a.service.ResolveDiscoveryTicket(ctx, input)
	return Resolution{ResolutionID: value.ResolutionID, Sequence: value.Sequence, ResolutionKind: value.ResolutionKind, ArtifactRowID: value.ArtifactRowID, RetainedArtifactRowID: value.RetainedArtifactRowID, ArtifactSha256: value.ArtifactSha256, SourceClosureRowID: value.SourceClosureRowID, CreatedAt: value.CreatedAt}, DiscoveryTicket{DiscoveryTicketID: ticket.DiscoveryTicketID, TicketKey: ticket.TicketKey, Subject: ticket.Subject, State: ticket.State, Version: ticket.Version, CreatedAt: ticket.CreatedAt, UpdatedAt: ticket.UpdatedAt}, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}

func (a appWayfinderAdapter) RouteWorkspace(ctx context.Context, input wayfinder.RouteWorkspaceInput) (RouteState, Workspace, error) {
	value, workspace, err := a.service.RouteWorkspace(ctx, input)
	return RouteState{RouteStateID: value.RouteStateID, Sequence: value.Sequence, WorkspaceVersion: value.WorkspaceVersion, State: value.State, CreatedAt: value.CreatedAt}, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}

type appAuthorityAdapter struct{ service *featureapp.Service }

func (a appAuthorityAdapter) ReadAuthority(ctx context.Context, workspaceID string) ([]featureapp.AuthorityRevisionDetail, error) {
	return a.service.ReadAuthority(ctx, workspaceID)
}

func (a appAuthorityAdapter) PublishAuthority(ctx context.Context, input featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, Workspace, error) {
	value, workspace, err := a.service.PublishAuthority(ctx, input)
	return value, Workspace{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt}, err
}
func (a appAuthorityAdapter) RecordAuthorityApproval(ctx context.Context, input featureapp.RecordAuthorityApprovalInput) (featureapp.RecordAuthorityApprovalResult, error) {
	value, err := a.service.RecordAuthorityApproval(ctx, input)
	return value, err
}

func (a appAuthorityAdapter) AssessDiscoveryDestination(ctx context.Context, workspaceID string) (GuidedAssessment, error) {
	value, err := a.service.AssessDiscoveryDestination(ctx, workspaceID)
	if err != nil {
		return GuidedAssessment{}, err
	}
	return guidedAssessmentDTO(value), nil
}
func (a appAuthorityAdapter) Currentness(ctx context.Context, workspaceID string) (featureapp.FeatureCurrentnessDecision, error) {
	return a.service.Currentness(ctx, workspaceID)
}
func (a appAuthorityAdapter) RecordDiscoveryDestinationAssessment(ctx context.Context, input featureapp.RecordDiscoveryDestinationAssessmentInput) error {
	_, _, err := a.service.RecordDiscoveryDestinationAssessment(ctx, input)
	return err
}
func (a appAuthorityAdapter) CloseFeatureDiscovery(ctx context.Context, input featureapp.CloseFeatureDiscoveryInput) error {
	_, _, err := a.service.CloseFeatureDiscovery(ctx, input)
	return err
}
func (a appAuthorityAdapter) ReadGuidedProjection(ctx context.Context, workspaceID string) (featureapp.GuidedFeatureProjection, error) {
	return a.service.ReadGuidedProjection(ctx, workspaceID)
}
func (a appAuthorityAdapter) ExecuteGuidedAction(ctx context.Context, input featureapp.GuidedActionInput) (featureapp.GuidedActionResult, error) {
	return a.service.ExecuteGuidedAction(ctx, input)
}

type createWorkspaceRequest struct {
	ProjectID   string `json:"projectId"`
	FeatureSlug string `json:"featureSlug"`
}
type expectedVersionRequest struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}
type admitInputRequest struct {
	ExpectedVersion       int64  `json:"expectedVersion"`
	Sequence              int64  `json:"sequence"`
	Name                  string `json:"name"`
	Role                  string `json:"role"`
	SourceKind            string `json:"sourceKind"`
	ArtifactRowID         *int64 `json:"artifactRowId"`
	RetainedArtifactRowID *int64 `json:"retainedArtifactRowId"`
	SourceClosureRowID    *int64 `json:"sourceClosureRowId"`
	ArtifactSHA256        string `json:"artifactSha256"`
	SourceReference       string `json:"sourceReference"`
}
type addDestinationRequest struct {
	ExpectedVersion    int64  `json:"expectedVersion"`
	Sequence           int64  `json:"sequence"`
	Kind               string `json:"kind"`
	Key                string `json:"key"`
	RepoTarget         string `json:"repoTarget"`
	SourceClosureRowID *int64 `json:"sourceClosureRowId"`
}
type createTicketRequest struct {
	ExpectedVersion    int64    `json:"expectedVersion"`
	TicketKey          string   `json:"ticketKey"`
	Subject            string   `json:"subject"`
	DependsOnTicketIDs []string `json:"dependsOnTicketIds"`
	DependencyKind     string   `json:"dependencyKind"`
}
type resolveTicketRequest struct {
	ExpectedVersion       int64  `json:"expectedVersion"`
	ExpectedTicketVersion int64  `json:"expectedTicketVersion"`
	Sequence              int64  `json:"sequence"`
	Kind                  string `json:"kind"`
	ArtifactRowID         *int64 `json:"artifactRowId"`
	RetainedArtifactRowID *int64 `json:"retainedArtifactRowId"`
	ArtifactSHA256        string `json:"artifactSha256"`
	SourceClosureRowID    *int64 `json:"sourceClosureRowId"`
}
type routeWorkspaceRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Sequence        int64  `json:"sequence"`
	State           string `json:"state"`
	TicketID        string `json:"ticketId"`
}
type authorityLayerRequest struct {
	Kind                  string `json:"kind"`
	ArtifactRowID         *int64 `json:"artifactRowId"`
	RetainedArtifactRowID *int64 `json:"retainedArtifactRowId"`
	ArtifactSHA256        string `json:"artifactSha256"`
	SourceClosureRowID    *int64 `json:"sourceClosureRowId"`
	ApprovalRowID         *int64 `json:"approvalRowId"`
}
type publishAuthorityRequest struct {
	ExpectedVersion    int64                   `json:"expectedVersion"`
	SourceClosureRowID *int64                  `json:"sourceClosureRowId"`
	Layers             []authorityLayerRequest `json:"layers"`
}
type recordAuthorityApprovalRequest struct {
	Family                       string `json:"family"`
	ArtifactRowID                *int64 `json:"artifactRowId"`
	RetainedArtifactRowID        *int64 `json:"retainedArtifactRowId"`
	ArtifactSHA256               string `json:"artifactSha256"`
	OperatorConfirmationEvidence string `json:"operatorConfirmationEvidence"`
}
type completeWorkspaceRequest struct {
	ExpectedVersion   int64 `json:"expectedVersion"`
	OperatorConfirmed bool  `json:"operatorConfirmed"`
}

var errGuidedActionBlocked = errors.New("guided action is not the presently enabled primary action")

type guidedActionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Action          string `json:"action"`
	Confirmation    bool   `json:"confirmation"`
	Destination     string `json:"destination"`
}
type cleanupRequest struct {
	ExpectedRunVersion int64  `json:"expectedRunVersion"`
	MutationIdentity   string `json:"mutationIdentity"`
}
type anotherExecutionRequest struct {
	ExpectedPriorRunVersion      int64  `json:"expectedPriorRunVersion"`
	MutationIdentity             string `json:"mutationIdentity"`
	OperatorConfirmationEvidence string `json:"operatorConfirmationEvidence"`
}
type qaPacketRequest struct {
	ExpectedRunVersion     int64    `json:"expectedRunVersion"`
	MutationIdentity       string   `json:"mutationIdentity"`
	OperatorPrompt         string   `json:"operatorPrompt"`
	ValidationInstructions []string `json:"validationInstructions"`
}
type qaEvidenceRequest struct {
	MutationIdentity             string           `json:"mutationIdentity"`
	OperatorConfirmationEvidence string           `json:"operatorConfirmationEvidence"`
	Evidence                     []qaEvidenceItem `json:"evidence"`
}
type qaEvidenceItem struct {
	SemanticRole string `json:"semanticRole"`
	MediaType    string `json:"mediaType"`
	Content      []byte `json:"content"`
	SHA256       string `json:"sha256"`
}

func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createWorkspaceRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid feature workspace request")
		return
	}
	workspace, err := h.wayfinder.CreateWorkspace(r.Context(), wayfinder.CreateWorkspaceInput{ProjectID: request.ProjectID, FeatureSlug: request.FeatureSlug})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) Get(w http.ResponseWriter, r *http.Request) {
	detail, err := h.wayfinder.ReadWorkspace(r.Context(), workspaceID(r))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	authority, err := h.authority.ReadAuthority(r.Context(), detail.Workspace.WorkspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, workspaceDetailDTO(detail, authority))
}

func (h *WorkspaceHandler) AdmitInput(w http.ResponseWriter, r *http.Request) {
	var request admitInputRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid admitted input request")
		return
	}
	input, workspace, err := h.wayfinder.AdmitInput(r.Context(), wayfinder.AdmitInputInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, Sequence: request.Sequence, Name: request.Name, Role: request.Role, SourceKind: request.SourceKind, ArtifactRowID: nullableInt(request.ArtifactRowID), RetainedArtifact: nullableInt(request.RetainedArtifactRowID), SourceClosureID: nullableInt(request.SourceClosureRowID), ArtifactSHA256: nullableString(request.ArtifactSHA256), SourceReference: request.SourceReference})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"input": admittedInputDTO(input), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) AddDestination(w http.ResponseWriter, r *http.Request) {
	var request addDestinationRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid destination request")
		return
	}
	destination, workspace, err := h.wayfinder.AddDestination(r.Context(), wayfinder.AddDestinationInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, Sequence: request.Sequence, Kind: request.Kind, Key: request.Key, RepoTarget: nullableString(request.RepoTarget), SourceClosureID: nullableInt(request.SourceClosureRowID)})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"destination": destinationDTO(destination), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	var request createTicketRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid discovery request")
		return
	}
	ticket, workspace, err := h.wayfinder.CreateDiscoveryTicket(r.Context(), wayfinder.CreateDiscoveryTicketInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, TicketKey: request.TicketKey, Subject: request.Subject, DependsOnTicketIDs: request.DependsOnTicketIDs, DependencyKind: request.DependencyKind})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"ticket": ticketDTO(TicketDetail{Ticket: ticket}), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) ResolveTicket(w http.ResponseWriter, r *http.Request) {
	var request resolveTicketRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid discovery resolution request")
		return
	}
	resolution, ticket, workspace, err := h.wayfinder.ResolveDiscoveryTicket(r.Context(), wayfinder.ResolveDiscoveryTicketInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, TicketID: strings.TrimSpace(chi.URLParam(r, "ticketID")), ExpectedTicketVer: request.ExpectedTicketVersion, ResolutionSequence: request.Sequence, ResolutionKind: request.Kind, ArtifactRowID: nullableInt(request.ArtifactRowID), RetainedArtifact: nullableInt(request.RetainedArtifactRowID), ArtifactSHA256: request.ArtifactSHA256, SourceClosureID: nullableInt(request.SourceClosureRowID)})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"resolution": resolutionDTO(resolution), "ticket": ticketDTO(TicketDetail{Ticket: ticket}), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) Route(w http.ResponseWriter, r *http.Request) {
	var request routeWorkspaceRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid route request")
		return
	}
	route, workspace, err := h.wayfinder.RouteWorkspace(r.Context(), wayfinder.RouteWorkspaceInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, Sequence: request.Sequence, State: request.State, TicketID: request.TicketID})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"route": routeDTO(route), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) PublishAuthority(w http.ResponseWriter, r *http.Request) {
	var request publishAuthorityRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid authority revision request")
		return
	}
	layers := make([]featureapp.AuthorityLayerInput, 0, len(request.Layers))
	for _, layer := range request.Layers {
		layers = append(layers, featureapp.AuthorityLayerInput{Kind: layer.Kind, ArtifactRowID: nullableInt(layer.ArtifactRowID), RetainedArtifact: nullableInt(layer.RetainedArtifactRowID), ArtifactSHA256: layer.ArtifactSHA256, SourceClosureID: nullableInt(layer.SourceClosureRowID), ApprovalRowID: nullableInt(layer.ApprovalRowID)})
	}
	revision, workspace, err := h.authority.PublishAuthority(r.Context(), featureapp.PublishAuthorityInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, SourceClosureID: nullableInt(request.SourceClosureRowID), Layers: layers})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"authorityRevision": authorityDTO(revision), "workspace": workspaceDTO(workspace)})
}

func (h *WorkspaceHandler) RecordApproval(w http.ResponseWriter, r *http.Request) {
	var request recordAuthorityApprovalRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid authority approval request")
		return
	}
	result, err := h.authority.RecordAuthorityApproval(r.Context(), featureapp.RecordAuthorityApprovalInput{
		WorkspaceID:                  workspaceID(r),
		Family:                       request.Family,
		ArtifactRowID:                nullableInt(request.ArtifactRowID),
		RetainedArtifact:             nullableInt(request.RetainedArtifactRowID),
		ArtifactSHA256:               request.ArtifactSHA256,
		OperatorConfirmationEvidence: request.OperatorConfirmationEvidence,
	})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"approval": approvalDTO(result.Approval), "workspace": workspaceDTO(Workspace{WorkspaceID: result.Workspace.WorkspaceID, FeatureSlug: result.Workspace.FeatureSlug, State: result.Workspace.State, Version: result.Workspace.Version, CreatedAt: result.Workspace.CreatedAt, UpdatedAt: result.Workspace.UpdatedAt})})
}

func (h *WorkspaceHandler) GuidedGet(w http.ResponseWriter, r *http.Request) {
	if h.guided == nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Guided feature service is unavailable")
		return
	}
	if reader, ok := h.guided.(GuidedProjectionService); ok {
		projection, err := reader.ReadGuidedProjection(r.Context(), workspaceID(r))
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		shared.JSON(w, http.StatusOK, map[string]any{"guided": guidedFeatureProjectionDTO(projection)})
		return
	}
	projection, err := h.guidedProjection(r.Context(), workspaceID(r))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"guided": projection})
}

func (h *WorkspaceHandler) GuidedAction(w http.ResponseWriter, r *http.Request) {
	if h.guided == nil || h.completion == nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Guided feature service is unavailable")
		return
	}
	var request guidedActionRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid guided feature action request")
		return
	}
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.Action) == "" {
		badRequest(w, "Guided feature action and expected version are required")
		return
	}
	workspaceID := workspaceID(r)
	action := strings.TrimSpace(request.Action)
	switch action {
	case "continue_discovery", "close_discovery", "author_requirements", "author_shared_design", "author_delivery_ticket", "review_planning_candidate", "approve_planning_candidate", "promote_planning_candidate", "continue_established_route", "complete_feature", "legacy_recovery":
	default:
		badRequest(w, "Unsupported guided feature action")
		return
	}
	if executor, ok := h.guided.(GuidedActionService); ok {
		result, err := executor.ExecuteGuidedAction(r.Context(), featureapp.GuidedActionInput{WorkspaceID: workspaceID, Action: action, ExpectedVersion: request.ExpectedVersion, Confirmation: request.Confirmation, Destination: featureapp.DiscoveryDestination(strings.TrimSpace(request.Destination))})
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		shared.JSON(w, http.StatusOK, map[string]any{"guided": guidedFeatureProjectionDTO(result.Projection)})
		return
	}
	// The compatibility interface predates discovery-lifecycle adoption and
	// cannot perform that mutation.  Reject rather than acknowledge a no-op;
	// production uses the richer GuidedActionService above.
	if action == "legacy_recovery" {
		writeWorkspaceError(w, errGuidedActionBlocked)
		return
	}

	// Recompute the current primary action and its enabled state immediately
	// before dispatching any mutation. The client action is only a request; the
	// current assessment and completion gates remain the authority.
	assessment, err := h.guided.AssessDiscoveryDestination(r.Context(), workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	currentness, err := h.guided.Currentness(r.Context(), workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	completion, err := h.completion.Evaluate(r.Context(), workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	authority, err := h.authority.ReadAuthority(r.Context(), workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	layers := []string{}
	for _, revision := range authority {
		if !revision.Historical {
			for _, layer := range revision.Layers {
				layers = append(layers, layer.LayerKind)
			}
		}
	}
	decision := guidedDecision(assessment, currentness, completion.CurrentDecision, completion.Gates, layers)
	primary := decision.AvailableActions[0]
	if primary.RequiresConfirmation && !request.Confirmation {
		writeWorkspaceError(w, featureapp.ErrFeatureCompletionConfirmation)
		return
	}
	if action != string(primary.Action) || !primary.Enabled {
		writeWorkspaceError(w, errGuidedActionBlocked)
		return
	}

	var guidedHandoff map[string]any
	switch action {
	case "continue_discovery":
		err = h.guided.RecordDiscoveryDestinationAssessment(r.Context(), featureapp.RecordDiscoveryDestinationAssessmentInput{
			WorkspaceID: workspaceID, ExpectedVersion: request.ExpectedVersion, CreatedIdentity: "guided-operator",
		})
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
	case "close_discovery":
		if assessment.CurrentRevisionID == "" {
			writeWorkspaceError(w, featureapp.ErrDiscoveryNotStarted)
			return
		}
		destination := assessment.Destination
		if strings.TrimSpace(request.Destination) != "" {
			destination = featureapp.DiscoveryDestination(strings.TrimSpace(request.Destination))
		}
		if destination == "" {
			writeWorkspaceError(w, featureapp.ErrDiscoveryInvalidDestination)
			return
		}
		err = h.guided.CloseFeatureDiscovery(r.Context(), featureapp.CloseFeatureDiscoveryInput{
			WorkspaceID: workspaceID, ExpectedVersion: request.ExpectedVersion,
			ExpectedRevisionID: assessment.CurrentRevisionID, Destination: destination,
			CreatedIdentity: "guided-operator",
		})
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
	case "author_requirements", "author_shared_design", "author_delivery_ticket", "continue_established_route":
		guidedHandoff = map[string]any{
			"role":        action,
			"summary":     "Continue through the existing bounded owner, then return to the guided workspace for a fresh currentness check.",
			"resumeRoute": "/feature-workspaces/{workspaceID}/guided",
			"context":     map[string]string{"destination": string(assessment.Destination), "currentness": string(currentness.Readiness), "continuation": assessment.Continuation},
		}
	case "complete_feature":
		if _, err := h.completion.Complete(r.Context(), featureapp.CompletionInput{WorkspaceID: workspaceID, ExpectedVersion: request.ExpectedVersion, OperatorConfirmed: request.Confirmation}); err != nil {
			writeWorkspaceError(w, err)
			return
		}
	}
	projection, err := h.guidedProjection(r.Context(), workspaceID)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if guidedHandoff != nil {
		projection["handoff"] = guidedHandoff
	}
	shared.JSON(w, http.StatusOK, map[string]any{"guided": projection})
}

func (h *WorkspaceHandler) guidedProjection(ctx context.Context, workspaceID string) (map[string]any, error) {
	detail, err := h.wayfinder.ReadWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	assessment, err := h.guided.AssessDiscoveryDestination(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	currentness, err := h.guided.Currentness(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	authority, err := h.authority.ReadAuthority(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	completion, err := h.completion.Evaluate(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return guidedProjectionDTO(detail, assessment, currentness, authority, completion), nil
}

func guidedAssessmentDTO(value featureapp.DiscoveryAssessment) GuidedAssessment {
	assessment := GuidedAssessment{
		State:               value.State,
		Destination:         value.Destination,
		Rationale:           value.Rationale,
		Blockers:            append([]string(nil), value.Blockers...),
		RestorationActions:  append([]string(nil), value.RestorationActions...),
		PendingIntegrations: append([]string(nil), value.PendingIntegrations...),
		ActiveOperations:    append([]string(nil), value.ActiveOperations...),
		RouteMaterialOpen:   append([]string(nil), value.RouteMaterialOpen...),
		RequiredEvidence:    append([]string(nil), value.RequiredEvidence...),
		Continuation:        value.Continuation,
		Currentness:         value.Currentness,
	}
	if value.Revision != nil {
		assessment.CurrentRevisionID = value.Revision.DiscoveryRevisionID
	}
	return assessment
}

func guidedProjectionDTO(detail wayfinder.WorkspaceDetail, assessment GuidedAssessment, currentness featureapp.FeatureCurrentnessDecision, authority []featureapp.AuthorityRevisionDetail, completion appoperations.FeatureCompletionStatus) map[string]any {
	gates := make([]map[string]any, 0, len(completion.Gates))
	completionReady := true
	for _, gate := range completion.Gates {
		gates = append(gates, map[string]any{"name": gate.Name, "ready": gate.Ready})
		completionReady = completionReady && gate.Ready
	}
	authorityRevisions := make([]map[string]any, 0, len(authority))
	currentAuthorityRevision := int64(0)
	for _, revision := range authority {
		layers := make([]string, 0, len(revision.Layers))
		for _, layer := range revision.Layers {
			layers = append(layers, layer.LayerKind)
		}
		if detail.Workspace.CurrentAuthorityRevisionRowID.Valid && revision.Revision.ID == detail.Workspace.CurrentAuthorityRevisionRowID.Int64 {
			currentAuthorityRevision = revision.Revision.RevisionNumber
		}
		authorityRevisions = append(authorityRevisions, map[string]any{"revisionNumber": revision.Revision.RevisionNumber, "layers": layers, "historical": revision.Historical})
	}
	currentLayers := []string{}
	for _, revision := range authority {
		if !revision.Historical {
			for _, layer := range revision.Layers {
				currentLayers = append(currentLayers, layer.LayerKind)
			}
		}
	}
	decision := guidedDecision(assessment, currentness, completion.CurrentDecision, completion.Gates, currentLayers)
	primaryAction := string(decision.PrimaryAction)
	availableActions := make([]map[string]any, 0, len(decision.AvailableActions))
	for _, action := range decision.AvailableActions {
		availableActions = append(availableActions, map[string]any{"action": string(action.Action), "primary": action.Primary, "enabled": action.Enabled, "requiresConfirmation": action.RequiresConfirmation, "blockedReason": action.BlockedReason, "handoff": action.Handoff})
	}
	workspace := Workspace{WorkspaceID: detail.Workspace.WorkspaceID, FeatureSlug: detail.Workspace.FeatureSlug, State: detail.Workspace.State, Version: detail.Workspace.Version, CreatedAt: detail.Workspace.CreatedAt, UpdatedAt: detail.Workspace.UpdatedAt}
	if completion.Workspace.Version > workspace.Version {
		workspace = Workspace{WorkspaceID: completion.Workspace.WorkspaceID, FeatureSlug: completion.Workspace.FeatureSlug, State: completion.Workspace.State, Version: completion.Workspace.Version, CreatedAt: completion.Workspace.CreatedAt, UpdatedAt: completion.Workspace.UpdatedAt}
	}
	return map[string]any{
		"workspace":  workspaceDTO(workspace),
		"project":    map[string]any{"projectId": detail.Project.ProjectID, "name": detail.Project.Name},
		"discovery":  map[string]any{"state": string(assessment.State), "destination": string(assessment.Destination), "rationale": assessment.Rationale, "continuation": assessment.Continuation, "currentness": string(assessment.Currentness)},
		"authority":  map[string]any{"currentRevisionNumber": currentAuthorityRevision, "revisions": authorityRevisions},
		"planning":   map[string]any{"readiness": string(currentness.Readiness), "status": guidedPlanningStatus(currentness), "recoveryCategory": currentness.RecoveryCategory},
		"completion": map[string]any{"gates": gates, "ready": completionReady, "recorded": completion.CurrentDecision != nil},
		"diagnostics": map[string]any{
			"history":   map[string]any{"discoveryCurrentness": string(assessment.Currentness), "status": guidedHistoryStatus(assessment, currentness)},
			"stale":     map[string]any{"readiness": string(currentness.Readiness), "owner": currentness.StaleOwner, "blockedOperation": currentness.BlockedOperation, "effect": currentness.Effect, "recoveryCategory": currentness.RecoveryCategory},
			"discovery": map[string]any{"blockers": assessment.Blockers, "restorationActions": assessment.RestorationActions, "pendingIntegrations": assessment.PendingIntegrations, "activeOperations": assessment.ActiveOperations, "routeMaterialOpen": len(assessment.RouteMaterialOpen) > 0, "requiredEvidence": assessment.RequiredEvidence},
		},
		"availableActions": availableActions,
		"primaryAction":    primaryAction,
	}
}

func guidedFeatureProjectionDTO(value featureapp.GuidedFeatureProjection) map[string]any {
	availableActions := make([]map[string]any, 0, len(value.AvailableActions))
	for _, action := range value.AvailableActions {
		availableActions = append(availableActions, map[string]any{
			"action": string(action.Action), "primary": action.Primary, "enabled": action.Enabled,
			"requiresConfirmation": action.RequiresConfirmation, "blockedReason": action.BlockedReason, "handoff": action.Handoff,
		})
	}
	return map[string]any{
		"workspace":        workspaceDTO(Workspace{WorkspaceID: value.Workspace.WorkspaceID, FeatureSlug: value.Workspace.FeatureSlug, State: value.Workspace.State, Version: value.Workspace.Version, CreatedAt: value.Workspace.CreatedAt, UpdatedAt: value.Workspace.UpdatedAt}),
		"project":          map[string]any{"projectId": value.Project.ProjectID, "name": value.Project.Name},
		"discovery":        map[string]any{"state": value.Discovery.State, "destination": value.Discovery.Destination, "rationale": value.Discovery.Rationale, "continuation": value.Discovery.Continuation, "currentness": value.Discovery.Currentness, "hasCurrentRevision": value.Discovery.HasCurrentRevision},
		"authority":        map[string]any{"currentRevisionNumber": value.Authority.CurrentRevisionNumber, "layers": value.Authority.Layers},
		"currentness":      map[string]any{"readiness": value.Currentness.Readiness, "owner": value.Currentness.Owner, "blockedOperation": value.Currentness.BlockedOperation, "effect": value.Currentness.Effect, "recoveryCategory": value.Currentness.RecoveryCategory},
		"planning":         map[string]any{"status": value.Planning.Status, "candidateState": value.Planning.CandidateState, "reviewState": value.Planning.ReviewState, "approvalState": value.Planning.ApprovalState, "promotionState": value.Planning.PromotionState, "candidateCount": value.Planning.CandidateCount, "awaitingReview": value.Planning.AwaitingReview, "awaitingApproval": value.Planning.AwaitingApproval, "awaitingPromotion": value.Planning.AwaitingPromotion, "promoted": value.Planning.Promoted, "historicalCount": value.Planning.HistoricalCount},
		"delivery":         map[string]any{"frontierCount": value.Delivery.FrontierCount, "selectionState": value.Delivery.SelectionState, "packageState": value.Delivery.PackageState, "runState": value.Delivery.RunState, "auditState": value.Delivery.AuditState, "remediationState": value.Delivery.RemediationState},
		"prototype":        map[string]any{"runState": value.Prototype.RunState, "cleanupState": value.Prototype.CleanupState, "qaState": value.Prototype.QAState, "evidenceState": value.Prototype.EvidenceState},
		"completion":       map[string]any{"gates": guidedCompletionGatesDTO(value.Completion.Gates), "ready": value.Completion.Ready, "recorded": value.Completion.Recorded},
		"recovery":         map[string]any{"state": value.Recovery.State, "category": value.Recovery.Category, "available": value.Recovery.Available},
		"diagnostics":      map[string]any{"stale": value.Diagnostics.Stale, "historical": value.Diagnostics.Historical, "discovery": value.Diagnostics.Discovery},
		"availableActions": availableActions, "primaryAction": string(value.PrimaryAction.Action),
		"handoff": guidedHandoffDTO(value.Handoff),
	}
}
func guidedCompletionGatesDTO(values []featureapp.GuidedCompletionGate) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"name": value.Name, "ready": value.Ready})
	}
	return result
}
func guidedHandoffDTO(value *featureapp.GuidedHandoff) any {
	if value == nil {
		return nil
	}
	return map[string]any{"role": value.Role, "summary": value.Summary, "resumeRoute": value.ResumeRoute, "context": value.Context}
}

func guidedDecision(assessment GuidedAssessment, currentness featureapp.FeatureCurrentnessDecision, decision *appoperations.FeatureCompletionDecision, gates []appoperations.FeatureCompletionGate, layers []string) featureapp.GuidedFeatureDecision {
	completionGates := make([]featureapp.GuidedCompletionGate, 0, len(gates))
	for _, gate := range gates {
		completionGates = append(completionGates, featureapp.GuidedCompletionGate{Name: gate.Name, Ready: gate.Ready})
	}
	return featureapp.DecideGuidedFeatureAction(featureapp.GuidedJourneyState{
		State: assessment.State, Destination: assessment.Destination, Continuation: assessment.Continuation,
		HasCurrentRevision: assessment.CurrentRevisionID != "", AuthorityLayers: layers, Blockers: assessment.Blockers,
		PendingIntegrations: assessment.PendingIntegrations, ActiveOperations: assessment.ActiveOperations,
		RouteMaterialOpen: assessment.RouteMaterialOpen, RequiredEvidence: assessment.RequiredEvidence,
	}, currentness, featureapp.GuidedCompletion{Gates: completionGates, Recorded: decision != nil})
}

func guidedPrimaryAction(assessment GuidedAssessment, currentness featureapp.FeatureCurrentnessDecision, decision *appoperations.FeatureCompletionDecision, completionReady bool) (string, bool) {
	applicationDecision := guidedDecision(assessment, currentness, decision, []appoperations.FeatureCompletionGate{{Name: "completion", Ready: completionReady}}, nil)
	primary := applicationDecision.AvailableActions[0]
	return string(primary.Action), primary.Enabled
}

func guidedCompletionReady(gates []appoperations.FeatureCompletionGate) bool {
	ready := true
	for _, gate := range gates {
		ready = ready && gate.Ready
	}
	return ready
}

func guidedHistoryStatus(assessment GuidedAssessment, currentness featureapp.FeatureCurrentnessDecision) string {
	if currentness.Readiness == featureapp.FeatureStale || assessment.Currentness == featureapp.DiscoveryHistorical {
		return "historical_basis_requires_recovery"
	}
	return "current_basis"
}

func guidedPlanningStatus(value featureapp.FeatureCurrentnessDecision) string {
	if value.Readiness == featureapp.FeatureCurrent {
		return "ready"
	}
	return "blocked"
}
func (h *WorkspaceHandler) CompletionStatus(w http.ResponseWriter, r *http.Request) {
	if h.completion == nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Feature completion service is unavailable")
		return
	}
	status, err := h.completion.Evaluate(r.Context(), workspaceID(r))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, completionStatusDTO(status))
}

func (h *WorkspaceHandler) Complete(w http.ResponseWriter, r *http.Request) {
	if h.completion == nil {
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Feature completion service is unavailable")
		return
	}
	var request completeWorkspaceRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid feature completion request")
		return
	}
	complete := featureapp.CompletionInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, OperatorConfirmed: request.OperatorConfirmed}
	result, err := h.completion.Complete(r.Context(), complete)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{
		"workspace": workspaceDTO(Workspace{WorkspaceID: result.Workspace.WorkspaceID, FeatureSlug: result.Workspace.FeatureSlug, State: result.Workspace.State, Version: result.Workspace.Version, CreatedAt: result.Workspace.CreatedAt, UpdatedAt: result.Workspace.UpdatedAt}),
		"decision":  completionDecisionDTO(result.Decision),
	})
}

func workspaceID(r *http.Request) string { return strings.TrimSpace(chi.URLParam(r, "workspaceID")) }
func nullableInt(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}
func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
func nullableIntDTO(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
func nullableStringDTO(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func workspaceDTO(value Workspace) map[string]any {
	return map[string]any{"workspaceId": value.WorkspaceID, "featureSlug": value.FeatureSlug, "state": value.State, "version": value.Version, "createdAt": value.CreatedAt, "updatedAt": value.UpdatedAt}
}
func admittedInputDTO(value AdmittedInput) map[string]any {
	return map[string]any{"inputId": value.AdmittedInputID, "sequence": value.Sequence, "name": value.InputName, "role": value.InputRole, "sourceKind": value.SourceKind, "artifactRowId": nullableIntDTO(value.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(value.RetainedArtifactRowID), "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "artifactSha256": nullableStringDTO(value.ArtifactSha256), "sourceReference": value.SourceReference, "createdAt": value.CreatedAt}
}
func destinationDTO(value Destination) map[string]any {
	return map[string]any{"destinationId": value.DestinationID, "sequence": value.Sequence, "kind": value.DestinationKind, "key": value.DestinationKey, "repoTarget": nullableStringDTO(value.RepoTarget), "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "createdAt": value.CreatedAt}
}
func resolutionDTO(value Resolution) map[string]any {
	return map[string]any{"resolutionId": value.ResolutionID, "sequence": value.Sequence, "kind": value.ResolutionKind, "artifactRowId": nullableIntDTO(value.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(value.RetainedArtifactRowID), "artifactSha256": value.ArtifactSha256, "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "createdAt": value.CreatedAt}
}
func routeDTO(value RouteState) map[string]any {
	return map[string]any{"routeId": value.RouteStateID, "sequence": value.Sequence, "workspaceVersion": value.WorkspaceVersion, "state": value.State, "createdAt": value.CreatedAt}
}
func ticketDTO(value TicketDetail) map[string]any {
	dependencies := make([]map[string]any, 0, len(value.Dependencies))
	for _, item := range value.Dependencies {
		dependencies = append(dependencies, map[string]any{"dependsOnTicketRowId": item.DependsOnTicketRowID, "kind": item.DependencyKind})
	}
	resolutions := make([]map[string]any, 0, len(value.Resolutions))
	for _, item := range value.Resolutions {
		resolutions = append(resolutions, resolutionDTO(item))
	}
	return map[string]any{"ticketId": value.Ticket.DiscoveryTicketID, "ticketKey": value.Ticket.TicketKey, "subject": value.Ticket.Subject, "state": value.Ticket.State, "version": value.Ticket.Version, "dependencies": dependencies, "resolutions": resolutions, "createdAt": value.Ticket.CreatedAt, "updatedAt": value.Ticket.UpdatedAt}
}
func authorityDTO(value featureapp.AuthorityRevisionDetail) map[string]any {
	layers := make([]map[string]any, 0, len(value.Layers))
	for _, layer := range value.Layers {
		layers = append(layers, map[string]any{"kind": layer.LayerKind, "sequence": layer.Sequence, "artifactRowId": nullableIntDTO(layer.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(layer.RetainedArtifactRowID), "artifactSha256": layer.ArtifactSha256, "sourceClosureRowId": nullableIntDTO(layer.SourceClosureRowID), "approvalRowId": nullableIntDTO(layer.ApprovalRowID)})
	}
	return map[string]any{"authorityRevisionId": value.Revision.AuthorityRevisionID, "revisionNumber": value.Revision.RevisionNumber, "sourceClosureRowId": nullableIntDTO(value.Revision.SourceClosureRowID), "layers": layers, "createdAt": value.Revision.CreatedAt}
}

func approvalDTO(value featureapp.GoverningArtifactApproval) map[string]any {
	return map[string]any{
		"approvalId":                   value.ApprovalID,
		"workspaceRowId":               value.WorkspaceRowID,
		"artifactRowId":                nullableIntDTO(value.ArtifactRowID),
		"retainedArtifactRowId":        nullableIntDTO(value.RetainedArtifactRowID),
		"family":                       value.Family,
		"artifactSha256":               value.ArtifactSha256,
		"operatorConfirmationEvidence": value.OperatorConfirmationEvidence,
		"invalidatedByApprovalRowId":   nullableIntDTO(value.InvalidatedByApprovalRowID),
		"supersededByApprovalRowId":    nullableIntDTO(value.SupersededByApprovalRowID),
		"createdAt":                    value.CreatedAt,
	}
}
func completionStatusDTO(value appoperations.FeatureCompletionStatus) map[string]any {
	gates := make([]map[string]any, 0, len(value.Gates))
	for _, gate := range value.Gates {
		gates = append(gates, map[string]any{"name": gate.Name, "ready": gate.Ready})
	}
	response := map[string]any{
		"workspace": workspaceDTO(Workspace{WorkspaceID: value.Workspace.WorkspaceID, FeatureSlug: value.Workspace.FeatureSlug, State: value.Workspace.State, Version: value.Workspace.Version, CreatedAt: value.Workspace.CreatedAt, UpdatedAt: value.Workspace.UpdatedAt}),
		"gates":     gates,
	}
	if value.CurrentDecision != nil {
		response["currentDecision"] = completionDecisionDTO(*value.CurrentDecision)
	}
	return response
}
func completionDecisionDTO(value appoperations.FeatureCompletionDecision) map[string]any {
	return map[string]any{
		"completionDecisionId": value.CompletionDecisionID, "authorityRevisionRowId": value.AuthorityRevisionRowID,
		"sourceClosureRowId": value.SourceClosureRowID, "decision": value.Decision, "createdAt": value.CreatedAt,
	}
}
func workspaceDetailDTO(detail wayfinder.WorkspaceDetail, authority []featureapp.AuthorityRevisionDetail) map[string]any {
	inputs := make([]map[string]any, 0, len(detail.Inputs))
	for _, item := range detail.Inputs {
		inputs = append(inputs, admittedInputDTO(AdmittedInput{AdmittedInputID: item.AdmittedInputID, Sequence: item.Sequence, InputName: item.InputName, InputRole: item.InputRole, SourceKind: item.SourceKind, ArtifactRowID: item.ArtifactRowID, RetainedArtifactRowID: item.RetainedArtifactRowID, SourceClosureRowID: item.SourceClosureRowID, ArtifactSha256: item.ArtifactSha256, SourceReference: item.SourceReference, CreatedAt: item.CreatedAt}))
	}
	destinations := make([]map[string]any, 0, len(detail.Destinations))
	for _, item := range detail.Destinations {
		destinations = append(destinations, destinationDTO(Destination{DestinationID: item.DestinationID, Sequence: item.Sequence, DestinationKind: item.DestinationKind, DestinationKey: item.DestinationKey, RepoTarget: item.RepoTarget, SourceClosureRowID: item.SourceClosureRowID, CreatedAt: item.CreatedAt}))
	}
	tickets := make([]map[string]any, 0, len(detail.Tickets))
	for _, item := range detail.Tickets {
		tickets = append(tickets, ticketDTO(ticketDetailProjection(item)))
	}
	routes := make([]map[string]any, 0, len(detail.Routes))
	for _, item := range detail.Routes {
		routes = append(routes, routeDTO(RouteState{RouteStateID: item.RouteStateID, Sequence: item.Sequence, WorkspaceVersion: item.WorkspaceVersion, State: item.State, CreatedAt: item.CreatedAt}))
	}
	revisions := make([]map[string]any, 0, len(authority))
	for _, item := range authority {
		revisions = append(revisions, authorityDTO(item))
	}
	recorded := false
	for _, item := range detail.Investigations {
		recorded = recorded || item.SourceClosureRowID.Valid
	}
	return map[string]any{"workspace": workspaceDTO(Workspace{WorkspaceID: detail.Workspace.WorkspaceID, FeatureSlug: detail.Workspace.FeatureSlug, State: detail.Workspace.State, Version: detail.Workspace.Version, CreatedAt: detail.Workspace.CreatedAt, UpdatedAt: detail.Workspace.UpdatedAt}), "project": map[string]any{"projectId": detail.Project.ProjectID, "name": detail.Project.Name}, "inputs": inputs, "destinations": destinations, "tickets": tickets, "routes": routes, "authorityRevisions": revisions, "sourceBasis": map[string]any{"status": sourceBasisStatus(recorded), "investigationCount": len(detail.Investigations)}}
}

func ticketDetailProjection(value wayfinder.TicketDetail) TicketDetail {
	result := TicketDetail{Ticket: DiscoveryTicket{DiscoveryTicketID: value.Ticket.DiscoveryTicketID, TicketKey: value.Ticket.TicketKey, Subject: value.Ticket.Subject, State: value.Ticket.State, Version: value.Ticket.Version, CreatedAt: value.Ticket.CreatedAt, UpdatedAt: value.Ticket.UpdatedAt}}
	result.Dependencies = make([]TicketDependency, 0, len(value.Dependencies))
	for _, item := range value.Dependencies {
		result.Dependencies = append(result.Dependencies, TicketDependency{DependsOnTicketRowID: item.DependsOnTicketRowID, DependencyKind: item.DependencyKind})
	}
	result.Resolutions = make([]Resolution, 0, len(value.Resolutions))
	for _, item := range value.Resolutions {
		result.Resolutions = append(result.Resolutions, Resolution{ResolutionID: item.ResolutionID, Sequence: item.Sequence, ResolutionKind: item.ResolutionKind, ArtifactRowID: item.ArtifactRowID, RetainedArtifactRowID: item.RetainedArtifactRowID, ArtifactSha256: item.ArtifactSha256, SourceClosureRowID: item.SourceClosureRowID, CreatedAt: item.CreatedAt})
	}
	return result
}
func sourceBasisStatus(recorded bool) string {
	if recorded {
		return "retained"
	}
	return "not_recorded"
}

func decodeStrict(r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		return false
	}
	var extra any
	return errors.Is(decoder.Decode(&extra), io.EOF)
}
func badRequest(w http.ResponseWriter, message string) {
	shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", message)
}
func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, featureapp.ErrPrototypeCapabilityDisabled):
		shared.Error(w, http.StatusForbidden, "CAPABILITY_DISABLED", err.Error())
	case errors.Is(err, prototypeexecution.ErrCleanupOwnershipMismatch), errors.Is(err, featureapp.ErrPrototypeOwnership):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Feature workspace or prototype run was not found")
	case errors.Is(err, prototypeexecution.ErrQAPacketInvalid), errors.Is(err, prototypeexecution.ErrQAEvidenceInvalid):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, prototypeexecution.ErrReconciliationIncomplete), errors.Is(err, prototypeexecution.ErrAnotherExecutionIneligible), errors.Is(err, featureapp.ErrPrototypeAnotherExecutionIneligible), errors.Is(err, featureapp.ErrPrototypeQAPacketInvalid), errors.Is(err, featureapp.ErrPrototypeQAEvidenceInvalid):
		shared.Error(w, http.StatusConflict, "PROTOTYPE_CONFLICT", err.Error())
	case errors.Is(err, prototypeexecution.ErrLaunchUncertain), errors.Is(err, prototypeexecution.ErrCleanupRequired), errors.Is(err, prototypeexecution.ErrLaunchAlreadyClaimed), errors.Is(err, prototypeexecution.ErrPreparationClaimed):
		shared.Error(w, http.StatusConflict, "PROTOTYPE_CONFLICT", err.Error())
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, wayfinder.ErrWorkspaceNotFound), errors.Is(err, wayfinder.ErrDiscoveryTicketNotFound), errors.Is(err, featureapp.ErrWorkspaceNotFound), errors.Is(err, featureapp.ErrApprovalNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Feature workspace or discovery ticket was not found")
	case errors.Is(err, wayfinder.ErrVersionConflict), errors.Is(err, featureapp.ErrVersionConflict), errors.Is(err, featureapp.ErrDiscoveryStaleState), errors.Is(err, featureapp.ErrDiscoveryStalePacket):
		shared.Error(w, http.StatusConflict, "VERSION_CONFLICT", "Feature workspace was changed by another operator. Reload before retrying.")
	case errors.Is(err, featureapp.ErrDiscoveryLegacyUnbound), errors.Is(err, featureapp.ErrDiscoveryIntegrity), errors.Is(err, featureapp.ErrDiscoveryManifestIntegrity), errors.Is(err, featureapp.ErrLegacyCurrentness), errors.Is(err, featureapp.ErrStaleCandidateBasis), errors.Is(err, featureapp.ErrHistoricalBasis):
		shared.Error(w, http.StatusConflict, "CURRENTNESS_BLOCKED", err.Error())
	case errors.Is(err, errGuidedActionBlocked), errors.Is(err, featureapp.ErrGuidedActionBlocked), errors.Is(err, featureapp.ErrDiscoveryBlocked), errors.Is(err, featureapp.ErrDiscoveryClosureIneligible), errors.Is(err, featureapp.ErrDiscoveryPendingIntegration), errors.Is(err, featureapp.ErrDiscoveryActiveOperation), errors.Is(err, featureapp.ErrDiscoveryUnadopted), errors.Is(err, featureapp.ErrDiscoveryNotStarted), errors.Is(err, featureapp.ErrDiscoveryAlreadyClosed), errors.Is(err, featureapp.ErrDiscoveryNotClosed):
		shared.Error(w, http.StatusConflict, "GUIDED_ACTION_BLOCKED", err.Error())
	case errors.Is(err, featureapp.ErrFeatureCompletionNotReady), errors.Is(err, featureapp.ErrFeatureCompletionRecorded), errors.Is(err, appoperations.ErrFeatureCompletionAdmission):
		shared.Error(w, http.StatusConflict, "COMPLETION_CONFLICT", "Feature Workspace completion is not currently eligible. Reload the completion gates.")
	case errors.Is(err, featureapp.ErrFeatureCompletionConfirmation):
		badRequest(w, err.Error())
	case errors.Is(err, wayfinder.ErrInvalidWorkspaceRequest), errors.Is(err, featureapp.ErrInvalidAuthorityRequest), errors.Is(err, featureapp.ErrInvalidApprovalInput), errors.Is(err, featureapp.ErrApprovalMismatch), errors.Is(err, featureapp.ErrApprovalInvalidated), errors.Is(err, featureapp.ErrInvalidDiscoveryConsequence), errors.Is(err, featureapp.ErrDiscoveryInvalidDestination):
		badRequest(w, err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Feature workspace operation failed")
	}
}

func MountWorkspaceRoutes(r chi.Router, handler *WorkspaceHandler) {
	r.Post("/feature-workspaces", handler.Create)
	r.Get("/feature-workspaces/{workspaceID}", handler.Get)
	r.Post("/feature-workspaces/{workspaceID}/inputs", handler.AdmitInput)
	r.Post("/feature-workspaces/{workspaceID}/destinations", handler.AddDestination)
	r.Post("/feature-workspaces/{workspaceID}/discovery-tickets", handler.CreateTicket)
	r.Post("/feature-workspaces/{workspaceID}/discovery-tickets/{ticketID}/resolutions", handler.ResolveTicket)
	r.Post("/feature-workspaces/{workspaceID}/routes", handler.Route)
	r.Get("/feature-workspaces/{workspaceID}/guided", handler.GuidedGet)
	r.Post("/feature-workspaces/{workspaceID}/guided", handler.GuidedAction)
	r.Post("/feature-workspaces/{workspaceID}/guided/actions", handler.GuidedAction)
	r.Post("/feature-workspaces/{workspaceID}/authority-revisions", handler.PublishAuthority)
	r.Post("/feature-workspaces/{workspaceID}/authority-approvals", handler.RecordApproval)
	r.Get("/feature-workspaces/{workspaceID}/completion", handler.CompletionStatus)
	r.Post("/feature-workspaces/{workspaceID}/completion", handler.Complete)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/launch", handler.LaunchPrototype)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/reconcile", handler.ReconcilePrototype)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/cancel", handler.CancelPrototype)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/timeout", handler.TimeoutPrototype)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/cleanup", handler.ReconcilePrototypeCleanup)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/another-execution", handler.PrepareAnotherPrototypeExecution)
	r.Post("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/qa-packets", handler.PreparePrototypeQAPacket)
	r.Post("/feature-workspaces/{workspaceID}/prototype-qa-packets/{packetID}/evidence", handler.AdmitPrototypeQAEvidence)
	r.Get("/feature-workspaces/{workspaceID}/prototype-runs/{runID}/wayfinder-evidence", handler.GetPrototypeWayfinderEvidence)
}

type prototypeOperationRequest struct {
	ExpectedRunVersion int64  `json:"expectedRunVersion"`
	MutationIdentity   string `json:"mutationIdentity"`
}

func prototypeResultDTO(v prototypeexecution.Result) map[string]any {
	return map[string]any{"run": v.Run, "runtime": v.Runtime, "target": v.Target, "lease": v.Lease, "evidenceBatches": v.EvidenceBatches, "finalResult": v.FinalResult, "evidence": v.Evidence}
}
func (h *WorkspaceHandler) prototypeOp(w http.ResponseWriter, r *http.Request, fn func(context.Context, prototypeexecution.OperationRequest) (prototypeexecution.Result, error), status int) {
	var q prototypeOperationRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid prototype operation request")
		return
	}
	v, e := fn(r.Context(), prototypeexecution.OperationRequest{WorkspaceID: workspaceID(r), RunID: strings.TrimSpace(chi.URLParam(r, "runID")), ExpectedRunVersion: q.ExpectedRunVersion, MutationIdentity: q.MutationIdentity})
	if e != nil {
		writeWorkspaceError(w, e)
		return
	}
	shared.JSON(w, status, map[string]any{"prototypeExecution": prototypeResultDTO(v)})
}
func (h *WorkspaceHandler) LaunchPrototype(w http.ResponseWriter, r *http.Request) {
	var q prototypeOperationRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid prototype operation request")
		return
	}
	v, e := h.authority.LaunchApprovedPrototype(r.Context(), prototypeexecution.LaunchRequest{WorkspaceID: workspaceID(r), RunID: strings.TrimSpace(chi.URLParam(r, "runID")), ExpectedRunVersion: q.ExpectedRunVersion, MutationIdentity: q.MutationIdentity})
	if e != nil {
		writeWorkspaceError(w, e)
		return
	}
	status := http.StatusConflict
	if v.Run.LifecycleState == "running" || v.Run.LifecycleState == "preparing" {
		status = http.StatusAccepted
	}
	shared.JSON(w, status, map[string]any{"prototypeExecution": prototypeResultDTO(v)})
}
func (h *WorkspaceHandler) ReconcilePrototype(w http.ResponseWriter, r *http.Request) {
	h.prototypeOp(w, r, h.authority.ReconcilePrototypeLaunch, http.StatusOK)
}
func (h *WorkspaceHandler) CancelPrototype(w http.ResponseWriter, r *http.Request) {
	h.prototypeOp(w, r, h.authority.CancelPrototypeExecution, http.StatusOK)
}
func (h *WorkspaceHandler) TimeoutPrototype(w http.ResponseWriter, r *http.Request) {
	h.prototypeOp(w, r, h.authority.SettlePrototypeTimeout, http.StatusOK)
}

func (a appAuthorityAdapter) LaunchApprovedPrototype(ctx context.Context, in prototypeexecution.LaunchRequest) (prototypeexecution.Result, error) {
	return a.service.LaunchApprovedPrototype(ctx, in)
}
func (a appAuthorityAdapter) ReconcilePrototypeLaunch(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return a.service.ReconcilePrototypeLaunch(ctx, in)
}
func (a appAuthorityAdapter) CancelPrototypeExecution(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return a.service.CancelPrototypeExecution(ctx, in)
}
func (a appAuthorityAdapter) SettlePrototypeTimeout(ctx context.Context, in prototypeexecution.OperationRequest) (prototypeexecution.Result, error) {
	return a.service.SettlePrototypeTimeout(ctx, in)
}
func (a appAuthorityAdapter) ReconcilePrototypeCleanup(ctx context.Context, in prototypeexecution.CleanupRequest) (prototypeexecution.CleanupResult, error) {
	return a.service.ReconcilePrototypeCleanup(ctx, in)
}
func (a appAuthorityAdapter) PrepareAnotherPrototypeExecution(ctx context.Context, in featureapp.PrepareAnotherPrototypeExecutionInput) (featureapp.PrototypeExecutionDetail, error) {
	return a.service.PrepareAnotherPrototypeExecution(ctx, in)
}
func (a appAuthorityAdapter) PrepareQADiscoveryPacket(ctx context.Context, in featureapp.PrepareQADiscoveryPacketInput) (featureapp.PrototypeQAPacketDetail, error) {
	return a.service.PrepareQADiscoveryPacket(ctx, in)
}
func (a appAuthorityAdapter) AdmitOperatorQAEvidence(ctx context.Context, in featureapp.AdmitOperatorQAEvidenceInput) (featureapp.PrototypeQAPacketDetail, error) {
	return a.service.AdmitOperatorQAEvidence(ctx, in)
}
func (a appAuthorityAdapter) ReadPrototypeEvidenceForWayfinder(ctx context.Context, workspaceID, runID string) (featureapp.PrototypeWayfinderEvidenceView, error) {
	return a.service.ReadPrototypeEvidenceForWayfinder(ctx, workspaceID, runID)
}

func (h *WorkspaceHandler) ReconcilePrototypeCleanup(w http.ResponseWriter, r *http.Request) {
	var q cleanupRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid prototype cleanup request")
		return
	}
	v, err := h.authority.ReconcilePrototypeCleanup(r.Context(), prototypeexecution.CleanupRequest{WorkspaceID: workspaceID(r), RunID: strings.TrimSpace(chi.URLParam(r, "runID")), ExpectedRunVersion: q.ExpectedRunVersion, MutationIdentity: q.MutationIdentity, TriggerKind: "explicit"})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"prototypeCleanup": map[string]any{"prototypeExecution": prototypeResultDTO(v.Result), "reconciliation": v.Reconciliation}})
}
func (h *WorkspaceHandler) PrepareAnotherPrototypeExecution(w http.ResponseWriter, r *http.Request) {
	var q anotherExecutionRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid another prototype execution request")
		return
	}
	v, err := h.authority.PrepareAnotherPrototypeExecution(r.Context(), featureapp.PrepareAnotherPrototypeExecutionInput{WorkspaceID: workspaceID(r), PriorRunID: strings.TrimSpace(chi.URLParam(r, "runID")), ExpectedPriorRunVersion: q.ExpectedPriorRunVersion, MutationIdentity: q.MutationIdentity, OperatorConfirmationEvidence: q.OperatorConfirmationEvidence})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"prototypeExecution": prototypeDetailDTO(v)})
}
func (h *WorkspaceHandler) PreparePrototypeQAPacket(w http.ResponseWriter, r *http.Request) {
	var q qaPacketRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid prototype QA packet request")
		return
	}
	v, err := h.authority.PrepareQADiscoveryPacket(r.Context(), featureapp.PrepareQADiscoveryPacketInput{WorkspaceID: workspaceID(r), RunID: strings.TrimSpace(chi.URLParam(r, "runID")), ExpectedRunVersion: q.ExpectedRunVersion, MutationIdentity: q.MutationIdentity, OperatorPrompt: q.OperatorPrompt, ValidationInstructions: q.ValidationInstructions})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"qaPacket": qaPacketDetailDTO(v)})
}
func (h *WorkspaceHandler) AdmitPrototypeQAEvidence(w http.ResponseWriter, r *http.Request) {
	var q qaEvidenceRequest
	if !decodeStrict(r, &q) {
		badRequest(w, "Invalid prototype QA evidence request")
		return
	}
	evidence := make([]featureapp.OperatorQAEvidenceInput, len(q.Evidence))
	for i, item := range q.Evidence {
		evidence[i] = featureapp.OperatorQAEvidenceInput{SemanticRole: item.SemanticRole, MediaType: item.MediaType, Content: item.Content, SHA256: item.SHA256}
	}
	v, err := h.authority.AdmitOperatorQAEvidence(r.Context(), featureapp.AdmitOperatorQAEvidenceInput{WorkspaceID: workspaceID(r), QAPacketID: strings.TrimSpace(chi.URLParam(r, "packetID")), MutationIdentity: q.MutationIdentity, OperatorConfirmationEvidence: q.OperatorConfirmationEvidence, Evidence: evidence})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"qaPacket": qaPacketDetailDTO(v)})
}
func (h *WorkspaceHandler) GetPrototypeWayfinderEvidence(w http.ResponseWriter, r *http.Request) {
	v, err := h.authority.ReadPrototypeEvidenceForWayfinder(r.Context(), workspaceID(r), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"wayfinderEvidence": v})
}
func prototypeDetailDTO(v featureapp.PrototypeExecutionDetail) map[string]any {
	return map[string]any{"run": v.Run, "runtime": v.Runtime, "target": v.Target, "lease": v.Lease, "evidenceBatches": v.EvidenceBatches, "finalResult": v.FinalResult, "evidence": v.Evidence}
}
func qaPacketDetailDTO(v featureapp.PrototypeQAPacketDetail) map[string]any {
	return map[string]any{"packet": v.Packet, "members": v.Members, "admission": v.Admission, "evidence": v.Evidence}
}
