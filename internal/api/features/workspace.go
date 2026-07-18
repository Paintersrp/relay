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
	workflowstore "relay/internal/store/workflow"

	"github.com/go-chi/chi/v5"
)

type WayfinderService interface {
	CreateWorkspace(context.Context, wayfinder.CreateWorkspaceInput) (workflowstore.FeatureWorkspace, error)
	ReadWorkspace(context.Context, string) (wayfinder.WorkspaceDetail, error)
	AdmitInput(context.Context, wayfinder.AdmitInputInput) (workflowstore.FeatureWorkspaceAdmittedInput, workflowstore.FeatureWorkspace, error)
	AddDestination(context.Context, wayfinder.AddDestinationInput) (workflowstore.FeatureWorkspaceDestination, workflowstore.FeatureWorkspace, error)
	CreateDiscoveryTicket(context.Context, wayfinder.CreateDiscoveryTicketInput) (workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error)
	ResolveDiscoveryTicket(context.Context, wayfinder.ResolveDiscoveryTicketInput) (workflowstore.FeatureWorkspaceTicketResolution, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error)
	RouteWorkspace(context.Context, wayfinder.RouteWorkspaceInput) (workflowstore.FeatureWorkspaceRouteState, workflowstore.FeatureWorkspace, error)
}

type AuthorityService interface {
	ReadAuthority(context.Context, string) ([]featureapp.AuthorityRevisionDetail, error)
	PublishAuthority(context.Context, featureapp.PublishAuthorityInput) (featureapp.AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error)
}

type WorkspaceHandler struct {
	wayfinder WayfinderService
	authority AuthorityService
}

func NewWorkspaceHandler(wayfinderService WayfinderService, authorityService AuthorityService) *WorkspaceHandler {
	return &WorkspaceHandler{wayfinder: wayfinderService, authority: authorityService}
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
	shared.JSON(w, http.StatusCreated, map[string]any{"ticket": ticketDTO(wayfinder.TicketDetail{Ticket: ticket}), "workspace": workspaceDTO(workspace)})
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
	shared.JSON(w, http.StatusCreated, map[string]any{"resolution": resolutionDTO(resolution), "ticket": ticketDTO(wayfinder.TicketDetail{Ticket: ticket}), "workspace": workspaceDTO(workspace)})
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

func workspaceDTO(value workflowstore.FeatureWorkspace) map[string]any {
	return map[string]any{"workspaceId": value.WorkspaceID, "featureSlug": value.FeatureSlug, "state": value.State, "version": value.Version, "createdAt": value.CreatedAt, "updatedAt": value.UpdatedAt}
}
func admittedInputDTO(value workflowstore.FeatureWorkspaceAdmittedInput) map[string]any {
	return map[string]any{"inputId": value.AdmittedInputID, "sequence": value.Sequence, "name": value.InputName, "role": value.InputRole, "sourceKind": value.SourceKind, "artifactRowId": nullableIntDTO(value.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(value.RetainedArtifactRowID), "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "artifactSha256": nullableStringDTO(value.ArtifactSha256), "sourceReference": value.SourceReference, "createdAt": value.CreatedAt}
}
func destinationDTO(value workflowstore.FeatureWorkspaceDestination) map[string]any {
	return map[string]any{"destinationId": value.DestinationID, "sequence": value.Sequence, "kind": value.DestinationKind, "key": value.DestinationKey, "repoTarget": nullableStringDTO(value.RepoTarget), "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "createdAt": value.CreatedAt}
}
func resolutionDTO(value workflowstore.FeatureWorkspaceTicketResolution) map[string]any {
	return map[string]any{"resolutionId": value.ResolutionID, "sequence": value.Sequence, "kind": value.ResolutionKind, "artifactRowId": nullableIntDTO(value.ArtifactRowID), "retainedArtifactRowId": nullableIntDTO(value.RetainedArtifactRowID), "artifactSha256": value.ArtifactSha256, "sourceClosureRowId": nullableIntDTO(value.SourceClosureRowID), "createdAt": value.CreatedAt}
}
func routeDTO(value workflowstore.FeatureWorkspaceRouteState) map[string]any {
	return map[string]any{"routeId": value.RouteStateID, "sequence": value.Sequence, "workspaceVersion": value.WorkspaceVersion, "state": value.State, "createdAt": value.CreatedAt}
}
func ticketDTO(value wayfinder.TicketDetail) map[string]any {
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
		inputs = append(inputs, admittedInputDTO(item))
	}
	destinations := make([]map[string]any, 0, len(detail.Destinations))
	for _, item := range detail.Destinations {
		destinations = append(destinations, destinationDTO(item))
	}
	tickets := make([]map[string]any, 0, len(detail.Tickets))
	for _, item := range detail.Tickets {
		tickets = append(tickets, ticketDTO(item))
	}
	routes := make([]map[string]any, 0, len(detail.Routes))
	for _, item := range detail.Routes {
		routes = append(routes, routeDTO(item))
	}
	revisions := make([]map[string]any, 0, len(authority))
	for _, item := range authority {
		revisions = append(revisions, authorityDTO(item))
	}
	recorded := false
	for _, item := range detail.Investigations {
		recorded = recorded || item.SourceClosureRowID.Valid
	}
	return map[string]any{"workspace": workspaceDTO(detail.Workspace), "inputs": inputs, "destinations": destinations, "tickets": tickets, "routes": routes, "authorityRevisions": revisions, "sourceBasis": map[string]any{"status": sourceBasisStatus(recorded), "investigationCount": len(detail.Investigations)}}
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
