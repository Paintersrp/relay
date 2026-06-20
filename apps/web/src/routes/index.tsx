import { createFileRoute, redirect } from "@tanstack/react-router";

// Redirect index to the runs list
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/runs" });
  },
});
