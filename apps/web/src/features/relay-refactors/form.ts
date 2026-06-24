// ============================================================
// Relay Refactor Backlog — pure form parsing helpers (PASS-006)
//
// Shared helpers for converting between multiline textarea content and the
// array / record fields used by the backend request shapes.
// ============================================================

/** Splits multiline textarea content into trimmed, non-empty lines. */
export function parseLines(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

/** Joins an array of values back into newline-separated textarea content. */
export function formatLines(values: string[] | undefined): string {
  return (values ?? []).join("\n");
}

/** Parses comma or newline separated tags into a trimmed, non-empty list. */
export function parseTags(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((tag) => tag.trim())
    .filter((tag) => tag.length > 0);
}

/**
 * Parses `key=value` lines into a string record. Lines without `=` or with an
 * empty key are ignored.
 */
export function parseMetadata(value: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of value.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf("=");
    if (idx <= 0) continue;
    const key = trimmed.slice(0, idx).trim();
    const val = trimmed.slice(idx + 1).trim();
    if (key) {
      out[key] = val;
    }
  }
  return out;
}

/** Formats a string record back into `key=value` newline content. */
export function formatMetadata(metadata: Record<string, string> | undefined): string {
  if (!metadata) return "";
  return Object.entries(metadata)
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}
