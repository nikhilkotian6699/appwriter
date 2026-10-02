import { useEffect, useReducer, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Chapter, Revision } from "../../api/client";
import { keys, useRevision, useRun } from "../../api/hooks";
import { useRunEvents, type RevisionPayload, type RunEventMessage } from "../../api/events";
import { Badge, Button, ErrorBanner, Spinner } from "../ui";
import { fmtCost, fmtTokens } from "../../lib/format";
import { RevisionDialog, RevisionStatusBadge } from "./RevisionDialog";

type State = {
  status: "waiting" | "writing" | "retrying" | "done";
  text: string;
  scenes?: number;
  notes?: number;
  retryReason?: string;
  revisionId?: string;
  payload?: RevisionPayload;
  connectionError?: string;
};

function reduce(state: State, ev: RunEventMessage | { type: "connection.error"; message: string }): State {
  switch (ev.type) {
    case "revision.started":
      return { ...state, status: "writing", scenes: ev.payload.scenes, notes: ev.payload.notes };
    case "revision.delta":
      return { ...state, status: "writing", text: state.text + (ev.payload.text ?? "") };
    case "revision.retry":
      return { ...state, status: "retrying", retryReason: ev.payload.reason, text: "" };
    case "revision.done":
      return { ...state, status: "done", revisionId: ev.payload.revision_id, payload: ev.payload };
    case "connection.error":
      return { ...state, connectionError: ev.message };
    default:
      return state;
  }
}

type Props = {
  /** The revision run to follow, if one was started in this session. */
  runId: string | null;
  /** A proposed revision found on load, to review without a run. */
  pending: Revision | null;
  chapterId: string;
  onApplied: (chapter: Chapter, bibleRunId: string | null) => void;
};

/** RevisionSection follows a revision run and opens the review dialog when the lead writer is done. */
export function RevisionSection({ runId, pending, chapterId, onApplied }: Props) {
  const qc = useQueryClient();
  const run = useRun(runId);
  const [state, dispatch] = useReducer(reduce, { status: "waiting", text: "" });
  const live = !!run.data && run.data.status === "running" && state.status !== "done";
  useRunEvents(
    runId,
    live,
    (ev) => {
      dispatch(ev);
      if (ev.type === "run.finished") {
        qc.invalidateQueries({ queryKey: keys.run(runId!) });
        qc.invalidateQueries({ queryKey: keys.chapterRevisions(chapterId) });
      }
    },
    (message) => dispatch({ type: "connection.error", message }),
  );
  const revisionId = state.revisionId ?? (run.data?.result?.revision_id as string | undefined) ?? pending?.id ?? null;
  const revision = useRevision(revisionId);
  const [open, setOpen] = useState(false);
  const openedFor = useRef<string | null>(null);
  useEffect(() => {
    // Open the review once per freshly finished run.
    if (state.status === "done" && revision.data && openedFor.current !== revision.data.id) {
      openedFor.current = revision.data.id;
      setOpen(true);
    }
  }, [state.status, revision.data]);
  const streamRef = useRef<HTMLPreElement | null>(null);
  useEffect(() => {
    if (streamRef.current) streamRef.current.scrollTop = streamRef.current.scrollHeight;
  }, [state.text]);

  if (!runId && !pending) return null;
  const failed = run.data?.status === "failed" || run.data?.status === "cancelled";
  const rev = revision.data;
  return (
    <section className="rounded-lg border border-stone-300 bg-white shadow-sm">
      <header className="flex items-center justify-between gap-2 border-b border-stone-200 bg-stone-50 px-3 py-2">
        <div>
          <div className="font-medium text-stone-900">Lead writer</div>
          <div className="text-[11px] text-stone-500">{live ? "applying the accepted notes" : rev ? "a revision to review" : failed ? "the revision failed" : "revision"}</div>
        </div>
        {live ? (
          <Badge tone="amber">
            <Spinner className="mr-1 inline h-3 w-3" />
            {state.status === "retrying" ? "asking again…" : "writing…"}
          </Badge>
        ) : rev ? (
          <RevisionStatusBadge revision={rev} />
        ) : failed ? (
          <Badge tone="red">{run.data!.status}</Badge>
        ) : (
          <Badge tone="stone">…</Badge>
        )}
      </header>
      <div className="px-3 py-2 text-sm">
        {live && (
          <>
            {state.notes !== undefined && (
              <p className="mb-1 text-xs text-stone-500">
                Applying {state.notes} note{state.notes === 1 ? "" : "s"}
                {state.scenes && state.scenes > 1 ? ` across ${state.scenes} scenes` : ""}.
              </p>
            )}
            {state.status === "retrying" && state.retryReason && <p className="mb-1 text-xs text-amber-800">The first reply could not be used ({state.retryReason}). Asking once more.</p>}
            <pre ref={streamRef} className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded bg-stone-50 p-2 font-serif text-xs leading-snug text-stone-600">
              {state.text || "…"}
            </pre>
          </>
        )}
        {state.connectionError && <p className="text-xs text-red-800">{state.connectionError}</p>}
        {failed && run.data?.error && <ErrorBanner error={new Error(run.data.error)} />}
        <ErrorBanner error={revision.error} />
        {rev && (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-xs text-stone-600">
              {rev.stats.hunks} change{rev.stats.hunks === 1 ? "" : "s"} · +{rev.stats.words_added} / −{rev.stats.words_removed} words
              {rev.skipped.length > 0 ? ` · ${rev.skipped.length} note${rev.skipped.length === 1 ? "" : "s"} not applied` : ""}
            </span>
            <Button size="sm" variant={rev.status === "proposed" && !rev.stale ? "primary" : "secondary"} onClick={() => setOpen(true)}>
              {rev.status === "proposed" ? "Review revision" : "View revision"}
            </Button>
          </div>
        )}
        {state.payload?.usage && state.status === "done" && (
          <p className="mt-2 text-[11px] text-stone-400">
            {fmtTokens(state.payload.usage.prompt_tokens + state.payload.usage.completion_tokens)} tokens · {fmtCost(state.payload.usage.cost_usd, state.payload.usage.cost_estimated)}
          </p>
        )}
      </div>
      <RevisionDialog
        revision={rev ?? null}
        open={open && !!rev}
        onClose={() => setOpen(false)}
        onApplied={(chapter, _revision, bibleRunId) => {
          setOpen(false);
          onApplied(chapter, bibleRunId);
        }}
        onDiscarded={() => setOpen(false)}
      />
    </section>
  );
}
