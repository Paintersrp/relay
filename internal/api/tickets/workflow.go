// Package tickets exposes the bounded operator surface for Delivery Tickets.
package tickets

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"relay/internal/api/shared"
	appoperations "relay/internal/app/operations"
	apptickets "relay/internal/app/tickets"

	"github.com/go-chi/chi/v5"
)

type WorkflowService interface {
	Publish(context.Context, apptickets.PublishInput, *appoperations.RemediationAuthoringReference) (apptickets.PublishedRevision, error)
	ReplaceDependencies(context.Context, apptickets.PublishInput) (apptickets.PublishedRevision, error)
	Approve(context.Context, apptickets.ApproveInput, int64) (RevisionApproval, error)
	UpdatePriority(context.Context, string, int64) (DeliveryTicket, error)
	ListFrontier(context.Context, string) (apptickets.Frontier, error)
	Select(context.Context, apptickets.SelectInput) (apptickets.SelectionResult, error)
	AdmitTicketDesignBrief(context.Context, apptickets.TicketDesignBriefAdmissionInput) (apptickets.TicketDesignBriefAdmissionResult, error)
	CompleteTicketDesignBriefReview(context.Context, apptickets.CompleteBriefReviewInput) (apptickets.TicketDesignBriefReviewResult, error)
	ApproveTicketDesignBrief(context.Context, apptickets.TicketDesignBriefApprovalInput) (apptickets.TicketDesignBriefApprovalResult, error)
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

func (a appWorkflowAdapter) Publish(ctx context.Context, input apptickets.PublishInput, reference *appoperations.RemediationAuthoringReference) (apptickets.PublishedRevision, error) {
	return a.service.Publish(ctx, input, reference)
}

func (a appWorkflowAdapter) ReplaceDependencies(ctx context.Context, input apptickets.PublishInput) (apptickets.PublishedRevision, error) {
	return a.service.ReplaceDependencies(ctx, input)
}

func (a appWorkflowAdapter) Approve(ctx context.Context, input apptickets.ApproveInput, sourceClosureRowID int64) (RevisionApproval, error) {
	value, err := a.service.Approve(ctx, input, sourceClosureRowID)
	return RevisionApproval{ApprovalID: value.ApprovalID, RevisionRowID: value.RevisionRowID, ApprovalKind: value.ApprovalKind, ApprovalState: value.ApprovalState, AuthorityRevisionRowID: value.AuthorityRevisionRowID, SourceClosureRowID: value.SourceClosureRowID, Rationale: value.Rationale, CreatedAt: value.CreatedAt}, err
}

func (a appWorkflowAdapter) UpdatePriority(ctx context.Context, ticketID string, externalPriority int64) (DeliveryTicket, error) {
	value, err := a.service.UpdatePriority(ctx, ticketID, externalPriority)
	return DeliveryTicket{TicketID: value.TicketID, ExternalPriority: value.ExternalPriority, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, err
}

func (a appWorkflowAdapter) ListFrontier(ctx context.Context, workspaceID string) (apptickets.Frontier, error) {
	return a.service.ListFrontier(ctx, workspaceID)
}

func (a appWorkflowAdapter) Select(ctx context.Context, input apptickets.SelectInput) (apptickets.SelectionResult, error) {
	return a.service.Select(ctx, input)
}

func (a appWorkflowAdapter) AdmitTicketDesignBrief(ctx context.Context, input apptickets.TicketDesignBriefAdmissionInput) (apptickets.TicketDesignBriefAdmissionResult, error) {
	return a.service.AdmitTicketDesignBrief(ctx, input)
}

func (a appWorkflowAdapter) CompleteTicketDesignBriefReview(ctx context.Context, input apptickets.CompleteBriefReviewInput) (apptickets.TicketDesignBriefReviewResult, error) {
	return a.service.CompleteTicketDesignBriefReview(ctx, input)
}
func (a appWorkflowAdapter) ApproveTicketDesignBrief(ctx context.Context, input apptickets.TicketDesignBriefApprovalInput) (apptickets.TicketDesignBriefApprovalResult, error) {
	return a.service.ApproveTicketDesignBrief(ctx, input)
}

// --- HTTP request types ---
// Legacy packet-admission fields (packetId, operationId, requiredDependencies)
// are rejected by strict JSON decoding. Remediation authoring fields are
// accepted only by delivery ticket publication.

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
	ExternalPriority              int64           `json:"externalPriority"`
	ExpectedRevisionNumber        int64           `json:"expectedRevisionNumber"`
	Revision                      revisionRequest `json:"revision"`
	RemediationSeedID             string          `json:"remediationSeedId"`
	AuthoringPacketID             string          `json:"authoringPacketId"`
	ExpectedAuthoringPacketSHA256 string          `json:"expectedAuthoringPacketSha256"`
}

type dependencyReplacementRequest struct {
	ExternalPriority       int64           `json:"externalPriority"`
	ExpectedRevisionNumber int64           `json:"expectedRevisionNumber"`
	Revision               revisionRequest `json:"revision"`
}

type approveRequest struct {
	RevisionRowID       int64  `json:"revisionRowId"`
	AuthorityRevisionID string `json:"authorityRevisionId"`
	SourceClosureRowID  int64  `json:"sourceClosureRowId"`
	Rationale           string `json:"rationale"`
}

type priorityRequest struct {
	ExternalPriority int64 `json:"externalPriority"`
}

type selectionRequest struct {
	TicketID      string `json:"ticketId"`
	RevisionRowID int64  `json:"revisionRowId"`
	Rationale     string `json:"rationale"`
}

// ticketDesignBriefAdmissionRequest carries the operator-authored Ticket
// Design Brief Markdown. The canonical filename, digest, and active selection
// basis are resolved server-side by the delivery owner.
type ticketDesignBriefAdmissionRequest struct {
	BytesBase64     string `json:"bytesBase64"`
	CreatedIdentity string `json:"createdIdentity"`
}

// reviewCompletionRequest carries the bounded disposition and the exact
// reviewed bytes the auditor reviewed (base64). The current brief is resolved
// server-side, which recalculates their SHA-256 and compares them against the
// verified current admissible artifact before either disposition is accepted;
// no findings, prose, approval evidence, or identity beyond the reviewer are
// accepted.
type reviewCompletionRequest struct {
	ReviewerIdentity    string `json:"reviewerIdentity"`
	Disposition         string `json:"disposition"`
	ReviewedBytesBase64 string `json:"bytesBase64"`
}

// ticketDesignBriefApprovalRequest carries only the explicit approval
// evidence. The workspace is resolved from the route and the current brief
// identity and digest are resolved server-side from the process-local
// ready-review continuation; no brief ID or digest is accepted.
type ticketDesignBriefApprovalRequest struct {
	ExpectedVersion              int64  `json:"expectedVersion"`
	OperatorConfirmationEvidence string `json:"operatorConfirmationEvidence"`
	CreatedIdentity              string `json:"createdIdentity"`
}

// --- Handlers ---

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
	workspaceID, ok := frontierWorkspaceID(r)
	if !ok {
		badRequest(w, "Invalid ticket frontier request")
		return
	}
	frontier, err := h.workflow.ListFrontier(r.Context(), workspaceID)
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
	input, err := buildPublishInput(request.ExternalPriority, request.ExpectedRevisionNumber, request.Revision, workspaceID(r), ticketID(r))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	input.RemediationSeedID = request.RemediationSeedID
	reference := remediationReference(request)
	if err := appoperations.ValidateTicketPublicationInput(input, reference); err != nil {
		badRequest(w, "Invalid delivery ticket revision request")
		return
	}
	result, err := h.workflow.Publish(r.Context(), input, reference)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"ticket": ticketDTO(apptickets.TicketDetail{Ticket: result.Ticket, Revision: result.Revision, Canonical: result.Canonical, Rendered: result.Rendered})})
}

func (h *WorkflowHandler) ReplaceDependencies(w http.ResponseWriter, r *http.Request) {
	var request dependencyReplacementRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid delivery ticket dependency replacement request")
		return
	}
	input, err := buildPublishInput(request.ExternalPriority, request.ExpectedRevisionNumber, request.Revision, workspaceID(r), ticketID(r))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	result, err := h.workflow.ReplaceDependencies(r.Context(), input)
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
	input := apptickets.ApproveInput{
		TicketID:            ticketID(r),
		RevisionRowID:       request.RevisionRowID,
		AuthorityRevisionID: request.AuthorityRevisionID,
		Rationale:           request.Rationale,
	}
	approval, err := h.workflow.Approve(r.Context(), input, request.SourceClosureRowID)
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
	ticket, err := h.workflow.UpdatePriority(r.Context(), ticketID(r), request.ExternalPriority)
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
	result, err := h.workflow.Select(r.Context(), input)
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, selectionDTO(result))
}

// AdmitTicketDesignBrief is the operational admission entry for the authored
// Ticket Design Brief. The delivery owner resolves the current active
// selection, canonical filename, and digest server-side; the request carries
// only the authored Markdown and an identity.
func (h *WorkflowHandler) AdmitTicketDesignBrief(w http.ResponseWriter, r *http.Request) {
	var request ticketDesignBriefAdmissionRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid Ticket Design Brief admission request")
		return
	}
	bytes, err := base64.StdEncoding.DecodeString(request.BytesBase64)
	if err != nil {
		badRequest(w, "Invalid Ticket Design Brief bytes")
		return
	}
	result, err := h.workflow.AdmitTicketDesignBrief(r.Context(), apptickets.TicketDesignBriefAdmissionInput{
		WorkspaceID: workspaceID(r), Bytes: bytes, CreatedIdentity: strings.TrimSpace(request.CreatedIdentity),
	})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{
		"briefId": result.Brief.BriefID, "filename": result.Filename,
		"sha256": result.Brief.ArtifactSha256, "sizeBytes": result.Brief.ArtifactSizeBytes,
	})
}

// CompleteTicketDesignBriefReview is the bounded completion entry the external
// auditor uses after performing the read-only review. The request must carry
// the exact bytes the auditor reviewed (base64); the owner recalculates their
// SHA-256 and rejects the completion unless they match the verified current
// admissible brief, so a stale or replaced brief can never receive a result. It
// records only the bounded disposition over the server-resolved brief and never
// performs or records approval; ready approval is a distinct transition on the
// separate approval route.
func (h *WorkflowHandler) CompleteTicketDesignBriefReview(w http.ResponseWriter, r *http.Request) {
	var request reviewCompletionRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid Ticket Design Brief review completion request")
		return
	}
	disposition := apptickets.TicketDesignBriefReviewDisposition(strings.TrimSpace(request.Disposition))
	if disposition != apptickets.TicketDesignBriefReviewReadyForApproval && disposition != apptickets.TicketDesignBriefReviewNeedsRevision {
		badRequest(w, "Invalid Ticket Design Brief review disposition")
		return
	}
	reviewedBytes, err := base64.StdEncoding.DecodeString(request.ReviewedBytesBase64)
	if err != nil || len(reviewedBytes) == 0 {
		badRequest(w, "Ticket Design Brief review requires the exact reviewed bytes")
		return
	}
	result, err := h.workflow.CompleteTicketDesignBriefReview(r.Context(), apptickets.CompleteBriefReviewInput{
		WorkspaceID: workspaceID(r), ReviewerIdentity: strings.TrimSpace(request.ReviewerIdentity), Disposition: disposition, ReviewedBytes: reviewedBytes,
	})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	response := map[string]any{"briefId": result.Brief.BriefID, "disposition": result.Disposition}
	if result.Refresh != nil {
		response["refresh"] = map[string]any{"operationId": result.Refresh.OperationID, "reviewedBrief": result.Refresh.ReviewedBrief, "auditorReviewResult": result.Refresh.AuditorReviewResult}
	}
	shared.JSON(w, http.StatusCreated, response)
}

// ApproveTicketDesignBrief is the distinct explicit approval transition. The
// current brief identity and digest are resolved server-side from the
// process-local ready-review continuation; the request carries only the
// expected workspace version, confirmation evidence, and identity.
func (h *WorkflowHandler) ApproveTicketDesignBrief(w http.ResponseWriter, r *http.Request) {
	var request ticketDesignBriefApprovalRequest
	if !decodeStrict(r, &request) {
		badRequest(w, "Invalid Ticket Design Brief approval request")
		return
	}
	if request.ExpectedVersion < 1 || strings.TrimSpace(request.OperatorConfirmationEvidence) == "" || strings.TrimSpace(request.CreatedIdentity) == "" {
		badRequest(w, "Ticket Design Brief approval requires expected version, confirmation evidence, and identity")
		return
	}
	approved, err := h.workflow.ApproveTicketDesignBrief(r.Context(), apptickets.TicketDesignBriefApprovalInput{
		WorkspaceID: workspaceID(r), ExpectedVersion: request.ExpectedVersion,
		OperatorConfirmationEvidence: request.OperatorConfirmationEvidence, CreatedIdentity: request.CreatedIdentity,
	})
	if err != nil {
		writeTicketError(w, err)
		return
	}
	shared.JSON(w, http.StatusCreated, map[string]any{"briefId": approved.Brief.BriefID, "approvalId": approved.Approval.ApprovalID})
}

func buildPublishInput(externalPriority, expectedRevisionNumber int64, revision revisionRequest, workspaceID, ticketID string) (apptickets.PublishInput, error) {
	if revision.CanonicalJSON == nil {
		return apptickets.PublishInput{}, errors.New("canonicalJson is required")
	}
	members := make([]apptickets.RevisionMemberInput, 0, len(revision.Members))
	for _, member := range revision.Members {
		members = append(members, apptickets.RevisionMemberInput{Kind: member.Kind, Path: member.Path, Text: member.Text})
	}
	dependencies := make([]apptickets.DependencyInput, 0, len(revision.Dependencies))
	for _, dependency := range revision.Dependencies {
		dependencies = append(dependencies, apptickets.DependencyInput{RevisionRowID: dependency.RevisionRowID, Outcome: dependency.Outcome})
	}
	return apptickets.PublishInput{WorkspaceID: workspaceID, TicketID: ticketID, ExternalPriority: externalPriority, ExpectedRevisionNumber: expectedRevisionNumber, Revision: apptickets.RevisionInput{
		RepoTarget: revision.RepoTarget, Branch: revision.Branch, BaseCommit: revision.BaseCommit, SourceClosureRowID: revision.SourceClosureRowID,
		SourcePath: revision.SourcePath, Goal: revision.Goal, Context: revision.Context, TransitionApplicability: revision.TransitionApplicability,
		CancellationReason: revision.CancellationReason, CanonicalJSON: revision.CanonicalJSON, RenderedMarkdown: []byte(revision.RenderedMarkdown), Members: members, Dependencies: dependencies,
	}}, nil
}

func remediationReference(request publishRequest) *appoperations.RemediationAuthoringReference {
	if request.RemediationSeedID == "" && request.AuthoringPacketID == "" && request.ExpectedAuthoringPacketSHA256 == "" {
		return nil
	}
	return &appoperations.RemediationAuthoringReference{PacketID: request.AuthoringPacketID, ExpectedPacketSHA256: request.ExpectedAuthoringPacketSHA256}
}

func frontierWorkspaceID(r *http.Request) (string, bool) {
	id := workspaceID(r)
	if id == "" || r.URL.RawQuery != "" {
		return "", false
	}
	return id, true
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
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, apptickets.ErrTicketNotFound), errors.Is(err, apptickets.ErrSelectionWorkspaceNotFound), errors.Is(err, apptickets.ErrTicketDesignBriefNotFound):
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Delivery ticket, workspace, or Ticket Design Brief was not found")
	case packetCode == appoperations.CodePacketNotFound:
		shared.Error(w, http.StatusNotFound, "NOT_FOUND", "Planner remediation authoring packet was not found")
	case packetCode != "" && packetCode != appoperations.CodeInternalFailure:
		shared.Error(w, http.StatusConflict, "CONFLICT", "Planner remediation authoring packet is stale, unavailable, or does not match this remediation publication")
	case errors.Is(err, apptickets.ErrSelectionConflict), errors.Is(err, apptickets.ErrSelectionMemberStale), errors.Is(err, apptickets.ErrSelectionSourceStale), errors.Is(err, apptickets.ErrSelectionAuthorityStale), errors.Is(err, apptickets.ErrSelectionDependenciesInvalid), errors.Is(err, apptickets.ErrRevisionConflict), errors.Is(err, apptickets.ErrTicketDesignBriefConflict), errors.Is(err, apptickets.ErrNoActiveSelection), errors.Is(err, appoperations.ErrTicketAdmission):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Delivery ticket state is stale or invalid")
	case errors.Is(err, apptickets.ErrRemediationSeed):
		shared.Error(w, http.StatusConflict, "CONFLICT", "Delivery ticket remediation seed is stale or already consumed")
	case errors.Is(err, apptickets.ErrInvalidTicket), errors.Is(err, apptickets.ErrInvalidSelection), errors.Is(err, apptickets.ErrSelectionMemberNotReady),
		errors.Is(err, apptickets.ErrInvalidTicketDesignBrief), errors.Is(err, apptickets.ErrTicketDesignBriefBytesMismatch),
		errors.Is(err, apptickets.ErrTicketDesignBriefApproval), errors.Is(err, apptickets.ErrTicketDesignBriefReview), errors.Is(err, apptickets.ErrBriefReviewIncomplete):
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
	r.Post("/feature-workspaces/{workspaceID}/ticket-design-briefs", handler.AdmitTicketDesignBrief)
	r.Post("/feature-workspaces/{workspaceID}/ticket-design-briefs/review-completions", handler.CompleteTicketDesignBriefReview)
	r.Post("/feature-workspaces/{workspaceID}/ticket-design-briefs/approvals", handler.ApproveTicketDesignBrief)
}
