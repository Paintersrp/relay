import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/runs/$runId")({
  component: RunLayout,
});

export function RunLayout() {
  return <Outlet />;
}
