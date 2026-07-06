import * as React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { createProject, relayProjectKeys } from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";
import type { ProjectValidationIssue } from "@/features/relay-projects/types";

export function RelayProjectForm() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [projectId, setProjectId] = React.useState("");
  const [name, setName] = React.useState("");
  const [description, setDescription] = React.useState("");
  const [status, setStatus] = React.useState("active");
  const [defaultRepositoryId, setDefaultRepositoryId] = React.useState("");

  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [validationErrors, setValidationErrors] = React.useState<ProjectValidationIssue[] | null>(null);

  const mutation = useMutation({
    mutationFn: createProject,
    onSuccess: (data) => {
      void queryClient.invalidateQueries({
        queryKey: relayProjectKeys.all,
      });
      void navigate({
        to: "/projects/$projectId",
        params: { projectId: data.project.projectId },
      });
    },
    onError: (err: unknown) => {
      if (err instanceof RelayApiError) {
        if (err.errorShape?.error === "VALIDATION_ERROR" && Array.isArray(err.errorShape.details?.validation)) {
          setValidationErrors(err.errorShape.details.validation as ProjectValidationIssue[]);
          setErrorMsg(err.errorShape.message || "Validation failed");
        } else {
          setErrorMsg(err.errorShape?.message || err.message);
          setValidationErrors(null);
        }
      } else {
        setErrorMsg(err instanceof Error ? err.message : "An unexpected error occurred");
        setValidationErrors(null);
      }
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);
    setValidationErrors(null);

    mutation.mutate({
      project_id: projectId.trim(),
      name: name.trim(),
      description: description.trim(),
      status,
      default_repository_id: defaultRepositoryId.trim() || undefined,
    });
  };

  return (
    <div className="mx-auto max-w-xl space-y-6">
      <div className="flex items-center gap-2">
        <Button asChild variant="ghost" size="sm" className="h-8 w-8 p-0">
          <Link to="/projects">
            <ArrowLeft className="h-4 w-4" />
            <span className="sr-only">Back to projects</span>
          </Link>
        </Button>
        <span className="text-sm font-medium text-muted-foreground">Back to Projects</span>
      </div>

      <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-6">
        <form onSubmit={handleSubmit} className="space-y-4">
          {validationErrors && validationErrors.length > 0 ? (
            <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
              <p className="font-semibold mb-1">Validation failed:</p>
              <ul className="list-inside list-disc space-y-1 text-xs">
                {validationErrors.map((issue, idx) => (
                  <li key={idx}>
                    <span className="font-medium">{issue.field}</span> — {issue.message}{" "}
                    <span className="opacity-70 text-[10px]">({issue.code})</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : errorMsg ? (
            <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
              {errorMsg}
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="projectId">Project ID <span className="text-destructive">*</span></Label>
            <Input
              id="projectId"
              placeholder="e.g. relay-specs"
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              required
              disabled={mutation.isPending}
              className="font-mono text-sm"
            />
            <p className="text-[10px] text-muted-foreground">
              A unique lowercase identifier with no spaces.
            </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="name">Project Name <span className="text-destructive">*</span></Label>
            <Input
              id="name"
              placeholder="e.g. Relay Contracts"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              disabled={mutation.isPending}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="description">Description</Label>
            <Textarea
              id="description"
              placeholder="Provide a brief description of this project..."
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={mutation.isPending}
              rows={3}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="status">Status</Label>
            <Select
              value={status}
              onValueChange={setStatus}
              disabled={mutation.isPending}
            >
              <SelectTrigger id="status">
                <SelectValue placeholder="Select status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="active">Active</SelectItem>
                <SelectItem value="archived">Archived</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="defaultRepositoryId">Default Repository ID</Label>
            <Input
              id="defaultRepositoryId"
              placeholder="e.g. main-repo (optional)"
              value={defaultRepositoryId}
              onChange={(e) => setDefaultRepositoryId(e.target.value)}
              disabled={mutation.isPending}
              className="font-mono text-sm"
            />
            <p className="text-[10px] text-muted-foreground">
              Optionally specify the primary repository registered under this project.
            </p>
          </div>

          <div className="flex items-center justify-end gap-3 pt-2">
            <Button asChild variant="outline" size="sm" disabled={mutation.isPending}>
              <Link to="/projects">Cancel</Link>
            </Button>
            <Button type="submit" size="sm" disabled={mutation.isPending}>
              {mutation.isPending ? (
                <>
                  <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
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
