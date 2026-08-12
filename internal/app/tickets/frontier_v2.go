package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

// The frontier is a derived read projection of the current definition: the
// current approved Delivery Plan when present, the current approved Delivery
// Ticket revision for each active ticket, and the recorded route facts for
// those revisions. It creates no artifact family, schema, lifecycle record, or
// roadmap authority and mutates nothing.

// Frontier state vocabulary. These are the exact v2.0 states.
const (
	FrontierStatePlanned     = "planned"
	FrontierStateAuthored    = "authored"
	FrontierStateBlocked     = "blocked"
	FrontierStateEligible    = "eligible"
	FrontierStateSelected    = "selected"
	FrontierStatePrepared    = "prepared"
	FrontierStateExecuting   = "executing"
	FrontierStateAudit       = "audit"
	FrontierStateRemediation = "remediation"
	FrontierStateCompleted   = "completed"
)

// Pre-execution block reasons. block_reason is non-null only for blocked
// entries and is the first applicable cause in this order.
const (
	frontierBlockDependencyUnmet           = "dependency_unmet"
	frontierBlockGoverningAuthorityMissing = "governing_authority_missing"
	frontierBlockTransitionPlanMissing     = "transition_plan_missing"
)

var (
	ErrInvalidFrontierRequest    = errors.New("invalid ticket frontier request")
	ErrFrontierWorkspaceNotFound = errors.New("feature workspace was not found")
)

// The canonical ticket identity patterns mirror the canonical filename
// conventions owned by the compiler: feature_slug is lowercase kebab-case and
// a unit/ticket identity is uppercase hyphenated.
var (
	frontierFeatureSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	frontierTicketIDPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`)
)

// ValidFrontierFeatureSlug reports whether value is a valid lowercase
// kebab-case feature_slug request identity.
func ValidFrontierFeatureSlug(value string) bool {
	return frontierFeatureSlugPattern.MatchString(value)
}

// ValidFrontierUnitTicketID reports whether value is a valid uppercase
// hyphenated unit/ticket identity for requested_unit_id.
func ValidFrontierUnitTicketID(value string) bool { return frontierTicketIDPattern.MatchString(value) }

// FrontierV2Input is the semantic frontier read request. ProjectID is the
// Relay project context that scopes feature_slug; RequestedUnitID is empty
// when no filter is supplied.
type FrontierV2Input struct {
	ProjectID       string
	FeatureSlug     string
	RequestedUnitID string
}

// FrontierV2 is the exact top-level v2 response. Field order is canonical JSON
// order.
type FrontierV2 struct {
	FeatureSlug       string            `json:"feature_slug"`
	RequestedUnitID   *string           `json:"requested_unit_id"`
	CurrentPlan       *FrontierV2Plan   `json:"current_plan"`
	Entries           []FrontierV2Entry `json:"entries"`
	ProgramCandidates []string          `json:"program_candidates"`
}

// FrontierV2Plan is the current approved Delivery Plan identity.
type FrontierV2Plan struct {
	SHA256 string `json:"sha256"`
}

// FrontierV2Entry is one planned unit, one active authored Ticket, or one
// planned unit realized by an authored Ticket. Field order is canonical JSON
// order; empty strings marshal as null through the pointer fields.
type FrontierV2Entry struct {
	UnitID            *string  `json:"unit_id"`
	TicketID          *string  `json:"ticket_id"`
	Revision          *int64   `json:"revision"`
	SHA256            *string  `json:"sha256"`
	State             string   `json:"state"`
	BlockReason       *string  `json:"block_reason"`
	DependsOn         []string `json:"depends_on"`
	UnmetDependencies []string `json:"unmet_dependencies"`
	DownstreamUnits   []string `json:"downstream_units"`
}

// FrontierPlanUnit is one planned unit of the current approved Delivery Plan
// with its planned semantic dependency topology in source order.
type FrontierPlanUnit struct {
	UnitID    string
	DependsOn []string
}

// frontierPlan is the current approved Delivery Plan surface consumed by the
// frontier: the artifact SHA-256 of its exact bytes and its planned units.
type frontierPlan struct {
	SHA256 string
	Units  []FrontierPlanUnit
}

// ReadFrontier projects the exact workspace Ticket Frontier v2 response for
// one Feature Workspace scoped to the supplied Relay project context. It is a
// pure read: no mutation, no lifecycle, no roadmap authority.
func (s *Service) ReadFrontier(ctx context.Context, input FrontierV2Input) (FrontierV2, error) {
	if s == nil || s.store == nil {
		return FrontierV2{}, ErrInvalidFrontierRequest
	}
	if strings.TrimSpace(input.ProjectID) != input.ProjectID || input.ProjectID == "" ||
		!ValidFrontierFeatureSlug(input.FeatureSlug) {
		return FrontierV2{}, ErrInvalidFrontierRequest
	}
	if input.RequestedUnitID != "" && !ValidFrontierUnitTicketID(input.RequestedUnitID) {
		return FrontierV2{}, ErrInvalidFrontierRequest
	}
	workspace, err := s.frontierWorkspace(ctx, input.ProjectID, input.FeatureSlug)
	if err != nil {
		return FrontierV2{}, err
	}
	plan, err := s.readFrontierPlan(ctx, workspace)
	if err != nil {
		return FrontierV2{}, err
	}
	entries, err := s.frontierEntries(ctx, workspace, plan)
	if err != nil {
		return FrontierV2{}, err
	}
	response := FrontierV2{FeatureSlug: input.FeatureSlug, Entries: entries, ProgramCandidates: []string{}}
	if input.RequestedUnitID != "" {
		requested := input.RequestedUnitID
		response.RequestedUnitID = &requested
	}
	if plan != nil {
		response.CurrentPlan = &FrontierV2Plan{SHA256: plan.SHA256}
	}
	// program_candidates is a whole-workspace fact computed from the complete
	// unfiltered projection in full workspace entry order; a filter narrows
	// entries only and never alters candidate membership or ordering.
	for _, entry := range entries {
		if entry.State == FrontierStateEligible {
			response.ProgramCandidates = append(response.ProgramCandidates, frontierEntryIdentity(entry))
		}
	}
	if input.RequestedUnitID != "" {
		filtered := make([]FrontierV2Entry, 0)
		for _, entry := range entries {
			if frontierEntryMatches(entry, input.RequestedUnitID) {
				filtered = append(filtered, entry)
			}
		}
		response.Entries = filtered
	}
	return response, nil
}

func (s *Service) frontierWorkspace(ctx context.Context, projectID, featureSlug string) (workflowstore.FeatureWorkspace, error) {
	project, err := s.store.GetProjectByProjectID(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.FeatureWorkspace{}, ErrFrontierWorkspaceNotFound
	}
	if err != nil {
		return workflowstore.FeatureWorkspace{}, err
	}
	workspaces, err := s.store.ListFeatureWorkspacesByProject(ctx, project.ID, 0)
	if err != nil {
		return workflowstore.FeatureWorkspace{}, err
	}
	for _, workspace := range workspaces {
		if workspace.FeatureSlug == featureSlug {
			return workspace, nil
		}
	}
	return workflowstore.FeatureWorkspace{}, ErrFrontierWorkspaceNotFound
}

// readFrontierPlan resolves the current approved Delivery Plan from the
// workspace's recorded current Plan row and its planned units and planned
// semantic dependencies in Plan source order.
func (s *Service) readFrontierPlan(ctx context.Context, workspace workflowstore.FeatureWorkspace) (*frontierPlan, error) {
	if !workspace.CurrentDeliveryPlanRowID.Valid {
		return nil, nil
	}
	plan, err := s.store.GetDeliveryPlanByRowID(ctx, workspace.CurrentDeliveryPlanRowID.Int64)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if plan.ArtifactSha256 == "" {
		return nil, nil
	}
	unitRows, err := s.store.ListDeliveryPlanUnitsByPlan(ctx, plan.ID)
	if err != nil {
		return nil, err
	}
	unitIDByRow := make(map[int64]string, len(unitRows))
	for _, unit := range unitRows {
		unitIDByRow[unit.ID] = unit.UnitID
	}
	units := make([]FrontierPlanUnit, 0, len(unitRows))
	for _, unit := range unitRows {
		dependencies, err := s.store.ListDeliveryPlanUnitDependenciesByUnit(ctx, unit.ID)
		if err != nil {
			return nil, err
		}
		dependsOn := make([]string, 0, len(dependencies))
		for _, dependency := range dependencies {
			if dependencyID, ok := unitIDByRow[dependency.DependsOnUnitRowID]; ok {
				dependsOn = append(dependsOn, dependencyID)
			}
		}
		units = append(units, FrontierPlanUnit{UnitID: unit.UnitID, DependsOn: dependsOn})
	}
	return &frontierPlan{SHA256: plan.ArtifactSha256, Units: units}, nil
}

// frontierEntries assembles the complete workspace projection: current Plan
// units in Plan source order, then authored Tickets not realized by a current
// planned unit in ASCII lexicographic ticket order. Downstream unlocks are
// computed over the assembled projection.
func (s *Service) frontierEntries(ctx context.Context, workspace workflowstore.FeatureWorkspace, plan *frontierPlan) ([]FrontierV2Entry, error) {
	tickets, err := s.store.ListDeliveryTicketsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	ticketByID := make(map[string]workflowstore.DeliveryTicket, len(tickets))
	for _, ticket := range tickets {
		ticketByID[ticket.TicketID] = ticket
	}
	candidateIDs, err := s.frontierCandidateTicketIDs(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	planned := make(map[string]struct{})
	entries := make([]FrontierV2Entry, 0, len(tickets))
	if plan != nil {
		for _, unit := range plan.Units {
			planned[unit.UnitID] = struct{}{}
			ticket, realized := ticketByID[unit.UnitID]
			if realized {
				entry, active, err := s.frontierTicketEntry(ctx, workspace, ticket, candidateIDs)
				if err != nil {
					return nil, err
				}
				if !active {
					// A planned unit whose Ticket is currently cancelled
					// projects as planned: the cancelled revision is inactive.
					entry = plannedUnitEntry(unit)
				} else {
					unitID := unit.UnitID
					entry.UnitID = &unitID
				}
				entries = append(entries, entry)
				continue
			}
			if _, authored := candidateIDs[unit.UnitID]; authored {
				entries = append(entries, authoredUnitEntry(unit))
			} else {
				entries = append(entries, plannedUnitEntry(unit))
			}
		}
	}
	unrealized := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		if _, inPlan := planned[ticket.TicketID]; !inPlan {
			unrealized = append(unrealized, ticket.TicketID)
		}
	}
	sort.Strings(unrealized)
	for _, ticketID := range unrealized {
		entry, active, err := s.frontierTicketEntry(ctx, workspace, ticketByID[ticketID], candidateIDs)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		entries = append(entries, entry)
	}
	annotateFrontierDownstream(entries)
	return entries, nil
}

// frontierCandidateTicketIDs resolves the ticket identities that have an
// authored Delivery Ticket candidate from the immutable candidate filenames.
func (s *Service) frontierCandidateTicketIDs(ctx context.Context, workspaceRowID int64) (map[string]struct{}, error) {
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspaceRowID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		info, diagnostics := speccompiler.ParseFilename(candidate.Filename)
		if len(diagnostics) != 0 || info.Kind != speccompiler.ArtifactDeliveryTicket || info.TicketID == "" {
			continue
		}
		result[info.TicketID] = struct{}{}
	}
	return result, nil
}

func plannedUnitEntry(unit FrontierPlanUnit) FrontierV2Entry {
	return FrontierV2Entry{
		UnitID:            stringPointer(unit.UnitID),
		State:             FrontierStatePlanned,
		DependsOn:         frontierStringList(unit.DependsOn),
		UnmetDependencies: []string{},
		DownstreamUnits:   []string{},
	}
}

func authoredUnitEntry(unit FrontierPlanUnit) FrontierV2Entry {
	return FrontierV2Entry{
		UnitID:            stringPointer(unit.UnitID),
		State:             FrontierStateAuthored,
		DependsOn:         frontierStringList(unit.DependsOn),
		UnmetDependencies: []string{},
		DownstreamUnits:   []string{},
	}
}

// frontierTicketEntry derives the entry and route state for one active
// Delivery Ticket. The second result is false when the ticket is inactive
// (its current revision is contractually cancelled) and appears nowhere in
// the projection.
func (s *Service) frontierTicketEntry(ctx context.Context, workspace workflowstore.FeatureWorkspace, ticket workflowstore.DeliveryTicket, candidateIDs map[string]struct{}) (FrontierV2Entry, bool, error) {
	entry := FrontierV2Entry{TicketID: stringPointer(ticket.TicketID), DependsOn: []string{}, UnmetDependencies: []string{}, DownstreamUnits: []string{}}
	if !ticket.CurrentRevisionRowID.Valid {
		// A Ticket row without a current revision is an authored candidate
		// awaiting exact-byte approval or currentness recording.
		entry.State = FrontierStateAuthored
		return entry, true, nil
	}
	revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
	if err != nil {
		return entry, false, err
	}
	if revision.CancellationReason.Valid {
		return entry, false, nil
	}
	dependencies, err := s.store.ListDeliveryTicketRevisionDependencies(ctx, revision.ID)
	if err != nil {
		return entry, false, err
	}
	dependencyFacts, err := s.frontierDependencyFacts(ctx, dependencies)
	if err != nil {
		return entry, false, err
	}
	for _, fact := range dependencyFacts {
		entry.DependsOn = append(entry.DependsOn, fact.ticketID)
	}
	approvals, err := s.store.ListDeliveryTicketRevisionApprovals(ctx, revision.ID)
	if err != nil {
		return entry, false, err
	}
	if _, approved := currentDeliveryApproval(workspace, revision, approvals); !approved {
		// No current approved revision is recorded; the authored candidate
		// awaits exact-byte approval or currentness recording.
		entry.State = FrontierStateAuthored
		return entry, true, nil
	}
	revisionNumber := revision.RevisionNumber
	entry.Revision = &revisionNumber
	canonicalSHA, err := s.frontierRevisionSHA256(ctx, ticket, revision)
	if err != nil {
		return entry, false, err
	}
	entry.SHA256 = &canonicalSHA
	// A recorded completed outcome for the current revision is the terminal
	// completed state; blocks are pre-execution and no pending audit decision
	// or remediation seed can coexist with a completed outcome.
	if _, err := s.store.GetDeliveryTicketRevisionSatisfaction(ctx, revision.ID); err == nil {
		entry.State = FrontierStateCompleted
		return entry, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return entry, false, err
	}
	for _, fact := range dependencyFacts {
		if !fact.satisfied {
			entry.UnmetDependencies = append(entry.UnmetDependencies, fact.ticketID)
		}
	}
	if len(entry.UnmetDependencies) > 0 {
		return blockedFrontierEntry(entry, frontierBlockDependencyUnmet), true, nil
	}
	if !s.frontierAuthorityAvailable(ctx, workspace, revision) {
		return blockedFrontierEntry(entry, frontierBlockGoverningAuthorityMissing), true, nil
	}
	facts, err := s.frontierRouteFacts(ctx, workspace, revision)
	if err != nil {
		return entry, false, err
	}
	// transition_plan_missing is the only pre-execution block that can coexist
	// with otherwise-recorded route facts; once execution is admitted the
	// entry reports its route state.
	if revision.TransitionApplicability == "required" && facts.run == nil {
		hasPlan, err := s.frontierHasTransitionPlan(ctx, workspace, ticket, revision)
		if err != nil {
			return entry, false, err
		}
		if !hasPlan {
			return blockedFrontierEntry(entry, frontierBlockTransitionPlanMissing), true, nil
		}
	}
	if facts.run != nil {
		switch {
		case facts.run.Status == workflowstore.RunStatusCompleted:
			entry.State = FrontierStateCompleted
		case facts.run.Status == workflowstore.RunStatusNeedsRevision && facts.remediationSeed:
			entry.State = FrontierStateRemediation
		case facts.terminal:
			entry.State = FrontierStateAudit
		case facts.executionAssignment:
			entry.State = FrontierStateExecuting
		default:
			entry.State = FrontierStatePrepared
		}
		return entry, true, nil
	}
	if facts.selected {
		entry.State = FrontierStateSelected
		return entry, true, nil
	}
	entry.State = FrontierStateEligible
	return entry, true, nil
}

func blockedFrontierEntry(entry FrontierV2Entry, reason string) FrontierV2Entry {
	entry.State = FrontierStateBlocked
	entry.BlockReason = &reason
	return entry
}

// frontierDependencyFacts resolves each depends_on reference to its ticket
// identity and whether the required current Delivery Ticket revision has a
// recorded completed outcome, preserving source order.
type frontierDependency struct {
	ticketID  string
	satisfied bool
}

func (s *Service) frontierDependencyFacts(ctx context.Context, dependencies []workflowstore.DeliveryTicketRevisionDependency) ([]frontierDependency, error) {
	facts := make([]frontierDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		revision, err := s.store.GetDeliveryTicketRevisionByRowID(ctx, dependency.DependsOnRevisionRowID)
		if err != nil {
			return nil, err
		}
		ticket, err := s.store.GetDeliveryTicketByRowID(ctx, revision.DeliveryTicketRowID)
		if err != nil {
			return nil, err
		}
		satisfied := dependency.Outcome == "satisfied" && ticket.CurrentRevisionRowID.Valid && ticket.CurrentRevisionRowID.Int64 == revision.ID
		if satisfied {
			if _, err := s.store.GetDeliveryTicketRevisionSatisfaction(ctx, revision.ID); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				satisfied = false
			}
		}
		facts = append(facts, frontierDependency{ticketID: ticket.TicketID, satisfied: satisfied})
	}
	return facts, nil
}

// frontierAuthorityAvailable reports whether the applicable current governing
// authority and the recorded source basis are available for the revision,
// mirroring the selection owner's currentness derivation.
func (s *Service) frontierAuthorityAvailable(ctx context.Context, workspace workflowstore.FeatureWorkspace, revision workflowstore.DeliveryTicketRevision) bool {
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, revision.SourceClosureRowID)
	if err != nil || closure.State != workflowstore.SourceVaultClosureStateReady {
		return false
	}
	if workspace.CurrentAuthorityRevisionRowID.Valid {
		authority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil || authority.WorkspaceRowID != workspace.ID || !authority.SourceClosureRowID.Valid || authority.SourceClosureRowID.Int64 != revision.SourceClosureRowID {
			return false
		}
		return true
	}
	current, err := s.store.GetReadySourceVaultClosureByRepositoryTargetAndCommit(ctx, revision.RepoTarget, revision.BaseCommit)
	if err != nil || current.ID != revision.SourceClosureRowID {
		return false
	}
	return true
}

// frontierRouteFacts assembles the recorded selection, package, execution, and
// remediation route facts for one current approved revision.
type frontierRouteFacts struct {
	selected            bool
	run                 *workflowstore.Run
	terminal            bool
	executionAssignment bool
	remediationSeed     bool
}

func (s *Service) frontierRouteFacts(ctx context.Context, workspace workflowstore.FeatureWorkspace, revision workflowstore.DeliveryTicketRevision) (frontierRouteFacts, error) {
	var facts frontierRouteFacts
	selections, err := s.store.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return facts, err
	}
	var selectedRowID int64
	for _, selection := range selections {
		if selection.State != "active" {
			continue
		}
		members, err := s.store.ListDeliveryTicketSelectionMembers(ctx, selection.ID)
		if err != nil {
			return facts, err
		}
		for _, member := range members {
			if member.RevisionRowID == revision.ID {
				facts.selected = true
				selectedRowID = selection.ID
				break
			}
		}
		if facts.selected {
			break
		}
	}
	if !facts.selected {
		return facts, nil
	}
	packageRow, err := s.store.GetExecutionPackageBySelectionRowID(ctx, selectedRowID)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, err
	}
	if _, err := s.store.GetExecutionPackageApprovalByPackageRowID(ctx, packageRow.ID); errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	} else if err != nil {
		return facts, err
	}
	run, err := s.store.GetRunByExecutionPackageRowID(ctx, packageRow.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return facts, nil
	}
	if err != nil {
		return facts, err
	}
	facts.run = &run
	facts.terminal = frontierTerminalRun(run.Status)
	if !facts.terminal {
		artifacts, err := s.store.ListArtifactsByRun(ctx, run.ID)
		if err != nil {
			return facts, err
		}
		for _, artifact := range artifacts {
			if artifact.Kind == frontierExecutionAssignmentKind {
				facts.executionAssignment = true
				break
			}
		}
	}
	if run.Status == workflowstore.RunStatusNeedsRevision {
		seeds, err := s.store.ListAuditRemediationSeedsByWorkspace(ctx, workspace.ID)
		if err != nil {
			return facts, err
		}
		for _, seed := range seeds {
			if seed.ExecutionPackageRowID == packageRow.ID {
				facts.remediationSeed = true
				break
			}
		}
	}
	return facts, nil
}

// frontierTerminalRun reports whether the run's execution has ended, including
// failed or aborted outcomes. The entry then reports audit until a decision is
// recorded.
func frontierTerminalRun(status string) bool {
	switch status {
	case workflowstore.RunStatusValidating,
		workflowstore.RunStatusExecutionFailed,
		workflowstore.RunStatusCancelled,
		workflowstore.RunStatusValidationFailed,
		workflowstore.RunStatusAuditReady,
		workflowstore.RunStatusNeedsRevision,
		workflowstore.RunStatusCompleted:
		return true
	default:
		return false
	}
}

// frontierHasTransitionPlan reports whether the current approved authority
// carries a Transition Plan for this exact Ticket revision.
func (s *Service) frontierHasTransitionPlan(ctx context.Context, workspace workflowstore.FeatureWorkspace, ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision) (bool, error) {
	if !workspace.CurrentAuthorityRevisionRowID.Valid {
		return false, nil
	}
	authority, err := s.store.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
	if err != nil {
		return false, err
	}
	layers, err := s.store.ListFeatureWorkspaceAuthorityLayers(ctx, authority.ID)
	if err != nil {
		return false, err
	}
	want := fmt.Sprintf("%s.ticket-%s.r%d.transition-plan.json", workspace.FeatureSlug, ticket.TicketID, revision.RevisionNumber)
	for _, layer := range layers {
		if layer.LayerKind != "transition_plan" && layer.LayerKind != "plan" {
			continue
		}
		path := ""
		if layer.ArtifactRowID.Valid {
			artifact, err := s.store.GetArtifactByRowID(ctx, layer.ArtifactRowID.Int64)
			if err != nil {
				return false, err
			}
			path = artifact.RelativePath
		} else if layer.RetainedArtifactRowID.Valid {
			artifact, err := s.store.GetOperationPacketRetainedArtifactByRowID(ctx, layer.RetainedArtifactRowID.Int64)
			if err != nil {
				return false, err
			}
			path = artifact.RelativePath
		}
		if filepath.Base(path) == want {
			return true, nil
		}
	}
	return false, nil
}

// frontierRevisionSHA256 resolves the artifact SHA-256 of the current approved
// revision's exact canonical bytes, including production-linked artifacts.
func (s *Service) frontierRevisionSHA256(ctx context.Context, ticket workflowstore.DeliveryTicket, revision workflowstore.DeliveryTicketRevision) (string, error) {
	canonical, err := s.readArtifact(ticket.TicketID, revision.RevisionNumber, "delivery-ticket.json")
	if err == nil {
		return canonical.SHA256, nil
	}
	link, linkErr := s.store.GetDeliveryTicketProductionLinkByProducedRevision(ctx, revision.ID)
	if linkErr != nil {
		return "", err
	}
	artifact, artifactErr := s.store.GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx, link.CanonicalJsonArtifactRowID)
	if artifactErr != nil {
		return "", artifactErr
	}
	linked, linkedErr := s.readLinkedArtifact(artifact, ticket.WorkspaceRowID, link.CanonicalJsonSha256, link.CanonicalJsonSizeBytes, "application/json")
	if linkedErr != nil {
		return "", linkedErr
	}
	return linked.SHA256, nil
}

// annotateFrontierDownstream fills downstream_units for every entry: every
// other entry for which this entry is a direct or transitive dependency, in
// workspace entry order.
func annotateFrontierDownstream(entries []FrontierV2Entry) {
	indexByID := make(map[string]int, len(entries))
	for index := range entries {
		if identity := frontierEntryIdentity(entries[index]); identity != "" {
			indexByID[identity] = index
		}
	}
	dependents := make([][]int, len(entries))
	for index := range entries {
		for _, dependency := range entries[index].DependsOn {
			if dependent, ok := indexByID[dependency]; ok && dependent != index {
				dependents[dependent] = append(dependents[dependent], index)
			}
		}
	}
	for index := range entries {
		reachable := map[int]struct{}{}
		queue := append([]int(nil), dependents[index]...)
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if _, seen := reachable[current]; seen {
				continue
			}
			reachable[current] = struct{}{}
			queue = append(queue, dependents[current]...)
		}
		downstream := make([]string, 0)
		for candidate := range entries {
			if _, unlocked := reachable[candidate]; unlocked {
				downstream = append(downstream, frontierEntryIdentity(entries[candidate]))
			}
		}
		entries[index].DownstreamUnits = downstream
	}
}

func frontierEntryIdentity(entry FrontierV2Entry) string {
	if entry.TicketID != nil {
		return *entry.TicketID
	}
	if entry.UnitID != nil {
		return *entry.UnitID
	}
	return ""
}

func frontierEntryMatches(entry FrontierV2Entry, requested string) bool {
	return (entry.UnitID != nil && *entry.UnitID == requested) || (entry.TicketID != nil && *entry.TicketID == requested)
}

func stringPointer(value string) *string { return &value }

func frontierStringList(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

const frontierExecutionAssignmentKind = "execution_assignment"
