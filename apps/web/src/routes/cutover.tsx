import { createFileRoute } from "@tanstack/react-router";
import { CutoverPage } from "@/features/cutover/CutoverPage";

export const Route = createFileRoute("/cutover")({
  component: CutoverPage,
});
