package refactors

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"relay/internal/artifacts"
	"relay/internal/plans"
	"relay/internal/store"
	"relay/internal/store/generated"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// PASS-004: plan promotion and refactor-only plan generation.
//
// This file adds two reviewed actions on top of the PASS-003 refactor candidate
// lifecycle:
//
//  1. Promote one ready candidate into an existing project-owned, active managed
//     plan as a normal pass_type:"refactor" pass (PromoteCandidateToPlan).
//  2. Generate reviewable refactor-only Plan of Passes JSON/Markdown artifacts
//     from selected ready candidates without submitting the plan or creating
//     runs (GenerateRefactorOnlyPlan).
//
// Neither action submits a plan, creates a run, dispatches an executor, applies
// audit decisions, registers MCP tools, or implements sidecar behavior.
// ---------------------------------------------------------------------------

// PASS-004 validation issue codes. These extend the PASS-003 codes in
// validation.go with promotion/generation-specific codes.
const (
	IssueRefactorProjectRequired           = "project_required"
	IssueRefactorProjectUnknown            = "project_unknown"
	IssueRefactorCandidateRequired         = "candidate_required"
	IssueRefactorCandidateUnknown          = "candidate_unknown"
	IssueRefactorCandidateProjectMismatch  = "candidate_project_mismatch"
	IssueRefactorCandidateNotReady         = "candidate_not_ready"
	IssueRefactorCandidateAlreadyScheduled = "candidate_already_scheduled"
	IssueRefactorPlanRequired              = "plan_required"
	IssueRefactorPlanUnknown               = "plan_unknown"
	IssueRefactorPlanProjectMismatch       = "plan_project_mismatch"
	IssueRefactorPlanStatusInvalid         = "plan_status_invalid"
	IssueRefactorPlacementInvalid          = "placement_invalid"
	IssueRefactorDependencyUnknown         = "dependency_unknown"
	IssueRefactorDependencyProjectMismatch = "dependency_project_mismatch"
	IssueRefactorDependencyNotSatisfied    = "dependency_not_satisfied"
	IssueRefactorDependencyCycle           = "dependency_cycle"
	IssueRefactorDuplicateCandidate        = "duplicate_candidate"
	IssueRefactorPlanValidationFailed      = "generated_plan_validation_failed"
	IssueRefactorArtifactWriteFailed       = "artifact_write_failed"
	IssueRefactorMissingDependency         = "missing_dependency"
	// IssueRefactorCandidateMissingContext is returned when a candidate has no
	// concrete (non-glob, repo-relative, allowed-extension) file targets that can
	// seed the generated pass context_plan.seed_files_to_read.
	IssueRefactorCandidateMissingContext = "candidate_missing_concrete_context"
)

// Placement reasons returned by the deterministic placement suggester.
const (
	PlacementExactFileOverlap   = "exact_file_overlap"
	PlacementSameDirectory      = "same_directory"
	PlacementSameSubsystem      = "same_subsystem"
	PlacementNoSuggestion       = "no_suggestion"
	PlacementManualSelection    = "manual_selection"
	PlacementAppendedNoSuggest  = "appended_no_suggestion"
	PlacementConfidenceHigh     = "high"
	PlacementConfidenceMedium   = "medium"
	PlacementConfidenceLow      = "low"
	PlacementConfidenceNone     = "none"
	schedulingModeExistingPlan  = "existing_plan_bonus_pass"
	schedulingModeGeneratedPlan = "generated_refactor_only_plan"
	refactorCandidateSource     = "refactor_backlog_candidate"
	generatedPlanRepoTarget     = "Paintersrp/relay"
	generatedPlanBranch         = "main"
	generatedPlanRepoID         = "relay"
)

const updatePlanPassSequenceSQL = `UPDATE plan_passes SET sequence = ?, updated_at = datetime('now') WHERE id = ?`

// ---------------------------------------------------------------------------
// Inputs / results
// ---------------------------------------------------------------------------

// PromoteCandidateInput is the service input for promoting a ready candidate into
// an existing project-owned managed plan as a normal refactor pass.
type PromoteCandidateInput struct {
	ProjectID             string
	CandidateID           string
	PlanID                string
	AfterPassID           string
	UseSuggestedPlacement bool
	Note                  string
}

// GenerateRefactorPlanInput is the service input for generating a reviewable
// refactor-only Plan of Passes artifact from selected ready candidates.
type GenerateRefactorPlanInput struct {
	ProjectID    string
	CandidateIDs []string
	Title        string
	Note         string
}

// PlacementSuggestion is the deterministic, advisory placement recommendation for
// a candidate within a plan.
type PlacementSuggestion struct {
	PlacementReason string   `json:"placementReason"`
	AfterPassID     string   `json:"afterPassId"`
	SequenceAfter   int64    `json:"sequenceAfter"`
	Confidence      string   `json:"confidence"`
	MatchedPassIDs  []string `json:"matchedPassIds"`
	MatchedPaths    []string `json:"matchedPaths"`
	Warnings        []string `json:"warnings"`
}

// SchedulingReference echoes the created candidate scheduling reference.
type SchedulingReference struct {
	PlanID string `json:"planId"`
	PassID string `json:"passId"`
	RunID  string `json:"runId"`
}

// PromotionPlacement summarizes how the promoted pass was placed.
type PromotionPlacement struct {
	PlacementReason string   `json:"placementReason"`
	AfterPassID     string   `json:"afterPassId"`
	Warnings        []string `json:"warnings"`
}

// PromoteCandidateResult is the successful promotion result.
type PromoteCandidateResult struct {
	CandidateID         string              `json:"candidateId"`
	PlanID              string              `json:"planId"`
	PassID              string              `json:"passId"`
	Sequence            int64               `json:"sequence"`
	CandidateStatus     string              `json:"candidateStatus"`
	SchedulingReference SchedulingReference `json:"schedulingReference"`
	Placement           PromotionPlacement  `json:"placement"`
	Warnings            []string            `json:"warnings"`
}

// GenerateRefactorPlanResult is the successful generation result.
type GenerateRefactorPlanResult struct {
	ProjectID            string   `json:"projectId"`
	PlanID               string   `json:"planId"`
	CandidateIDs         []string `json:"candidateIds"`
	JSONArtifactPath     string   `json:"jsonArtifactPath"`
	MarkdownArtifactPath string   `json:"markdownArtifactPath"`
	SubmissionPolicy     string   `json:"submissionPolicy"`
	Warnings             []string `json:"warnings"`
}

// ---------------------------------------------------------------------------
// Placement suggestion (S4)
// ---------------------------------------------------------------------------

// SuggestCandidatePlacement returns a deterministic, advisory placement
// suggestion for the candidate within the target plan. It does not mutate
// candidate, plan, pass, or artifact state.
func (s *Service) SuggestCandidatePlacement(ctx context.Context, projectID, candidateID, planID string) (*PlacementSuggestion, []ValidationIssue, error) {
	_ = ctx

	project, candidate, plan, issues, err := s.resolvePromotionTargets(projectID, candidateID, planID, false)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	passes, err := s.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return nil, nil, err
	}

	suggestion := computePlacementSuggestion(candidate, passes)
	_ = project
	return &suggestion, nil, nil
}

// computePlacementSuggestion runs the deterministic placement algorithm:
// exact_file_overlap > same_directory > same_subsystem > no_suggestion, with a
// highest-sequence then lexicographically-highest pass_id tie-break.
func computePlacementSuggestion(candidate *store.RefactorCandidate, passes []store.PlanPass) PlacementSuggestion {
	candFiles, candDirs, candSubs := candidatePlacementUnits(unmarshalStringSlice(candidate.TargetFilesJson))
	placements := buildPassPlacements(passes)

	// Determine, per pass, the best match level and the matched units.
	const (
		levelExact = 3
		levelDir   = 2
		levelSub   = 1
	)

	bestLevel := 0
	var winner *passPlacement
	var winnerMatched []string

	for i := range placements {
		p := &placements[i]
		level, matched := passMatchLevel(candFiles, candDirs, candSubs, p)
		if level == 0 {
			continue
		}
		if level > bestLevel {
			bestLevel = level
			winner = p
			winnerMatched = matched
			continue
		}
		if level == bestLevel && winner != nil {
			if p.Sequence > winner.Sequence || (p.Sequence == winner.Sequence && p.PassID > winner.PassID) {
				winner = p
				winnerMatched = matched
			}
		}
	}

	if winner == nil {
		return PlacementSuggestion{
			PlacementReason: PlacementNoSuggestion,
			Confidence:      PlacementConfidenceNone,
			MatchedPassIDs:  []string{},
			MatchedPaths:    []string{},
			Warnings:        []string{},
		}
	}

	reason := PlacementNoSuggestion
	confidence := PlacementConfidenceNone
	switch bestLevel {
	case levelExact:
		reason, confidence = PlacementExactFileOverlap, PlacementConfidenceHigh
	case levelDir:
		reason, confidence = PlacementSameDirectory, PlacementConfidenceMedium
	case levelSub:
		reason, confidence = PlacementSameSubsystem, PlacementConfidenceLow
	}

	sort.Strings(winnerMatched)
	return PlacementSuggestion{
		PlacementReason: reason,
		AfterPassID:     winner.PassID,
		SequenceAfter:   winner.Sequence,
		Confidence:      confidence,
		MatchedPassIDs:  []string{winner.PassID},
		MatchedPaths:    winnerMatched,
		Warnings:        []string{},
	}
}

type passPlacement struct {
	PassID     string
	Sequence   int64
	Files      map[string]bool
	Dirs       map[string]bool
	Subsystems map[string]bool
}

// buildPassPlacements extracts repo-relative path sets from each pass's
// raw_pass_json: context_plan.seed_files_to_read[].path and any
// intended_execution_scope entries that look like repo-relative code/doc/config
// paths.
func buildPassPlacements(passes []store.PlanPass) []passPlacement {
	out := make([]passPlacement, 0, len(passes))
	for _, pass := range passes {
		files := map[string]bool{}
		dirs := map[string]bool{}
		subs := map[string]bool{}

		var raw plans.PlanPassInput
		if strings.TrimSpace(pass.RawPassJson) != "" {
			_ = json.Unmarshal([]byte(pass.RawPassJson), &raw)
		}

		addPath := func(p string) {
			n := normalizeRepoPath(p)
			if n == "" {
				return
			}
			files[n] = true
			if d := parentDir(n); d != "" {
				dirs[d] = true
			}
			if sub := subsystemOf(n); sub != "" {
				subs[sub] = true
			}
		}

		for _, f := range raw.ContextPlan.SeedFilesToRead {
			if p := strings.TrimSpace(f.Path); p != "" {
				addPath(p)
			}
		}
		for _, scope := range raw.IntendedExecutionScope {
			if looksLikeRepoPath(scope) {
				addPath(scope)
			}
		}

		out = append(out, passPlacement{
			PassID:     pass.PassID,
			Sequence:   pass.Sequence,
			Files:      files,
			Dirs:       dirs,
			Subsystems: subs,
		})
	}
	return out
}

// candidatePlacementUnits builds the candidate's exact-file, directory, and
// subsystem match sets from its file targets. Glob targets do not contribute an
// exact file, but their non-glob directory prefix participates in directory and
// subsystem matching.
func candidatePlacementUnits(targetFiles []string) (files, dirs, subs map[string]bool) {
	files = map[string]bool{}
	dirs = map[string]bool{}
	subs = map[string]bool{}
	for _, t := range targetFiles {
		n := normalizeRepoPath(t)
		if n == "" {
			continue
		}
		if isGlobPath(n) {
			pre := nonGlobPrefix(n)
			if pre == "" {
				continue
			}
			dirs[pre] = true
			if sub := subsystemOf(pre); sub != "" {
				subs[sub] = true
			}
			continue
		}
		files[n] = true
		if d := parentDir(n); d != "" {
			dirs[d] = true
		}
		if sub := subsystemOf(n); sub != "" {
			subs[sub] = true
		}
	}
	return files, dirs, subs
}

// passMatchLevel returns the best match level (3 exact, 2 directory, 1 subsystem,
// 0 none) between candidate units and a pass, plus the overlapping units.
func passMatchLevel(candFiles, candDirs, candSubs map[string]bool, p *passPlacement) (int, []string) {
	if m := intersect(candFiles, p.Files); len(m) > 0 {
		return 3, m
	}
	if m := intersect(candDirs, p.Dirs); len(m) > 0 {
		return 2, m
	}
	if m := intersect(candSubs, p.Subsystems); len(m) > 0 {
		return 1, m
	}
	return 0, nil
}

func intersect(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if b[k] {
			out = append(out, k)
		}
	}
	return out
}

func normalizeRepoPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.TrimSuffix(p, "/")
	return p
}

func isGlobPath(p string) bool {
	return strings.ContainsAny(p, "*?[]{}")
}

func nonGlobPrefix(p string) string {
	segs := strings.Split(p, "/")
	keep := make([]string, 0, len(segs))
	for _, s := range segs {
		if strings.ContainsAny(s, "*?[]{}") {
			break
		}
		keep = append(keep, s)
	}
	return strings.Join(keep, "/")
}

func parentDir(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return ""
	}
	return p[:idx]
}

func subsystemOf(p string) string {
	segs := strings.Split(p, "/")
	if len(segs) >= 2 {
		return segs[0] + "/" + segs[1]
	}
	return ""
}

var repoPathExtPattern = regexp.MustCompile(`(?i)\.(md|txt|json|xml|go|ts|tsx|js|jsx|css|html|yml|yaml|sql|toml|mod|sum)$`)

// looksLikeRepoPath reports whether a free-form scope string looks like a single
// repo-relative path with a known code/doc/config extension.
func looksLikeRepoPath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	n := normalizeRepoPath(s)
	if n == "" || strings.HasPrefix(n, "/") || isGlobPath(n) {
		return false
	}
	return repoPathExtPattern.MatchString(n)
}

// isSeedableFile reports whether a normalized path is a concrete, safe,
// repo-relative file with an allowed extension suitable for seed_files_to_read.
func isSeedableFile(p string) bool {
	if p == "" || isGlobPath(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return false
	}
	return repoPathExtPattern.MatchString(p)
}

// concreteSeedFiles returns the deduplicated, normalized, seedable file targets.
func concreteSeedFiles(targetFiles []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targetFiles {
		n := normalizeRepoPath(t)
		if !isSeedableFile(n) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// ---------------------------------------------------------------------------
// Shared resolution helpers
// ---------------------------------------------------------------------------

// resolvePromotionTargets resolves and validates the project, candidate, and
// plan for a placement/promotion request. When requireReady is true, candidate
// readiness and absence of an active schedule reference are enforced.
func (s *Service) resolvePromotionTargets(projectID, candidateID, planID string, requireReady bool) (*store.Project, *store.RefactorCandidate, *store.Plan, []ValidationIssue, error) {
	var issues []ValidationIssue
	if strings.TrimSpace(projectID) == "" {
		issues = append(issues, ValidationIssue{Field: "project_id", Code: IssueRefactorProjectRequired, Message: "project_id is required"})
	}
	if strings.TrimSpace(candidateID) == "" {
		issues = append(issues, ValidationIssue{Field: "candidate_id", Code: IssueRefactorCandidateRequired, Message: "candidate_id is required"})
	}
	if strings.TrimSpace(planID) == "" {
		issues = append(issues, ValidationIssue{Field: "plan_id", Code: IssueRefactorPlanRequired, Message: "plan_id is required"})
	}
	if len(issues) > 0 {
		return nil, nil, nil, issues, nil
	}

	project, err := s.store.GetProjectByProjectID(strings.TrimSpace(projectID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, []ValidationIssue{{Field: "project_id", Code: IssueRefactorProjectUnknown, Message: fmt.Sprintf("project %q is unknown", projectID)}}, nil
		}
		return nil, nil, nil, nil, err
	}

	candidate, err := s.store.GetRefactorCandidateByCandidateID(project.ID, strings.TrimSpace(candidateID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, []ValidationIssue{{Field: "candidate_id", Code: IssueRefactorCandidateUnknown, Message: fmt.Sprintf("candidate %q is unknown in project", candidateID)}}, nil
		}
		return nil, nil, nil, nil, err
	}
	if candidate.ProjectID != project.ProjectID {
		return nil, nil, nil, []ValidationIssue{{Field: "candidate_id", Code: IssueRefactorCandidateProjectMismatch, Message: "candidate does not belong to the target project"}}, nil
	}

	// Resolve the plan globally so a plan that belongs to another project is
	// reported as a project mismatch rather than as merely unknown.
	plan, err := s.store.GetPlanByPlanID(strings.TrimSpace(planID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, []ValidationIssue{{Field: "plan_id", Code: IssueRefactorPlanUnknown, Message: fmt.Sprintf("plan %q is unknown", planID)}}, nil
		}
		return nil, nil, nil, nil, err
	}
	if plan.ProjectRowID != project.ID {
		return nil, nil, nil, []ValidationIssue{{Field: "plan_id", Code: IssueRefactorPlanProjectMismatch, Message: "plan does not belong to the target project"}}, nil
	}
	if strings.TrimSpace(plan.Status) != "active" {
		return nil, nil, nil, []ValidationIssue{{Field: "plan_id", Code: IssueRefactorPlanStatusInvalid, Message: "plan must be active"}}, nil
	}

	if requireReady {
		if candidate.Status != CandidateStatusReady {
			return nil, nil, nil, []ValidationIssue{{Field: "candidate_id", Code: IssueRefactorCandidateNotReady, Message: "candidate must be ready before promotion"}}, nil
		}
		active, err := s.store.GetActiveRefactorCandidateScheduleRef(project.ID, candidate.ID)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if active != nil {
			return nil, nil, nil, []ValidationIssue{{Field: "candidate_id", Code: IssueRefactorCandidateAlreadyScheduled, Message: "candidate already has an active scheduling reference"}}, nil
		}
	}

	return project, candidate, plan, nil, nil
}

// candidatePassReadyIssues re-validates a persisted candidate against the
// PASS-003 pass-ready rules before it is promoted or generated into a plan.
func candidatePassReadyIssues(candidate *store.RefactorCandidate) []ValidationIssue {
	input := CandidateInput{
		CandidateID:        candidate.CandidateID,
		Title:              candidate.Title,
		ProblemSummary:     candidate.ProblemSummary,
		CurrentBehavior:    candidate.CurrentBehavior,
		DesiredBehavior:    candidate.DesiredBehavior,
		Rationale:          candidate.Rationale,
		ProposedPassName:   candidate.ProposedPassName,
		ProposedPassGoal:   candidate.ProposedPassGoal,
		ProposedPassScope:  unmarshalStringSlice(candidate.ProposedPassScopeJson),
		NonGoals:           unmarshalStringSlice(candidate.ProposedNonGoalsJson),
		TargetFiles:        unmarshalStringSlice(candidate.TargetFilesJson),
		ValidationCommands: unmarshalStringSlice(candidate.ValidationCommandsJson),
		AuditFocus:         unmarshalStringSlice(candidate.AuditFocusJson),
		Constraints:        unmarshalStringSlice(candidate.ConstraintsJson),
		RiskLevel:          candidate.RiskLevel,
	}
	return validateCandidateInput(input, false)
}

// dependencyOutcome describes a resolved candidate dependency.
type dependencyOutcome struct {
	CandidateID string
	Status      string
}

// listCandidateDependencyOutcomes resolves a candidate's dependency rows back to
// candidate IDs and statuses within the same project.
func (s *Service) listCandidateDependencyOutcomes(project *store.Project, candidateRowID int64) ([]dependencyOutcome, []ValidationIssue, error) {
	deps, err := s.store.ListRefactorCandidateDependencies(project.ID, candidateRowID)
	if err != nil {
		return nil, nil, err
	}
	var outcomes []dependencyOutcome
	var issues []ValidationIssue
	for _, dep := range deps {
		depRow, err := s.store.GetRefactorCandidateByRowID(project.ID, dep.DependsOnCandidateRowID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				issues = append(issues, ValidationIssue{Field: "dependencies", Code: IssueRefactorDependencyUnknown, Message: "a candidate dependency could not be resolved in this project"})
				continue
			}
			return nil, nil, err
		}
		if depRow.ProjectID != project.ProjectID {
			issues = append(issues, ValidationIssue{Field: "dependencies", Code: IssueRefactorDependencyProjectMismatch, Message: "a candidate dependency belongs to a different project"})
			continue
		}
		outcomes = append(outcomes, dependencyOutcome{CandidateID: depRow.CandidateID, Status: depRow.Status})
	}
	return outcomes, issues, nil
}

func isCompletedStatus(status string) bool {
	return status == CandidateStatusCompleted || status == CandidateStatusCompletedWithWarnings
}

// ---------------------------------------------------------------------------
// Existing-plan promotion (S5)
// ---------------------------------------------------------------------------

// PromoteCandidateToPlan promotes one ready candidate into an existing
// project-owned active plan as a normal refactor pass. It validates all blocked
// cases before any write, then atomically inserts the pass, creates the candidate
// scheduling reference, flips the candidate to scheduled, and records a status
// event. It never submits a plan, creates a run, or dispatches an executor.
func (s *Service) PromoteCandidateToPlan(ctx context.Context, input PromoteCandidateInput) (*PromoteCandidateResult, []ValidationIssue, error) {
	project, candidate, plan, issues, err := s.resolvePromotionTargets(input.ProjectID, input.CandidateID, input.PlanID, true)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}

	if readyIssues := candidatePassReadyIssues(candidate); len(readyIssues) > 0 {
		return nil, readyIssues, nil
	}

	// Dependency validation: same-project, and either satisfied (completed) or a
	// warning. Unsatisfied dependencies block promotion. Candidate dependencies
	// are not converted into plan pass dependencies for existing-plan promotion.
	depOutcomes, depIssues, err := s.listCandidateDependencyOutcomes(project, candidate.ID)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	for _, dep := range depOutcomes {
		if isCompletedStatus(dep.Status) {
			warnings = append(warnings, fmt.Sprintf("dependency %q is %s", dep.CandidateID, dep.Status))
			continue
		}
		depIssues = append(depIssues, ValidationIssue{Field: "dependencies", Code: IssueRefactorDependencyNotSatisfied, Message: fmt.Sprintf("dependency %q is not satisfied (status %q)", dep.CandidateID, dep.Status)})
	}
	if len(depIssues) > 0 {
		return nil, depIssues, nil
	}

	seedFiles := concreteSeedFiles(unmarshalStringSlice(candidate.TargetFilesJson))
	if len(seedFiles) == 0 {
		return nil, []ValidationIssue{{Field: "candidate_id", Code: IssueRefactorCandidateMissingContext, Message: "candidate has no concrete repo-relative file targets to seed pass context"}}, nil
	}

	existingPasses, err := s.store.ListPlanPassesByPlan(plan.ID)
	if err != nil {
		return nil, nil, err
	}

	// Determine placement (sequence + reason).
	maxSeq := int64(0)
	passByID := map[string]store.PlanPass{}
	for _, p := range existingPasses {
		if p.Sequence > maxSeq {
			maxSeq = p.Sequence
		}
		passByID[p.PassID] = p
	}

	insertAfterSeq := maxSeq
	placementReason := PlacementAppendedNoSuggest
	placementAfterPassID := ""
	var placementWarnings []string

	switch {
	case strings.TrimSpace(input.AfterPassID) != "":
		target, ok := passByID[strings.TrimSpace(input.AfterPassID)]
		if !ok {
			return nil, []ValidationIssue{{Field: "after_pass_id", Code: IssueRefactorPlacementInvalid, Message: fmt.Sprintf("after_pass_id %q does not belong to the target plan", input.AfterPassID)}}, nil
		}
		insertAfterSeq = target.Sequence
		placementReason = PlacementManualSelection
		placementAfterPassID = target.PassID
		if input.UseSuggestedPlacement {
			placementWarnings = append(placementWarnings, "explicit after_pass_id overrode the placement suggestion")
		}
	case input.UseSuggestedPlacement:
		suggestion := computePlacementSuggestion(candidate, existingPasses)
		if suggestion.AfterPassID != "" {
			insertAfterSeq = suggestion.SequenceAfter
			placementReason = suggestion.PlacementReason
			placementAfterPassID = suggestion.AfterPassID
		} else {
			insertAfterSeq = maxSeq
			placementReason = PlacementAppendedNoSuggest
			placementWarnings = append(placementWarnings, "no deterministic placement suggestion; appended after the last pass")
		}
	default:
		insertAfterSeq = maxSeq
		placementReason = PlacementAppendedNoSuggest
	}
	if placementAfterPassID == "" && len(existingPasses) > 0 {
		// Record the append anchor (last pass) for transparency.
		placementAfterPassID = highestSequencePassID(existingPasses)
	}

	newPassID, placementIssue := nextPassID(existingPasses)
	if placementIssue != nil {
		return nil, []ValidationIssue{*placementIssue}, nil
	}
	insertSeq := insertAfterSeq + 1

	pass := s.buildRefactorPass(newPassID, insertSeq, candidate, seedFiles, schedulingModeExistingPlan, nil)

	// Atomic write: bump later sequences, insert the pass, create the scheduling
	// reference, flip candidate status, and record the status event.
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	txq := generated.New(tx)

	// Re-read passes within the transaction and bump sequences greater than the
	// insertion point in descending order to avoid UNIQUE(plan_row_id, sequence)
	// collisions.
	txPasses, err := txq.ListPlanPassesByPlan(ctx, plan.ID)
	if err != nil {
		return nil, nil, err
	}
	bump := make([]store.PlanPass, 0, len(txPasses))
	for _, p := range txPasses {
		if p.Sequence > insertAfterSeq {
			bump = append(bump, p)
		}
	}
	sort.Slice(bump, func(i, j int) bool { return bump[i].Sequence > bump[j].Sequence })
	for _, p := range bump {
		if _, err := tx.ExecContext(ctx, updatePlanPassSequenceSQL, p.Sequence+1, p.ID); err != nil {
			return nil, nil, err
		}
	}

	createdPass, err := txq.CreatePlanPass(ctx, planPassParamsFor(plan.ID, pass))
	if err != nil {
		return nil, nil, fmt.Errorf("create plan pass: %w", err)
	}

	scheduleRefID := "rsched-" + uuid.NewString()
	if _, err := txq.CreateRefactorCandidateScheduleRef(ctx, generated.CreateRefactorCandidateScheduleRefParams{
		ScheduleRefID:  scheduleRefID,
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: candidate.ID,
		ScheduleKind:   schedulingModeExistingPlan,
		Status:         "scheduled",
		PlanRowID:      sql.NullInt64{Int64: plan.ID, Valid: true},
		PlanPassRowID:  sql.NullInt64{Int64: createdPass.ID, Valid: true},
		PlanID:         plan.PlanID,
		PassID:         newPassID,
		Note:           input.Note,
	}); err != nil {
		return nil, nil, fmt.Errorf("create candidate schedule ref: %w", err)
	}

	if _, err := txq.UpdateRefactorCandidateStatusMetadata(ctx, generated.UpdateRefactorCandidateStatusMetadataParams{
		Status:       CandidateStatusScheduled,
		ScheduledAt:  nowRFC3339(),
		ProjectRowID: project.ID,
		CandidateID:  candidate.CandidateID,
	}); err != nil {
		return nil, nil, fmt.Errorf("update candidate status: %w", err)
	}

	if _, err := txq.CreateRefactorCandidateStatusEvent(ctx, generated.CreateRefactorCandidateStatusEventParams{
		EventID:        "revent-" + uuid.NewString(),
		ProjectRowID:   project.ID,
		ProjectID:      project.ProjectID,
		CandidateRowID: candidate.ID,
		EventType:      "scheduled",
		FromStatus:     CandidateStatusReady,
		ToStatus:       CandidateStatusScheduled,
		Reason:         "promoted_to_existing_plan",
		DetailJson:     "{}",
	}); err != nil {
		return nil, nil, fmt.Errorf("create candidate status event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit promotion: %w", err)
	}
	committed = true

	if placementWarnings != nil {
		warnings = append(warnings, placementWarnings...)
	}
	if warnings == nil {
		warnings = []string{}
	}
	if placementWarnings == nil {
		placementWarnings = []string{}
	}

	return &PromoteCandidateResult{
		CandidateID:     candidate.CandidateID,
		PlanID:          plan.PlanID,
		PassID:          newPassID,
		Sequence:        insertSeq,
		CandidateStatus: CandidateStatusScheduled,
		SchedulingReference: SchedulingReference{
			PlanID: plan.PlanID,
			PassID: newPassID,
			RunID:  "",
		},
		Placement: PromotionPlacement{
			PlacementReason: placementReason,
			AfterPassID:     placementAfterPassID,
			Warnings:        placementWarnings,
		},
		Warnings: warnings,
	}, nil, nil
}

func highestSequencePassID(passes []store.PlanPass) string {
	best := ""
	bestSeq := int64(-1)
	for _, p := range passes {
		if p.Sequence > bestSeq || (p.Sequence == bestSeq && p.PassID > best) {
			bestSeq = p.Sequence
			best = p.PassID
		}
	}
	return best
}

var passIDPattern = regexp.MustCompile(`^PASS-(\d{3})$`)

// nextPassID computes the next zero-padded pass ID from existing PASS-NNN ids.
func nextPassID(passes []store.PlanPass) (string, *ValidationIssue) {
	maxNum := 0
	matched := false
	for _, p := range passes {
		m := passIDPattern.FindStringSubmatch(strings.TrimSpace(p.PassID))
		if m == nil {
			continue
		}
		matched = true
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		if n > maxNum {
			maxNum = n
		}
	}
	if !matched {
		if len(passes) == 0 {
			return "PASS-001", nil
		}
		return "", &ValidationIssue{Field: "plan_id", Code: IssueRefactorPlacementInvalid, Message: "cannot determine next pass id: existing passes do not use the PASS-NNN convention"}
	}
	return fmt.Sprintf("PASS-%03d", maxNum+1), nil
}

// ---------------------------------------------------------------------------
// Pass building
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

// buildRefactorPass builds a plans.PlanPassInput for a refactor candidate. The
// dependencies argument is used only for generated refactor-only plans; for
// existing-plan promotion it is nil (no plan pass dependencies are created).
func (s *Service) buildRefactorPass(passID string, sequence int64, candidate *store.RefactorCandidate, seedFiles []string, schedulingMode string, dependencies []string) plans.PlanPassInput {
	scope := unmarshalStringSlice(candidate.ProposedPassScopeJson)
	nonGoals := unmarshalStringSlice(candidate.ProposedNonGoalsJson)

	seed := make([]plans.ContextFileRead, 0, len(seedFiles))
	for _, f := range seedFiles {
		seed = append(seed, plans.ContextFileRead{
			RepoID:   generatedPlanRepoID,
			Path:     f,
			Purpose:  "Ground selected refactor target.",
			Required: boolPtr(true),
		})
	}

	query := strings.TrimSpace(candidate.Title)
	if sub := firstSubsystemTerm(seedFiles); sub != "" {
		query = strings.TrimSpace(query + " " + sub)
	}
	if query == "" {
		query = candidate.CandidateID
	}

	if dependencies == nil {
		dependencies = []string{}
	}

	return plans.PlanPassInput{
		PassID:                 passID,
		Sequence:               sequence,
		Name:                   "Refactor: " + candidate.Title,
		Goal:                   candidate.ProposedPassGoal,
		IntendedExecutionScope: scope,
		NonGoals:               nonGoals,
		Dependencies:           dependencies,
		Status:                 "planned",
		PassType:               "refactor",
		ContextPlan: plans.ContextPlan{
			RequiredRepositories: []string{generatedPlanRepoID},
			SeedSearchTerms: []plans.ContextSearchTerm{{
				RepoID:   generatedPlanRepoID,
				Query:    query,
				Purpose:  "Ground selected refactor candidate before handoff.",
				Required: boolPtr(true),
			}},
			SeedFilesToRead: seed,
			ContextCoverageExpectations: []string{
				"Source context covers the selected refactor candidate file targets.",
				"Handoff preserves candidate scope, non-goals, validation expectations, and audit priorities.",
			},
			BlockedIfMissing: []string{
				"Selected candidate file targets cannot be read or safely searched.",
				"Candidate scope cannot be represented as one bounded managed pass.",
			},
		},
		SourceSnapshotRequirements: plans.SourceSnapshotRequirements{
			RequireGitStatus:   boolPtr(true),
			RequireCommitSHA:   boolPtr(true),
			AllowDirtyWorktree: boolPtr(true),
		},
		HandoffReadinessCriteria: []string{
			"The handoff maps the refactor candidate into one bounded implementation pass.",
			"The handoff preserves candidate non-goals, validation expectations, and audit priorities.",
			"The handoff confirms no sidecar execution and no automatic run creation.",
		},
		RiskLevel: candidate.RiskLevel,
		RefactorCandidate: &plans.RefactorCandidateMetadata{
			CandidateID:    candidate.CandidateID,
			Source:         refactorCandidateSource,
			SchedulingMode: schedulingMode,
		},
	}
}

func firstSubsystemTerm(seedFiles []string) string {
	for _, f := range seedFiles {
		if sub := subsystemOf(f); sub != "" {
			return sub
		}
	}
	return ""
}

// planPassParamsFor maps a plans.PlanPassInput into generated CreatePlanPass
// params, marshaling the structured JSON fields the same way plan submission does.
func planPassParamsFor(planRowID int64, pass plans.PlanPassInput) generated.CreatePlanPassParams {
	return generated.CreatePlanPassParams{
		PlanRowID:                      planRowID,
		PassID:                         pass.PassID,
		Sequence:                       pass.Sequence,
		Name:                           pass.Name,
		Goal:                           pass.Goal,
		IntendedExecutionScopeJson:     mustMarshalJSON(pass.IntendedExecutionScope),
		NonGoalsJson:                   mustMarshalJSON(pass.NonGoals),
		DependenciesJson:               mustMarshalJSON(pass.Dependencies),
		Status:                         pass.Status,
		PassType:                       pass.PassType,
		ContextPlanJson:                mustMarshalJSON(pass.ContextPlan),
		SourceSnapshotRequirementsJson: mustMarshalJSON(pass.SourceSnapshotRequirements),
		HandoffReadinessCriteriaJson:   mustMarshalJSON(pass.HandoffReadinessCriteria),
		RiskLevel:                      pass.RiskLevel,
		ContextBudgetJson:              "{}",
		RawPassJson:                    mustMarshalJSON(pass),
	}
}

func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Generated refactor-only plan (S6)
// ---------------------------------------------------------------------------

// GenerateRefactorOnlyPlan generates reviewable refactor-only Plan of Passes
// JSON and Markdown artifacts from selected ready candidates. It validates the
// generated plan through the existing plan service, but never submits the plan,
// creates a run, dispatches an executor, or mutates candidate status.
func (s *Service) GenerateRefactorOnlyPlan(ctx context.Context, input GenerateRefactorPlanInput) (*GenerateRefactorPlanResult, []ValidationIssue, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return nil, []ValidationIssue{{Field: "project_id", Code: IssueRefactorProjectRequired, Message: "project_id is required"}}, nil
	}
	project, err := s.store.GetProjectByProjectID(strings.TrimSpace(input.ProjectID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, []ValidationIssue{{Field: "project_id", Code: IssueRefactorProjectUnknown, Message: fmt.Sprintf("project %q is unknown", input.ProjectID)}}, nil
		}
		return nil, nil, err
	}

	// Trim, drop blanks, and reject duplicates.
	ordered := make([]string, 0, len(input.CandidateIDs))
	seen := map[string]bool{}
	for _, raw := range input.CandidateIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if seen[id] {
			return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorDuplicateCandidate, Message: fmt.Sprintf("candidate %q is listed more than once", id)}}, nil
		}
		seen[id] = true
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorCandidateRequired, Message: "candidate_ids must contain at least one candidate"}}, nil
	}

	// Resolve and validate each selected candidate.
	rows := make(map[string]*store.RefactorCandidate, len(ordered))
	seedByCandidate := map[string][]string{}
	for _, id := range ordered {
		candidate, err := s.store.GetRefactorCandidateByCandidateID(project.ID, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorCandidateUnknown, Message: fmt.Sprintf("candidate %q is unknown in project", id)}}, nil
			}
			return nil, nil, err
		}
		if candidate.Status != CandidateStatusReady {
			return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorCandidateNotReady, Message: fmt.Sprintf("candidate %q must be ready", id)}}, nil
		}
		active, err := s.store.GetActiveRefactorCandidateScheduleRef(project.ID, candidate.ID)
		if err != nil {
			return nil, nil, err
		}
		if active != nil {
			return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorCandidateAlreadyScheduled, Message: fmt.Sprintf("candidate %q already has a scheduling reference", id)}}, nil
		}
		if readyIssues := candidatePassReadyIssues(candidate); len(readyIssues) > 0 {
			return nil, readyIssues, nil
		}
		seed := concreteSeedFiles(unmarshalStringSlice(candidate.TargetFilesJson))
		if len(seed) == 0 {
			return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorCandidateMissingContext, Message: fmt.Sprintf("candidate %q has no concrete repo-relative file targets", id)}}, nil
		}
		rows[id] = candidate
		seedByCandidate[id] = seed
	}

	selectedSet := map[string]bool{}
	for _, id := range ordered {
		selectedSet[id] = true
	}

	// Build dependency edges among selected candidates and collect warnings for
	// external (completed) dependencies.
	edges := map[string][]string{} // dependsOn (B) -> []A
	depCount := map[string]int{}   // A -> number of selected dependencies
	var warnings []string
	for _, id := range ordered {
		outcomes, depIssues, err := s.listCandidateDependencyOutcomes(project, rows[id].ID)
		if err != nil {
			return nil, nil, err
		}
		if len(depIssues) > 0 {
			return nil, depIssues, nil
		}
		for _, dep := range outcomes {
			switch {
			case selectedSet[dep.CandidateID]:
				edges[dep.CandidateID] = append(edges[dep.CandidateID], id)
				depCount[id]++
			case isCompletedStatus(dep.Status):
				warnings = append(warnings, fmt.Sprintf("candidate %q depends on %q which is %s and not selected; no pass dependency created", id, dep.CandidateID, dep.Status))
			default:
				return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorDependencyNotSatisfied, Message: fmt.Sprintf("candidate %q depends on %q (status %q) which is not selected and not completed", id, dep.CandidateID, dep.Status)}}, nil
			}
		}
	}

	topo, ok := topoOrder(ordered, edges, depCount)
	if !ok {
		return nil, []ValidationIssue{{Field: "candidate_ids", Code: IssueRefactorDependencyCycle, Message: "selected candidates contain a dependency cycle"}}, nil
	}

	// Assign generated pass ids in topo order and build passes.
	genPassID := make(map[string]string, len(topo))
	for i, id := range topo {
		genPassID[id] = fmt.Sprintf("PASS-%03d", i+1)
	}

	passes := make([]plans.PlanPassInput, 0, len(topo))
	for i, id := range topo {
		candidate := rows[id]
		var deps []string
		for _, dep := range dependenciesForCandidate(id, edges) {
			if gp, exists := genPassID[dep]; exists {
				deps = append(deps, gp)
			}
		}
		sort.Strings(deps)
		passes = append(passes, s.buildRefactorPass(genPassID[id], int64(i+1), candidate, seedByCandidate[id], schedulingModeGeneratedPlan, deps))
	}

	now := time.Now().UTC()
	safeProject := safeProjectSlug(project.ProjectID)
	hash8 := candidateHash8(topo)
	planID := fmt.Sprintf("refactor-plan-%s-%s-%s", safeProject, now.Format("20060102"), hash8)
	slug := fmt.Sprintf("refactor-plan-%s-%s", safeProject, hash8)

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "Review selected refactor candidates"
	}

	plan := buildGeneratedPlan(project.ProjectID, planID, title, now, topo, passes)

	jsonBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	report, validationIssues := validateGeneratedPlan(ctx, s.store, jsonBytes)
	if validationIssues != nil {
		return nil, validationIssues, nil
	}
	_ = report

	mdBytes := buildGeneratedPlanMarkdown(project.ProjectID, planID, title, input.Note, topo, genPassID, rows, warnings)

	dateSlug := now.Format("2006-01-02")
	jsonPath, err := artifacts.WritePlan(dateSlug, slug, "planner_pass_plan_json", jsonBytes)
	if err != nil {
		return nil, []ValidationIssue{{Field: "artifacts", Code: IssueRefactorArtifactWriteFailed, Message: "failed to write generated plan JSON artifact"}}, nil
	}
	mdPath, err := artifacts.WritePlan(dateSlug, slug, "planner_pass_plan_markdown", mdBytes)
	if err != nil {
		return nil, []ValidationIssue{{Field: "artifacts", Code: IssueRefactorArtifactWriteFailed, Message: "failed to write generated plan Markdown artifact"}}, nil
	}

	if warnings == nil {
		warnings = []string{}
	}
	return &GenerateRefactorPlanResult{
		ProjectID:            project.ProjectID,
		PlanID:               planID,
		CandidateIDs:         topo,
		JSONArtifactPath:     jsonPath,
		MarkdownArtifactPath: mdPath,
		SubmissionPolicy:     "review_required_no_auto_submit",
		Warnings:             warnings,
	}, nil, nil
}

func dependenciesForCandidate(id string, edges map[string][]string) []string {
	var deps []string
	for dep, targets := range edges {
		for _, t := range targets {
			if t == id {
				deps = append(deps, dep)
			}
		}
	}
	return deps
}

// topoOrder produces a stable topological order over selected candidates.
// edges maps a dependency (B) to the candidates that depend on it (A); depCount
// is the in-degree (number of selected dependencies) per candidate. When several
// candidates are simultaneously ready, the one with the smallest candidate ID is
// chosen for determinism. Returns false when a cycle is present.
func topoOrder(ordered []string, edges map[string][]string, depCount map[string]int) ([]string, bool) {
	indeg := make(map[string]int, len(ordered))
	for _, id := range ordered {
		indeg[id] = depCount[id]
	}

	var order []string
	remaining := map[string]bool{}
	for _, id := range ordered {
		remaining[id] = true
	}

	for len(remaining) > 0 {
		ready := make([]string, 0)
		for id := range remaining {
			if indeg[id] == 0 {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			return nil, false
		}
		sort.Strings(ready)
		pick := ready[0]
		order = append(order, pick)
		delete(remaining, pick)
		for _, target := range edges[pick] {
			indeg[target]--
		}
	}
	return order, true
}

func safeProjectSlug(projectID string) string {
	lower := strings.ToLower(strings.TrimSpace(projectID))
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	slug := b.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "project"
	}
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug
}

func candidateHash8(orderedIDs []string) string {
	sum := sha256.Sum256([]byte(strings.Join(orderedIDs, "\n")))
	return hex.EncodeToString(sum[:])[:8]
}

func buildGeneratedPlan(projectID, planID, title string, now time.Time, orderedCandidateIDs []string, passes []plans.PlanPassInput) plans.PlannerPassPlan {
	return plans.PlannerPassPlan{
		PlanMeta: plans.PlanMeta{
			PlanID:        planID,
			SchemaVersion: "2.0.0",
			CreatedAt:     now.Format(time.RFC3339),
			Title:         title,
			Goal:          "Review and execute selected refactor backlog candidates as normal managed refactor passes.",
			RepoTarget:    generatedPlanRepoTarget,
			BranchContext: generatedPlanBranch,
			Status:        "active",
			ProjectID:     projectID,
			ProjectContext: &plans.ProjectContext{
				PrimaryProject:     projectID,
				PrimaryRepository:  generatedPlanRepoID,
				ContractRepository: "relay-contracts",
				GitHubRole:         "local-first",
			},
			MCPCapabilityProfile: &plans.MCPCapabilityProfile{
				ProfileID:            "relay-local-project-orchestrator-v1",
				Mode:                 "local_first_context_broker",
				ContextBrokerEnabled: boolPtr(true),
				Notes:                "Generated refactor-only plan artifact; review required before submission.",
			},
			SubmissionNote: "Generated from selected ready refactor candidates. Review required; do not auto-submit.",
			RefactorPlanMetadata: &plans.RefactorPlanMetadata{
				Source:             "selected_refactor_candidates",
				SourceCandidateIDs: orderedCandidateIDs,
				SubmissionPolicy:   "review_required_no_auto_submit",
				Notes:              "Generated artifact only. No plan submission, run creation, executor dispatch, or candidate status mutation occurred.",
			},
		},
		SourceIntent: plans.SourceIntent{
			Summary: "Generated from selected ready refactor backlog candidates for human review.",
		},
		GlobalContextRules: &plans.GlobalContextRules{
			DefaultSourceOfTruth:    "github_source_control_plus_local_project_state",
			PlannerContextBoundary:  "Generated refactor-only plan must be reviewed before Relay plan submission.",
			ForbiddenContextDomains: []string{"chat_context_as_source_of_truth", "implicit_repo_inference", "sidecar_execution"},
		},
		Passes: passes,
	}
}

// validateGeneratedPlan validates the generated plan JSON through the existing
// plan service. It returns validation issues (to be surfaced to the caller)
// only when the plan is invalid; artifacts must not be written in that case.
func validateGeneratedPlan(ctx context.Context, st *store.Store, jsonBytes []byte) (*plans.PlanValidationReport, []ValidationIssue) {
	_, report, err := plans.NewService(st).ValidatePlanJSON(ctx, jsonBytes)
	if err != nil {
		return nil, []ValidationIssue{{Field: "artifacts", Code: IssueRefactorPlanValidationFailed, Message: fmt.Sprintf("generated plan validation error: %v", err)}}
	}
	if !report.Valid {
		msg := "generated plan did not pass plan validation"
		if len(report.Issues) > 0 {
			msg = fmt.Sprintf("generated plan invalid: %s (%s)", report.Issues[0].Message, report.Issues[0].Path)
		}
		return &report, []ValidationIssue{{Field: "artifacts", Code: IssueRefactorPlanValidationFailed, Message: msg}}
	}
	return &report, nil
}

func buildGeneratedPlanMarkdown(projectID, planID, title, note string, orderedCandidateIDs []string, genPassID map[string]string, rows map[string]*store.RefactorCandidate, warnings []string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated Refactor-Only Plan\n\n")
	fmt.Fprintf(&b, "- **Plan ID:** %s\n", planID)
	fmt.Fprintf(&b, "- **Project ID:** %s\n", projectID)
	fmt.Fprintf(&b, "- **Title:** %s\n", title)
	fmt.Fprintf(&b, "- **Submission policy:** review_required_no_auto_submit\n\n")
	if strings.TrimSpace(note) != "" {
		fmt.Fprintf(&b, "> Note: %s\n\n", note)
	}

	fmt.Fprintf(&b, "## Review required\n\n")
	fmt.Fprintf(&b, "This is a review-only artifact. It has **not** been submitted as a managed plan, ")
	fmt.Fprintf(&b, "and no runs were created. Submission requires explicit user confirmation through the ")
	fmt.Fprintf(&b, "normal plan submission flow.\n\n")

	fmt.Fprintf(&b, "## Selected candidates (generated order)\n\n")
	for _, id := range orderedCandidateIDs {
		fmt.Fprintf(&b, "- %s\n", id)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Candidate to generated pass mapping\n\n")
	fmt.Fprintf(&b, "| Candidate ID | Generated Pass ID | Title | Risk |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- |\n")
	for _, id := range orderedCandidateIDs {
		row := rows[id]
		title := ""
		risk := ""
		if row != nil {
			title = row.Title
			risk = row.RiskLevel
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", id, genPassID[id], title, risk)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Warnings\n\n")
	if len(warnings) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	} else {
		for _, w := range warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}

	return []byte(b.String())
}
