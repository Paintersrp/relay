// ============================================================
// Run Workbench Refinement — Run identity derivation (Requirement 1)
// ============================================================
//
// Pure, presentation-only helper that derives the Run_Summary_Header's
// identity view from an existing `RelayRunDetail`. Status is intentionally
// out of scope here — the existing `StatusBadge` binds directly to
// `RelayRunDetail.status` (Canonical_Run_Status) in the rendering component
// (see task 2.4); `statusSeverity`/`state` are display-only supporting
// fields consumed only by `StatusBadge`, never by this helper.

import type { RelayRun, RunIdentityView } from "./runWorkbenchViews";

/**
 * Derives the identity view for the Run_Summary_Header from a run's detail.
 *
 * - `primaryText` is the trimmed run title when non-empty, otherwise the run id.
 * - `runId` and `repo` are always populated from the source run.
 * - `showBranch`/`showModel` and the optional `branch`/`model` values are only
 *   set when the corresponding source value is a non-empty (post-trim) string;
 *   otherwise the field is omitted entirely (no placeholder text/value).
 *
 * Accepts `RelayRun` (rather than `RelayRunDetail`) because it only reads
 * fields already present on `RelayRun` (`title`, `id`, `repo`, `branch`,
 * `model`); `RelayRunDetail extends RelayRun` so every `RelayRunDetail` is
 * still a valid argument. This keeps the helper compatible with
 * `RunWorkbenchLayout`, whose `run` prop is typed as `RelayRun`.
 */
export function deriveRunIdentity(run: RelayRun): RunIdentityView {
  const trimmedTitle = run.title?.trim() ?? "";
  const primaryText = trimmedTitle.length > 0 ? trimmedTitle : run.id;

  const trimmedBranch = run.branch?.trim() ?? "";
  const showBranch = trimmedBranch.length > 0;

  const trimmedModel = run.model?.trim() ?? "";
  const showModel = trimmedModel.length > 0;

  return {
    primaryText,
    runId: run.id,
    repo: run.repo,
    ...(showBranch ? { branch: run.branch } : {}),
    ...(showModel ? { model: run.model } : {}),
    showBranch,
    showModel,
  };
}
