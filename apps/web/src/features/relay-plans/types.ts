export type PlanAPIStatus = "active" | "complete" | "abandoned";

export type PlanAPIPassStatus =
  | "planned"
  | "in_progress"
  | "completed"
  | "skipped";

export interface PlannerPassPlanMeta {
  plan_id: string;
  schema_version: string;
  created_at: string;
  title: string;
  goal: string;
  repo_target: string;
  branch_context: string;
  status: PlanAPIStatus;
}

export interface PlannerPassPlanSourceIntent {
  summary: string;
}

export interface PlannerPassPlanPass {
  pass_id: string;
  sequence: number;
  name: string;
  goal: string;
  intended_execution_scope: string[];
  non_goals: string[];
  dependencies: string[];
  status: PlanAPIPassStatus;
}

export interface PlannerPassPlan {
  plan_meta: PlannerPassPlanMeta;
  source_intent: PlannerPassPlanSourceIntent;
  passes: PlannerPassPlanPass[];
}

export interface PlanAPIPlan {
  id: string;
  planId: string;
  schemaVersion: string;
  title: string;
  goal: string;
  repoTarget: string;
  branchContext: string;
  status: PlanAPIStatus;
  sourceIntentSummary: string;
  sourceArtifactPath?: string;
  createdAt: string;
  updatedAt: string;
}

export interface PlanAPIPass {
  id: string;
  planRowId: string;
  passId: string;
  sequence: number;
  name: string;
  goal: string;
  intendedExecutionScope: string[];
  nonGoals: string[];
  dependencies: string[];
  status: PlanAPIPassStatus;
  createdAt: string;
  updatedAt: string;
}

export interface PlanAPIReadPlan extends PlanAPIPlan {
  passCount: number;
  completionReady: boolean;
  completedPassCount?: number;
  inProgressPassCount?: number;
  plannedPassCount?: number;
  skippedPassCount?: number;
}

export interface PlanValidationIssue {
  severity?: "error" | "warning" | "info" | string;
  code?: string;
  message: string;
  path?: string;
  details?: Record<string, unknown>;
}

export interface PlanValidationResult {
  valid: boolean;
  issues: PlanValidationIssue[];
}

export interface ValidatePlanRequest {
  plan: PlannerPassPlan;
  sourceArtifactPath?: string;
}

export interface ValidatePlanResponse {
  success: boolean;
  validation: PlanValidationResult;
}

export interface SubmitPlanRequest {
  plan: PlannerPassPlan;
  sourceArtifactPath?: string;
}

export interface SubmitPlanResponse {
  success: boolean;
  plan: PlanAPIPlan;
  passes: PlanAPIPass[];
  validation: PlanValidationResult;
}

export interface PlanListFilters {
  status?: PlanAPIStatus;
  limit?: number;
}

export interface PlanListResponse {
  success: boolean;
  count: number;
  plans: PlanAPIReadPlan[];
}

export interface PlanDetailResponse {
  success: boolean;
  plan: PlanAPIReadPlan;
  passes: PlanAPIPass[];
  completionReady: boolean;
}

export interface PlanPassDetailResponse {
  success: boolean;
  plan: PlanAPIPlan;
  pass: PlanAPIPass;
  completionReady: boolean;
}
