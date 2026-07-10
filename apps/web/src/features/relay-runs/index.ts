export * from "./types";
export * from "./api";
export * from "./queries";
export { API_BASE_URL, RelayApiError } from "../workflow-api";
export type { RelayApiErrorShape } from "../workflow-api";
export {
  EXECUTOR_ADAPTER_OPTIONS,
  KIRO_MODEL_OPTIONS,
  getDefaultModelForAdapter,
  getModelOptionsForAdapter,
  isKnownExecutorAdapter,
} from "./executorOptions";

// Legacy stub exports for compatibility
export function formatRunDate(date: string | Date): string {
  const d = typeof date === 'string' ? new Date(date) : date;
  return d.toLocaleDateString();
}

export function formatRunDateRelative(date: string | Date): string {
  const d = typeof date === 'string' ? new Date(date) : date;
  const now = new Date();
  const diff = now.getTime() - d.getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days === 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 7) return `${days} days ago`;
  return d.toLocaleDateString();
}