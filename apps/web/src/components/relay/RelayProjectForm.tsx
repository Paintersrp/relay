import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  createWorkflowProject,
  workflowProjectKeys,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

function projectErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  if (error instanceof Error) return error.message;
  return "Project creation failed.";
}

export function RelayProjectForm() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: createWorkflowProject,
    onSuccess: (project) => {
      void queryClient.invalidateQueries({ queryKey: workflowProjectKeys.all });
      const navigation = {
        to: "/projects/$projectId" as const,
        params: { projectId: project.projectId },
      };
      void navigate(navigation);
    },
    onError: (error) => {
      setErrorMessage(projectErrorMessage(error));
    },
  });

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setErrorMessage(null);
    mutation.mutate({
      name: name.trim(),
      description: description.trim(),
    });
  };

  return (
    <div className="mx-auto w-full max-w-xl">
      <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
        <form onSubmit={handleSubmit} className="space-y-4">
          {errorMessage ? (
            <div
              role="alert"
              className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {errorMessage}
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="project-name">
              Project Name <span className="text-destructive">*</span>
            </Label>
            <Input
              id="project-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Relay"
              autoFocus
              required
              disabled={mutation.isPending}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="project-description">Description</Label>
            <Textarea
              id="project-description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Describe the work organized by this Project."
              rows={4}
              disabled={mutation.isPending}
            />
            <p className="text-[10px] text-muted-foreground">
              Relay assigns the Project identity and creates it in the active state.
            </p>
          </div>

          <div className="flex justify-end gap-3 pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={mutation.isPending}
              onClick={() => void navigate({ to: "/projects" })}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={mutation.isPending || name.trim().length === 0}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="size-3.5 animate-spin" />
                  Creating...
                </>
              ) : (
                "Create Project"
              )}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
