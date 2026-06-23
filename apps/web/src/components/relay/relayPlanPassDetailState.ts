import type { PlanAPIPass, PlanAPIPlan, PlanAPIReadPlan } from "@/features/relay-plans";

export type RelayPlanPassDetailState =
  | "ready"
  | "blocked"
  | "in_progress"
  | "completed"
  | "skipped";

export interface PassBlockingDependency {
  passId: string;
  pass?: PlanAPIPass;
}

export function getPassBlockingDependencies(
  pass: PlanAPIPass,
  allPasses: PlanAPIPass[],
): PassBlockingDependency[] {
  const passMap = new Map(allPasses.map((candidate) => [candidate.passId, candidate]));

  return pass.dependencies.reduce<PassBlockingDependency[]>((blocking, dependencyId) => {
    const dependency = passMap.get(dependencyId);

    if (!dependency) {
      blocking.push({ passId: dependencyId });
      return blocking;
    }

    if (dependency.status !== "completed" && dependency.status !== "skipped") {
      blocking.push({ passId: dependencyId, pass: dependency });
    }

    return blocking;
  }, []);
}

export function getPassDetailState(
  pass: PlanAPIPass,
  allPasses: PlanAPIPass[],
): RelayPlanPassDetailState {
  if (pass.status === "in_progress") return "in_progress";
  if (pass.status === "completed") return "completed";
  if (pass.status === "skipped") return "skipped";

  return getPassBlockingDependencies(pass, allPasses).length > 0 ? "blocked" : "ready";
}

export function canCreateRunForPass(
  pass: PlanAPIPass,
  allPasses: PlanAPIPass[],
): boolean {
  return pass.status === "planned" && getPassBlockingDependencies(pass, allPasses).length === 0;
}

export function getCreateRunSearch(
  planId: string,
  passId: string,
): { planId: string; passId: string } {
  return { planId, passId };
}

export function buildPassContextText({
  plan,
  pass,
  blockingDependencies,
}: {
  plan: PlanAPIPlan | PlanAPIReadPlan;
  pass: PlanAPIPass;
  blockingDependencies: PassBlockingDependency[];
}): string {
  return [
    `Plan ID: ${plan.planId}`,
    `Pass ID: ${pass.passId}`,
    `Pass name: ${pass.name}`,
    `Pass status: ${pass.status}`,
    `Pass goal: ${pass.goal}`,
    `Repository: ${plan.repoTarget}`,
    `Branch: ${plan.branchContext}`,
    `Pass type: ${pass.passType || "unspecified"}`,
    `Risk level: ${pass.riskLevel || "unspecified"}`,
    `Intended execution scope: ${pass.intendedExecutionScope.length > 0 ? pass.intendedExecutionScope.join(", ") : "none"}`,
    `Non-goals: ${pass.nonGoals.length > 0 ? pass.nonGoals.join(", ") : "none"}`,
    `Dependencies: ${pass.dependencies.length > 0 ? pass.dependencies.join(", ") : "none"}`,
    `Blocking dependencies: ${blockingDependencies.length > 0 ? blockingDependencies.map((dependency) => dependency.passId).join(", ") : "none"}`,
    `Required repositories: ${pass.contextPlan?.requiredRepositories.length ? pass.contextPlan.requiredRepositories.join(", ") : "none"}`,
    `Seed searches: ${pass.contextPlan?.seedSearchTerms.length ?? 0}`,
    `Seed files: ${pass.contextPlan?.seedFilesToRead.length ?? 0}`,
    `Readiness criteria: ${pass.handoffReadinessCriteria?.length ? pass.handoffReadinessCriteria.join(", ") : "none"}`,
  ].join("\n");
}
