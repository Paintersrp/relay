// ============================================================
// Relay Navigation — Status color token resolution
// ============================================================
//
// Pure, presentation-only mapping from a canonical Run `status` to a
// Theme_System status color token (an oklch CSS custom property defined in
// `styles.css`). This is the single resolver every Shell surface uses so that
// an identical canonical `status` resolves to the same color token everywhere
// (Requirement 7.4).
//
// The mapping reuses the existing `getRelayStatusConfig(...).role` visual role
// rather than re-deriving color from the raw status string, so the shell agrees
// with the existing StatusBadge / RelayStatusIndicator surfaces. Each visual
// role corresponds one-to-one to a `--relay-status-*` token.
//
// Any status whose resolved role has no mapped token — and any unmapped /
// out-of-enum status, which `getRelayStatusConfig` already resolves to the
// `neutral` role — defaults to `--relay-status-neutral` (Requirement 7.5).
//
// Deterministic and total: the same input always yields the same token, and
// every input yields a token.

import type { RelayRunStatus } from "@/features/relay-runs";
import {
  getRelayStatusConfig,
  type RelayVisualStatusRole,
} from "@/components/relay/relayVisualState";

/**
 * The closed set of Theme_System status color tokens. Each value is a
 * `--relay-status-*` custom property defined in `apps/web/src/styles.css`.
 * `--relay-status-neutral` is the default token (Requirement 7.5).
 */
export type StatusColorToken =
  | "--relay-status-running"
  | "--relay-status-blocked"
  | "--relay-status-complete"
  | "--relay-status-audit"
  | "--relay-status-validation"
  | "--relay-status-neutral"; // default

/**
 * One-to-one mapping from a visual status role to its Theme_System color token.
 * Declared as an exhaustive `Record<RelayVisualStatusRole, StatusColorToken>`
 * so the compiler enforces coverage of every role the shared visual-state
 * helper can produce.
 */
const ROLE_TO_TOKEN: Record<RelayVisualStatusRole, StatusColorToken> = {
  running: "--relay-status-running",
  blocked: "--relay-status-blocked",
  complete: "--relay-status-complete",
  audit: "--relay-status-audit",
  validation: "--relay-status-validation",
  neutral: "--relay-status-neutral",
};

/**
 * Resolve a canonical Run `status` to its Theme_System status color token.
 *
 * Reuses `getRelayStatusConfig(status).role` so every shell surface that
 * represents Run status agrees on the color for a given canonical `status`
 * (Requirement 7.4). Any status with no mapped token — including unmapped or
 * out-of-enum values, which resolve to the `neutral` role — defaults to
 * `--relay-status-neutral` (Requirement 7.5).
 */
export function resolveStatusColorToken(status: RelayRunStatus | string): StatusColorToken {
  const { role } = getRelayStatusConfig(status);
  return ROLE_TO_TOKEN[role] ?? "--relay-status-neutral";
}
