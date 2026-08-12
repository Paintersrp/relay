package programs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowstore "relay/internal/store/workflow"
	"relay/internal/testsupport/workflowfixture"
)

const programSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// programRuntimeFixture deliberately creates the already-approved package/run
// boundary.  Program dispatch tests therefore exercise the program store
// contract rather than re-testing package construction.
type programRuntimeFixture struct {
	store        *workflowstore.Store
	svc          *Service
	workspace    string
	workspaceRow int64
	closure      int64
	authority    int64
	base         string
}

type fakeAssignmentPreparer struct {
	artifact  workflowstore.Artifact
	artifacts map[string]workflowstore.Artifact
	bytes     map[string][]byte
	errors    map[string]error
}

func (f fakeAssignmentPreparer) PrepareExecutionAssignment(context.Context, string) (executor.ExecutionAssignmentResult, error) {
	return executor.ExecutionAssignmentResult{Artifact: f.artifact}, nil
}

func (f fakeAssignmentPreparer) LoadExecutionAssignment(_ context.Context, runID string) (executor.ExecutionAssignmentResult, error) {
	if f.errors != nil && f.errors[runID] != nil {
		return executor.ExecutionAssignmentResult{}, f.errors[runID]
	}
	content := f.bytes[runID]
	if content == nil {
		return executor.ExecutionAssignmentResult{}, ErrDispatch
	}
	artifact := f.artifacts[runID]
	if artifact.ArtifactID == "" {
		artifact = f.artifact
	}
	return executor.ExecutionAssignmentResult{Artifact: artifact, Bytes: append([]byte(nil), content...)}, nil
}

// canonicalAssignmentBytes is the exact immutable Execution Assignment content
// the fake assignment service serves per Run. It is valid canonical JSON so
// the handoff wire embedding can be verified byte-for-byte.
func canonicalAssignmentBytes(runID, ticketID string, revision int64, repo, branch, base string) []byte {
	content, err := json.Marshal(executor.ExecutionAssignment{
		SchemaVersion: "1.0",
		Run:           executor.ExecutionAssignmentRun{RunID: runID},
		Ticket:        executor.ExecutionAssignmentTicket{TicketID: ticketID, RevisionNumber: revision},
		Repository:    executor.ExecutionAssignmentRepository{Target: repo, Branch: branch, BaseCommit: base},
	})
	if err != nil {
		panic(err)
	}
	return append(content, '\n')
}

func TestPrepareAdmitsApprovedSetupReadyPackageAndRejectsInvalidTransitions(t *testing.T) {
	ctx := context.Background()
	t.Run("valid and duplicate", func(t *testing.T) {
		f := newProgramRuntimeFixture(t)
		seed := f.member(t, "prepare", "relay", "main", programSHA, "absent")
		f.svc.assignments = fakeAssignmentPreparer{artifact: f.assignmentArtifact(t, seed.AssignmentArtifactID)}
		prepared, err := f.svc.Prepare(ctx, f.workspace, seed.PackageID, 1)
		if err != nil || prepared.State != "prepared" || prepared.PackageID != seed.PackageID || prepared.AssignmentArtifactID != seed.AssignmentArtifactID {
			t.Fatalf("Prepare() = %#v, %v", prepared, err)
		}
		if _, err := f.svc.Prepare(ctx, f.workspace, seed.PackageID, 1); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate Prepare() = %v", err)
		}
	})
	for _, tc := range []struct {
		name  string
		alter func(*programRuntimeFixture, PreparedMember)
	}{
		{"executing run", func(f *programRuntimeFixture, seed PreparedMember) {
			f.update(t, "UPDATE runs SET status='executing' WHERE run_id=?", seed.RunID)
		}},
		{"stale ticket revision", func(f *programRuntimeFixture, seed PreparedMember) {
			f.advanceTicketRevision(t, seed.TicketRevisionRowID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProgramRuntimeFixture(t)
			seed := f.member(t, "reject", "relay", "main", programSHA, "absent")
			f.svc.assignments = fakeAssignmentPreparer{artifact: f.assignmentArtifact(t, seed.AssignmentArtifactID)}
			tc.alter(f, seed)
			if _, err := f.svc.Prepare(ctx, f.workspace, seed.PackageID, 1); !errors.Is(err, ErrAdmission) {
				t.Fatalf("Prepare() = %v", err)
			}
		})
	}
}

func TestCancelReleasesPreparedMemberAndRejectsStaleWorkspaceVersion(t *testing.T) {
	ctx := context.Background()
	f := newProgramRuntimeFixture(t)
	member := f.member(t, "cancel", "relay", "main", programSHA, "prepared")
	if err := f.svc.Cancel(ctx, f.workspace, member.ID, 1); err != nil {
		t.Fatal(err)
	}
	prepared, err := f.svc.ListPrepared(ctx, f.workspace)
	if err != nil || len(prepared) != 1 || prepared[0].State != "cancelled" {
		t.Fatalf("ListPrepared() = %#v, %v", prepared, err)
	}
	if err := f.svc.Cancel(ctx, f.workspace, member.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat Cancel() = %v", err)
	}
	if err := f.svc.Cancel(ctx, f.workspace, member.ID, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Cancel() = %v", err)
	}
	if _, err := f.store.DB().ExecContext(ctx, "INSERT INTO delivery_ticket_selections(selection_id,workspace_row_id,state,rationale,source_closure_row_id) VALUES('selection-after-cancel',?,'active','next selection',?)", f.workspaceRow, f.closure); err != nil {
		t.Fatalf("active selection after consumed ready lineage: %v", err)
	}
}

func TestProgramDispatchStoreLifecycleAndGuards(t *testing.T) {
	ctx := context.Background()
	f := newProgramRuntimeFixture(t)
	members := []PreparedMember{
		f.member(t, "one", "relay", "main", programSHA, "prepared"),
		f.member(t, "two", "relay", "main", programSHA, "prepared"),
	}
	dispatch, err := f.svc.CreateDispatch(ctx, f.workspace, 1, []string{members[0].ID, members[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if dispatch.Status != "dispatched" || len(dispatch.Members) != 2 {
		t.Fatalf("dispatch = %#v", dispatch)
	}

	read, err := f.svc.Read(ctx, f.workspace, dispatch.ID)
	if err != nil || len(read.Members) != 2 {
		t.Fatalf("read = %#v, %v", read, err)
	}
	for i, want := range members {
		got := read.Members[i]
		if got.ID != want.ID || got.PackageID != want.PackageID || got.RunID != want.RunID || got.AssignmentArtifactID != want.AssignmentArtifactID || got.TicketRevisionRowID != want.TicketRevisionRowID || got.RepoTarget != want.RepoTarget || got.Branch != want.Branch || got.BaseCommit != want.BaseCommit {
			t.Fatalf("member %d = %#v, want %#v", i, got, want)
		}
	}
	beforeSatisfaction, beforeAudit := f.count(t, "delivery_ticket_revision_satisfactions"), f.count(t, "audit_decisions")
	result := DispatchResultInput{LaterIntegrationRisks: "manual integration", Members: []MemberResultInput{{MemberID: members[0].ID, Outcome: "done", Branch: "feature/one", BranchHeadSHA: strings.Repeat("b", 40)}, {MemberID: members[1].ID, Outcome: "blocked", Blocker: "external gate"}}}
	if err := f.svc.RecordDispatchResult(ctx, f.workspace, dispatch.ID, 1, result); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecordDispatchResult(ctx, f.workspace, dispatch.ID, 1, result); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat = %v", err)
	}
	read, err = f.svc.Read(ctx, f.workspace, dispatch.ID)
	if err != nil || read.Status != "reported" || read.LaterIntegrationRisks != result.LaterIntegrationRisks || read.Members[0].ResultBranch != "feature/one" || read.Members[0].BranchHeadSHA != strings.Repeat("b", 40) || read.Members[1].Blocker != "external gate" {
		t.Fatalf("reported read = %#v, %v", read, err)
	}
	if f.count(t, "delivery_ticket_revision_satisfactions") != beforeSatisfaction || f.count(t, "audit_decisions") != beforeAudit {
		t.Fatal("program reporting changed delivery lifecycle")
	}
	if _, err := f.store.DB().ExecContext(ctx, "UPDATE program_dispatches SET branch='other' WHERE dispatch_id=?", dispatch.ID); err == nil {
		t.Fatal("dispatch identity update succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM program_dispatch_results"); err == nil {
		t.Fatal("dispatch result delete succeeded")
	}
	if _, err := f.store.DB().ExecContext(ctx, "DELETE FROM program_prepared_members"); err == nil {
		t.Fatal("prepared member delete succeeded")
	}
}

func TestReadHandoffRoundTripsCanonicalMemberAuthorityInDispatchSequence(t *testing.T) {
	ctx := context.Background()
	f := newProgramRuntimeFixture(t)
	members := []PreparedMember{
		f.member(t, "one", "relay", "main", programSHA, "prepared"),
		f.member(t, "two", "relay", "main", programSHA, "prepared"),
	}
	dispatch, err := f.svc.CreateDispatch(ctx, f.workspace, 1, []string{members[0].ID, members[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	ticketIDs := []string{"T-ONE", "T-TWO"}
	bytesByRun := map[string][]byte{}
	fake := fakeAssignmentPreparer{bytes: bytesByRun, artifacts: map[string]workflowstore.Artifact{}}
	for i, m := range members {
		content := canonicalAssignmentBytes(m.RunID, ticketIDs[i], 1, m.RepoTarget, m.Branch, m.BaseCommit)
		bytesByRun[m.RunID] = content
		fake.artifacts[m.RunID] = f.assignmentArtifact(t, m.AssignmentArtifactID)
	}
	f.svc.assignments = fake
	handoff, err := f.svc.ReadHandoff(ctx, f.workspace, dispatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.DispatchID != dispatch.ID || handoff.WorkspaceID != f.workspace || handoff.RepoTarget != "relay" || handoff.Branch != "main" || handoff.BaseCommit != programSHA {
		t.Fatalf("handoff header = %#v", handoff)
	}
	if len(handoff.Members) != 2 {
		t.Fatalf("handoff members = %#v", handoff.Members)
	}
	for i, m := range members {
		got := handoff.Members[i]
		if got.Sequence != i+1 || got.MemberID != m.ID || got.TicketID != ticketIDs[i] || got.TicketRevision != 1 || got.PackageID != m.PackageID || got.RunID != m.RunID || got.AssignmentArtifactID != m.AssignmentArtifactID || got.AssignmentSHA256 != strings.Repeat("2", 64) || got.RepoTarget != m.RepoTarget || got.Branch != m.Branch || got.BaseCommit != m.BaseCommit {
			t.Fatalf("handoff member %d = %#v", i, got)
		}
		if !bytes.Equal(got.Assignment, bytesByRun[m.RunID]) {
			t.Fatalf("handoff member %d assignment bytes differ from canonical content", i)
		}
	}
	// The Assignment content survives the wire serialization exactly: the
	// marshaled handoff embeds the canonical Execution Assignment JSON
	// document (Go's Marshal compacts only insignificant surrounding
	// whitespace and never alters the document structure), so decoding the
	// wire member Assignment yields the identical authority content.
	wire, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Members []struct{ Assignment json.RawMessage }
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	for i, m := range members {
		var want, gotWire executor.ExecutionAssignment
		if err := json.Unmarshal(bytesByRun[m.RunID], &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(decoded.Members[i].Assignment, &gotWire); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotWire, want) {
			t.Fatalf("wire member %d assignment document = %#v, want %#v", i, gotWire, want)
		}
	}
}

func TestReadHandoffIsReadOnlyAndFailsClosedOnUnresolvableAssignment(t *testing.T) {
	ctx := context.Background()
	f := newProgramRuntimeFixture(t)
	members := []PreparedMember{
		f.member(t, "one", "relay", "main", programSHA, "prepared"),
		f.member(t, "two", "relay", "main", programSHA, "prepared"),
	}
	dispatch, err := f.svc.CreateDispatch(ctx, f.workspace, 1, []string{members[0].ID, members[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	bytesByRun := map[string][]byte{}
	for i, m := range members {
		bytesByRun[m.RunID] = canonicalAssignmentBytes(m.RunID, []string{"T-ONE", "T-TWO"}[i], 1, m.RepoTarget, m.Branch, m.BaseCommit)
	}
	before := f.programStateSnapshot(t)
	// The second member's bound Execution Assignment is unresolvable: the
	// handoff must fail closed without emitting a partial projection.
	f.svc.assignments = fakeAssignmentPreparer{
		bytes:     map[string][]byte{members[0].RunID: bytesByRun[members[0].RunID]},
		artifacts: map[string]workflowstore.Artifact{members[0].RunID: f.assignmentArtifact(t, members[0].AssignmentArtifactID)},
		errors:    map[string]error{members[1].RunID: errors.New("bound execution assignment is missing")},
	}
	if _, err := f.svc.ReadHandoff(ctx, f.workspace, dispatch.ID); err == nil {
		t.Fatal("handoff succeeded despite unresolvable member assignment")
	}
	f.assertProgramStateUnchanged(t, before)
	// A bound assignment that resolves to a different artifact also fails
	// closed instead of transporting mismatched authority.
	f.svc.assignments = fakeAssignmentPreparer{
		bytes:     bytesByRun,
		artifacts: map[string]workflowstore.Artifact{members[0].RunID: f.assignmentArtifact(t, members[0].AssignmentArtifactID), members[1].RunID: f.assignmentArtifact(t, members[0].AssignmentArtifactID)},
	}
	if _, err := f.svc.ReadHandoff(ctx, f.workspace, dispatch.ID); !errors.Is(err, ErrDispatch) {
		t.Fatalf("mismatched assignment error = %v, want ErrDispatch", err)
	}
	f.assertProgramStateUnchanged(t, before)
	// The healed read emits the complete handoff without mutating anything.
	f.svc.assignments = fakeAssignmentPreparer{
		bytes:     bytesByRun,
		artifacts: map[string]workflowstore.Artifact{members[0].RunID: f.assignmentArtifact(t, members[0].AssignmentArtifactID), members[1].RunID: f.assignmentArtifact(t, members[1].AssignmentArtifactID)},
	}
	handoff, err := f.svc.ReadHandoff(ctx, f.workspace, dispatch.ID)
	if err != nil || len(handoff.Members) != 2 {
		t.Fatalf("healed handoff = %#v, %v", handoff, err)
	}
	f.assertProgramStateUnchanged(t, before)
}

func TestReadHandoffRejectsUnknownDispatch(t *testing.T) {
	ctx := context.Background()
	f := newProgramRuntimeFixture(t)
	if _, err := f.svc.ReadHandoff(ctx, f.workspace, "dispatch-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadHandoff() = %v, want ErrNotFound", err)
	}
}

func TestCreateDispatchRejectsIncompatibleAndUnsatisfiedMembers(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		alter func(*programRuntimeFixture, []PreparedMember)
	}{
		{"base", func(f *programRuntimeFixture, m []PreparedMember) {}},
		{"branch", func(f *programRuntimeFixture, m []PreparedMember) {}},
		{"repo", func(f *programRuntimeFixture, m []PreparedMember) {}},
		{"external-unsatisfied", func(f *programRuntimeFixture, m []PreparedMember) { f.dependency(t, m[0], 0) }},
		{"proposed-sibling", func(f *programRuntimeFixture, m []PreparedMember) { f.dependency(t, m[0], m[1].TicketRevisionRowID) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProgramRuntimeFixture(t)
			secondRepo, secondBranch, secondBase := "relay", "main", programSHA
			if tc.name == "repo" {
				secondRepo = "other"
			}
			if tc.name == "branch" {
				secondBranch = "other"
			}
			if tc.name == "base" {
				secondBase = strings.Repeat("c", 40)
			}
			m := []PreparedMember{f.member(t, "one", "relay", "main", programSHA, "prepared"), f.member(t, "two", secondRepo, secondBranch, secondBase, "prepared")}
			tc.alter(f, m)
			if _, err := f.svc.CreateDispatch(ctx, f.workspace, 1, []string{m[0].ID, m[1].ID}); !errors.Is(err, ErrDispatch) {
				t.Fatalf("CreateDispatch() = %v", err)
			}
		})
	}
}

func newProgramRuntimeFixture(t *testing.T) *programRuntimeFixture {
	t.Helper()
	ctx := context.Background()
	s := workflowfixture.Open(t, workflowstore.Open)
	db := s.DB()
	q := func(sql string, args ...any) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	project := q("INSERT INTO projects(project_id,name) VALUES('project-program','Program') RETURNING id")
	if _, err := db.ExecContext(ctx, "INSERT INTO repository_targets(repo_target,local_path,configured_branch_ref,configuration_version) VALUES('relay','C:/relay','refs/heads/main',1),('other','C:/other','refs/heads/main',1)"); err != nil {
		t.Fatal(err)
	}
	vault := q("INSERT INTO source_vaults(vault_id,repo_target,relative_path) VALUES('vault-program','relay','vault') RETURNING id")
	closure := q("INSERT INTO source_vault_closures(closure_id,vault_row_id,commit_oid,tree_oid,generation,ref_name,state,import_started_at,verified_at) VALUES('closure-program',?,?,?,1,'refs/relay/closures/program','ready','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z') RETURNING id", vault, programSHA, strings.Repeat("b", 40))
	workspace := q("INSERT INTO feature_workspaces(workspace_id,project_row_id,feature_slug) VALUES('workspace-program',?,'program') RETURNING id", project)
	authority := q("INSERT INTO feature_workspace_authority_revisions(authority_revision_id,workspace_row_id,revision_number,source_closure_row_id) VALUES('authority-program',?,1,?) RETURNING id", workspace, closure)
	svc := &Service{store: s, repositoryVerifier: func(context.Context, string, string, string, string, []string, []string, string) error { return nil }}
	return &programRuntimeFixture{store: s, svc: svc, workspace: "workspace-program", workspaceRow: workspace, closure: closure, authority: authority, base: programSHA}
}

func (f *programRuntimeFixture) member(t *testing.T, suffix, repo, branch, base, state string) PreparedMember {
	t.Helper()
	ctx := context.Background()
	db := f.store.DB()
	ticketID := "T-" + strings.ToUpper(suffix)
	var ticket, revision, packageRow, run, artifact int64
	packageBranch := branch
	if branch != "main" {
		packageBranch = "main"
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO delivery_tickets(ticket_id,workspace_row_id) VALUES(?,?) RETURNING id", ticketID, f.workspaceRow).Scan(&ticket); err != nil {
		t.Fatal(err)
	}
	closure, authority := f.closure, f.authority
	if repo != "relay" || base != f.base {
		closure, authority = f.sourceBasis(t, repo, base, suffix)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO delivery_ticket_revisions(delivery_ticket_row_id,revision_number,repo_target,branch,base_commit,source_closure_row_id,source_path,goal,context,transition_applicability) VALUES(?,1,?,?,?,?,?,'goal','context','not_required') RETURNING id", ticket, repo, packageBranch, base, closure, "tickets/"+suffix+".json").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE delivery_tickets SET current_revision_row_id=? WHERE id=?", revision, ticket); err != nil {
		t.Fatal(err)
	}
	var approval, selection, selectionMember, packageMember, packageApproval int64
	if err := db.QueryRowContext(ctx, "INSERT INTO delivery_ticket_revision_approvals(approval_id,revision_row_id,approval_kind,approval_state,rationale,source_closure_row_id,authority_revision_row_id) VALUES(?,?,'delivery','approved','approved',?,?) RETURNING id", "approval-"+suffix, revision, closure, authority).Scan(&approval); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO delivery_ticket_selections(selection_id,workspace_row_id,state,rationale,source_closure_row_id) VALUES(?,?,'active','selected',?) RETURNING id", "selection-"+suffix, f.workspaceRow, closure).Scan(&selection); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO delivery_ticket_selection_members(selection_row_id,sequence,revision_row_id,approval_row_id) VALUES(?,1,?,?) RETURNING id", selection, revision, approval).Scan(&selectionMember); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO execution_packages(package_id,selection_row_id,workspace_row_id,repo_target,branch,base_commit,source_closure_row_id,authority_revision_row_id,package_sha256,authority_sha256,source_sha256) VALUES(?,?,?,?,?,?,?,?,?,?,?) RETURNING id", "package-"+suffix, selection, f.workspaceRow, repo, packageBranch, base, closure, authority, strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)).Scan(&packageRow); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO execution_package_members(package_row_id,selection_member_row_id,sequence,revision_row_id,member_sha256) VALUES(?,?,1,?,?) RETURNING id", packageRow, selectionMember, revision, strings.Repeat("f", 64)).Scan(&packageMember); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO execution_package_approval_bindings(package_row_id,package_member_row_id,approval_row_id,authority_revision_row_id,source_closure_row_id,approval_basis_sha256) VALUES(?,?,?,?,?,?)", packageRow, packageMember, approval, authority, closure, strings.Repeat("1", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE delivery_ticket_selections SET state='consumed' WHERE id=?", selection); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO execution_package_approvals(approval_id,package_row_id,package_sha256,operator_confirmation_evidence) VALUES(?,?,?,'approved') RETURNING id", "pkg-approval-"+suffix, packageRow, strings.Repeat("c", 64)).Scan(&packageApproval); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO runs(run_id,feature_slug,repo_target,execution_package_row_id,package_approval_row_id,branch,base_commit) VALUES(?,?,?,?,?,?,?) RETURNING id", "run-"+suffix, "program", repo, packageRow, packageApproval, packageBranch, base).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE runs SET status='setup_ready' WHERE id=?", run); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO artifacts(artifact_id,owner_type,run_row_id,kind,relative_path,media_type,sha256,size_bytes) VALUES(?,'run',?,'execution_assignment',?,'application/json',?,1) RETURNING id", "artifact-"+suffix, run, "runs/run-"+suffix+"/assignment.json", strings.Repeat("2", 64)).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	id := "program-member-" + suffix
	if state != "absent" {
		if _, err := db.ExecContext(ctx, "INSERT INTO program_prepared_members(prepared_member_id,workspace_row_id,execution_package_row_id,run_row_id,ticket_revision_row_id,assignment_artifact_row_id,repo_target,branch,base_commit,state) VALUES(?,?,?,?,?,?,?,?,?,?)", id, f.workspaceRow, packageRow, run, revision, artifact, repo, branch, base, state); err != nil {
			t.Fatal(err)
		}
	}
	return PreparedMember{ID: id, PackageID: "package-" + suffix, RunID: "run-" + suffix, AssignmentArtifactID: "artifact-" + suffix, RepoTarget: repo, Branch: branch, BaseCommit: base, State: state, TicketRevisionRowID: revision}
}

func (f *programRuntimeFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *programRuntimeFixture) update(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.store.DB().ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *programRuntimeFixture) assignmentArtifact(t *testing.T, artifactID string) workflowstore.Artifact {
	t.Helper()
	var artifact workflowstore.Artifact
	if err := f.store.DB().QueryRowContext(context.Background(), "SELECT id,artifact_id,sha256 FROM artifacts WHERE artifact_id=?", artifactID).Scan(&artifact.ID, &artifact.ArtifactID, &artifact.SHA256); err != nil {
		t.Fatal(err)
	}
	return artifact
}

// programStateSnapshot captures every lifecycle surface the Program handoff
// read must leave untouched: audit, satisfaction, result, dispatch, prepared
// member, and Delivery Ticket current-revision state.
type programStateSnapshot struct {
	satisfactions, audits, dispatchResults, executionResults int
	dispatchStatus                                           string
	preparedStates, ticketCurrentRevisions                   string
}

func (f *programRuntimeFixture) programStateSnapshot(t *testing.T) programStateSnapshot {
	t.Helper()
	var s programStateSnapshot
	s.satisfactions = f.count(t, "delivery_ticket_revision_satisfactions")
	s.audits = f.count(t, "audit_decisions")
	s.dispatchResults = f.count(t, "program_dispatch_results")
	s.executionResults = f.count(t, "program_execution_results")
	if err := f.store.DB().QueryRow("SELECT status FROM program_dispatches").Scan(&s.dispatchStatus); err != nil {
		t.Fatal(err)
	}
	s.preparedStates = f.column(t, "SELECT prepared_member_id,state FROM program_prepared_members ORDER BY id")
	s.ticketCurrentRevisions = f.column(t, "SELECT COALESCE(current_revision_row_id,0) FROM delivery_tickets ORDER BY id")
	return s
}

func (f *programRuntimeFixture) assertProgramStateUnchanged(t *testing.T, before programStateSnapshot) {
	t.Helper()
	if after := f.programStateSnapshot(t); after != before {
		t.Fatalf("program read mutated lifecycle: before=%+v after=%+v", before, after)
	}
}

func (f *programRuntimeFixture) column(t *testing.T, query string) string {
	t.Helper()
	rows, err := f.store.DB().Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out strings.Builder
	width, _ := rows.Columns()
	for rows.Next() {
		values := make([]any, len(width))
		for i := range values {
			values[i] = new(any)
		}
		if err := rows.Scan(values...); err != nil {
			t.Fatal(err)
		}
		for i := range values {
			out.WriteString(strings.TrimSpace(string(fmt.Sprintf("%v", *values[i].(*any)))))
			out.WriteString("|")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func (f *programRuntimeFixture) advanceTicketRevision(t *testing.T, revision int64) {
	t.Helper()
	ctx := context.Background()
	var next int64
	if err := f.store.DB().QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions(delivery_ticket_row_id,revision_number,replaces_revision_row_id,repo_target,branch,base_commit,source_closure_row_id,source_path,goal,context,transition_applicability) SELECT delivery_ticket_row_id,2,id,repo_target,branch,base_commit,source_closure_row_id,source_path,goal,context,transition_applicability FROM delivery_ticket_revisions WHERE id=? RETURNING id`, revision).Scan(&next); err != nil {
		t.Fatal(err)
	}
	f.update(t, "UPDATE delivery_tickets SET current_revision_row_id=? WHERE id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?)", next, revision)
}

func (f *programRuntimeFixture) dependency(t *testing.T, member PreparedMember, dependency int64) {
	t.Helper()
	ctx := context.Background()
	db := f.store.DB()
	if dependency == 0 {
		var ticket int64
		if err := db.QueryRowContext(ctx, "INSERT INTO delivery_tickets(ticket_id,workspace_row_id) VALUES('T-EXTERNAL',?) RETURNING id", f.workspaceRow).Scan(&ticket); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "INSERT INTO delivery_ticket_revisions(delivery_ticket_row_id,revision_number,repo_target,branch,base_commit,source_closure_row_id,source_path,goal,context,transition_applicability) VALUES(?,1,'relay','main',?,?,?,'goal','context','not_required') RETURNING id", ticket, f.base, f.closure, "tickets/external.json").Scan(&dependency); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE delivery_tickets SET current_revision_row_id=? WHERE id=?", dependency, ticket); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO delivery_ticket_revision_dependencies(revision_row_id,sequence,depends_on_revision_row_id,outcome) VALUES(?,1,?,'satisfied')", member.TicketRevisionRowID, dependency); err != nil {
		t.Fatal(err)
	}
}

func (f *programRuntimeFixture) sourceBasis(t *testing.T, repo, base, suffix string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	db := f.store.DB()
	var vault, closure, authority int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM source_vaults WHERE repo_target=?", repo).Scan(&vault); err == nil {
		// The workspace's source vault may own several closure generations.
	} else if err := db.QueryRowContext(ctx, "INSERT INTO source_vaults(vault_id,repo_target,relative_path) VALUES(?,?,?) RETURNING id", "vault-"+suffix, repo, "vault/"+suffix).Scan(&vault); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO source_vault_closures(closure_id,vault_row_id,commit_oid,tree_oid,generation,ref_name,state,import_started_at,verified_at) VALUES(?,?,?,?,1,?,'ready','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z') RETURNING id", "closure-"+suffix, vault, base, strings.Repeat("b", 40), "refs/relay/closures/"+suffix).Scan(&closure); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO feature_workspace_authority_revisions(authority_revision_id,workspace_row_id,revision_number,source_closure_row_id) VALUES(?,?,?,?) RETURNING id", "authority-"+suffix, f.workspaceRow, 2, closure).Scan(&authority); err != nil {
		t.Fatal(err)
	}
	return closure, authority
}
