import * as React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  upsertProjectRepository,
  updateProjectRepository,
  relayProjectKeys,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";
import type {
  RelayProjectRepository,
  ProjectValidationIssue,
  RepositoryRole,
} from "@/features/relay-projects/types";

interface RelayProjectRepositoryFormProps {
  projectId: string;
  repository?: RelayProjectRepository; // If set, we are in Edit mode
  onSuccess: () => void;
  onCancel: () => void;
}

export function RelayProjectRepositoryForm({
  projectId,
  repository,
  onSuccess,
  onCancel,
}: RelayProjectRepositoryFormProps) {
  const isEdit = !!repository;
  const queryClient = useQueryClient();

  const [repoId, setRepoId] = React.useState(repository?.repoId ?? "");
  const [role, setRole] = React.useState<RepositoryRole>(repository?.role ?? "primary");
  const [localPath, setLocalPath] = React.useState(repository?.localPath ?? "");
  const [remoteLabel, setRemoteLabel] = React.useState(repository?.remoteLabel ?? "");
  const [remoteUrl, setRemoteUrl] = React.useState(repository?.remoteUrl ?? "");
  const [defaultBranch, setDefaultBranch] = React.useState(repository?.defaultBranch ?? "main");

  const [allowedRootsText, setAllowedRootsText] = React.useState(
    repository?.allowedRoots?.join("\n") ?? ""
  );
  const [ignoredGlobsText, setIgnoredGlobsText] = React.useState(
    repository?.ignoredGlobs?.join("\n") ?? ""
  );

  const [maxFileSizeBytes, setMaxFileSizeBytes] = React.useState(
    repository?.maxFileSizeBytes?.toString() ?? "262144"
  );
  const [includeUntracked, setIncludeUntracked] = React.useState(
    repository?.includeUntracked ?? false
  );
  const [enabled, setEnabled] = React.useState(repository?.enabled ?? true);

  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [validationErrors, setValidationErrors] = React.useState<ProjectValidationIssue[] | null>(null);

  const mutation = useMutation({
    mutationFn: (variables: { projectId: string; repoId: string; request: any }) => {
      if (isEdit) {
        return updateProjectRepository(variables.projectId, variables.repoId, variables.request);
      }
      return upsertProjectRepository(variables.projectId, variables.request);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: relayProjectKeys.detail(projectId),
      });
      void queryClient.invalidateQueries({
        queryKey: relayProjectKeys.all,
      });
      onSuccess();
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

    const allowedRoots = allowedRootsText
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean);

    const ignoredGlobs = ignoredGlobsText
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean);

    const parsedSize = parseInt(maxFileSizeBytes, 10);
    const maxBytes = isNaN(parsedSize) ? 262144 : parsedSize;

    const request = {
      repo_id: repoId.trim(),
      role,
      local_path: localPath.trim(),
      remote_label: remoteLabel.trim() || undefined,
      remote_url: remoteUrl.trim() || undefined,
      default_branch: defaultBranch.trim() || undefined,
      allowed_roots: allowedRoots,
      ignored_globs: ignoredGlobs,
      max_file_size_bytes: maxBytes,
      include_untracked: includeUntracked,
      enabled,
    };

    mutation.mutate({
      projectId,
      repoId: repository?.repoId ?? repoId.trim(),
      request,
    });
  };

  return (
    <div className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-5 space-y-4">
      <h3 className="text-sm font-semibold text-foreground">
        {isEdit ? `Edit Repository: ${repository.repoId}` : "Register Repository"}
      </h3>

      <form onSubmit={handleSubmit} className="space-y-4">
        {validationErrors && validationErrors.length > 0 ? (
          <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
            <p className="font-semibold mb-1 text-xs">Validation failed:</p>
            <ul className="list-inside list-disc space-y-1 text-[11px]">
              {validationErrors.map((issue, idx) => (
                <li key={idx}>
                  <span className="font-medium">{issue.field}</span> — {issue.message}{" "}
                  <span className="opacity-70 text-[9px]">({issue.code})</span>
                </li>
              ))}
            </ul>
          </div>
        ) : errorMsg ? (
          <div className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
            {errorMsg}
          </div>
        ) : null}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="repoId">Repository ID <span className="text-destructive">*</span></Label>
            <Input
              id="repoId"
              placeholder="e.g. main-repo"
              value={repoId}
              onChange={(e) => setRepoId(e.target.value)}
              required
              disabled={isEdit || mutation.isPending}
              className="font-mono text-xs"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="role">Role</Label>
            <Select
              value={role}
              onValueChange={(val) => setRole(val as RepositoryRole)}
              disabled={mutation.isPending}
            >
              <SelectTrigger id="role" size="sm">
                <SelectValue placeholder="Select role" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="primary">Primary</SelectItem>
                <SelectItem value="reference">Reference</SelectItem>
                <SelectItem value="contracts">Contracts</SelectItem>
                <SelectItem value="docs">Docs</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="localPath">Local Directory Path <span className="text-destructive">*</span></Label>
          <Input
            id="localPath"
            placeholder="e.g. d:\Code\my-repo or /home/user/my-repo"
            value={localPath}
            onChange={(e) => setLocalPath(e.target.value)}
            required
            disabled={mutation.isPending}
            className="text-xs"
          />
          <p className="text-[10px] text-muted-foreground">
            Provide the absolute filesystem path on this machine.
          </p>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="remoteLabel">Remote Label</Label>
            <Input
              id="remoteLabel"
              placeholder="e.g. origin"
              value={remoteLabel}
              onChange={(e) => setRemoteLabel(e.target.value)}
              disabled={mutation.isPending}
              className="text-xs"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="defaultBranch">Default Branch</Label>
            <Input
              id="defaultBranch"
              placeholder="main"
              value={defaultBranch}
              onChange={(e) => setDefaultBranch(e.target.value)}
              disabled={mutation.isPending}
              className="text-xs"
            />
          </div>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="remoteUrl">Remote Repository URL</Label>
          <Input
            id="remoteUrl"
            placeholder="e.g. https://github.com/org/repo.git"
            value={remoteUrl}
            onChange={(e) => setRemoteUrl(e.target.value)}
            disabled={mutation.isPending}
            className="text-xs"
          />
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="allowedRoots">Allowed Directory Roots (one per line)</Label>
            <Textarea
              id="allowedRoots"
              placeholder="e.g.&#10;internal/&#10;apps/web/"
              value={allowedRootsText}
              onChange={(e) => setAllowedRootsText(e.target.value)}
              disabled={mutation.isPending}
              className="text-xs font-mono"
              rows={3}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="ignoredGlobs">Ignored Globs (one per line)</Label>
            <Textarea
              id="ignoredGlobs"
              placeholder="e.g.&#10;node_modules/**&#10;dist/**"
              value={ignoredGlobsText}
              onChange={(e) => setIgnoredGlobsText(e.target.value)}
              disabled={mutation.isPending}
              className="text-xs font-mono"
              rows={3}
            />
          </div>
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="maxFileSizeBytes">Max File Size Policy (bytes)</Label>
            <Input
              id="maxFileSizeBytes"
              type="number"
              placeholder="262144"
              value={maxFileSizeBytes}
              onChange={(e) => setMaxFileSizeBytes(e.target.value)}
              disabled={mutation.isPending}
              className="text-xs font-mono"
            />
          </div>

          <div className="flex flex-col gap-3 justify-center pt-2">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="includeUntracked"
                checked={includeUntracked}
                onCheckedChange={(checked) => setIncludeUntracked(checked === true)}
                disabled={mutation.isPending}
              />
              <Label
                htmlFor="includeUntracked"
                className="text-xs font-normal cursor-pointer select-none"
              >
                Include untracked workspace files
              </Label>
            </div>

            <div className="flex items-center space-x-2">
              <Checkbox
                id="enabled"
                checked={enabled}
                onCheckedChange={(checked) => setEnabled(checked === true)}
                disabled={mutation.isPending}
              />
              <Label
                htmlFor="enabled"
                className="text-xs font-normal cursor-pointer select-none"
              >
                Enable this repository in project context
              </Label>
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-3 pt-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onCancel}
            disabled={mutation.isPending}
          >
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={mutation.isPending}>
            {mutation.isPending ? (
              <>
                <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                Saving...
              </>
            ) : isEdit ? (
              "Save Changes"
            ) : (
              "Add Repository"
            )}
          </Button>
        </div>
      </form>
    </div>
  );
}
