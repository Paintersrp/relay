import * as React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Edit2, Loader2, Plus, RotateCcw, Trash2 } from "lucide-react";

import {
  RelayFilterTabs,
  type RelayFilterTabItem,
} from "@/components/relay/RelayFilterTabs";
import { formatPlanDate } from "@/components/relay/relayPlanVisualState";
import { Badge } from "@/components/ui/badge";
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
import { Textarea } from "@/components/ui/textarea";
import {
  createWorkflowProjectNote,
  deleteWorkflowProjectNote,
  updateWorkflowProjectNote,
  workflowProjectKeys,
  type WorkflowProjectNote,
  type WorkflowProjectNoteStatus,
} from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

type NoteFilter = WorkflowProjectNoteStatus | "all";
type NoteFormMode = "create" | "edit";

interface RelayProjectNotesPanelProps {
  projectId: string;
  notes: WorkflowProjectNote[];
}

function noteErrorMessage(error: unknown): string {
  if (error instanceof RelayApiError) {
    return error.errorShape?.message || error.message;
  }
  if (error instanceof Error) return error.message;
  return "Project Note operation failed.";
}

export function RelayProjectNotesPanel({
  projectId,
  notes,
}: RelayProjectNotesPanelProps) {
  const queryClient = useQueryClient();
  const [filter, setFilter] = React.useState<NoteFilter>("open");
  const [formOpen, setFormOpen] = React.useState(false);
  const [formMode, setFormMode] = React.useState<NoteFormMode>("create");
  const [editingNote, setEditingNote] = React.useState<WorkflowProjectNote | null>(null);
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState("");
  const [deleteNote, setDeleteNote] = React.useState<WorkflowProjectNote | null>(null);
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  const invalidateProject = React.useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: workflowProjectKeys.details() });
  }, [queryClient]);

  const createMutation = useMutation({
    mutationFn: () => createWorkflowProjectNote(projectId, {
      title: title.trim(),
      body: body.trim(),
    }),
    onSuccess: () => {
      setFormOpen(false);
      setTitle("");
      setBody("");
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(noteErrorMessage(error)),
  });

  const editMutation = useMutation({
    mutationFn: () => updateWorkflowProjectNote(
      projectId,
      editingNote?.noteId ?? "",
      {
        title: title.trim(),
        body: body.trim(),
      },
    ),
    onSuccess: () => {
      setFormOpen(false);
      setEditingNote(null);
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(noteErrorMessage(error)),
  });

  const statusMutation = useMutation({
    mutationFn: (input: { noteId: string; status: WorkflowProjectNoteStatus }) =>
      updateWorkflowProjectNote(projectId, input.noteId, { status: input.status }),
    onSuccess: () => {
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(noteErrorMessage(error)),
  });

  const deleteMutation = useMutation({
    mutationFn: (noteId: string) => deleteWorkflowProjectNote(projectId, noteId),
    onSuccess: () => {
      setDeleteNote(null);
      setErrorMessage(null);
      invalidateProject();
    },
    onError: (error) => setErrorMessage(noteErrorMessage(error)),
  });

  const openCount = notes.filter((note) => note.status === "open").length;
  const doneCount = notes.filter((note) => note.status === "done").length;
  const filteredNotes = notes.filter((note) => filter === "all" || note.status === filter);
  const filterItems: RelayFilterTabItem[] = [
    { value: "open", label: "Open", count: openCount },
    { value: "done", label: "Done", count: doneCount },
    { value: "all", label: "All", count: notes.length },
  ];
  const formPending = createMutation.isPending || editMutation.isPending;

  const openCreateForm = () => {
    setFormMode("create");
    setEditingNote(null);
    setTitle("");
    setBody("");
    setErrorMessage(null);
    setFormOpen(true);
  };

  const openEditForm = (note: WorkflowProjectNote) => {
    setFormMode("edit");
    setEditingNote(note);
    setTitle(note.title);
    setBody(note.body);
    setErrorMessage(null);
    setFormOpen(true);
  };

  const submitForm = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setErrorMessage(null);
    if (formMode === "create") {
      createMutation.mutate();
      return;
    }
    editMutation.mutate();
  };

  return (
    <section className="border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)]">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--relay-row-border)] px-5 py-3">
        <div>
          <h2 className="font-mono text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
            Project Notes
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Keep lightweight context without creating planning or execution records.
          </p>
        </div>
        <Button type="button" size="sm" onClick={openCreateForm}>
          <Plus className="size-3.5" />
          New Note
        </Button>
      </div>

      <RelayFilterTabs
        value={filter}
        items={filterItems}
        onValueChange={(value) => setFilter(value as NoteFilter)}
        listClassName="gap-0 px-4 pb-0"
        triggerClassName="h-auto flex-none gap-1.5 rounded-none border-b-2 border-transparent px-3 py-2.5 text-[12px] font-medium text-muted-foreground after:bottom-[-1px] after:h-px after:bg-info hover:text-foreground data-active:border-info data-active:text-foreground"
        countClassName="rounded-sm bg-muted px-1.5 py-0.5 text-[9px] text-muted-foreground data-active:bg-info/12 data-active:text-info"
      />

      <div className="space-y-3 p-5" aria-live="polite">
        {errorMessage ? (
          <div
            role="alert"
            className="rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
          >
            {errorMessage}
          </div>
        ) : null}

        {filteredNotes.length === 0 ? (
          <div className="rounded border border-dashed border-[var(--relay-row-border)] p-6 text-center">
            <p className="text-sm font-medium text-foreground">
              {filter === "all" ? "No Project Notes" : `No ${filter} Project Notes`}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Create a Note or choose another filter.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {filteredNotes.map((note) => (
              <article
                key={note.noteId}
                className="rounded border border-[var(--relay-row-border)] bg-[var(--relay-content-bg)] p-4"
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-sm font-semibold text-foreground">{note.title}</h3>
                      <Badge variant={note.status === "done" ? "secondary" : "info"}>
                        {note.status === "done" ? "Done" : "Open"}
                      </Badge>
                    </div>
                    <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed text-foreground/85">
                      {note.body}
                    </p>
                    <p className="mt-3 font-mono text-[10px] text-muted-foreground">
                      Updated {formatPlanDate(note.updatedAt)}
                    </p>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={statusMutation.isPending}
                      onClick={() => statusMutation.mutate({
                        noteId: note.noteId,
                        status: note.status === "open" ? "done" : "open",
                      })}
                    >
                      {note.status === "open" ? (
                        <Check className="size-3.5" />
                      ) : (
                        <RotateCcw className="size-3.5" />
                      )}
                      {note.status === "open" ? "Complete" : "Reopen"}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => openEditForm(note)}
                    >
                      <Edit2 className="size-3.5" />
                      Edit
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="text-destructive hover:bg-destructive/10"
                      onClick={() => {
                        setErrorMessage(null);
                        setDeleteNote(note);
                      }}
                    >
                      <Trash2 className="size-3.5" />
                      Delete
                    </Button>
                  </div>
                </div>
              </article>
            ))}
          </div>
        )}
      </div>

      <Dialog
        open={formOpen}
        onOpenChange={(open) => {
          if (!formPending) setFormOpen(open);
        }}
      >
        <DialogContent>
          <form onSubmit={submitForm}>
            <DialogHeader>
              <DialogTitle>
                {formMode === "create" ? "Create Project Note" : "Edit Project Note"}
              </DialogTitle>
              <DialogDescription>
                Notes are Project context only. They do not create Plans, Runs, or planning attempts.
              </DialogDescription>
            </DialogHeader>
            {errorMessage ? (
              <div
                role="alert"
                aria-live="assertive"
                className="mt-4 rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
              >
                {errorMessage}
              </div>
            ) : null}
            <div className="space-y-4 py-4">
              <div className="space-y-1.5">
                <Label htmlFor="project-note-title">Title</Label>
                <Input
                  id="project-note-title"
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                  required
                  disabled={formPending}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="project-note-body">Body</Label>
                <Textarea
                  id="project-note-body"
                  value={body}
                  onChange={(event) => setBody(event.target.value)}
                  rows={6}
                  required
                  disabled={formPending}
                />
              </div>
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={formPending}
                onClick={() => setFormOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={formPending || title.trim().length === 0 || body.trim().length === 0}
              >
                {formPending ? <Loader2 className="size-3.5 animate-spin" /> : null}
                {formMode === "create" ? "Create Note" : "Save Note"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={deleteNote !== null}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setDeleteNote(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Project Note?</DialogTitle>
            <DialogDescription>
              This removes the Note only. It does not affect attached Plans, Runs, repositories, or canonical artifacts.
            </DialogDescription>
          </DialogHeader>
          {errorMessage ? (
            <div
              role="alert"
              aria-live="assertive"
              className="mt-4 rounded border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {errorMessage}
            </div>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={deleteMutation.isPending}
              onClick={() => setDeleteNote(null)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={!deleteNote || deleteMutation.isPending}
              onClick={() => {
                if (deleteNote) deleteMutation.mutate(deleteNote.noteId);
              }}
            >
              {deleteMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Trash2 className="size-3.5" />
              )}
              Delete Note
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
