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
	wayfinder "relay/internal/app/wayfinder"

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
}

type Workspace struct {
	WorkspaceID string
	FeatureSlug string
	State       string
	Version     int64
	CreatedAt   string
	UpdatedAt   string
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
	wayfinder WayfinderService
	authority AuthorityService
}

func NewWorkspaceHandler(wayfinderService WayfinderService, authorityService AuthorityService) *WorkspaceHandler {
	return &WorkspaceHandler{wayfinder: wayfinderService, authority: authorityService}
}

// NewWorkspaceHandlerFromServices binds the application owners to the HTTP
// projection boundary without exposing persistence models from this package.
func NewWorkspaceHandlerFromServices(wayfinderService *wayfinder.Service, authorityService *featureapp.Service) *WorkspaceHandler {
	return NewWorkspaceHandler(appWayfinderAdapter{service: wayfinderService}, appAuthorityAdapter{service: authorityService})
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
}
type publishAuthorityRequest struct {
	ExpectedVersion    int64                   `json:"expectedVersion"`
	SourceClosureRowID *int64                  `json:"sourceClosureRowId"`
	Layers             []authorityLayerRequest `json:"layers"`
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
		layers = append(layers, featureapp.AuthorityLayerInput{Kind: layer.Kind, ArtifactRowID: nullableInt(layer.ArtifactRowID), RetainedArtifact: nullableInt(layer.RetainedArtifactRowID), ArtifactSHA256: layer.ArtifactSHA256, SourceClosureID: nullableInt(layer.SourceClosureRowID)})
	}
	revision, workspace, err := h.authority.PublishAuthority(r.Context(), featureapp.PublishAuthorityInput{WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion, SourceClosureID: nullableInt(request.SourceClosureRowID), Layers: layers})
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"authorityRevision": authorityDTO(revision), "workspace": workspaceDTO(workspace)})
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
		layers = append(layers, map[string]any{"kind": layer.LayerKind, "sequence": layer.Sequence, "artifactRowId": nullableIntDTO(layer.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(layer.RetainedArtifactRowID), "artifactSha256": layer.ArtifactSha256, "sourceClosureRowId": nullableIntDTO(layer.SourceClosureRowID)})
	}
	return map[string]any{"authorityRevisionId": value.Revision.AuthorityRevisionID, "revisionNumber": value.Revision.RevisionNumber, "sourceClosureRowId": nullableIntDTO(value.Revision.SourceClosureRowID), "layers": layers, "createdAt": value.Revision.CreatedAt}
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
	return map[string]any{"workspace": workspaceDTO(Workspace{WorkspaceID: detail.Workspace.WorkspaceID, FeatureSlug: detail.Workspace.FeatureSlug, State: detail.Workspace.State, Version: detail.Workspace.Version, CreatedAt: detail.Workspace.CreatedAt, UpdatedAt: detail.Workspace.UpdatedAt}), "inputs": inputs, "destinations": destinations, "tickets": tickets, "routes": routes, "authorityRevisions": revisions, "sourceBasis": map[string]any{"status": sourceBasisStatus(recorded), "investigationCount": len(detail.Investigations)}}
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
	case errors.Is(err, wayfinder.ErrWorkspaceNotFound), errors.Is(err, wayfinder.ErrDiscoveryTicketNotFound), errors.Is(err, featureapp.ErrWorkspaceNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Feature workspace or discovery ticket was not found")
	case errors.Is(err, wayfinder.ErrVersionConflict), errors.Is(err, featureapp.ErrVersionConflict):
		shared.Error(w, http.StatusConflict, "VERSION_CONFLICT", "Feature workspace was changed by another operator. Reload before retrying.")
	case errors.Is(err, wayfinder.ErrInvalidWorkspaceRequest), errors.Is(err, featureapp.ErrInvalidAuthorityRequest):
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
	r.Post("/feature-workspaces/{workspaceID}/authority-revisions", handler.PublishAuthority)
}
