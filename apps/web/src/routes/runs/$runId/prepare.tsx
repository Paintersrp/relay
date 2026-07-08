// ============================================================
// STUB — Prepare/Compile route redirected to Execute (Pass 4)
// ============================================================
//
// The legacy Compile/Render stage has been replaced by the canonical Execute
// stage. This stub redirects any visitor to /runs/$runId/execute.
// The file is preserved for Pass 5 deletion; the active source has been
// removed.

import { createFileRoute, Navigate } from "@tanstack/react-router";

export const Route = createFileRoute("/runs/$runId/prepare")({
  component: PrepareRedirect,
});

function PrepareRedirect() {
  const { runId } = Route.useParams();
  return (
    <Navigate
      to="/runs/$runId/execute"
      params={{ runId }}
      replace
    />
  );
}
