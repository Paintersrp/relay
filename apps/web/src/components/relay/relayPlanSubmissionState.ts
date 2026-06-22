import type {
  PlanValidationIssue,
  PlannerPassPlan,
  PlannerPassPlanPass,
} from "@/features/relay-plans";

export type PlanSubmissionState =
  | "draft"
  | "parse_failed"
  | "validating"
  | "validated"
  | "validation_failed"
  | "submitting"
  | "submitted"
  | "submit_failed";

export interface PlanPassPreview {
  passId: string;
  sequence: number;
  name: string;
  goal: string;
  dependencies: string[];
  status: string;
  intendedExecutionScope: string[];
}

export interface PlanSubmissionPreview {
  title: string;
  planId: string;
  goal: string;
  repoTarget: string;
  branchContext: string;
  sourceIntentSummary: string;
  passCount: number;
  dependencyCount: number;
  passes: PlanPassPreview[];
}

type ParsePlanResult =
  | { ok: true; plan: PlannerPassPlan }
  | { ok: false; issue: PlanValidationIssue };

function hasString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function hasStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function issue(path: string, message: string, code = "invalid_shape"): PlanValidationIssue {
  return {
    severity: "error",
    code,
    path,
    message,
  };
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validatePassShape(pass: unknown, index: number): PlanValidationIssue | undefined {
  if (!isObject(pass)) {
    return issue(`passes.${index}`, "Pass entry must be an object.");
  }

  const base = `passes.${index}`;
  if (!hasString(pass.pass_id)) return issue(`${base}.pass_id`, "Pass ID is required.");
  if (typeof pass.sequence !== "number") {
    return issue(`${base}.sequence`, "Pass sequence must be a number.");
  }
  if (!hasString(pass.name)) return issue(`${base}.name`, "Pass name is required.");
  if (!hasString(pass.goal)) return issue(`${base}.goal`, "Pass goal is required.");
  if (!hasStringArray(pass.intended_execution_scope)) {
    return issue(
      `${base}.intended_execution_scope`,
      "Pass intended execution scope must be an array of strings.",
    );
  }
  if (!hasStringArray(pass.non_goals)) {
    return issue(`${base}.non_goals`, "Pass non-goals must be an array of strings.");
  }
  if (!hasStringArray(pass.dependencies)) {
    return issue(
      `${base}.dependencies`,
      "Pass dependencies must be an array of strings.",
    );
  }
  if (!hasString(pass.status)) return issue(`${base}.status`, "Pass status is required.");

  return undefined;
}

function validatePlanShape(value: unknown): PlanValidationIssue | undefined {
  if (!isObject(value)) return issue("root", "Plan JSON must be an object.");

  const { plan_meta, source_intent, passes } = value;
  if (!isObject(plan_meta)) return issue("plan_meta", "plan_meta is required.");
  if (!isObject(source_intent)) {
    return issue("source_intent", "source_intent is required.");
  }
  if (!Array.isArray(passes)) return issue("passes", "passes must be an array.");

  const requiredMetaFields = [
    "plan_id",
    "schema_version",
    "created_at",
    "title",
    "goal",
    "repo_target",
    "branch_context",
    "status",
  ];

  for (const field of requiredMetaFields) {
    if (!hasString(plan_meta[field])) {
      return issue(`plan_meta.${field}`, `${field} is required.`);
    }
  }

  if (!hasString(source_intent.summary)) {
    return issue("source_intent.summary", "source intent summary is required.");
  }

  for (let index = 0; index < passes.length; index += 1) {
    const passIssue = validatePassShape(passes[index], index);
    if (passIssue) return passIssue;
  }

  return undefined;
}

export function parsePlanJson(raw: string): ParsePlanResult {
  try {
    const parsed = JSON.parse(raw) as unknown;
    const shapeIssue = validatePlanShape(parsed);
    if (shapeIssue) return { ok: false, issue: shapeIssue };

    return { ok: true, plan: parsed as PlannerPassPlan };
  } catch (error) {
    const message = error instanceof Error ? error.message : "Invalid JSON.";
    return {
      ok: false,
      issue: issue("root", message, "json_parse_error"),
    };
  }
}

export function getEditorLineCount(raw: string): number {
  if (raw.length === 0) return 1;
  return raw.split(/\r\n|\r|\n/).length;
}

function comparePassSequence(a: PlannerPassPlanPass, b: PlannerPassPlanPass): number {
  return a.sequence - b.sequence;
}

export function getPlanSubmissionPreview(
  plan: PlannerPassPlan,
): PlanSubmissionPreview {
  const passes = [...plan.passes].sort(comparePassSequence);
  const dependencyCount = passes.reduce(
    (count, pass) => count + pass.dependencies.length,
    0,
  );

  return {
    title: plan.plan_meta.title,
    planId: plan.plan_meta.plan_id,
    goal: plan.plan_meta.goal,
    repoTarget: plan.plan_meta.repo_target,
    branchContext: plan.plan_meta.branch_context,
    sourceIntentSummary: plan.source_intent.summary,
    passCount: passes.length,
    dependencyCount,
    passes: passes.map((pass) => ({
      passId: pass.pass_id,
      sequence: pass.sequence,
      name: pass.name,
      goal: pass.goal,
      dependencies: pass.dependencies,
      status: pass.status,
      intendedExecutionScope: pass.intended_execution_scope,
    })),
  };
}
