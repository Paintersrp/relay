import { AlertCircle } from "lucide-react";

import type { RefactorValidationIssue } from "@/features/relay-refactors";

interface RelayRefactorValidationIssuesProps {
  issues: RefactorValidationIssue[];
  /** Optional generic error message shown when there are no structured issues. */
  message?: string | null;
}

/**
 * Renders backend validation issues as specific field/code/message rows so that
 * validation failures are never swallowed as generic success. Falls back to a
 * generic destructive message when no structured issues are present.
 */
export function RelayRefactorValidationIssues({
  issues,
  message,
}: RelayRefactorValidationIssuesProps) {
  if (issues.length === 0 && !message) {
    return null;
  }

  return (
    <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
      <div className="flex items-start gap-2">
        <AlertCircle className="size-4 shrink-0 mt-0.5" />
        <div className="flex-1 space-y-1.5">
          {message ? <p className="font-medium">{message}</p> : null}
          {issues.length > 0 ? (
            <ul className="space-y-1">
              {issues.map((issue, index) => (
                <li
                  key={`${issue.field}-${issue.code}-${index}`}
                  className="font-mono text-xs leading-relaxed"
                >
                  <span className="font-semibold">{issue.field || "(general)"}</span>
                  {issue.code ? (
                    <span className="text-destructive/80"> [{issue.code}]</span>
                  ) : null}
                  {issue.message ? <span> — {issue.message}</span> : null}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </div>
    </div>
  );
}
