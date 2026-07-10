import { Link, createFileRoute } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";

import { AppPageFrame } from "@/components/relay/AppPageFrame";
import { RelayProjectForm } from "@/components/relay/RelayProjectForm";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/projects/new")({
  component: NewProjectPage,
});

function NewProjectPage() {
  return (
    <AppPageFrame
      title="New Project"
      description="Create a lightweight organizational Project."
      leading={
        <Button asChild variant="ghost" size="icon-sm" aria-label="Back to Projects">
          <Link to="/projects">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
      }
    >
      <RelayProjectForm />
    </AppPageFrame>
  );
}
