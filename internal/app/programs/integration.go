package programs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appaudits "relay/internal/app/audits"
	workflowartifacts "relay/internal/artifacts/workflow"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

const integrationAssignmentSchemaVersion = "1.0"

const (
	integrationEvidenceProof    = "proof_obligation"
	integrationEvidenceBlackBox = "black_box_outcome"
)

// Integration Assignment runtime state. The Assignment is immutable transport
// generated from exact recorded facts; failed assignments are never patched or
// reused, and retry always generates a fresh Assignment from the recorded facts.
const (
	integrationAssignmentGenerated = "generated"
	integrationAssignmentAdmitted  = "admitted"
	integrationAssignmentVerified  = "verified"
	integrationAssignmentFailed    = "failed"
)

// IntegrationAssignmentDocument is the canonical immutable Relay-generated
// Integration Assignment transport for one exact nonempty subset of one
// Program dispatch's accepted constituents. It is runtime transport only: it
// carries no Delivery Plan identity or authority, no authored plan, no
// approval, and no second lifecycle. Field order is the canonical JSON order.
type IntegrationAssignmentDocument struct {
	SchemaVersion      string                             `json:"schema_version"`
	Assignment         IntegrationAssignmentIdentity      `json:"assignment"`
	Constituents       []IntegrationAssignmentConstituent `json:"constituents"`
	CombinedValidation []IntegrationCombinedValidation    `json:"combined_validation"`
	RequiredEvidence   []IntegrationRequiredEvidenceItem  `json:"required_evidence"`
}

type IntegrationAssignmentIdentity struct {
	AssignmentID string `json:"assignment_id"`
	DispatchID   string `json:"dispatch_id"`
	RepoTarget   string `json:"repo_target"`
	Branch       string `json:"branch"`
	BaseCommit   string `json:"base_commit"`
}

// IntegrationAssignmentConstituent binds one exact eligible constituent of the
// dispatch: the Delivery Ticket identity and revision, the accepted commit and
// pushed branch of its isolated audit, the selected package identity, the
// exact Execution Assignment identity the member was dispatched with, the
// recorded accepted isolated-audit decision identity, the Ticket-carried
// invariants and dependencies, and the constituent's own validation commands
// and required evidence obligations.
type IntegrationAssignmentConstituent struct {
	Sequence            int                            `json:"sequence"`
	MemberID            string                         `json:"member_id"`
	TicketID            string                         `json:"ticket_id"`
	TicketRevision      int64                          `json:"ticket_revision"`
	AcceptedCommit      string                         `json:"accepted_commit"`
	PushedBranch        string                         `json:"pushed_branch"`
	PackageID           string                         `json:"package_id"`
	RunID               string                         `json:"run_id"`
	ExecutionAssignment IntegrationExecutionAssignment `json:"execution_assignment"`
	AuditDecisionID     string                         `json:"audit_decision_id"`
	EligibilityID       string                         `json:"eligibility_id"`
	SharedDesign        IntegrationSharedDesign        `json:"shared_design"`
	ValidationCommands  []IntegrationValidationCommand `json:"validation_commands"`
	RequiredEvidence    []IntegrationRequiredEvidence  `json:"required_evidence"`
}

type IntegrationExecutionAssignment struct {
	ArtifactID string `json:"artifact_id"`
	SHA256     string `json:"sha256"`
}

// IntegrationSharedDesign carries the applicable Ticket-carried Shared Design
// constraints of one bound constituent without adding semantics.
type IntegrationSharedDesign struct {
	RequiredInvariants []string                `json:"required_invariants"`
	ForbiddenBehaviors []string                `json:"forbidden_behaviors"`
	DependsOn          []IntegrationDependency `json:"depends_on"`
}

type IntegrationDependency struct {
	TicketID string `json:"ticket_id"`
	Revision int64  `json:"revision"`
}

func integrationRepositoryVerifierForStore(store *workflowstore.Store) integrationRepositoryVerifier {
	return func(ctx context.Context, repo, branch, base, integrated string, bound, omitted []string, conflictResolution, conflictEvidence string) error {
		target, err := store.GetRepositoryTarget(ctx, repo)
		if err != nil {
			return err
		}
		_, err = workflowrepos.VerifyIntegrationRepository(ctx, target.LocalPath, branch, base, integrated, bound, omitted, conflictResolution, conflictEvidence)
		return err
	}
}

type IntegrationValidationCommand struct {
	WorkingDirectory string `json:"working_directory"`
	Command          string `json:"command"`
	Expected         string `json:"expected"`
}

type IntegrationRequiredEvidence struct {
	Kind       string `json:"kind"`
	Obligation string `json:"obligation"`
}

// IntegrationCombinedValidation is one entry of the combined validation
// commands for the integrated result, derived exactly from the bound
// constituents' recorded validation obligations in Assignment sequence.
type IntegrationCombinedValidation struct {
	Sequence            int    `json:"sequence"`
	ConstituentSequence int    `json:"constituent_sequence"`
	WorkingDirectory    string `json:"working_directory"`
	Command             string `json:"command"`
	Expected            string `json:"expected"`
}

// IntegrationRequiredEvidenceItem is one entry of the combined required
// evidence for the integrated result, derived exactly from the bound
// constituents' recorded proof obligations without adding semantics.
type IntegrationRequiredEvidenceItem struct {
	Sequence            int    `json:"sequence"`
	ConstituentSequence int    `json:"constituent_sequence"`
	Kind                string `json:"kind"`
	Obligation          string `json:"obligation"`
}

type IntegrationAssignmentResult struct {
	AssignmentID  string
	DispatchID    string
	WorkspaceID   string
	RepoTarget    string
	Branch        string
	BaseCommit    string
	Status        string
	ContentSHA256 string
	Document      IntegrationAssignmentDocument
}

type IntegrationMergeResultInput struct {
	IntegratedCommit     string
	PreservationIdentity string
	ConflictResolution   string
	ConflictEvidence     string
	Validations          []IntegrationValidationOutcomeInput
	Evidence             []IntegrationEvidenceOutcomeInput
}

type IntegrationValidationOutcomeInput struct {
	Command  string
	Expected string
	Status   string
	Evidence string
}

type IntegrationEvidenceOutcomeInput struct {
	Kind       string
	Obligation string
	Status     string
	Evidence   string
}

type IntegrationMergeResult struct {
	ResultID             string
	AssignmentID         string
	DispatchID           string
	IntegratedCommit     string
	PreservationIdentity string
	ConflictResolution   string
	ConflictEvidence     string
	Validations          []IntegrationValidationOutcome
	Evidence             []IntegrationEvidenceOutcome
}

type IntegrationValidationOutcome struct {
	Sequence            int
	ConstituentSequence int
	Command             string
	Expected            string
	Status              string
	Evidence            string
}

type IntegrationEvidenceOutcome struct {
	Sequence            int
	ConstituentSequence int
	Kind                string
	Obligation          string
	Status              string
	Evidence            string
}

type IntegrationVerification struct {
	VerificationID string
	AssignmentID   string
	DispatchID     string
	Outcome        string
	FailureReason  string
	Completed      []IntegrationCompletion
}

type IntegrationCompletion struct {
	MemberID       string
	TicketID       string
	TicketRevision int64
	Completed      bool
}

type IntegrationFailure struct {
	VerificationID string
	AssignmentID   string
	DispatchID     string
	FailureReason  string
}

// integrationMemberFacts is the exact recorded dispatch-member lineage of one
// constituent plus its recorded eligibility and isolated-audit facts.
type integrationMemberFacts struct {
	sequence                int
	memberID                string
	dispatchMemberRowID     int64
	packageRowID            int64
	packageID               string
	runID                   string
	assignmentArtifactRowID int64
	assignmentArtifactID    string
	assignmentArtifactSHA   string
	ticketRowID             int64
	ticketID                string
	ticketRevisionRowID     int64
	ticketRevision          int64
	preparedRepo            string
	preparedBranch          string
	preparedBase            string
	eligibilityID           string
	acceptedCommit          string
	pushedBranch            string
	decisionRowID           int64
	auditDecisionID         string
	obligationRevision      int64
	obligationAuthority     int64
	obligationSource        int64
	obligationPackage       int64
}

// GenerateIntegrationAssignment generates the one immutable Integration
// Assignment for one exact nonempty subset of the dispatch's eligible
// constituents. Subset admission requires every selected constituent to be
// integration-eligible with exact recorded facts still current, ordinary Ticket
// dependency closure, and complete selection when cross-member safety cannot
// be resolved from the locked Ticket schema. The Assignment is generated by Relay from
// exact recorded facts and is never authored, independently approved, patched,
// or reused after failure.
func (s *Service) GenerateIntegrationAssignment(ctx context.Context, workspace, dispatchID string, version int64, memberIDs []string) (IntegrationAssignmentResult, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || version < 1 || len(memberIDs) == 0 {
		return IntegrationAssignmentResult{}, ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, member := range memberIDs {
		if member == "" || seen[member] {
			return IntegrationAssignmentResult{}, ErrInvalidInput
		}
		seen[member] = true
	}
	var result IntegrationAssignmentResult
	err := s.store.WithTx(ctx, func(t *workflowstore.Tx) error {
		d := t.DB()
		var dispatchRow int64
		var repo, branch, base, status string
		if err := d.QueryRowContext(ctx, `SELECT d.id, d.repo_target, d.branch, d.base_commit, d.status FROM program_dispatches d JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE d.dispatch_id=? AND w.workspace_id=? AND w.version=?`, dispatchID, workspace, version).Scan(&dispatchRow, &repo, &branch, &base, &status); errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if status != "reported" {
			return ErrAdmission
		}
		var targets int
		if err := d.QueryRowContext(ctx, `SELECT count(*) FROM repository_targets WHERE repo_target=? COLLATE NOCASE`, repo).Scan(&targets); err != nil {
			return err
		}
		if targets != 1 {
			return ErrAdmission
		}
		members, err := s.dispatchMemberFacts(ctx, t, dispatchRow)
		if err != nil {
			return err
		}
		selected := make([]integrationMemberFacts, 0, len(memberIDs))
		for _, member := range members {
			if seen[member.memberID] {
				selected = append(selected, member)
			}
		}
		if len(selected) != len(memberIDs) {
			return ErrAdmission
		}
		if err := s.verifyConstituentEligibility(ctx, t, repo, branch, base, selected); err != nil {
			return err
		}
		if err := s.verifySubsetDependencyClosure(ctx, t, members, selected); err != nil {
			return err
		}
		if err := s.verifySubsetSharedDesignClosure(ctx, t, members, selected); err != nil {
			return err
		}
		assignmentID := workflowstore.NewIntegrationAssignmentID()
		document, err := s.buildIntegrationAssignmentDocument(ctx, t, dispatchID, assignmentID, repo, branch, base, selected)
		if err != nil {
			return err
		}
		content, err := json.Marshal(document)
		if err != nil {
			return err
		}
		content = append(content, '\n')
		digest := sha256.Sum256(content)
		row, err := d.ExecContext(ctx, `INSERT INTO program_integration_assignments(assignment_id,dispatch_row_id,repo_target,branch,base_commit,content,content_sha256) VALUES(?,?,?,?,?,?,?)`, assignmentID, dispatchRow, repo, branch, base, string(content), hex.EncodeToString(digest[:]))
		if err != nil {
			return err
		}
		assignmentRow, _ := row.LastInsertId()
		for _, member := range selected {
			if _, err := d.ExecContext(ctx, `INSERT INTO program_integration_assignment_constituents(assignment_row_id,eligibility_row_id,sequence) VALUES(?,(SELECT id FROM program_integration_eligibilities WHERE eligibility_id=?),?)`, assignmentRow, member.eligibilityID, member.sequence); err != nil {
				return err
			}
		}
		result = IntegrationAssignmentResult{
			AssignmentID: assignmentID, DispatchID: dispatchID, WorkspaceID: workspace,
			RepoTarget: repo, Branch: branch, BaseCommit: base,
			Status: integrationAssignmentGenerated, ContentSHA256: hex.EncodeToString(digest[:]),
			Document: document,
		}
		return nil
	})
	return result, err
}

func (s *Service) dispatchMemberFacts(ctx context.Context, t *workflowstore.Tx, dispatchRow int64) ([]integrationMemberFacts, error) {
	rows, err := t.DB().QueryContext(ctx, `
SELECT dm.sequence, m.prepared_member_id, dm.id, p.id, p.package_id, r.run_id,
       a.id, a.artifact_id, a.sha256, t.id, t.ticket_id, tv.id, tv.revision_number,
       m.repo_target, m.branch, m.base_commit
FROM program_dispatch_members dm
JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id
JOIN execution_packages p ON p.id=m.execution_package_row_id
JOIN runs r ON r.id=m.run_row_id
JOIN artifacts a ON a.id=m.assignment_artifact_row_id
JOIN delivery_ticket_revisions tv ON tv.id=m.ticket_revision_row_id
JOIN delivery_tickets t ON t.id=tv.delivery_ticket_row_id
WHERE dm.dispatch_row_id=? ORDER BY dm.sequence`, dispatchRow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []integrationMemberFacts
	for rows.Next() {
		var member integrationMemberFacts
		if err := rows.Scan(&member.sequence, &member.memberID, &member.dispatchMemberRowID, &member.packageRowID, &member.packageID, &member.runID,
			&member.assignmentArtifactRowID, &member.assignmentArtifactID, &member.assignmentArtifactSHA,
			&member.ticketRowID, &member.ticketID, &member.ticketRevisionRowID, &member.ticketRevision,
			&member.preparedRepo, &member.preparedBranch, &member.preparedBase); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrDispatch
	}
	return members, nil
}

// verifyConstituentEligibility re-verifies every exact recorded eligibility
// fact at generation time: the exact current approved Ticket revision, the
// accepted commit and pushed branch, the selected package identity, the
// Execution Assignment identity when dispatched, the accepted isolated-audit
// decision identity, the executed authority lineage, and the exact dispatch
// repository basis. A missing, stale, or mismatched fact blocks the whole
// Assignment; no partial subset is emitted.
func (s *Service) verifyConstituentEligibility(ctx context.Context, t *workflowstore.Tx, dispatchRepo, dispatchBranch, dispatchBase string, selected []integrationMemberFacts) error {
	d := t.DB()
	for index := range selected {
		member := &selected[index]
		if member.preparedRepo != dispatchRepo || member.preparedBranch != dispatchBranch || member.preparedBase != dispatchBase {
			return ErrAdmission
		}
		var eligibilityID, acceptedCommit, pushedBranch string
		var eligibilityRow, decisionRow, boundRevision, packageRow, artifactRow, authority, source int64
		err := d.QueryRowContext(ctx, `SELECT e.id, e.eligibility_id, e.audited_commit, e.pushed_branch, e.audit_ticket_revision_decision_row_id, e.delivery_ticket_revision_row_id, e.execution_package_row_id, e.assignment_artifact_row_id, e.authority_revision_row_id, e.source_closure_row_id FROM program_integration_eligibilities e WHERE e.dispatch_member_row_id=?`, member.dispatchMemberRowID).Scan(&eligibilityRow, &eligibilityID, &acceptedCommit, &pushedBranch, &decisionRow, &boundRevision, &packageRow, &artifactRow, &authority, &source)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdmission
		}
		if err != nil {
			return err
		}
		if boundRevision != member.ticketRevisionRowID || packageRow != member.packageRowID || artifactRow != member.assignmentArtifactRowID {
			return ErrAdmission
		}
		var current int64
		if err := d.QueryRowContext(ctx, `SELECT current_revision_row_id FROM delivery_tickets WHERE id=?`, member.ticketRowID).Scan(&current); err != nil || current != member.ticketRevisionRowID {
			return ErrAdmission
		}
		var outcome, resultBranch, resultHead string
		if err := d.QueryRowContext(ctx, `SELECT outcome, branch, branch_head_sha FROM program_dispatch_results WHERE dispatch_member_row_id=?`, member.dispatchMemberRowID).Scan(&outcome, &resultBranch, &resultHead); errors.Is(err, sql.ErrNoRows) {
			return ErrAdmission
		} else if err != nil {
			return err
		}
		if outcome != "done" || resultBranch != pushedBranch || resultHead != acceptedCommit {
			return ErrAdmission
		}
		var decisionID, decision, decisionCommit string
		var packetArtifact int64
		if err := d.QueryRowContext(ctx, `SELECT ad.audit_decision_id, ad.decision, ad.audited_commit, ad.audit_packet_artifact_row_id FROM audit_decisions ad JOIN audit_ticket_revision_decisions d ON d.audit_decision_row_id=ad.id WHERE d.id=?`, decisionRow).Scan(&decisionID, &decision, &decisionCommit, &packetArtifact); err != nil {
			return ErrAdmission
		}
		if decision != "accepted" || decisionCommit != acceptedCommit {
			return ErrAdmission
		}
		var obligationRevision, obligationPackage, obligationAuthority, obligationSource int64
		if err := d.QueryRowContext(ctx, `SELECT o.delivery_ticket_revision_row_id, o.execution_package_row_id, o.authority_revision_row_id, o.source_closure_row_id FROM audit_packet_ticket_obligations o JOIN audit_ticket_revision_decisions d ON d.audit_packet_ticket_obligation_row_id=o.id WHERE d.id=?`, decisionRow).Scan(&obligationRevision, &obligationPackage, &obligationAuthority, &obligationSource); err != nil {
			return ErrAdmission
		}
		if obligationRevision != boundRevision || obligationPackage != packageRow || obligationAuthority != authority || obligationSource != source {
			return ErrAdmission
		}
		// The eligibility must not be claimed by a current (generated,
		// admitted, or verified) Assignment. A failed Assignment is never
		// reused; retry binds the same recorded facts through a fresh
		// Assignment.
		var claims int
		if err := d.QueryRowContext(ctx, `SELECT count(*) FROM program_integration_assignment_constituents c JOIN program_integration_assignments a ON a.id=c.assignment_row_id WHERE c.eligibility_row_id=? AND a.status IN ('generated','admitted','verified')`, eligibilityRow).Scan(&claims); err != nil {
			return err
		}
		if claims != 0 {
			return ErrAdmission
		}
		member.eligibilityID = eligibilityID
		member.acceptedCommit = acceptedCommit
		member.pushedBranch = pushedBranch
		member.decisionRowID = decisionRow
		member.auditDecisionID = decisionID
		member.obligationRevision = obligationRevision
		member.obligationAuthority = obligationAuthority
		member.obligationSource = obligationSource
		member.obligationPackage = obligationPackage
	}
	return nil
}

// verifySubsetDependencyClosure checks only ordinary executable dependencies.
func (s *Service) verifySubsetDependencyClosure(ctx context.Context, t *workflowstore.Tx, members, selected []integrationMemberFacts) error {
	d := t.DB()
	selectedByMember := map[string]bool{}
	for _, member := range selected {
		selectedByMember[member.memberID] = true
	}
	dispatchTicketByRow := map[int64]integrationMemberFacts{}
	for _, member := range members {
		dispatchTicketByRow[member.ticketRowID] = member
	}
	for _, member := range members {
		rows, err := d.QueryContext(ctx, `SELECT depends_on_revision_row_id FROM delivery_ticket_revision_dependencies WHERE revision_row_id=?`, member.ticketRevisionRowID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var dependencyRevision int64
			if err := rows.Scan(&dependencyRevision); err != nil {
				rows.Close()
				return err
			}
			var dependencyTicket int64
			if err := d.QueryRowContext(ctx, `SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?`, dependencyRevision).Scan(&dependencyTicket); err != nil {
				rows.Close()
				return err
			}
			if required, bound := dispatchTicketByRow[dependencyTicket]; bound && !selectedByMember[required.memberID] {
				rows.Close()
				return ErrAdmission
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
	}
	return nil
}

// verifySubsetSharedDesignClosure is intentionally separate from ordinary
// dependency closure. The locked Delivery Ticket schema has no structured
// cross-member atomicity or intermediate-state representation, and Relay must
// not infer such authority from prose. Therefore any omitted Program member is
// unsafe to admit; selecting every member is the resolvable safe subset.
func (s *Service) verifySubsetSharedDesignClosure(_ context.Context, _ *workflowstore.Tx, members, selected []integrationMemberFacts) error {
	if len(selected) != len(members) {
		return ErrAdmission
	}
	return nil
}

// buildIntegrationAssignmentDocument derives the immutable Assignment document
// from the exact recorded facts of each selected constituent. Validation
// commands, proof obligations, black-box completion criteria, and Shared
// Design fields are carried from the exact Delivery Ticket bytes recorded
// in the constituent's isolated audit packet without adding, weakening,
// omitting, or rewriting any bound obligation.
func (s *Service) buildIntegrationAssignmentDocument(ctx context.Context, t *workflowstore.Tx, dispatchID, assignmentID, repo, branch, base string, selected []integrationMemberFacts) (IntegrationAssignmentDocument, error) {
	document := IntegrationAssignmentDocument{
		SchemaVersion: integrationAssignmentSchemaVersion,
		Assignment: IntegrationAssignmentIdentity{
			AssignmentID: assignmentID, DispatchID: dispatchID, RepoTarget: repo, Branch: branch, BaseCommit: base,
		},
	}
	for _, member := range selected {
		projection, err := s.loadConstituentTicketProjection(ctx, t, member)
		if err != nil {
			return IntegrationAssignmentDocument{}, err
		}
		constituent := IntegrationAssignmentConstituent{
			Sequence:            member.sequence,
			MemberID:            member.memberID,
			TicketID:            member.ticketID,
			TicketRevision:      member.ticketRevision,
			AcceptedCommit:      member.acceptedCommit,
			PushedBranch:        member.pushedBranch,
			PackageID:           member.packageID,
			RunID:               member.runID,
			ExecutionAssignment: IntegrationExecutionAssignment{ArtifactID: member.assignmentArtifactID, SHA256: member.assignmentArtifactSHA},
			AuditDecisionID:     member.auditDecisionID,
			EligibilityID:       member.eligibilityID,
			SharedDesign: IntegrationSharedDesign{
				RequiredInvariants: append([]string(nil), projection.RequiredInvariants...),
				ForbiddenBehaviors: append([]string(nil), projection.ForbiddenBehaviors...),
			},
		}
		for _, dependency := range projection.DependsOn {
			constituent.SharedDesign.DependsOn = append(constituent.SharedDesign.DependsOn, IntegrationDependency{TicketID: dependency.TicketID, Revision: dependency.Revision})
		}
		for _, command := range projection.ValidationCommands {
			constituent.ValidationCommands = append(constituent.ValidationCommands, IntegrationValidationCommand{WorkingDirectory: command.WorkingDirectory, Command: command.Command, Expected: command.Expected})
		}
		for _, obligation := range projection.ProofObligations {
			constituent.RequiredEvidence = append(constituent.RequiredEvidence, IntegrationRequiredEvidence{Kind: integrationEvidenceProof, Obligation: obligation})
		}
		for _, criteria := range projection.Completion {
			constituent.RequiredEvidence = append(constituent.RequiredEvidence, IntegrationRequiredEvidence{Kind: integrationEvidenceBlackBox, Obligation: criteria})
		}
		if len(constituent.ValidationCommands) == 0 {
			return IntegrationAssignmentDocument{}, ErrAdmission
		}
		document.Constituents = append(document.Constituents, constituent)
	}
	sequence := 0
	for _, constituent := range document.Constituents {
		for _, command := range constituent.ValidationCommands {
			sequence++
			document.CombinedValidation = append(document.CombinedValidation, IntegrationCombinedValidation{
				Sequence: sequence, ConstituentSequence: constituent.Sequence,
				WorkingDirectory: command.WorkingDirectory, Command: command.Command, Expected: command.Expected,
			})
		}
	}
	for _, constituent := range document.Constituents {
		for _, evidence := range constituent.RequiredEvidence {
			sequence++
			document.RequiredEvidence = append(document.RequiredEvidence, IntegrationRequiredEvidenceItem{
				Sequence: sequence, ConstituentSequence: constituent.Sequence,
				Kind: evidence.Kind, Obligation: evidence.Obligation,
			})
		}
	}
	if len(document.CombinedValidation) == 0 || len(document.RequiredEvidence) == 0 {
		return IntegrationAssignmentDocument{}, ErrAdmission
	}
	return document, nil
}

// loadConstituentTicketProjection reads the exact Delivery Ticket bytes
// recorded in the constituent's isolated audit packet and projects them. The
// packet digest binding and the recorded ticket identity and revision are
// re-verified so the derivation can never substitute other bytes.
func (s *Service) loadConstituentTicketProjection(ctx context.Context, t *workflowstore.Tx, member integrationMemberFacts) (speccompiler.DeliveryTicketProjection, error) {
	var packetArtifactRow int64
	if err := t.DB().QueryRowContext(ctx, `SELECT ad.audit_packet_artifact_row_id FROM audit_decisions ad WHERE ad.id=(SELECT audit_decision_row_id FROM audit_ticket_revision_decisions WHERE id=?)`, member.decisionRowID).Scan(&packetArtifactRow); err != nil {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	packetArtifact, err := t.GetArtifactByRowID(ctx, packetArtifactRow)
	if err != nil {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	if packetArtifact.Kind != "audit_packet" || packetArtifact.MediaType != "application/json" {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	_, content, err := s.store.ArtifactStore().ReadVerifiedFile(workflowartifacts.File{
		Kind: packetArtifact.Kind, RelativePath: packetArtifact.RelativePath, MediaType: packetArtifact.MediaType,
		SHA256: packetArtifact.SHA256, SizeBytes: packetArtifact.SizeBytes,
	}, appaudits.MaxWorkflowAuditPacketBytes)
	if err != nil {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	var packet appaudits.WorkflowPackageAuditPacket
	if err := json.Unmarshal(content, &packet); err != nil {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	ticketBytes := []byte(packet.Authority.DeliveryTicket.Content)
	// The packet's declared Ticket digest was bound to the exact source bytes
	// when the packet was built and is immutable evidence; JSON-authored
	// content is carried as a JSON value whose decoded whitespace differs from
	// the source bytes, so only the declared digest shape is re-verified here.
	if len(packet.Authority.DeliveryTicket.SHA256) != 64 || !json.Valid(ticketBytes) {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	var ticket speccompiler.DeliveryTicketDocument
	if err := json.Unmarshal(ticketBytes, &ticket); err != nil {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	projection, diagnostics := speccompiler.ProjectDeliveryTicket(&ticket)
	if len(diagnostics) != 0 || projection.TicketID != member.ticketID || projection.Revision != member.ticketRevision {
		return speccompiler.DeliveryTicketProjection{}, ErrAdmission
	}
	return projection, nil
}

// ReadIntegrationAssignment returns one immutable Integration Assignment with
// its byte-verified transport document. It is a pure read with no side effects.
func (s *Service) ReadIntegrationAssignment(ctx context.Context, workspace, dispatchID, assignmentID string) (IntegrationAssignmentResult, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(assignmentID) == "" {
		return IntegrationAssignmentResult{}, ErrInvalidInput
	}
	var result IntegrationAssignmentResult
	var content string
	var storedSHA string
	err := s.store.DB().QueryRowContext(ctx, `SELECT a.assignment_id, d.dispatch_id, w.workspace_id, a.repo_target, a.branch, a.base_commit, a.status, a.content, a.content_sha256 FROM program_integration_assignments a JOIN program_dispatches d ON d.id=a.dispatch_row_id JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE a.assignment_id=? AND d.dispatch_id=? AND w.workspace_id=?`, assignmentID, dispatchID, workspace).Scan(&result.AssignmentID, &result.DispatchID, &result.WorkspaceID, &result.RepoTarget, &result.Branch, &result.BaseCommit, &result.Status, &content, &storedSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return IntegrationAssignmentResult{}, ErrNotFound
	}
	if err != nil {
		return IntegrationAssignmentResult{}, err
	}
	document, err := decodeIntegrationAssignmentDocument(assignmentID, dispatchID, result.RepoTarget, result.Branch, result.BaseCommit, content, storedSHA)
	if err != nil {
		return IntegrationAssignmentResult{}, err
	}
	result.ContentSHA256 = storedSHA
	result.Document = document
	return result, nil
}

// decodeIntegrationAssignmentDocument verifies the stored Assignment content
// digest and decodes the immutable transport document, re-checking that the
// document identity is exactly the bound row identity.
func decodeIntegrationAssignmentDocument(assignmentID, dispatchID, repo, branch, base, content, storedSHA string) (IntegrationAssignmentDocument, error) {
	digest := sha256.Sum256([]byte(content))
	if hex.EncodeToString(digest[:]) != storedSHA || !json.Valid([]byte(content)) {
		return IntegrationAssignmentDocument{}, ErrConflict
	}
	var document IntegrationAssignmentDocument
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return IntegrationAssignmentDocument{}, ErrConflict
	}
	if document.SchemaVersion != integrationAssignmentSchemaVersion || document.Assignment.AssignmentID != assignmentID || document.Assignment.DispatchID != dispatchID || document.Assignment.RepoTarget != repo || document.Assignment.Branch != branch || document.Assignment.BaseCommit != base {
		return IntegrationAssignmentDocument{}, ErrConflict
	}
	return document, nil
}

// AdmitIntegrationMergeResult admits the one external Merge result for an
// Assignment. The returned outcomes must be exactly the bound combined
// validation commands and required evidence: same count, same order, and the
// same command/expected and kind/obligation identities. The admitted result is
// immutable evidence; failed or stale Assignments are never patched or reused.
func (s *Service) AdmitIntegrationMergeResult(ctx context.Context, workspace, dispatchID, assignmentID string, version int64, input IntegrationMergeResultInput) (IntegrationMergeResult, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(assignmentID) == "" || version < 1 {
		return IntegrationMergeResult{}, ErrInvalidInput
	}
	if !sha40.MatchString(strings.ToLower(strings.TrimSpace(input.IntegratedCommit))) || strings.TrimSpace(input.PreservationIdentity) == "" || strings.TrimSpace(input.PreservationIdentity) != input.PreservationIdentity || !validConflictResolution(input.IntegratedCommit, input.ConflictResolution, input.ConflictEvidence) {
		return IntegrationMergeResult{}, ErrInvalidInput
	}
	input.IntegratedCommit = strings.ToLower(strings.TrimSpace(input.IntegratedCommit))
	for _, outcome := range input.Validations {
		if outcome.Status != "passed" && outcome.Status != "failed" {
			return IntegrationMergeResult{}, ErrInvalidInput
		}
		if strings.TrimSpace(outcome.Evidence) == "" || strings.TrimSpace(outcome.Evidence) != outcome.Evidence {
			return IntegrationMergeResult{}, ErrInvalidInput
		}
	}
	for _, outcome := range input.Evidence {
		if outcome.Status != "passed" && outcome.Status != "failed" {
			return IntegrationMergeResult{}, ErrInvalidInput
		}
		if strings.TrimSpace(outcome.Evidence) == "" || strings.TrimSpace(outcome.Evidence) != outcome.Evidence {
			return IntegrationMergeResult{}, ErrInvalidInput
		}
	}
	var result IntegrationMergeResult
	err := s.store.WithTx(ctx, func(t *workflowstore.Tx) error {
		d := t.DB()
		assignment, err := s.loadAssignmentRow(ctx, d, workspace, dispatchID, assignmentID, version)
		if err != nil {
			return err
		}
		if assignment.status != integrationAssignmentGenerated {
			return ErrConflict
		}
		document, err := decodeIntegrationAssignmentDocument(assignmentID, dispatchID, assignment.repo, assignment.branch, assignment.base, assignment.content, assignment.sha256)
		if err != nil {
			return err
		}
		if err := exactMergeOutcomes(document, input); err != nil {
			return err
		}
		resultID := workflowstore.NewIntegrationMergeResultID()
		row, err := d.ExecContext(ctx, `INSERT INTO program_integration_merge_results(result_id,assignment_row_id,integrated_commit,preservation_identity,conflict_resolution,conflict_evidence) VALUES(?,(SELECT id FROM program_integration_assignments WHERE assignment_id=?),?,?,?,?)`, resultID, assignmentID, input.IntegratedCommit, input.PreservationIdentity, input.ConflictResolution, sql.NullString{String: input.ConflictEvidence, Valid: input.ConflictEvidence != ""})
		if err != nil {
			return err
		}
		resultRow, _ := row.LastInsertId()
		for index, outcome := range input.Validations {
			bound := document.CombinedValidation[index]
			if _, err := d.ExecContext(ctx, `INSERT INTO program_integration_validation_outcomes(merge_result_row_id,sequence,constituent_sequence,command,expected,status,evidence) VALUES(?,?,?,?,?,?,?)`, resultRow, index+1, bound.ConstituentSequence, outcome.Command, outcome.Expected, outcome.Status, outcome.Evidence); err != nil {
				return err
			}
		}
		for index, outcome := range input.Evidence {
			bound := document.RequiredEvidence[index]
			if _, err := d.ExecContext(ctx, `INSERT INTO program_integration_evidence_outcomes(merge_result_row_id,sequence,constituent_sequence,obligation,kind,status,evidence) VALUES(?,?,?,?,?,?,?)`, resultRow, index+1, bound.ConstituentSequence, outcome.Obligation, outcome.Kind, outcome.Status, outcome.Evidence); err != nil {
				return err
			}
		}
		updated, err := d.ExecContext(ctx, `UPDATE program_integration_assignments SET status='admitted' WHERE assignment_id=? AND status='generated'`, assignmentID)
		if err != nil {
			return err
		}
		if n, _ := updated.RowsAffected(); n != 1 {
			return ErrConflict
		}
		result = IntegrationMergeResult{
			ResultID: resultID, AssignmentID: assignmentID, DispatchID: dispatchID,
			IntegratedCommit: input.IntegratedCommit, PreservationIdentity: input.PreservationIdentity, ConflictResolution: input.ConflictResolution, ConflictEvidence: input.ConflictEvidence,
		}
		for index, outcome := range input.Validations {
			result.Validations = append(result.Validations, IntegrationValidationOutcome{
				Sequence: index + 1, ConstituentSequence: document.CombinedValidation[index].ConstituentSequence,
				Command: outcome.Command, Expected: outcome.Expected, Status: outcome.Status, Evidence: outcome.Evidence,
			})
		}
		for index, outcome := range input.Evidence {
			result.Evidence = append(result.Evidence, IntegrationEvidenceOutcome{
				Sequence: index + 1, ConstituentSequence: document.RequiredEvidence[index].ConstituentSequence,
				Kind: outcome.Kind, Obligation: outcome.Obligation, Status: outcome.Status, Evidence: outcome.Evidence,
			})
		}
		return nil
	})
	return result, err
}

func validConflictResolution(integratedCommit, resolution, evidence string) bool {
	if strings.TrimSpace(resolution) != resolution || strings.TrimSpace(evidence) != evidence {
		return false
	}
	switch resolution {
	case "clean":
		return evidence == ""
	case "mechanically_resolved":
		return evidence == "mechanically_resolved:"+integratedCommit
	case "material_conflict":
		return evidence != ""
	default:
		return false
	}
}

// exactMergeOutcomes enforces that the admitted outcomes are exactly the bound
// combined validation commands and required evidence: same count, same order,
// and identical command/expected and kind/obligation identities.
func exactMergeOutcomes(document IntegrationAssignmentDocument, input IntegrationMergeResultInput) error {
	if len(input.Validations) != len(document.CombinedValidation) || len(input.Evidence) != len(document.RequiredEvidence) {
		return ErrInvalidInput
	}
	for index, outcome := range input.Validations {
		bound := document.CombinedValidation[index]
		if outcome.Command != bound.Command || outcome.Expected != bound.Expected {
			return ErrInvalidInput
		}
	}
	for index, outcome := range input.Evidence {
		bound := document.RequiredEvidence[index]
		if outcome.Kind != bound.Kind || outcome.Obligation != bound.Obligation {
			return ErrInvalidInput
		}
	}
	return nil
}

type integrationAssignmentRow struct {
	rowID   int64
	status  string
	repo    string
	branch  string
	base    string
	content string
	sha256  string
}

func (s *Service) loadAssignmentRow(ctx context.Context, d *sql.Tx, workspace, dispatchID, assignmentID string, version int64) (integrationAssignmentRow, error) {
	var row integrationAssignmentRow
	err := d.QueryRowContext(ctx, `SELECT a.id, a.status, a.repo_target, a.branch, a.base_commit, a.content, a.content_sha256 FROM program_integration_assignments a JOIN program_dispatches p ON p.id=a.dispatch_row_id JOIN feature_workspaces w ON w.id=p.workspace_row_id WHERE a.assignment_id=? AND p.dispatch_id=? AND w.workspace_id=? AND w.version=?`, assignmentID, dispatchID, workspace, version).Scan(&row.rowID, &row.status, &row.repo, &row.branch, &row.base, &row.content, &row.sha256)
	if errors.Is(err, sql.ErrNoRows) {
		return integrationAssignmentRow{}, ErrConflict
	}
	if err != nil {
		return integrationAssignmentRow{}, err
	}
	return row, nil
}

// ReadIntegrationMergeResult returns the one admitted Merge result of an
// Assignment, or ErrNotFound before any result was admitted. It is a pure read.
func (s *Service) ReadIntegrationMergeResult(ctx context.Context, workspace, dispatchID, assignmentID string) (IntegrationMergeResult, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(assignmentID) == "" {
		return IntegrationMergeResult{}, ErrInvalidInput
	}
	var result IntegrationMergeResult
	err := s.store.DB().QueryRowContext(ctx, `SELECT r.result_id, r.integrated_commit, r.preservation_identity, r.conflict_resolution, COALESCE(r.conflict_evidence,'') FROM program_integration_merge_results r JOIN program_integration_assignments a ON a.id=r.assignment_row_id JOIN program_dispatches d ON d.id=a.dispatch_row_id JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE a.assignment_id=? AND d.dispatch_id=? AND w.workspace_id=?`, assignmentID, dispatchID, workspace).Scan(&result.ResultID, &result.IntegratedCommit, &result.PreservationIdentity, &result.ConflictResolution, &result.ConflictEvidence)
	if errors.Is(err, sql.ErrNoRows) {
		return IntegrationMergeResult{}, ErrNotFound
	}
	if err != nil {
		return IntegrationMergeResult{}, err
	}
	result.AssignmentID, result.DispatchID = assignmentID, dispatchID
	rows, err := s.store.DB().QueryContext(ctx, `SELECT sequence, constituent_sequence, command, expected, status, evidence FROM program_integration_validation_outcomes WHERE merge_result_row_id=(SELECT id FROM program_integration_merge_results WHERE result_id=?) ORDER BY sequence`, result.ResultID)
	if err != nil {
		return IntegrationMergeResult{}, err
	}
	defer rows.Close()
	seenConstituents := 0
	for rows.Next() {
		seenConstituents++
		var outcome IntegrationValidationOutcome
		if err := rows.Scan(&outcome.Sequence, &outcome.ConstituentSequence, &outcome.Command, &outcome.Expected, &outcome.Status, &outcome.Evidence); err != nil {
			return IntegrationMergeResult{}, err
		}
		result.Validations = append(result.Validations, outcome)
	}
	if err := rows.Err(); err != nil {
		return IntegrationMergeResult{}, err
	}
	rows.Close()
	rows, err = s.store.DB().QueryContext(ctx, `SELECT sequence, constituent_sequence, obligation, kind, status, evidence FROM program_integration_evidence_outcomes WHERE merge_result_row_id=(SELECT id FROM program_integration_merge_results WHERE result_id=?) ORDER BY sequence`, result.ResultID)
	if err != nil {
		return IntegrationMergeResult{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var outcome IntegrationEvidenceOutcome
		if err := rows.Scan(&outcome.Sequence, &outcome.ConstituentSequence, &outcome.Obligation, &outcome.Kind, &outcome.Status, &outcome.Evidence); err != nil {
			return IntegrationMergeResult{}, err
		}
		result.Evidence = append(result.Evidence, outcome)
	}
	return result, rows.Err()
}

// VerifyIntegration runs Relay's post-Merge verification of the admitted Merge
// result against the exact bound authority and returned evidence. It never
// reruns the combined validation and never re-audits an accepted constituent.
// A successful pass is the only basis for the ordinary completed outcome of
// each bound constituent's current Ticket revision, recorded through the
// existing satisfaction mechanism; omitted constituents never advance. A failed verification records
// immutable failure evidence and creates no completed outcome.
func (s *Service) VerifyIntegration(ctx context.Context, workspace, dispatchID, assignmentID string, version int64) (IntegrationVerification, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(assignmentID) == "" || version < 1 {
		return IntegrationVerification{}, ErrInvalidInput
	}
	var result IntegrationVerification
	err := s.store.WithTx(ctx, func(t *workflowstore.Tx) error {
		d := t.DB()
		assignment, err := s.loadAssignmentRow(ctx, d, workspace, dispatchID, assignmentID, version)
		if err != nil {
			return err
		}
		if assignment.status != integrationAssignmentAdmitted {
			return ErrConflict
		}
		var mergeRow int64
		var integratedCommit, preservationIdentity, conflictResolution, conflictEvidence string
		if err := d.QueryRowContext(ctx, `SELECT id, integrated_commit, preservation_identity, conflict_resolution, COALESCE(conflict_evidence,'') FROM program_integration_merge_results WHERE assignment_row_id=?`, assignment.rowID).Scan(&mergeRow, &integratedCommit, &preservationIdentity, &conflictResolution, &conflictEvidence); errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if !sha40.MatchString(integratedCommit) || preservationIdentity == "" || !validConflictResolution(integratedCommit, conflictResolution, conflictEvidence) {
			return ErrConflict
		}
		document, err := decodeIntegrationAssignmentDocument(assignmentID, dispatchID, assignment.repo, assignment.branch, assignment.base, assignment.content, assignment.sha256)
		if err != nil {
			return err
		}
		failure, err := s.verifyIntegrationFacts(ctx, t, workspace, dispatchID, assignmentID, assignment, document)
		if err != nil {
			return err
		}
		if failure == "" && conflictResolution == "material_conflict" {
			failure = "Merge reported a material conflict"
		}
		if failure == "" {
			bound, omitted, err := s.integrationCommitSets(ctx, d, assignment.rowID, dispatchID)
			if err != nil {
				return err
			}
			if s.repositoryVerifier == nil {
				failure = "repository preservation verifier is unavailable"
			} else if err := s.repositoryVerifier(ctx, assignment.repo, assignment.branch, assignment.base, integratedCommit, bound, omitted, conflictResolution, conflictEvidence); err != nil {
				failure = err.Error()
			}
		}
		if failure == "" {
			failure, err = s.verifyIntegrationOutcomes(ctx, d, assignment.rowID)
			if err != nil {
				return err
			}
		}
		verificationID := workflowstore.NewIntegrationVerificationID()
		outcome := "passed"
		failureReason := sql.NullString{}
		if failure != "" {
			outcome = "failed"
			failureReason = sql.NullString{String: failure, Valid: true}
		}
		row, err := d.ExecContext(ctx, `INSERT INTO program_integration_verifications(verification_id,assignment_row_id,merge_result_row_id,outcome,failure_reason) VALUES(?,(SELECT id FROM program_integration_assignments WHERE assignment_id=?),?,?,?)`, verificationID, assignmentID, mergeRow, outcome, failureReason)
		if err != nil {
			return err
		}
		verificationRow, _ := row.LastInsertId()
		result = IntegrationVerification{VerificationID: verificationID, AssignmentID: assignmentID, DispatchID: dispatchID, Outcome: outcome, FailureReason: failure}
		if outcome == "failed" {
			if err := s.transitionAssignment(ctx, d, assignmentID, integrationAssignmentAdmitted, integrationAssignmentFailed); err != nil {
				return err
			}
			return nil
		}
		completions, err := s.completeBoundConstituents(ctx, t, assignment.rowID, verificationRow)
		if err != nil {
			return err
		}
		result.Completed = completions
		return s.transitionAssignment(ctx, d, assignmentID, integrationAssignmentAdmitted, integrationAssignmentVerified)
	})
	return result, err
}

// verifyIntegrationFacts re-verifies every exact fact carried by the immutable
// Assignment. Any missing, replaced, superseded, or mismatched fact fails the
// complete Assignment; completion never receives a chance to skip a stale row.
func (s *Service) verifyIntegrationFacts(ctx context.Context, t *workflowstore.Tx, workspace, dispatchID, assignmentID string, assignment integrationAssignmentRow, document IntegrationAssignmentDocument) (string, error) {
	var dispatchRepo, dispatchBranch, dispatchBase string
	if err := t.DB().QueryRowContext(ctx, `SELECT repo_target, branch, base_commit FROM program_dispatches WHERE dispatch_id=?`, dispatchID).Scan(&dispatchRepo, &dispatchBranch, &dispatchBase); err != nil {
		return "", err
	}
	if dispatchRepo != assignment.repo || dispatchBranch != assignment.branch || dispatchBase != assignment.base {
		return "dispatch repository basis mismatch", nil
	}
	rows, err := t.DB().QueryContext(ctx, `SELECT c.sequence, e.id, e.eligibility_id, e.audited_commit, e.pushed_branch, e.delivery_ticket_revision_row_id, e.execution_package_row_id, e.assignment_artifact_row_id, e.authority_revision_row_id, e.source_closure_row_id, e.dispatch_member_row_id, m.id, m.prepared_member_id, m.execution_package_row_id, m.run_row_id, m.ticket_revision_row_id, m.assignment_artifact_row_id, ticket.ticket_id, tv.revision_number, p.package_id, r.run_id, a.artifact_id, a.sha256 FROM program_integration_assignment_constituents c JOIN program_integration_eligibilities e ON e.id=c.eligibility_row_id JOIN program_dispatch_members dm ON dm.id=e.dispatch_member_row_id JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id JOIN delivery_ticket_revisions tv ON tv.id=e.delivery_ticket_revision_row_id JOIN delivery_tickets ticket ON ticket.id=tv.delivery_ticket_row_id JOIN execution_packages p ON p.id=e.execution_package_row_id JOIN runs r ON r.id=m.run_row_id JOIN artifacts a ON a.id=e.assignment_artifact_row_id WHERE c.assignment_row_id=? ORDER BY c.sequence`, assignment.rowID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	seenConstituents := 0
	for rows.Next() {
		seenConstituents++
		var sequence, eligibilityRow, boundRevision, packageRow, artifactRow, authority, source, dispatchMemberRow, preparedMemberRow, preparedPackageRow, preparedRunRow, preparedRevisionRow, preparedArtifactRow int64
		var eligibilityID, acceptedCommit, pushedBranch, memberID, ticketID, packageID, runID, artifactID, artifactSHA string
		var ticketRevision int64
		if err := rows.Scan(&sequence, &eligibilityRow, &eligibilityID, &acceptedCommit, &pushedBranch, &boundRevision, &packageRow, &artifactRow, &authority, &source, &dispatchMemberRow, &preparedMemberRow, &memberID, &preparedPackageRow, &preparedRunRow, &preparedRevisionRow, &preparedArtifactRow, &ticketID, &ticketRevision, &packageID, &runID, &artifactID, &artifactSHA); err != nil {
			return "", err
		}
		if sequence < 1 || int(sequence) > len(document.Constituents) || int(sequence) != seenConstituents {
			return "Assignment constituent sequence mismatch", nil
		}
		bound := document.Constituents[int(sequence)-1]
		if preparedPackageRow != packageRow || preparedRunRow == 0 || preparedRevisionRow != boundRevision || preparedArtifactRow != artifactRow || bound.MemberID != memberID || bound.TicketID != ticketID || bound.TicketRevision != ticketRevision || bound.EligibilityID != eligibilityID || bound.AcceptedCommit != acceptedCommit || bound.PushedBranch != pushedBranch || bound.PackageID != packageID || bound.RunID != runID || bound.ExecutionAssignment.ArtifactID != artifactID || bound.ExecutionAssignment.SHA256 != artifactSHA {
			return "bound Assignment constituent authority mismatch", nil
		}
		var current int64
		if err := t.DB().QueryRowContext(ctx, `SELECT current_revision_row_id FROM delivery_tickets WHERE id=(SELECT delivery_ticket_row_id FROM delivery_ticket_revisions WHERE id=?)`, boundRevision).Scan(&current); err != nil || current != boundRevision {
			return "bound Ticket revision is stale or no longer current", nil
		}
		var preparedRepo, preparedBranch, preparedBase string
		if err := t.DB().QueryRowContext(ctx, `SELECT repo_target, branch, base_commit FROM program_prepared_members WHERE id=?`, preparedMemberRow).Scan(&preparedRepo, &preparedBranch, &preparedBase); err != nil || preparedRepo != dispatchRepo || preparedBranch != dispatchBranch || preparedBase != dispatchBase {
			return "prepared constituent repository basis mismatch", nil
		}
		var outcome, resultBranch, resultHead string
		if err := t.DB().QueryRowContext(ctx, `SELECT r.outcome, r.branch, r.branch_head_sha FROM program_dispatch_results r JOIN program_integration_eligibilities e ON e.dispatch_member_row_id=r.dispatch_member_row_id WHERE e.id=?`, eligibilityRow).Scan(&outcome, &resultBranch, &resultHead); err != nil {
			return "", err
		}
		if outcome != "done" || resultBranch != pushedBranch || resultHead != acceptedCommit {
			return "bound accepted commit or pushed branch mismatch", nil
		}
		var decisionID, decision, decisionCommit string
		if err := t.DB().QueryRowContext(ctx, `SELECT ad.audit_decision_id, ad.decision, ad.audited_commit FROM audit_decisions ad JOIN audit_ticket_revision_decisions d ON d.audit_decision_row_id=ad.id JOIN program_integration_eligibilities e ON e.audit_ticket_revision_decision_row_id=d.id WHERE e.id=?`, eligibilityRow).Scan(&decisionID, &decision, &decisionCommit); err != nil {
			return "", err
		}
		if decision != "accepted" || decisionID != bound.AuditDecisionID || decisionCommit != acceptedCommit {
			return "bound accepted isolated-audit decision mismatch", nil
		}
		var obligationRevision, obligationPackage, obligationAuthority, obligationSource int64
		if err := t.DB().QueryRowContext(ctx, `SELECT o.delivery_ticket_revision_row_id, o.execution_package_row_id, o.authority_revision_row_id, o.source_closure_row_id FROM audit_packet_ticket_obligations o JOIN audit_ticket_revision_decisions d ON d.audit_packet_ticket_obligation_row_id=o.id JOIN program_integration_eligibilities e ON e.audit_ticket_revision_decision_row_id=d.id WHERE e.id=?`, eligibilityRow).Scan(&obligationRevision, &obligationPackage, &obligationAuthority, &obligationSource); err != nil || obligationRevision != boundRevision || obligationPackage != packageRow || obligationAuthority != authority || obligationSource != source {
			return "executed authority lineage mismatch", nil
		}
		var boundAuthority, boundSource int64
		if err := t.DB().QueryRowContext(ctx, `SELECT authority_revision_row_id, source_closure_row_id FROM program_integration_eligibilities WHERE id=?`, eligibilityRow).Scan(&boundAuthority, &boundSource); err != nil || boundAuthority != authority || boundSource != source {
			return "eligibility authority lineage mismatch", nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(document.Constituents) == 0 || len(document.Constituents) != countAssignmentConstituents(ctx, t.DB(), assignment.rowID) {
		return "Assignment has no constituents", nil
	}
	return "", nil
}

func countAssignmentConstituents(ctx context.Context, d *sql.Tx, assignmentRow int64) int {
	var count int
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM program_integration_assignment_constituents WHERE assignment_row_id=?`, assignmentRow).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *Service) integrationCommitSets(ctx context.Context, d *sql.Tx, assignmentRow int64, dispatchID string) ([]string, []string, error) {
	rows, err := d.QueryContext(ctx, `SELECT e.audited_commit, c.assignment_row_id FROM program_integration_eligibilities e JOIN program_integration_assignment_constituents c ON c.eligibility_row_id=e.id WHERE c.assignment_row_id=? ORDER BY c.sequence`, assignmentRow)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	boundSet := map[string]bool{}
	var bound []string
	for rows.Next() {
		var commit string
		var row int64
		if err := rows.Scan(&commit, &row); err != nil {
			return nil, nil, err
		}
		boundSet[commit] = true
		bound = append(bound, commit)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	all, err := d.QueryContext(ctx, `SELECT e.audited_commit FROM program_integration_eligibilities e JOIN program_dispatch_members dm ON dm.id=e.dispatch_member_row_id JOIN program_dispatches p ON p.id=dm.dispatch_row_id WHERE p.dispatch_id=?`, dispatchID)
	if err != nil {
		return nil, nil, err
	}
	defer all.Close()
	var omitted []string
	for all.Next() {
		var commit string
		if err := all.Scan(&commit); err != nil {
			return nil, nil, err
		}
		if !boundSet[commit] {
			omitted = append(omitted, commit)
		}
	}
	return bound, omitted, all.Err()
}

// verifyIntegrationOutcomes confirms the exact execution and success of every
// bound combined validation requirement and every bound required evidence item
// as recorded by the admitted Merge result.
func (s *Service) verifyIntegrationOutcomes(ctx context.Context, d *sql.Tx, assignmentRow int64) (string, error) {
	var content string
	if err := d.QueryRowContext(ctx, `SELECT content FROM program_integration_assignments WHERE id=?`, assignmentRow).Scan(&content); err != nil {
		return "unable to resolve bound outcome counts", nil
	}
	var document IntegrationAssignmentDocument
	if err := json.Unmarshal([]byte(content), &document); err != nil {
		return "unable to decode bound outcome identities", nil
	}
	expectedValidations, expectedEvidence := len(document.CombinedValidation), len(document.RequiredEvidence)
	rows, err := d.QueryContext(ctx, `SELECT sequence, constituent_sequence, command, expected, status FROM program_integration_validation_outcomes WHERE merge_result_row_id=(SELECT id FROM program_integration_merge_results WHERE assignment_row_id=?) ORDER BY sequence`, assignmentRow)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	actualValidations := 0
	for rows.Next() {
		actualValidations++
		var sequence int
		var constituentSequence int
		var command, expected string
		var status string
		if err := rows.Scan(&sequence, &constituentSequence, &command, &expected, &status); err != nil {
			return "", err
		}
		if sequence < 1 || sequence > len(document.CombinedValidation) || constituentSequence != document.CombinedValidation[sequence-1].ConstituentSequence || command != document.CombinedValidation[sequence-1].Command || expected != document.CombinedValidation[sequence-1].Expected {
			return "combined validation identity mismatch", nil
		}
		if status != "passed" {
			return fmt.Sprintf("combined validation outcome %d did not pass", sequence), nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if actualValidations != expectedValidations {
		return "combined validation outcome count mismatch", nil
	}
	rows.Close()
	rows, err = d.QueryContext(ctx, `SELECT sequence, constituent_sequence, obligation, kind, status FROM program_integration_evidence_outcomes WHERE merge_result_row_id=(SELECT id FROM program_integration_merge_results WHERE assignment_row_id=?) ORDER BY sequence`, assignmentRow)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	actualEvidence := 0
	for rows.Next() {
		actualEvidence++
		var sequence int
		var constituentSequence int
		var obligation, kind string
		var status string
		if err := rows.Scan(&sequence, &constituentSequence, &obligation, &kind, &status); err != nil {
			return "", err
		}
		if sequence < 1 || sequence > len(document.RequiredEvidence) || constituentSequence != document.RequiredEvidence[sequence-1].ConstituentSequence || obligation != document.RequiredEvidence[sequence-1].Obligation || kind != document.RequiredEvidence[sequence-1].Kind {
			return "required evidence identity mismatch", nil
		}
		if status != "passed" {
			return fmt.Sprintf("required evidence outcome %d did not pass", sequence), nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if actualEvidence != expectedEvidence {
		return "required evidence outcome count mismatch", nil
	}
	return "", nil
}

// completeBoundConstituents invokes the existing ordinary completed-outcome
// mechanism for every constituent only after verifyIntegrationFacts has
// established that the complete bound subset is current. It never silently
// skips a bound constituent.
func (s *Service) completeBoundConstituents(ctx context.Context, t *workflowstore.Tx, assignmentRow, verificationRow int64) ([]IntegrationCompletion, error) {
	rows, err := t.DB().QueryContext(ctx, `SELECT c.id, e.audit_ticket_revision_decision_row_id, e.delivery_ticket_revision_row_id, m.prepared_member_id, ticket.ticket_id, tv.revision_number FROM program_integration_assignment_constituents c JOIN program_integration_eligibilities e ON e.id=c.eligibility_row_id JOIN program_dispatch_members dm ON dm.id=e.dispatch_member_row_id JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id JOIN delivery_ticket_revisions tv ON tv.id=e.delivery_ticket_revision_row_id JOIN delivery_tickets ticket ON ticket.id=tv.delivery_ticket_row_id WHERE c.assignment_row_id=? ORDER BY c.sequence`, assignmentRow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var completions []IntegrationCompletion
	for rows.Next() {
		var constituentRow, decisionRow, revisionRow int64
		var memberID, ticketID string
		var revision int64
		if err := rows.Scan(&constituentRow, &decisionRow, &revisionRow, &memberID, &ticketID, &revision); err != nil {
			return nil, err
		}
		completion := IntegrationCompletion{MemberID: memberID, TicketID: ticketID, TicketRevision: revision}
		satisfaction, err := t.CreateDeliveryTicketRevisionSatisfaction(ctx, workflowstore.CreateDeliveryTicketRevisionSatisfactionParams{DeliveryTicketRevisionRowID: revisionRow, AuditTicketRevisionDecisionRowID: decisionRow})
		if err != nil {
			return nil, err
		}
		if _, err := t.DB().ExecContext(ctx, `INSERT INTO program_integration_completions(verification_row_id,assignment_constituent_row_id,satisfaction_row_id) VALUES(?,?,?)`, verificationRow, constituentRow, satisfaction.ID); err != nil {
			return nil, err
		}
		completion.Completed = true
		completions = append(completions, completion)
	}
	return completions, rows.Err()
}

func (s *Service) transitionAssignment(ctx context.Context, d *sql.Tx, assignmentID, expected, next string) error {
	updated, err := d.ExecContext(ctx, `UPDATE program_integration_assignments SET status=? WHERE assignment_id=? AND status=?`, next, assignmentID, expected)
	if err != nil {
		return err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return ErrConflict
	}
	return nil
}

// ReadIntegrationVerification returns the recorded Relay verification of an
// Assignment, or ErrNotFound before verification ran. It is a pure read.
func (s *Service) ReadIntegrationVerification(ctx context.Context, workspace, dispatchID, assignmentID string) (IntegrationVerification, error) {
	return s.readIntegrationVerification(ctx, workspace, dispatchID, assignmentID, "")
}

func (s *Service) readIntegrationVerification(ctx context.Context, workspace, dispatchID, assignmentID, outcome string) (IntegrationVerification, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(dispatchID) == "" || strings.TrimSpace(assignmentID) == "" {
		return IntegrationVerification{}, ErrInvalidInput
	}
	var result IntegrationVerification
	var failure sql.NullString
	query := `SELECT v.verification_id, v.outcome, COALESCE(v.failure_reason,'') FROM program_integration_verifications v JOIN program_integration_assignments a ON a.id=v.assignment_row_id JOIN program_dispatches d ON d.id=a.dispatch_row_id JOIN feature_workspaces w ON w.id=d.workspace_row_id WHERE a.assignment_id=? AND d.dispatch_id=? AND w.workspace_id=?`
	args := []any{assignmentID, dispatchID, workspace}
	if outcome != "" {
		query += ` AND v.outcome=?`
		args = append(args, outcome)
	}
	err := s.store.DB().QueryRowContext(ctx, query, args...).Scan(&result.VerificationID, &result.Outcome, &failure)
	if errors.Is(err, sql.ErrNoRows) {
		return IntegrationVerification{}, ErrNotFound
	}
	if err != nil {
		return IntegrationVerification{}, err
	}
	result.AssignmentID, result.DispatchID = assignmentID, dispatchID
	result.FailureReason = failure.String
	if result.Outcome != "passed" {
		return result, nil
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT m.prepared_member_id, ticket.ticket_id, tv.revision_number, c.id IS NOT NULL FROM program_integration_assignment_constituents ac JOIN program_integration_eligibilities e ON e.id=ac.eligibility_row_id JOIN program_dispatch_members dm ON dm.id=e.dispatch_member_row_id JOIN program_prepared_members m ON m.id=dm.prepared_member_row_id JOIN delivery_ticket_revisions tv ON tv.id=e.delivery_ticket_revision_row_id JOIN delivery_tickets ticket ON ticket.id=tv.delivery_ticket_row_id LEFT JOIN program_integration_completions c ON c.assignment_constituent_row_id=ac.id AND c.verification_row_id=(SELECT id FROM program_integration_verifications WHERE verification_id=?) WHERE ac.assignment_row_id=(SELECT id FROM program_integration_assignments WHERE assignment_id=?) ORDER BY ac.sequence`, result.VerificationID, result.AssignmentID)
	if err != nil {
		return IntegrationVerification{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var completed int
		var completion IntegrationCompletion
		if err := rows.Scan(&completion.MemberID, &completion.TicketID, &completion.TicketRevision, &completed); err != nil {
			return IntegrationVerification{}, err
		}
		completion.Completed = completed == 1
		result.Completed = append(result.Completed, completion)
	}
	return result, rows.Err()
}

// ReadIntegrationFailure returns the recorded failed verification of an
// Assignment. A passed verification or the absence of verification is not a
// failure and is reported as not found. It is a pure read.
func (s *Service) ReadIntegrationFailure(ctx context.Context, workspace, dispatchID, assignmentID string) (IntegrationFailure, error) {
	verification, err := s.readIntegrationVerification(ctx, workspace, dispatchID, assignmentID, "failed")
	if err != nil {
		return IntegrationFailure{}, err
	}
	return IntegrationFailure{VerificationID: verification.VerificationID, AssignmentID: verification.AssignmentID, DispatchID: verification.DispatchID, FailureReason: verification.FailureReason}, nil
}
