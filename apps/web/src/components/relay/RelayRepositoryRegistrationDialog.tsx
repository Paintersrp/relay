import * as React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  attachWorkflowProjectRepository,
  confirmWorkflowRepository,
  inspectWorkflowRepository,
  WorkflowRepositoryConfirmationError,
  workflowProjectKeys,
  type WorkflowRepositoryInspection,
  type WorkflowRepositoryRegistrationResult,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

interface RelayRepositoryRegistrationDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId?: string;
  onCompleted?: (result: WorkflowRepositoryRegistrationResult) => void;
  onPartialSuccess?: (
    result: WorkflowRepositoryRegistrationResult,
    message: string,
  ) => void;
}

function repositoryErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  if (error instanceof Error) return error.message;
  return "Repository registration failed.";
}

export function RelayRepositoryRegistrationDialog({
  open,
  onOpenChange,
  projectId,
  onCompleted,
  onPartialSuccess,
}: RelayRepositoryRegistrationDialogProps) {
  const queryClient = useQueryClient();
  const [localPath, setLocalPath] = React.useState("");
  const [remoteName, setRemoteName] = React.useState("");
  const [repoTargetOverride, setRepoTargetOverride] = React.useState("");
  const [inspection, setInspection] =
    React.useState<WorkflowRepositoryInspection | null>(null);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);
  const [successMessage, setSuccessMessage] = React.useState<string | null>(null);
  const [partialSuccessMessage, setPartialSuccessMessage] =
    React.useState<string | null>(null);

  const resetInspection = React.useCallback(() => {
    setInspection(null);
    setErrorMessage(null);
    setSuccessMessage(null);
    setPartialSuccessMessage(null);
  }, []);

  React.useEffect(() => {
    if (!open) {
      setLocalPath("");
      setRemoteName("");
      setRepoTargetOverride("");
      resetInspection();
    }
  }, [open, resetInspection]);

  const inspectMutation = useMutation({
    mutationFn: () =>
      inspectWorkflowRepository({
        localPath: localPath.trim(),
        remoteName: remoteName || undefined,
        repoTargetOverride: repoTargetOverride.trim() || undefined,
      }),
    onSuccess: (value) => {
      setInspection(value);
      setErrorMessage(null);
      setSuccessMessage(null);
      if ("selectedRemote" in value && value.selectedRemote) {
        setRemoteName(value.selectedRemote.name);
      }
    },
    onError: (error) => {
      setInspection(null);
      setSuccessMessage(null);
      setErrorMessage(repositoryErrorMessage(error));
    },
  });

  const confirmMutation = useMutation({
    mutationFn: async () => {
      if (!inspection || inspection.state !== "ready") {
        throw new Error("Inspect the repository before confirming registration.");
      }
      const registration = await confirmWorkflowRepository({
        localPath: localPath.trim(),
        remoteName: remoteName || undefined,
        repoTargetOverride: repoTargetOverride.trim() || undefined,
        expectedConfirmationHash: inspection.confirmationHash,
      });
      if (!projectId) {
        return {
          registration,
          attachment: "not_requested" as const,
        };
      }
      try {
        await attachWorkflowProjectRepository(
          projectId,
          registration.repository.repoTarget,
        );
        return {
          registration,
          attachment: "attached" as const,
        };
      } catch (error) {
        return {
          registration,
          attachment: "failed" as const,
          attachmentError: repositoryErrorMessage(error),
        };
      }
    },
    onSuccess: (result) => {
      void queryClient.invalidateQueries({
        queryKey: workflowProjectKeys.repositories(),
      });
      if (result.attachment === "attached") {
        void queryClient.invalidateQueries({
          queryKey: workflowProjectKeys.details(),
        });
      }

      if (result.attachment === "failed") {
        const message =
          `Repository ${result.registration.repository.repoTarget} was ` +
          `${result.registration.outcome} globally but was not attached to this Project: ` +
          result.attachmentError;
        setErrorMessage(null);
        setSuccessMessage(null);
        setPartialSuccessMessage(message);
        onPartialSuccess?.(result.registration, message);
        return;
      }

      const message =
        result.attachment === "attached"
          ? `Repository ${result.registration.repository.repoTarget} was ${result.registration.outcome} and attached to this Project.`
          : `Repository ${result.registration.repository.repoTarget} was ${result.registration.outcome}.`;
      setErrorMessage(null);
      setPartialSuccessMessage(null);
      setSuccessMessage(message);
      onCompleted?.(result.registration);
    },
    onError: (error) => {
      setSuccessMessage(null);
      setPartialSuccessMessage(null);
      setErrorMessage(repositoryErrorMessage(error));
      if (error instanceof WorkflowRepositoryConfirmationError) {
        setInspection(error.inspection);
        if (
          "selectedRemote" in error.inspection &&
          error.inspection.selectedRemote
        ) {
          setRemoteName(error.inspection.selectedRemote.name);
        } else {
          setRemoteName("");
        }
      }
    },
  });

  const updateLocalPath = (value: string) => {
    setLocalPath(value);
    setRemoteName("");
    resetInspection();
  };
  const updateRemoteName = (value: string) => {
    setRemoteName(value);
    resetInspection();
  };
  const updateOverride = (value: string) => {
    setRepoTargetOverride(value);
    resetInspection();
  };

  const canInspect =
    localPath.trim().length > 0 &&
    !inspectMutation.isPending &&
    !confirmMutation.isPending;
  const canConfirm =
    inspection?.state === "ready" && !confirmMutation.isPending;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!inspectMutation.isPending && !confirmMutation.isPending) {
          onOpenChange(nextOpen);
        }
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Register local repository</DialogTitle>
          <DialogDescription>
            Enter a local path. Relay reads local Git metadata, shows the resolved
            registration, and persists it only after confirmation.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {errorMessage ? (
            <div
              role="alert"
              aria-live="assertive"
              className="flex gap-2 rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
            >
              <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span>{errorMessage}</span>
            </div>
          ) : null}

          {successMessage ? (
            <div
              role="status"
              aria-live="polite"
              className="flex gap-2 rounded border border-emerald-500/20 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-300"
            >
              <CheckCircle2 className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span>{successMessage}</span>
            </div>
          ) : null}

          {partialSuccessMessage ? (
            <div
              role="status"
              aria-live="polite"
              className="flex gap-2 rounded border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-200"
            >
              <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
              <span>{partialSuccessMessage}</span>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="repository-local-path">Local repository path</Label>
            <Input
              id="repository-local-path"
              value={localPath}
              onChange={(event) => updateLocalPath(event.target.value)}
              placeholder={"C:\\Projects\\relay"}
              disabled={inspectMutation.isPending || confirmMutation.isPending}
              autoComplete="off"
              required
            />
          </div>

          {inspection?.state === "needs_remote_selection" ? (
            <div className="space-y-1.5">
              <Label htmlFor="repository-remote">Repository remote</Label>
              <Select value={remoteName} onValueChange={updateRemoteName}>
                <SelectTrigger id="repository-remote">
                  <SelectValue placeholder="Select a configured remote" />
                </SelectTrigger>
                <SelectContent>
                  {inspection.remotes.map((remote) => (
                    <SelectItem key={remote.name} value={remote.name}>
                      {remote.name} — {remote.url}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}

          {inspection?.state === "needs_target_override" ? (
            <div
              role="status"
              aria-live="polite"
              className="rounded border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-900 dark:text-amber-100"
            >
              <p className="font-medium">Repository identity needs an override</p>
              <p className="mt-1">
                {inspection.targetOverrideReason === "no_usable_remote"
                  ? "No usable configured Git remote was found. Enter a valid slash-free Relay repository target to continue."
                  : `Remote ${inspection.selectedRemote?.name ?? "selection"} uses a URL that Relay cannot normalize into a repository target. Enter a valid slash-free Relay repository target to continue.`}
              </p>
            </div>
          ) : null}

          {inspection?.state === "needs_target_override" ||
          (inspection?.state === "conflict" &&
            inspection.conflictKind === "target") ||
          repoTargetOverride ? (
            <div className="space-y-1.5">
              <Label htmlFor="repository-target-override">
                Repository target override
              </Label>
              <Input
                id="repository-target-override"
                value={repoTargetOverride}
                onChange={(event) => updateOverride(event.target.value)}
                placeholder="paintersrp-relay"
                disabled={inspectMutation.isPending || confirmMutation.isPending}
                autoComplete="off"
              />
              <p className="text-xs text-muted-foreground">
                {inspection?.state === "needs_target_override" &&
                inspection.targetOverrideReason === "no_usable_remote"
                  ? "Use a slash-free global key. Relay will not create or change a Git remote."
                  : "Use a slash-free global key. The configured remote is not changed."}
              </p>
            </div>
          ) : null}

          {inspection?.state === "conflict" ? (
            <div role="alert" className="rounded border border-destructive/20 p-3 text-sm">
              <p className="font-medium text-destructive">Registration conflict</p>
              <p className="mt-1 text-muted-foreground">
                {inspection.conflictKind === "target"
                  ? `Target ${inspection.repoTarget} is already registered at ${inspection.existingRepository.localPath}.`
                  : `Path ${inspection.resolvedLocalPath} is already registered as ${inspection.existingRepository.repoTarget}.`}
              </p>
            </div>
          ) : null}

          {inspection?.state === "ready" ? (
            <dl className="grid gap-3 rounded border border-[var(--relay-row-border)] bg-muted/20 p-4 text-sm">
              <div>
                <dt className="text-xs font-medium text-muted-foreground">
                  Resolved root
                </dt>
                <dd className="mt-1 break-all font-mono text-xs">
                  {inspection.resolvedLocalPath}
                </dd>
              </div>
              <div>
                <dt className="text-xs font-medium text-muted-foreground">
                  Selected remote
                </dt>
                <dd className="mt-1 break-all font-mono text-xs">
                  {inspection.selectedRemote
                    ? `${inspection.selectedRemote.name}: ${inspection.selectedRemote.url}`
                    : "No remote selected; operator target override used."}
                </dd>
              </div>
              {inspection.suggestedRepoTarget ? (
                <div>
                  <dt className="text-xs font-medium text-muted-foreground">
                    Suggested target
                  </dt>
                  <dd className="mt-1 font-mono text-xs">
                    {inspection.suggestedRepoTarget}
                  </dd>
                </div>
              ) : null}
              <div>
                <dt className="text-xs font-medium text-muted-foreground">
                  Effective target
                </dt>
                <dd className="mt-1 font-mono text-xs">
                  {inspection.repoTarget}
                </dd>
              </div>
              <div>
                <dt className="text-xs font-medium text-muted-foreground">
                  Outcome
                </dt>
                <dd className="mt-1 text-xs">
                  {inspection.registrationDisposition === "reuse"
                    ? "Reuse the equivalent global registration."
                    : "Create a new global registration."}
                </dd>
              </div>
            </dl>
          ) : null}

          {inspection?.notices.map((notice) => (
            <p key={notice} className="text-xs text-muted-foreground">
              {notice}
            </p>
          ))}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={inspectMutation.isPending || confirmMutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            Close
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!canInspect}
            onClick={() => inspectMutation.mutate()}
          >
            {inspectMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : null}
            Inspect repository
          </Button>
          <Button
            type="button"
            disabled={!canConfirm}
            onClick={() => confirmMutation.mutate()}
          >
            {confirmMutation.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : null}
            {projectId ? "Register and attach" : "Confirm registration"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
