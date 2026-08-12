// Package programs owns durable multi-member dispatches without advancing any
// audit, satisfaction, merge, integration, or completion lifecycle.
package programs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/guidedapp"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidInput = errors.New("invalid program dispatch input")
	ErrAdmission    = errors.New("program member is not admissible")
	ErrDispatch     = errors.New("program dispatch is not admissible")
	ErrNotFound     = errors.New("program dispatch resource not found")
	ErrConflict     = errors.New("program dispatch conflict")
)
var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Service struct {
	store       *workflowstore.Store
	assignments assignmentPreparer
}

type assignmentPreparer interface {
	PrepareExecutionAssignment(context.Context, string) (executor.ExecutionAssignmentResult, error)
	LoadExecutionAssignment(context.Context, string) (executor.ExecutionAssignmentResult, error)
}

// ReadWorkspaceProgramState implements the guided program-owner read without
// exposing storage rows or creating any delivery lifecycle transition.
func (s *Service) ReadWorkspaceProgramState(c context.Context, workspaceID string) (guidedapp.ProgramState, error) {
	prepared, err := s.ListPrepared(c, workspaceID)
	if err != nil {
		return guidedapp.ProgramState{}, err
	}
	state := guidedapp.ProgramState{Prepared: make([]guidedapp.ProgramMember, 0, len(prepared))}
	for _, member := range prepared {
		state.Prepared = append(state.Prepared, programMemberState(member))
	}
	rows, err := s.store.DB().QueryContext(c, `SELECT d.dispatch_id FROM program_dispatches d JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE w.workspace_id=? ORDER BY d.id`, workspaceID)
	if err != nil {
		return guidedapp.ProgramState{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return guidedapp.ProgramState{}, err
		}
		dispatch, err := s.Read(c, workspaceID, id)
		if err != nil {
			return guidedapp.ProgramState{}, err
		}
		value := guidedapp.ProgramDispatch{DispatchID: dispatch.ID, Status: dispatch.Status, RepoTarget: dispatch.RepoTarget, Branch: dispatch.Branch, BaseCommit: dispatch.BaseCommit, LaterIntegrationRisks: dispatch.LaterIntegrationRisks}
		for _, member := range dispatch.Members {
			value.Members = append(value.Members, programMemberState(member))
		}
		state.Dispatch = append(state.Dispatch, value)
	}
	return state, rows.Err()
}

func programMemberState(member PreparedMember) guidedapp.ProgramMember {
	return guidedapp.ProgramMember{MemberID: member.ID, State: member.State, Outcome: member.Outcome, Branch: member.ResultBranch, BranchHeadSHA: member.BranchHeadSHA, Blocker: member.Blocker}
}

func NewService(s *workflowstore.Store, v packages.SourceVaultReader) (*Service, error) {
	if s == nil || v == nil {
		return nil, fmt.Errorf("workflow store and source-vault reader are required")
	}
	a, e := executor.NewExecutionAssignmentService(s, v)
	if e != nil {
		return nil, e
	}
	return &Service{s, a}, nil
}

type PreparedMember struct {
	ID, PackageID, RunID, AssignmentArtifactID, RepoTarget, Branch, BaseCommit, State string
	Outcome, ResultBranch, BranchHeadSHA, Blocker                                     string
	TicketRevisionRowID                                                               int64
}
type Dispatch struct {
	ID, WorkspaceID, RepoTarget, Branch, BaseCommit string
	Status                                          string
	LaterIntegrationRisks                           string
	Members                                         []PreparedMember
}
type DispatchResultInput struct {
	Members               []MemberResultInput
	LaterIntegrationRisks string
}
type MemberResultInput struct{ MemberID, Outcome, Branch, BranchHeadSHA, Blocker string }

// Handoff is the read-only Program Dispatch transport projection for the
// external Program Orchestrator. It is derived entirely from already-persisted
// immutable Relay authority and never creates or mutates semantic authority.
// It transports the exact canonical Execution Assignment content per member so
// the orchestrator needs no further Relay lookup for member authority. It is
// transport only: it replaces no Delivery Ticket or Execution Assignment
// authority, adds no Program-level compatibility decision, and performs no
// merge, integration, audit, or remediation transition. Ticket-carried Shared
// Design constraints already bound by each Execution Assignment (authority
// layers, repository instructions, validation commands, deterministic
// operations, delivery ticket document identity) ride inside the embedded
// Assignment content rather than being reinterpreted here.
type Handoff struct {
	DispatchID  string
	WorkspaceID string
	RepoTarget  string
	Branch      string
	BaseCommit  string
	Members     []HandoffMember
}

// HandoffMember carries the canonical Delivery Ticket identity (TicketID and
// exact revision number), the immutable prepared-member identity, the bound
// Run/package/artifact identities, and the exact immutable Execution
// Assignment content bytes for one Dispatch member, in Dispatch sequence.
type HandoffMember struct {
	Sequence             int
	MemberID             string
	TicketID             string
	TicketRevision       int64
	PackageID            string
	RunID                string
	AssignmentArtifactID string
	AssignmentSHA256     string
	// Assignment is the exact canonical Execution Assignment JSON content as
	// produced and byte-verified by the execution assignment service. It is
	// embedded verbatim so the Program Orchestrator can execute from this
	// handoff alone.
	Assignment json.RawMessage
	RepoTarget string
	Branch     string
	BaseCommit string
}

func (s *Service) Read(c context.Context, w, id string) (Dispatch, error) {
	var o Dispatch
	err := s.store.DB().QueryRowContext(c, `SELECT d.dispatch_id,w.workspace_id,d.repo_target,d.branch,d.base_commit,d.status,COALESCE(x.later_integration_risks,'') FROM program_dispatches d JOIN feature_workspaces w ON w.id=d.workspace_row_id LEFT JOIN program_execution_results x ON x.dispatch_row_id=d.id WHERE d.dispatch_id=? AND w.workspace_id=?`, id, w).Scan(&o.ID, &o.WorkspaceID, &o.RepoTarget, &o.Branch, &o.BaseCommit, &o.Status, &o.LaterIntegrationRisks)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	rows, err := s.store.DB().QueryContext(c, `SELECT m.prepared_member_id,p.package_id,r.run_id,a.artifact_id,m.repo_target,m.branch,m.base_commit,m.state,m.ticket_revision_row_id,COALESCE(x.outcome,''),COALESCE(x.branch,''),COALESCE(x.branch_head_sha,''),COALESCE(x.blocker,'') FROM program_dispatch_members dm JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id JOIN execution_packages p ON p.id=m.execution_package_row_id JOIN runs r ON r.id=m.run_row_id JOIN artifacts a ON a.id=m.assignment_artifact_row_id LEFT JOIN program_dispatch_results x ON x.dispatch_member_row_id=dm.id WHERE dm.dispatch_row_id=(SELECT id FROM program_dispatches WHERE dispatch_id=?) ORDER BY dm.sequence`, id)
	if err != nil {
		return Dispatch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m PreparedMember
		if err := rows.Scan(&m.ID, &m.PackageID, &m.RunID, &m.AssignmentArtifactID, &m.RepoTarget, &m.Branch, &m.BaseCommit, &m.State, &m.TicketRevisionRowID, &m.Outcome, &m.ResultBranch, &m.BranchHeadSHA, &m.Blocker); err != nil {
			return Dispatch{}, err
		}
		o.Members = append(o.Members, m)
	}
	return o, rows.Err()
}

// ReadHandoff projects one immutable Dispatch as a self-contained Program
// Orchestrator handoff. Member order is the immutable Dispatch sequence and
// each member embeds the exact canonical Execution Assignment content loaded
// through the same byte-verifying application service that generated it. A
// missing, corrupt, or unverifiable bound assignment fails closed: no partial
// handoff is emitted and nothing is written.
func (s *Service) ReadHandoff(c context.Context, w, id string) (Handoff, error) {
	var o Handoff
	err := s.store.DB().QueryRowContext(c, `SELECT d.dispatch_id,w.workspace_id,d.repo_target,d.branch,d.base_commit FROM program_dispatches d JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE d.dispatch_id=? AND w.workspace_id=?`, id, w).Scan(&o.DispatchID, &o.WorkspaceID, &o.RepoTarget, &o.Branch, &o.BaseCommit)
	if errors.Is(err, sql.ErrNoRows) {
		return Handoff{}, ErrNotFound
	}
	if err != nil {
		return Handoff{}, err
	}
	rows, err := s.store.DB().QueryContext(c, `SELECT dm.sequence,m.prepared_member_id,t.ticket_id,tv.revision_number,p.package_id,r.run_id,a.artifact_id,a.sha256,m.repo_target,m.branch,m.base_commit,m.assignment_artifact_row_id FROM program_dispatch_members dm JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id JOIN execution_packages p ON p.id=m.execution_package_row_id JOIN runs r ON r.id=m.run_row_id JOIN artifacts a ON a.id=m.assignment_artifact_row_id JOIN delivery_ticket_revisions tv ON tv.id=m.ticket_revision_row_id JOIN delivery_tickets t ON t.id=tv.delivery_ticket_row_id WHERE dm.dispatch_row_id=(SELECT id FROM program_dispatches WHERE dispatch_id=?) ORDER BY dm.sequence`, id)
	if err != nil {
		return Handoff{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m HandoffMember
		var artifactRowID int64
		if err := rows.Scan(&m.Sequence, &m.MemberID, &m.TicketID, &m.TicketRevision, &m.PackageID, &m.RunID, &m.AssignmentArtifactID, &m.AssignmentSHA256, &m.RepoTarget, &m.Branch, &m.BaseCommit, &artifactRowID); err != nil {
			return Handoff{}, err
		}
		loaded, e := s.assignments.LoadExecutionAssignment(c, m.RunID)
		if e != nil {
			return Handoff{}, e
		}
		// Fail closed unless the verified assignment resolved to the exact
		// artifact the immutable prepared member is bound to.
		if loaded.Artifact.ID != artifactRowID || loaded.Artifact.ArtifactID != m.AssignmentArtifactID || loaded.Artifact.SHA256 != m.AssignmentSHA256 {
			return Handoff{}, ErrDispatch
		}
		m.Assignment = append(json.RawMessage(nil), loaded.Bytes...)
		o.Members = append(o.Members, m)
	}
	return o, rows.Err()
}

func (s *Service) ListPrepared(c context.Context, w string) ([]PreparedMember, error) {
	rows, err := s.store.DB().QueryContext(c, `SELECT m.prepared_member_id,p.package_id,r.run_id,a.artifact_id,m.repo_target,m.branch,m.base_commit,m.state,m.ticket_revision_row_id FROM program_prepared_members m JOIN execution_packages p ON p.id=m.execution_package_row_id JOIN runs r ON r.id=m.run_row_id JOIN artifacts a ON a.id=m.assignment_artifact_row_id JOIN feature_workspaces w ON w.id=m.workspace_row_id WHERE w.workspace_id=? ORDER BY m.id`, w)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PreparedMember
	for rows.Next() {
		var m PreparedMember
		if err := rows.Scan(&m.ID, &m.PackageID, &m.RunID, &m.AssignmentArtifactID, &m.RepoTarget, &m.Branch, &m.BaseCommit, &m.State, &m.TicketRevisionRowID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Prepare creates a member after preflighting the exact approved package Run.
// The transaction repeats admission after assignment artifact generation.
func (s *Service) Prepare(c context.Context, w, p string, v int64) (PreparedMember, error) {
	if w == "" || p == "" || v < 1 {
		return PreparedMember{}, ErrInvalidInput
	}
	var run string
	if e := s.store.DB().QueryRowContext(c, `SELECT r.run_id FROM execution_packages p JOIN runs r ON r.execution_package_row_id=p.id JOIN feature_workspaces w ON w.id=p.workspace_row_id JOIN execution_package_approvals a ON a.id=r.package_approval_row_id AND a.package_row_id=p.id JOIN delivery_ticket_selections sel ON sel.id=p.selection_row_id JOIN delivery_ticket_selection_members sm ON sm.selection_row_id=sel.id JOIN delivery_tickets ticket ON ticket.id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=sm.revision_row_id) WHERE p.package_id=? AND w.workspace_id=? AND w.version=? AND r.status='setup_ready' AND sel.state='consumed' AND ticket.current_revision_row_id=sm.revision_row_id`, p, w, v).Scan(&run); errors.Is(e, sql.ErrNoRows) {
		return PreparedMember{}, ErrAdmission
	} else if e != nil {
		return PreparedMember{}, e
	}
	a, e := s.assignments.PrepareExecutionAssignment(c, run)
	if e != nil {
		return PreparedMember{}, e
	}
	var o PreparedMember
	e = s.store.WithTx(c, func(t *workflowstore.Tx) error {
		d := t.DB()
		id := "program-member-" + strings.TrimPrefix(workflowstore.NewArtifactID(), "artifact-")
		var wid, pid, rid, rev int64
		var repo, branch, base, sel, status string
		e := d.QueryRowContext(c, `SELECT w.id,p.id,r.id,sm.revision_row_id,p.repo_target,p.branch,p.base_commit,sel.state,r.status FROM execution_packages p JOIN runs r ON r.execution_package_row_id=p.id JOIN feature_workspaces w ON w.id=p.workspace_row_id JOIN delivery_ticket_selections sel ON sel.id=p.selection_row_id JOIN delivery_ticket_selection_members sm ON sm.selection_row_id=sel.id JOIN execution_package_approvals approval ON approval.id=r.package_approval_row_id AND approval.package_row_id=p.id WHERE p.package_id=? AND r.run_id=? AND w.workspace_id=? AND w.version=?`, p, run, w, v).Scan(&wid, &pid, &rid, &rev, &repo, &branch, &base, &sel, &status)
		if e != nil {
			return ErrAdmission
		}
		if sel != "consumed" || status != workflowstore.RunStatusSetupReady {
			return ErrAdmission
		}
		var cur int64
		if e = d.QueryRowContext(c, `SELECT current_revision_row_id FROM delivery_tickets WHERE id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?)`, rev).Scan(&cur); e != nil || cur != rev {
			return ErrAdmission
		}
		var n int
		if e = d.QueryRowContext(c, `SELECT count(*) FROM program_prepared_members WHERE execution_package_row_id=?`, pid).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return ErrConflict
		}
		_, e = d.ExecContext(c, `INSERT INTO program_prepared_members(prepared_member_id,workspace_row_id,execution_package_row_id,run_row_id,ticket_revision_row_id,assignment_artifact_row_id,repo_target,branch,base_commit,state) VALUES(?,?,?,?,?,?,?,?,?,'prepared')`, id, wid, pid, rid, rev, a.Artifact.ID, repo, branch, base)
		if e != nil {
			return e
		}
		o = PreparedMember{ID: id, PackageID: p, RunID: run, AssignmentArtifactID: a.Artifact.ArtifactID, RepoTarget: repo, Branch: branch, BaseCommit: base, State: "prepared", TicketRevisionRowID: rev}
		return nil
	})
	return o, e
}
func (s *Service) Cancel(c context.Context, w, m string, v int64) error {
	return s.store.WithTx(c, func(t *workflowstore.Tx) error {
		r, e := t.DB().ExecContext(c, `UPDATE program_prepared_members SET state='cancelled' WHERE prepared_member_id=? AND state='prepared' AND workspace_row_id=(SELECT id FROM feature_workspaces WHERE workspace_id=? AND version=?)`, m, w, v)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}
func (s *Service) CreateDispatch(c context.Context, w string, v int64, ids []string) (Dispatch, error) {
	if w == "" || v < 1 || len(ids) < 2 {
		return Dispatch{}, ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, x := range ids {
		if x == "" || seen[x] {
			return Dispatch{}, ErrInvalidInput
		}
		seen[x] = true
	}
	var o Dispatch
	e := s.store.WithTx(c, func(t *workflowstore.Tx) error {
		d := t.DB()
		var wid int64
		if e := d.QueryRowContext(c, `SELECT id FROM feature_workspaces WHERE workspace_id=? AND version=?`, w, v).Scan(&wid); e != nil {
			return ErrConflict
		}
		ms := []PreparedMember{}
		for _, x := range ids {
			var m PreparedMember
			e := d.QueryRowContext(c, `SELECT m.prepared_member_id,p.package_id,r.run_id,a.artifact_id,m.repo_target,m.branch,m.base_commit,m.state,m.ticket_revision_row_id FROM program_prepared_members m JOIN execution_packages p ON p.id=m.execution_package_row_id JOIN runs r ON r.id=m.run_row_id JOIN artifacts a ON a.id=m.assignment_artifact_row_id WHERE m.prepared_member_id=? AND m.workspace_row_id=?`, x, wid).Scan(&m.ID, &m.PackageID, &m.RunID, &m.AssignmentArtifactID, &m.RepoTarget, &m.Branch, &m.BaseCommit, &m.State, &m.TicketRevisionRowID)
			if e != nil || m.State != "prepared" {
				return ErrDispatch
			}
			ms = append(ms, m)
		}
		f := ms[0]
		for _, m := range ms {
			if m.RepoTarget != f.RepoTarget || m.Branch != f.Branch || m.BaseCommit != f.BaseCommit {
				return ErrDispatch
			}
			var n int
			if e := d.QueryRowContext(c, `SELECT count(*) FROM runs r JOIN execution_packages p ON p.id=r.execution_package_row_id JOIN execution_package_approvals a ON a.id=r.package_approval_row_id AND a.package_row_id=p.id JOIN program_prepared_members x ON x.run_row_id=r.id AND x.execution_package_row_id=p.id JOIN feature_workspaces w ON w.id=p.workspace_row_id WHERE r.run_id=? AND r.status='setup_ready' AND w.id=? AND w.version=? AND x.ticket_revision_row_id=(SELECT current_revision_row_id FROM delivery_tickets WHERE id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=x.ticket_revision_row_id))`, m.RunID, wid, v).Scan(&n); e != nil || n != 1 {
				return ErrDispatch
			}
			rows, e := d.QueryContext(c, `SELECT depends_on_revision_row_id,outcome FROM delivery_ticket_revision_dependencies WHERE revision_row_id=?`, m.TicketRevisionRowID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var dep int64
				var out string
				_ = rows.Scan(&dep, &out)
				for _, q := range ms {
					var depTicket, memberTicket int64
					_ = d.QueryRowContext(c, `SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?`, dep).Scan(&depTicket)
					_ = d.QueryRowContext(c, `SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?`, q.TicketRevisionRowID).Scan(&memberTicket)
					if depTicket == memberTicket {
						rows.Close()
						return ErrDispatch
					}
				}
				var n int
				var currentDep int64
				if d.QueryRowContext(c, `SELECT current_revision_row_id FROM delivery_tickets WHERE id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?)`, dep).Scan(&currentDep) != nil || out != "satisfied" || d.QueryRowContext(c, `SELECT count(*) FROM delivery_ticket_revision_satisfactions WHERE delivery_ticket_revision_row_id=?`, currentDep).Scan(&n) != nil || n != 1 {
					rows.Close()
					return ErrDispatch
				}
			}
			rows.Close()
		}
		id := "dispatch-" + strings.TrimPrefix(workflowstore.NewArtifactID(), "artifact-")
		r, e := d.ExecContext(c, `INSERT INTO program_dispatches(dispatch_id,workspace_row_id,repo_target,branch,base_commit)VALUES(?,?,?,?,?)`, id, wid, f.RepoTarget, f.Branch, f.BaseCommit)
		if e != nil {
			return e
		}
		did, _ := r.LastInsertId()
		for i, m := range ms {
			if _, e = d.ExecContext(c, `INSERT INTO program_dispatch_members(dispatch_row_id,prepared_member_row_id,sequence)VALUES(?,(SELECT id FROM program_prepared_members WHERE prepared_member_id=?),?)`, did, m.ID, i+1); e != nil {
				return e
			}
			if _, e = d.ExecContext(c, `UPDATE program_prepared_members SET state='dispatched' WHERE prepared_member_id=? AND state='prepared'`, m.ID); e != nil {
				return e
			}
		}
		o = Dispatch{ID: id, WorkspaceID: w, RepoTarget: f.RepoTarget, Branch: f.Branch, BaseCommit: f.BaseCommit, Status: "dispatched", Members: ms}
		return nil
	})
	return o, e
}
func (s *Service) RecordDispatchResult(c context.Context, w, did string, v int64, in DispatchResultInput) error {
	if w == "" || did == "" || v < 1 || strings.TrimSpace(in.LaterIntegrationRisks) != in.LaterIntegrationRisks {
		return ErrInvalidInput
	}
	for _, result := range in.Members {
		if !validMemberResult(result) {
			return ErrInvalidInput
		}
	}
	return s.store.WithTx(c, func(t *workflowstore.Tx) error {
		d := t.DB()
		var dispatchRow int64
		if err := d.QueryRowContext(c, `SELECT d.id FROM program_dispatches d JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE d.dispatch_id=? AND w.workspace_id=? AND w.version=? AND d.status='dispatched'`, did, w, v).Scan(&dispatchRow); err != nil {
			return ErrConflict
		}
		rows, err := d.QueryContext(c, `SELECT m.prepared_member_id,dm.id FROM program_dispatch_members dm JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id WHERE dm.dispatch_row_id=?`, dispatchRow)
		if err != nil {
			return err
		}
		defer rows.Close()
		expected := map[string]int64{}
		for rows.Next() {
			var id string
			var row int64
			if err := rows.Scan(&id, &row); err != nil {
				return err
			}
			expected[id] = row
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(in.Members) != len(expected) {
			return ErrInvalidInput
		}
		seen := map[string]bool{}
		for _, x := range in.Members {
			if seen[x.MemberID] || expected[x.MemberID] == 0 || !validMemberResult(x) {
				return ErrInvalidInput
			}
			seen[x.MemberID] = true
		}
		for _, x := range in.Members {
			if _, err := d.ExecContext(c, `INSERT INTO program_dispatch_results(dispatch_member_row_id,outcome,branch,branch_head_sha,blocker)VALUES(?,?,?,?,?)`, expected[x.MemberID], x.Outcome, null(x.Outcome == "done", x.Branch), null(x.Outcome == "done", x.BranchHeadSHA), null(x.Outcome == "blocked", x.Blocker)); err != nil {
				return ErrConflict
			}
		}
		if _, err := d.ExecContext(c, `INSERT INTO program_execution_results(dispatch_row_id,later_integration_risks)VALUES(?,?)`, dispatchRow, in.LaterIntegrationRisks); err != nil {
			return err
		}
		r, err := d.ExecContext(c, `UPDATE program_dispatches SET status='reported' WHERE id=? AND status='dispatched'`, dispatchRow)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		return nil
	})
}
func validMemberResult(result MemberResultInput) bool {
	done := result.Outcome == "done" && strings.TrimSpace(result.Branch) != "" && strings.TrimSpace(result.Branch) == result.Branch && sha40.MatchString(result.BranchHeadSHA) && result.Blocker == ""
	blocked := result.Outcome == "blocked" && result.Branch == "" && result.BranchHeadSHA == "" && strings.TrimSpace(result.Blocker) != "" && strings.TrimSpace(result.Blocker) == result.Blocker
	return done || blocked
}
func null(ok bool, s string) any {
	if ok {
		return s
	}
	return nil
}
