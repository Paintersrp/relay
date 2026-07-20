import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Clipboard, Download, FileText, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  workflowArtifactContentQueryOptions,
  type WorkflowArtifactReference,
} from "@/features/relay-runs";
import { cn } from "@/lib/utils";

interface RelayArtifactViewerProps {
  artifact: WorkflowArtifactReference;
  className?: string;
}

function decodeBase64(content: string): string {
  const binary = atob(content);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function artifactText(content: string, encoding: "utf-8" | "base64"): string {
  if (encoding === "utf-8") return content;
  try {
    return decodeBase64(content);
  } catch {
    return "Unable to decode this base64 artifact for display.";
  }
}

function artifactFileName(artifact: WorkflowArtifactReference): string {
  const baseName = artifact.kind.trim().replace(/[^a-zA-Z0-9._-]+/g, "-") || "artifact";
  if (baseName.includes(".")) return baseName;
  const extension = artifact.mediaType.includes("json")
    ? "json"
    : artifact.mediaType.includes("markdown")
      ? "md"
      : "txt";
  return `${baseName}.${extension}`;
}

async function copyText(value: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("Clipboard access is unavailable.");
}

export function RelayArtifactViewer({
  artifact,
  className,
}: RelayArtifactViewerProps) {
  const [open, setOpen] = React.useState(false);
  const [actionMessage, setActionMessage] = React.useState<string | null>(null);
  const contentQuery = useQuery({
    ...workflowArtifactContentQueryOptions(artifact.contentUrl),
    enabled: open,
  });
  const content = contentQuery.data;
  const text = content ? artifactText(content.content, content.encoding) : "";

  const handleCopy = async () => {
    try {
      await copyText(text);
      setActionMessage("Artifact contents copied.");
    } catch (error) {
      setActionMessage(error instanceof Error ? error.message : "Copy failed.");
    }
  };

  const handleDownload = () => {
    if (!content) return;
    const blob = new Blob([text], { type: artifact.mediaType || "text/plain" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = artifactFileName(artifact);
    anchor.click();
    URL.revokeObjectURL(url);
    setActionMessage("Artifact download started.");
  };

  return (
    <>
      <button
        type="button"
        className={cn(
          "block w-full rounded border border-[var(--relay-row-border)] bg-[var(--relay-panel-bg)] p-4 text-left hover:bg-[var(--relay-content-bg)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--relay-accent)]",
          className,
        )}
        onClick={() => {
          setActionMessage(null);
          setOpen(true);
        }}
      >
        <span className="flex items-start justify-between gap-3">
          <span className="flex min-w-0 items-center gap-2">
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <span className="truncate text-sm font-medium">{artifact.kind}</span>
          </span>
          <span className="shrink-0 text-xs text-muted-foreground">View</span>
        </span>
        <span className="mt-2 block break-all font-mono text-[10px] text-muted-foreground">
          {artifact.artifactId} · {artifact.sha256}
        </span>
        <span className="mt-3 block text-xs text-muted-foreground">
          Open canonical artifact
        </span>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>{artifact.kind}</DialogTitle>
            <DialogDescription>
              {artifact.mediaType} · {artifact.sizeBytes.toLocaleString()} bytes · SHA-256 {artifact.sha256}
            </DialogDescription>
          </DialogHeader>
          {contentQuery.isLoading ? (
            <div className="flex min-h-64 items-center justify-center gap-2 rounded border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)] text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Loading artifact contents…
            </div>
          ) : contentQuery.error ? (
            <div role="alert" className="rounded border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
              Unable to load artifact contents: {contentQuery.error instanceof Error ? contentQuery.error.message : "Unknown error."}
            </div>
          ) : content ? (
            <>
              <pre className="max-h-[55vh] min-h-64 overflow-auto whitespace-pre-wrap rounded border border-[var(--relay-row-border)] bg-[var(--relay-code-bg)] p-4 font-mono text-xs">
                {text || "Artifact is empty."}
              </pre>
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span>Showing {content.byteCount.toLocaleString()} bytes from offset {content.offset.toLocaleString()}.</span>
                {content.truncated ? <span className="text-warning">Additional content is available from the API.</span> : null}
              </div>
            </>
          ) : null}
          {actionMessage ? <p role="status" className="text-xs text-muted-foreground">{actionMessage}</p> : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={handleCopy} disabled={!content || !text}>
              {actionMessage === "Artifact contents copied." ? <Check className="size-4" /> : <Clipboard className="size-4" />}
              Copy contents
            </Button>
            <Button type="button" onClick={handleDownload} disabled={!content}>
              <Download className="size-4" /> Download file
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
