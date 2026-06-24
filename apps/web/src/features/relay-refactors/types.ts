// ============================================================
// Relay Refactor Backlog — frontend domain types (PASS-006)
//
// These types mirror the checked-in backend shapes in
// internal/refactors/types.go, internal/api/refactor_backlog.go, and
// internal/api/refactors.go. Requests use snake_case (matching the Go
// *APIRequest structs); responses use camelCase (matching the Go *Result
// structs).
// ============================================================

export type RefactorDiscoveryTaskStatus =
  | "open"
  | "completed"
  | "closed"
  | "superseded"
  | string;

export type RefactorCandidateStatus =
  | "ready"
  | "scheduled"
  | "scheduled_revision_required"
  | "completed"
  | "completed_with_warnings"
  | "deferred"
  | "rejected"
  | "superseded"
  | string;

export type RefactorRiskLevel = "low" | "medium" | "high" | string;

export type RefactorTargetScopeKind =
  | "repository"
  | "subsystem"
  | "directory"
  | "file_set"
  | "plan"
  | "pass"
  | string;

export interface RefactorTargetScope {
  kind: RefactorTargetScopeKind;
  values: string[];
}

// ------------------------------------------------------------
// Discovery task
// ------------------------------------------------------------

export interface RefactorDiscoveryTask {
  discoveryTaskId: string;
  projectId: string;
  title: string;
  analysisPrompt: string;
  targetScope: RefactorTargetScope;
  status: RefactorDiscoveryTaskStatus;
  priority: string;
  tags: string[];
  createdFrom: string;
  metadata: Record<string, string>;
  closureReason?: string;
  completedAt?: string;
  closedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface RefactorDiscoveryTaskRequest {
  discovery_task_id?: string;
  title: string;
  analysis_prompt: string;
  target_scope: RefactorTargetScope;
  priority?: string;
  tags?: string[];
  metadata?: Record<string, string>;
}

export interface RefactorDiscoveryLifecycleRequest {
  closure_reason?: string;
  superseded_by_task_id?: string;
}

export interface RefactorDiscoveryTaskListFilters {
  status?: RefactorDiscoveryTaskStatus;
  limit?: number;
}

// ------------------------------------------------------------
// Candidate
// ------------------------------------------------------------

export interface RefactorCandidate {
  candidateId: string;
  projectId: string;
  title: string;
  problemSummary: string;
  currentBehavior: string;
  desiredBehavior: string;
  rationale: string;
  proposedPassName: string;
  proposedPassGoal: string;
  proposedPassScope: string[];
  nonGoals: string[];
  targetFiles: string[];
  validationCommands: string[];
  auditFocus: string[];
  constraints: string[];
  riskLevel: RefactorRiskLevel;
  status: RefactorCandidateStatus;
  dependencyNotes?: string;
  deferReason?: string;
  rejectReason?: string;
  supersededByCandidateId?: string;
  supersedeReason?: string;
  metadata: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

export interface RefactorCandidateRequest {
  candidate_id?: string;
  title: string;
  problem_summary: string;
  current_behavior?: string;
  desired_behavior: string;
  rationale: string;
  proposed_pass_name: string;
  proposed_pass_goal: string;
  proposed_pass_scope: string[];
  non_goals: string[];
  target_files: string[];
  validation_commands: string[];
  audit_focus: string[];
  constraints?: string[];
  risk_level: RefactorRiskLevel;
  dependency_notes?: string;
  source_discovery_task_ids?: string[];
  candidate_dependency_ids?: string[];
  metadata?: Record<string, string>;
}

export interface RefactorCandidateLifecycleRequest {
  defer_reason?: string;
  reject_reason?: string;
  supersede_reason?: string;
  superseded_by_candidate_id?: string;
}

export interface RefactorCandidateScheduleRequest {
  schedule_ref_id?: string;
  schedule_kind:
    | "existing_plan_bonus_pass"
    | "generated_refactor_only_plan"
    | string;
  plan_id: string;
  pass_id: string;
  run_id?: string;
  note?: string;
}

export interface RefactorCandidateListFilters {
  status?: RefactorCandidateStatus;
  q?: string;
  limit?: number;
}

// ------------------------------------------------------------
// Promotion / generated plan
// ------------------------------------------------------------

export interface RefactorPlacementSuggestion {
  placementReason: string;
  afterPassId: string;
  sequenceAfter: number;
  confidence: "high" | "medium" | "low" | "none" | string;
  matchedPassIds: string[];
  matchedPaths: string[];
  warnings: string[];
}

export interface PromoteRefactorCandidateRequest {
  plan_id: string;
  after_pass_id?: string;
  use_suggested_placement?: boolean;
  note?: string;
}

export interface RefactorSchedulingReference {
  planId: string;
  passId: string;
  runId: string;
}

export interface PromoteRefactorCandidateResult {
  candidateId: string;
  planId: string;
  passId: string;
  sequence: number;
  candidateStatus: string;
  schedulingReference: RefactorSchedulingReference;
  placement: {
    placementReason: string;
    afterPassId: string;
    warnings: string[];
  };
  warnings: string[];
}

export interface GenerateRefactorOnlyPlanRequest {
  candidate_ids: string[];
  title?: string;
  note?: string;
}

export interface GenerateRefactorOnlyPlanResult {
  projectId: string;
  planId: string;
  candidateIds: string[];
  jsonArtifactPath: string;
  markdownArtifactPath: string;
  submissionPolicy: "review_required_no_auto_submit" | string;
  warnings: string[];
}

// ------------------------------------------------------------
// API response / validation
// ------------------------------------------------------------

export interface RefactorValidationIssue {
  field: string;
  code: string;
  message: string;
}

export interface RefactorBacklogResponse {
  success: boolean;
  count?: number;
  discoveryTask?: RefactorDiscoveryTask;
  discoveryTasks?: RefactorDiscoveryTask[];
  candidate?: RefactorCandidate;
  candidates?: RefactorCandidate[];
  validation?: RefactorValidationIssue[];
  placementSuggestion?: RefactorPlacementSuggestion;
  promotion?: PromoteRefactorCandidateResult;
  generatedPlan?: GenerateRefactorOnlyPlanResult;
}
