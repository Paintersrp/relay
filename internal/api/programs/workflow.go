package programs

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"relay/internal/api/shared"
	app "relay/internal/app/programs"
	"strings"
)

type Service interface {
	Prepare(context.Context, string, string, int64) (app.PreparedMember, error)
	Cancel(context.Context, string, string, int64) error
	CreateDispatch(context.Context, string, int64, []string) (app.Dispatch, error)
	RecordDispatchResult(context.Context, string, string, int64, app.DispatchResultInput) error
	Read(context.Context, string, string) (app.Dispatch, error)
	ReadHandoff(context.Context, string, string) (app.Handoff, error)
	ListPrepared(context.Context, string) ([]app.PreparedMember, error)
}

type Handler struct{ service Service }

func NewHandler(s Service) *Handler { return &Handler{s} }

type prepareRequest struct {
	PackageID       string `json:"packageId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type dispatchRequest struct {
	ExpectedVersion int64    `json:"expectedVersion"`
	MemberIDs       []string `json:"memberIds"`
}
type dispatchResultRequest struct {
	ExpectedVersion       int64                 `json:"expectedVersion"`
	Members               []memberResultRequest `json:"members"`
	LaterIntegrationRisks string                `json:"laterIntegrationRisks"`
}
type memberResultRequest struct {
	MemberID      string `json:"memberId"`
	Outcome       string `json:"outcome"`
	Branch        string `json:"branch"`
	BranchHeadSHA string `json:"branchHeadSha"`
	Blocker       string `json:"blocker"`
}

func decode(r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v) == nil && d.Decode(&struct{}{}) == io.EOF
}
func bad(w http.ResponseWriter) {
	shared.Error(w, 400, "BAD_REQUEST", "Invalid program dispatch request")
}
func reply(w http.ResponseWriter, v any, e error, status int) {
	if e == nil {
		shared.JSON(w, status, v)
		return
	}
	code := 409
	if e == app.ErrInvalidInput {
		code = 400
	}
	if e == app.ErrNotFound {
		code = 404
	}
	shared.Error(w, code, "PROGRAM_DISPATCH_CONFLICT", e.Error())
}
func (h *Handler) Prepare(w http.ResponseWriter, r *http.Request) {
	var q prepareRequest
	if !decode(r, &q) {
		bad(w)
		return
	}
	x, e := h.service.Prepare(r.Context(), chi.URLParam(r, "workspaceID"), strings.TrimSpace(q.PackageID), q.ExpectedVersion)
	reply(w, x, e, 201)
}
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if !decode(r, &q) {
		bad(w)
		return
	}
	reply(w, map[string]bool{"cancelled": true}, h.service.Cancel(r.Context(), chi.URLParam(r, "workspaceID"), chi.URLParam(r, "memberID"), q.ExpectedVersion), 200)
}
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	var q dispatchRequest
	if !decode(r, &q) {
		bad(w)
		return
	}
	x, e := h.service.CreateDispatch(r.Context(), chi.URLParam(r, "workspaceID"), q.ExpectedVersion, q.MemberIDs)
	reply(w, x, e, 201)
}
func (h *Handler) Result(w http.ResponseWriter, r *http.Request) {
	var q dispatchResultRequest
	if !decode(r, &q) {
		bad(w)
		return
	}
	members := make([]app.MemberResultInput, 0, len(q.Members))
	for _, m := range q.Members {
		members = append(members, app.MemberResultInput{MemberID: m.MemberID, Outcome: m.Outcome, Branch: m.Branch, BranchHeadSHA: m.BranchHeadSHA, Blocker: m.Blocker})
	}
	e := h.service.RecordDispatchResult(r.Context(), chi.URLParam(r, "workspaceID"), chi.URLParam(r, "dispatchID"), q.ExpectedVersion, app.DispatchResultInput{Members: members, LaterIntegrationRisks: q.LaterIntegrationRisks})
	reply(w, map[string]bool{"recorded": true}, e, 201)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	x, e := h.service.Read(r.Context(), chi.URLParam(r, "workspaceID"), chi.URLParam(r, "dispatchID"))
	reply(w, x, e, 200)
}

// Handoff returns the exact read-only Program Orchestrator handoff projection
// for one immutable Dispatch: canonical Ticket ID and revision per member plus
// the embedded immutable Execution Assignment authority content. It is a pure
// transport read with no side effects.
func (h *Handler) Handoff(w http.ResponseWriter, r *http.Request) {
	x, e := h.service.ReadHandoff(r.Context(), chi.URLParam(r, "workspaceID"), chi.URLParam(r, "dispatchID"))
	reply(w, x, e, 200)
}
func (h *Handler) ListPrepared(w http.ResponseWriter, r *http.Request) {
	x, e := h.service.ListPrepared(r.Context(), chi.URLParam(r, "workspaceID"))
	reply(w, x, e, 200)
}
func MountRoutes(r chi.Router, h *Handler) {
	r.Post("/feature-workspaces/{workspaceID}/program-members", h.Prepare)
	r.Post("/feature-workspaces/{workspaceID}/program-members/{memberID}/cancel", h.Cancel)
	r.Post("/feature-workspaces/{workspaceID}/program-dispatches", h.Dispatch)
	r.Get("/feature-workspaces/{workspaceID}/program-members", h.ListPrepared)
	r.Get("/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}", h.Get)
	r.Get("/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/handoff", h.Handoff)
	r.Post("/feature-workspaces/{workspaceID}/program-dispatches/{dispatchID}/result", h.Result)
}
