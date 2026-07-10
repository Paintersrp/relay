// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RelayProjectNotesPanel } from "./RelayProjectNotesPanel";
import type { WorkflowProjectNote } from "@/features/relay-projects";
import { RelayApiError } from "@/features/relay-runs";

const mocks = vi.hoisted(() => ({
  createNote: vi.fn(),
  deleteNote: vi.fn(),
  updateNote: vi.fn(),
}));

vi.mock("@/features/relay-projects", () => ({
  createWorkflowProjectNote: mocks.createNote,
  deleteWorkflowProjectNote: mocks.deleteNote,
  updateWorkflowProjectNote: mocks.updateNote,
  workflowProjectKeys: {
    details: () => ["workflow-projects", "detail"],
  },
}));

const notes: WorkflowProjectNote[] = [
  {
    noteId: "note-open",
    title: "Open Note",
    body: "Still relevant",
    status: "open",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  },
  {
    noteId: "note-done",
    title: "Done Note",
    body: "Already handled",
    status: "done",
    createdAt: "2026-07-07T00:00:00Z",
    updatedAt: "2026-07-07T01:00:00Z",
  },
];

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RelayProjectNotesPanel projectId="project-1" notes={notes} />
    </QueryClientProvider>,
  );
}

describe("RelayProjectNotesPanel", () => {
  beforeEach(() => {
    mocks.createNote.mockReset();
    mocks.deleteNote.mockReset();
    mocks.updateNote.mockReset();
    mocks.updateNote.mockResolvedValue({});
  });

  it("defaults to open Notes and supports complete and reopen transitions", async () => {
    const user = userEvent.setup();
    renderPanel();

    expect(screen.getByText("Open Note")).toBeInTheDocument();
    expect(screen.queryByText("Done Note")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Complete" }));
    await waitFor(() => {
      expect(mocks.updateNote).toHaveBeenCalledWith(
        "project-1",
        "note-open",
        { status: "done" },
      );
    });

    await user.click(screen.getByRole("tab", { name: /Done/ }));
    expect(screen.getByText("Done Note")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reopen" }));
    await waitFor(() => {
      expect(mocks.updateNote).toHaveBeenCalledWith(
        "project-1",
        "note-done",
        { status: "open" },
      );
    });
  });
  it("announces create failures inside the Note form and preserves entered text", async () => {
    const user = userEvent.setup();
    mocks.createNote.mockRejectedValue(new RelayApiError(
      "Project Note create rejected",
      400,
      "/api/projects/project-1/notes",
      "POST",
      { error: "BAD_REQUEST", message: "Project Note create rejected" },
    ));
    renderPanel();

    await user.click(screen.getByRole("button", { name: "New Note" }));
    const dialog = screen.getByRole("dialog");
    await user.type(within(dialog).getByLabelText("Title"), "New Note");
    await user.type(within(dialog).getByLabelText("Body"), "New body");
    await user.click(within(dialog).getByRole("button", { name: "Create Note" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Project Note create rejected",
    );
    expect(within(dialog).getByLabelText("Title")).toHaveValue("New Note");
    expect(within(dialog).getByLabelText("Body")).toHaveValue("New body");
  });

  it("announces edit failures inside the Note form and preserves edited text", async () => {
    const user = userEvent.setup();
    mocks.updateNote.mockRejectedValueOnce(new RelayApiError(
      "Project Note edit rejected",
      409,
      "/api/projects/project-1/notes/note-open",
      "PATCH",
      { error: "CONFLICT", message: "Project Note edit rejected" },
    ));
    renderPanel();

    await user.click(screen.getByRole("button", { name: "Edit" }));
    const dialog = screen.getByRole("dialog");
    const titleInput = within(dialog).getByLabelText("Title");
    await user.clear(titleInput);
    await user.type(titleInput, "Updated Note");
    await user.click(within(dialog).getByRole("button", { name: "Save Note" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Project Note edit rejected",
    );
    expect(within(dialog).getByLabelText("Title")).toHaveValue("Updated Note");
  });

  it("announces delete failures inside the confirmation dialog", async () => {
    const user = userEvent.setup();
    mocks.deleteNote.mockRejectedValue(new RelayApiError(
      "Project Note delete rejected",
      409,
      "/api/projects/project-1/notes/note-open",
      "DELETE",
      { error: "CONFLICT", message: "Project Note delete rejected" },
    ));
    renderPanel();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = screen.getByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Delete Note" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Project Note delete rejected",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("creates, edits, and deletes ordinary Notes through explicit dialogs", async () => {
    const user = userEvent.setup();
    mocks.createNote.mockResolvedValue({});
    mocks.updateNote.mockResolvedValue({});
    mocks.deleteNote.mockResolvedValue(undefined);
    renderPanel();

    await user.click(screen.getByRole("button", { name: "New Note" }));
    await user.type(screen.getByLabelText("Title"), "New Note");
    await user.type(screen.getByLabelText("Body"), "New body");
    await user.click(screen.getByRole("button", { name: "Create Note" }));
    await waitFor(() => {
      expect(mocks.createNote).toHaveBeenCalledWith("project-1", {
        title: "New Note",
        body: "New body",
      });
    });

    await user.click(screen.getByRole("button", { name: "Edit" }));
    const titleInput = screen.getByLabelText("Title");
    await user.clear(titleInput);
    await user.type(titleInput, "Updated Note");
    await user.click(screen.getByRole("button", { name: "Save Note" }));
    await waitFor(() => {
      expect(mocks.updateNote).toHaveBeenCalledWith("project-1", "note-open", {
        title: "Updated Note",
        body: "Still relevant",
      });
    });

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const deleteButtons = screen.getAllByRole("button", { name: "Delete Note" });
    await user.click(deleteButtons[deleteButtons.length - 1]);
    await waitFor(() => {
      expect(mocks.deleteNote).toHaveBeenCalledWith("project-1", "note-open");
    });
  });

});
