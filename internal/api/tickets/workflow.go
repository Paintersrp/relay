// Package tickets exposes the bounded operator surface for Delivery Tickets.
package tickets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appoperations "relay/internal/app/operations"
	apptickets "relay/internal/app/tickets"
	"relay/internal/operations/registry"

	"github.com/go-chi/chi/v5"
)

type WorkflowService interface {
	Publish(context.Context, appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error)
	ReplaceDependencies(context.Context, appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error)
	Approve(context.Context, appoperations.TicketApprovalOperationInput) (RevisionApproval, error)
	UpdatePriority(context.Context, appoperations.TicketOperationRequest) (DeliveryTicket, error)
	ListFrontier(context.Context, appoperations.TicketOperationRequest) (apptickets.Frontier, error)
	Select(context.Context, appoperations.TicketSelectionOperationInput) (apptickets.SelectionResult, error)
}

type ReadService interface {
	Read(context.Context, string) (apptickets.TicketDetail, error)
	ListHistory(context.Context, string) ([]RevisionHistory, error)
}

type DeliveryTicket struct {
	TicketID         string
	ExternalPriority int64
	CreatedAt        string
	UpdatedAt        string
}

type RevisionApproval struct {
	ApprovalID             string
	RevisionRowID          int64
	ApprovalKind           string
	ApprovalState          string
	AuthorityRevisionRowID sql.NullInt64
	SourceClosureRowID     int64
	Rationale              string
	CreatedAt              string
}

type RevisionHistory struct {
	RowID                 int64
	RevisionNumber        int64
	ReplacesRevisionRowID sql.NullInt64
	SourceClosureRowID    int64
	CreatedAt             string
	Goal                  string
	CancellationReason    sql.NullString
}

type WorkflowHandler struct {
	workflow WorkflowService
	read     ReadService
}

func NewWorkflowHandler(workflow WorkflowService, read ReadService) *WorkflowHandler {
	return &WorkflowHandler{workflow: workflow, read: read}
}

// NewWorkflowHandlerFromServices binds application owners to the ticket HTTP
// projection boundary without exposing persistence models from this package.
func NewWorkflowHandlerFromServices(workflow *appoperations.TicketWorkflowService, read ReadService) *WorkflowHandler {
	return NewWorkflowHandler(appWorkflowAdapter{service: workflow}, read)
}

type appWorkflowAdapter struct {
	service *appoperations.TicketWorkflowService
}

func (a appWorkflowAdapter) Publish(ctx context.Context, input appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error) {
	return a.service.Publish(ctx, input)
}

func (a appWorkflowAdapter) ReplaceDependencies(ctx context.Context, input appoperations.TicketPublishOperationInput) (apptickets.PublishedRevision, error) {
	return a.service.ReplaceDependencies(ctx, input)
}

func (a appWorkflowAdapter) Approve(ctx context.Context, input appoperations.TicketApprovalOperationInput) (RevisionApproval, error) {
	value, err := a.service.Approve(ctx, input)
	return RevisionApproval{ApprovalID: value.ApprovalID, RevisionRowID: value.RevisionRowID, ApprovalKind: value.ApprovalKind, ApprovalState: value.ApprovalState, AuthorityRevisionRowID: value.AuthorityRevisionRowID, SourceClosureRowID: value.SourceClosureRowID, Rationale: value.Rationale, CreatedAt: value.CreatedAt}, err
}

func (a appWorkflowAdapter) UpdatePriority(ctx context.Context, input appoperations.TicketOperationRequest) (DeliveryTicket, error) {
	value, err := a.service.UpdatePriority(ctx, input)
	return DeliveryTicket{TicketID: value.TicketID, ExternalPriority: value.ExternalPriority, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, err
}

func (a appWorkflowAdapter) ListFrontier(ctx context.Context, input appoperations.TicketOperationRequest) (apptickets.Frontier, error) {
	return a.service.ListFrontier(ctx, input)
}

func (a appWorkflowAdapter) Select(ctx context.Context, input appoperations.TicketSelectionOperationInput) (apptickets.SelectionResult, error) {
	return a.service.Select(ctx, input)
}

type dependencyRequest struct {
	Class string `json:"class"`
	Key   string `json:"key"`
}

type TicketAdmissionRequest struct {
	PacketID             string              `json:"packetId"`
	OperationID          string              `json:"operationId"`
	RequiredDependencies []dependencyRequest `json:"requiredDependencies"`
}

type revisionMemberRequest struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Text string `json:"text"`
}

type dependencyInputRequest struct {
	RevisionRowID int64  `json:"revisionRowId"`
	Outcome       string `json:"outcome"`
}

type revisionRequest struct {
	RepoTarget              string                   `json:"repoTarget"`
	Branch                  string                   `json:"branch"`
	BaseCommit              string                   `json:"baseCommit"`
	SourceClosureRowID      int64                    `json:"sourceClosureRowId"`
	SourcePath              string                   `json:"sourcePath"`
	Goal                    string                   `json:"goal"`
	Context                 string                   `json:"context"`
	TransitionApplicability string                   `json:"transitionApplicability"`
	CancellationReason      string                   `json:"cancellationReason"`
	CanonicalJSON           json.RawMessage          `json:"canonicalJson"`
	RenderedMarkdown        string                   `json:"renderedMarkdown"`
	Members                 []revisionMemberRequest  `json:"members"`
	Dependencies            []dependencyInputRequest `json:"dependencies"`
}

type publishRequest struct {
	TicketAdmissionRequest
	ExternalPriority       int64           `json:"externalPriority"`
	ExpectedRevisionNumber int64           `json:"expectedRevisionNumber"`
	Revision               revisionRequest `json:"revision"`
}

type approveRequest struct {
	TicketAdmissionRequest
	RevisionRowID       int64  `json:"revisionRowId"`
	AuthorityRevisionID string `json:"authorityRevisionId"`
	SourceClosureRowID  int64  `json:"sourceClosureRowId"`
	Rationale           string `json:"rationale"`
}

type priorityRequest struct {
	TicketAdmissionRequest
	ExternalPriority int64 `json:"externalPriority"`
}

type selectionRequest struct {
	TicketAdmissionRequest
	TicketID      string `json:"ticketId"`
	RevisionRowID int64  `json:"revisionRowId"`
	Rationale     string `json:"rationale"`
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	ticketID := strings.TrimSpace(chi.URLParam(r, "ticketID"))
	detail, err := h.read.Read(r.Context(), ticketID)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	history, err := h.read.ListHistory(r.Context(), ticketID)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"ticket": ticketDTO(detail), "history": revisionHistoryDTO(history)})
}

func (h *WorkflowHandler) Frontier(w http.ResponseWriter, r *http.Request) {
	request, ok := frontierRequest(r)
	if !ok {
		badRequest(w, "Invalid ticket frontier request")
		return
	}
	frontier, err := h.workflow.ListFrontier(r.Context(), request)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	entries := make([]map[string]any, 0, len(frontier.Entries))
	for _, entry := range frontier.Entries {
		value := map[string]any{
			"ticketId": entry.TicketID, "revisionRowId": entry.RevisionRowID, "revisionNumber": entry.RevisionNumber,
			"externalPriority": entry.ExternalPriority, "createdAt": entry.CreatedAt, "repoTarget": entry.RepoTarget,
			"branch": entry.Branch, "sourceClosureRowId": entry.SourceClosureRowID,
		}
		if entry.TieWithPrevious != nil {
			value["tieWithPrevious"] = map[string]any{"previousTicketId": entry.TieWithPrevious.PreviousTicketID, "rule": entry.TieWithPrevious.Rule}
		}
		entries = append(entries, value)
	}
	shared.JSON(w, http.StatusOK, map[string]any{"workspaceId": frontier.WorkspaceID, "entries": entries})
}

func (h *WorkflowHandler) Publish(w http.ResponseWriter, r *http.Request) {
	var request publishRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket revision request")
		return
	}
	input, admission, err := publishInput(request, workspaceID(r), ticketID(r), registry.TicketActionPublish)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	result, err := h.workflow.Publish(r.Context(), appoperations.TicketPublishOperationInput{Admission: admission, Publish: input})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"ticket": ticketDTO(apptickets.TicketDetail{Ticket: result.Ticket, Revision: result.Revision, Canonical: result.Canonical, Rendered: result.Rendered})})
}

func (h *WorkflowHandler) ReplaceDependencies(w http.ResponseWriter, r *http.Request) {
	var request publishRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket dependency replacement request")
		return
	}
	input, admission, err := publishInput(request, workspaceID(r), ticketID(r), registry.TicketActionReplaceDependencies)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	result, err := h.workflow.ReplaceDependencies(r.Context(), appoperations.TicketPublishOperationInput{Admission: admission, Publish: input})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"ticket": ticketDTO(apptickets.TicketDetail{Ticket: result.Ticket, Revision: result.Revision, Canonical: result.Canonical, Rendered: result.Rendered})})
}

func (h *WorkflowHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var request approveRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket approval request")
		return
	}
	admission, err := admissionFrom(request.TicketAdmissionRequest, workspaceID(r), ticketID(r), registry.TicketActionApprove, request.SourceClosureRowID, request.RevisionRowID, 0, request.AuthorityRevisionID, "")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	input := apptickets.ApproveInput{TicketID: ticketID(r), RevisionRowID: request.RevisionRowID, AuthorityRevisionID: request.AuthorityRevisionID, Rationale: request.Rationale}
	payload, err := appoperations.TicketApprovalPayloadSHA256(input)
	if err != nil {
		badRequest(w, "Invalid delivery ticket approval request")
		return
	}
	admission.PayloadSHA256 = payload
	approval, err := h.workflow.Approve(r.Context(), appoperations.TicketApprovalOperationInput{Admission: admission, Approve: input})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"approval": approvalDTO(approval)})
}

func (h *WorkflowHandler) UpdatePriority(w http.ResponseWriter, r *http.Request) {
	var request priorityRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket priority request")
		return
	}
	admission, err := admissionFrom(request.TicketAdmissionRequest, workspaceID(r), ticketID(r), registry.TicketActionUpdatePriority, 0, 0, 0, "", "")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	admission.ExternalPriority = request.ExternalPriority
	admission.PayloadSHA256, err = appoperations.TicketPriorityPayloadSHA256(ticketID(r), request.ExternalPriority)
	if err != nil {
		badRequest(w, "Invalid delivery ticket priority request")
		return
	}
	ticket, err := h.workflow.UpdatePriority(r.Context(), admission)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusOK, map[string]any{"ticket": ticketIdentityDTO(ticket)})
}

func (h *WorkflowHandler) Select(w http.ResponseWriter, r *http.Request) {
	var request selectionRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket selection request")
		return
	}
	input := apptickets.SelectInput{WorkspaceID: workspaceID(r), TicketID: request.TicketID, RevisionRowID: request.RevisionRowID, Rationale: request.Rationale}
	payload, err := appoperations.TicketSelectionPayloadSHA256(input)
	if err != nil {
		badRequest(w, "Invalid delivery ticket selection request")
		return
	}
	admission, err := admissionFrom(request.TicketAdmissionRequest, workspaceID(r), request.TicketID, registry.TicketActionSelect, input.RevisionRowID, input.RevisionRowID, 0, "", payload)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	result, err := h.workflow.Select(r.Context(), appoperations.TicketSelectionOperationInput{Admission: admission, Select: input})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, selectionDTO(result))
}

func publishInput(request publishRequest, workspaceID, ticketID string, action registry.AllowedAction) (apptickets.PublishInput, appoperations.TicketOperationRequest, error) {
	if request.Revision.CanonicalJSON == nil {
		return apptickets.PublishInput{}, appoperations.TicketOperationRequest{}, errors.New("canonicalJson is required")
	}
	members := make([]apptickets.RevisionMemberInput, 0, len(request.Revision.Members))
	for _, member := range request.Revision.Members {
		members = append(members, apptickets.RevisionMemberInput{Kind: member.Kind, Path: member.Path, Text: member.Text})
	}
	dependencies := make([]apptickets.DependencyInput, 0, len(request.Revision.Dependencies))
	for _, dependency := range request.Revision.Dependencies {
		dependencies = append(dependencies, apptickets.DependencyInput{RevisionRowID: dependency.RevisionRowID, Outcome: dependency.Outcome})
	}
	input := apptickets.PublishInput{WorkspaceID: workspaceID, TicketID: ticketID, ExternalPriority: request.ExternalPriority, ExpectedRevisionNumber: request.ExpectedRevisionNumber, Revision: apptickets.RevisionInput{
		RepoTarget: request.Revision.RepoTarget, Branch: request.Revision.Branch, BaseCommit: request.Revision.BaseCommit, SourceClosureRowID: request.Revision.SourceClosureRowID,
		SourcePath: request.Revision.SourcePath, Goal: request.Revision.Goal, Context: request.Revision.Context, TransitionApplicability: request.Revision.TransitionApplicability,
		CancellationReason: request.Revision.CancellationReason, CanonicalJSON: request.Revision.CanonicalJSON, RenderedMarkdown: []byte(request.Revision.RenderedMarkdown), Members: members, Dependencies: dependencies,
	}}
	payload, err := appoperations.TicketPublishPayloadSHA256(input)
	if err != nil {
		return apptickets.PublishInput{}, appoperations.TicketOperationRequest{}, err
	}
	admission, err := admissionFrom(request.TicketAdmissionRequest, workspaceID, ticketID, action, input.Revision.SourceClosureRowID, 0, request.ExpectedRevisionNumber, "", payload)
	return input, admission, err
}

func admissionFrom(request TicketAdmissionRequest, workspaceID, ticketID string, action registry.AllowedAction, sourceClosureID, revisionID, expectedRevision int64, authorityID, payload string) (appoperations.TicketOperationRequest, error) {
	dependencies := make([]appoperations.DependencyRequirement, 0, len(request.RequiredDependencies))
	for _, dependency := range request.RequiredDependencies {
		dependencies = append(dependencies, appoperations.DependencyRequirement{Class: dependency.Class, Key: dependency.Key})
	}
	return appoperations.TicketOperationRequest{PacketID: request.PacketID, OperationID: registry.OperationID(request.OperationID), Action: action, WorkspaceID: workspaceID, TicketID: ticketID, RevisionRowID: revisionID, ExpectedRevisionNumber: expectedRevision, AuthorityRevisionID: authorityID, SourceClosureRowID: sourceClosureID, PayloadSHA256: payload, RequiredDependencies: dependencies}, nil
}

func frontierRequest(r *http.Request) (appoperations.TicketOperationRequest, bool) {
	packetID := strings.TrimSpace(r.URL.Query().Get("packetId"))
	operationID := strings.TrimSpace(r.URL.Query().Get("operationId"))
	if packetID == "" || operationID == "" || workspaceID(r) == "" {
		return appoperations.TicketOperationRequest{}, false
	}
	return appoperations.TicketOperationRequest{PacketID: packetID, OperationID: registry.OperationID(operationID), Action: registry.TicketActionReadFrontier, WorkspaceID: workspaceID(r)}, true
}

func ticketDTO(detail apptickets.TicketDetail) map[string]any {
	value := map[string]any{"ticketId": detail.Ticket.TicketID, "externalPriority": detail.Ticket.ExternalPriority, "createdAt": detail.Ticket.CreatedAt, "revision": nil, "readiness": map[string]any{"ready": detail.Readiness.Ready, "selected": detail.Readiness.Selected, "reasons": detail.Readiness.Reasons}}
	if detail.Revision.ID != 0 {
		members := make([]map[string]any, 0, len(detail.Members))
		for _, member := range detail.Members {
			members = append(members, map[string]any{"sequence": member.Sequence, "kind": member.MemberKind, "path": nullableString(member.MemberPath), "text": member.MemberText})
		}
		dependencies := make([]map[string]any, 0, len(detail.Dependencies))
		for _, dependency := range detail.Dependencies {
			dependencies = append(dependencies, map[string]any{"sequence": dependency.Sequence, "revisionRowId": dependency.DependsOnRevisionRowID, "outcome": dependency.Outcome})
		}
		approvals := make([]map[string]any, 0, len(detail.Approvals))
		for _, approval := range detail.Approvals {
			approvals = append(approvals, approvalDTO(RevisionApproval{ApprovalID: approval.ApprovalID, RevisionRowID: approval.RevisionRowID, ApprovalKind: approval.ApprovalKind, ApprovalState: approval.ApprovalState, AuthorityRevisionRowID: approval.AuthorityRevisionRowID, SourceClosureRowID: approval.SourceClosureRowID, Rationale: approval.Rationale, CreatedAt: approval.CreatedAt}))
		}
		value["revision"] = map[string]any{"rowId": detail.Revision.ID, "number": detail.Revision.RevisionNumber, "replacesRevisionRowId": nullableInt(detail.Revision.ReplacesRevisionRowID), "repoTarget": detail.Revision.RepoTarget, "branch": detail.Revision.Branch, "baseCommit": detail.Revision.BaseCommit, "sourceClosureRowId": detail.Revision.SourceClosureRowID, "sourcePath": detail.Revision.SourcePath, "goal": detail.Revision.Goal, "context": detail.Revision.Context, "transitionApplicability": detail.Revision.TransitionApplicability, "cancellationReason": nullableString(detail.Revision.CancellationReason), "canonical": detail.Canonical, "rendered": detail.Rendered, "members": members, "dependencies": dependencies, "approvals": approvals}
	}
	return value
}

func ticketIdentityDTO(value DeliveryTicket) map[string]any {
	return map[string]any{"ticketId": value.TicketID, "externalPriority": value.ExternalPriority, "createdAt": value.CreatedAt, "updatedAt": value.UpdatedAt}
}
func approvalDTO(value RevisionApproval) map[string]any {
	return map[string]any{"approvalId": value.ApprovalID, "revisionRowId": value.RevisionRowID, "kind": value.ApprovalKind, "state": value.ApprovalState, "authorityRevisionId": nullableInt(value.AuthorityRevisionRowID), "sourceClosureRowId": value.SourceClosureRowID, "rationale": value.Rationale, "createdAt": value.CreatedAt}
}
func revisionHistoryDTO(values []RevisionHistory) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"rowId": value.RowID, "number": value.RevisionNumber, "replacesRevisionRowId": nullableInt(value.ReplacesRevisionRowID), "sourceClosureRowId": value.SourceClosureRowID, "createdAt": value.CreatedAt, "goal": value.Goal, "cancellationReason": nullableString(value.CancellationReason)})
	}
	return result
}
func selectionDTO(value apptickets.SelectionResult) map[string]any {
	return map[string]any{"selection": map[string]any{"selectionId": value.Selection.SelectionID, "state": value.Selection.State, "rationale": value.Selection.Rationale, "createdAt": value.Selection.CreatedAt}, "selectedTicket": map[string]any{"ticketId": value.SelectedTicket.TicketID, "revisionRowId": value.SelectedTicket.RevisionRowID, "revisionNumber": value.SelectedTicket.RevisionNumber, "approvalRowId": value.SelectedTicket.ApprovalRowID}}
}
func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
func nullableInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
func workspaceID(r *http.Request) string { return strings.TrimSpace(chi.URLParam(r, "workspaceID")) }
func ticketID(r *http.Request) string    { return strings.TrimSpace(chi.URLParam(r, "ticketID")) }

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
func writeTicketError(w http.ResponseWriter, err error) {
	packetCode := appoperations.ErrorCode(err)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, apptickets.ErrTicketNotFound), errors.Is(err, apptickets.ErrSelectionWorkspaceNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Delivery ticket or workspace was not found")
	case packetCode == appoperations.CodePacketNotFound:
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Operation packet was not found")
	case packetCode != "" && packetCode != appoperations.CodeInternalFailure:
		shared.Error(w, http.StatusConflict, "CONFLICT", "Operation packet is stale, unavailable, or does not authorize this ticket action")
	case errors.Is(err, apptickets.ErrSelectionConflict), errors.Is(err, apptickets.ErrSelectionMemberStale), errors.Is(err, apptickets.ErrSelectionSourceStale), errors.Is(err, apptickets.ErrSelectionAuthorityStale), errors.Is(err, apptickets.ErrSelectionDependenciesInvalid), errors.Is(err, apptickets.ErrRevisionConflict), errors.Is(err, appoperations.ErrTicketAdmission):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Delivery ticket state is stale or packet admission failed")
	case errors.Is(err, apptickets.ErrInvalidTicket), errors.Is(err, apptickets.ErrInvalidSelection), errors.Is(err, apptickets.ErrSelectionMemberNotReady):
		shared.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		shared.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Delivery ticket operation failed")
	}
}

func MountWorkflowRoutes(r chi.Router, handler *WorkflowHandler) {
	r.Get("/feature-workspaces/{workspaceID}/tickets/frontier", handler.Frontier)
	r.Post("/feature-workspaces/{workspaceID}/tickets/{ticketID}/revisions", handler.Publish)
	r.Post("/feature-workspaces/{workspaceID}/tickets/{ticketID}/dependencies", handler.ReplaceDependencies)
	r.Get("/delivery-tickets/{ticketID}", handler.Get)
	r.Post("/delivery-tickets/{ticketID}/approvals", handler.Approve)
	r.Patch("/delivery-tickets/{ticketID}/priority", handler.UpdatePriority)
	r.Post("/feature-workspaces/{workspaceID}/tickets/selection", handler.Select)
}
