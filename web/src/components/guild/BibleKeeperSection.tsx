import { useEffect, useReducer, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { keys, useRun, useRunBibleProposals } from "../../api/hooks";
import { useRunEvents, type RunEventMessage } from "../../api/events";
import { Badge, ErrorBanner, Spinner } from "../ui";
import { BibleProposalsList } from "./BibleProposals";

type State = { status: "waiting" | "reading" | "retrying" | "done"; text: string; retryReason?: string; warnings?: string[]; connectionError?: string };

function reduce(state: State, ev: RunEventMessage | { type: "connection.error"; message: string }): State {
  switch (ev.type) {
    case "bible.started":
      return { ...state, status: "reading" };
    case "bible.delta":
      return { ...state, status: "reading", text: state.text + (ev.payload.text ?? "") };
    case "bible.retry":
      return { ...state, status: "retrying", retryReason: ev.payload.reason, text: "" };
    case "bible.done":
      return { ...state, status: "done", warnings: ev.payload.warnings };
    case "connection.error":
      return { ...state, connectionError: ev.message };
    default:
      return state;
  }
}

/** BibleKeeperSection follows a bible update run and shows its proposals for decision. */
export function BibleKeeperSection({ runId, projectId }: { runId: string; projectId: string }) {
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
        qc.invalidateQueries({ queryKey: keys.run(runId) });
        qc.invalidateQueries({ queryKey: keys.runProposals(runId) });
        qc.invalidateQueries({ queryKey: ["projects", projectId, "proposals"] });
      }
    },
    (message) => dispatch({ type: "connection.error", message }),
  );
  const proposals = useRunBibleProposals(runId, !!run.data && run.data.status !== "running");
  const streamRef = useRef<HTMLPreElement | null>(null);
  useEffect(() => {
    if (streamRef.current) streamRef.current.scrollTop = streamRef.current.scrollHeight;
  }, [state.text]);
  const failed = run.data?.status === "failed" || run.data?.status === "cancelled";
  const pendingCount = proposals.data?.filter((p) => p.status === "pending").length ?? 0;
  return (
    <section className="rounded-lg border border-stone-300 bg-white shadow-sm">
      <header className="flex items-center justify-between gap-2 border-b border-stone-200 bg-stone-50 px-3 py-2">
        <div>
          <div className="font-medium text-stone-900">Bible keeper</div>
          <div className="text-[11px] text-stone-500">{live ? "reading the revised chapter" : "story bible proposals"}</div>
        </div>
        {live ? (
          <Badge tone="amber">
            <Spinner className="mr-1 inline h-3 w-3" />
            {state.status === "retrying" ? "asking again…" : "proposing…"}
          </Badge>
        ) : failed ? (
          <Badge tone="red">{run.data!.status}</Badge>
        ) : proposals.data ? (
          <Badge tone={pendingCount > 0 ? "amber" : "green"}>{pendingCount > 0 ? `${pendingCount} to decide` : "all decided"}</Badge>
        ) : (
          <Badge tone="stone">…</Badge>
        )}
      </header>
      <div className="px-3 py-2 text-sm">
        {live && (
          <>
            {state.status === "retrying" && state.retryReason && <p className="mb-1 text-xs text-amber-800">The first reply was not valid ({state.retryReason}). Asking once more.</p>}
            <pre ref={streamRef} className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words rounded bg-stone-50 p-2 font-mono text-[11px] leading-snug text-stone-600">
              {state.text || "…"}
            </pre>
          </>
        )}
        {state.connectionError && <p className="text-xs text-red-800">{state.connectionError}</p>}
        {failed && run.data?.error && <ErrorBanner error={new Error(run.data.error)} />}
        <ErrorBanner error={proposals.error} />
        {proposals.isLoading && !live && <Spinner />}
        {proposals.data && proposals.data.length === 0 && <p className="text-xs text-stone-500">The bible keeper found nothing to change.</p>}
        {proposals.data && proposals.data.length > 0 && (
          <>
            <p className="mb-1 text-xs text-stone-500">Approve, edit, or reject each proposal before it touches the story bible. Pending ones also wait on the bible page.</p>
            <BibleProposalsList proposals={proposals.data} projectId={projectId} compact />
          </>
        )}
        {state.warnings && state.warnings.length > 0 && (
          <details className="mt-2 text-xs text-stone-500">
            <summary className="cursor-pointer">{state.warnings.length} note{state.warnings.length === 1 ? "" : "s"} from validation</summary>
            <ul className="mt-1 list-disc pl-4">
              {state.warnings.map((m, i) => (
                <li key={i}>{m}</li>
              ))}
            </ul>
          </details>
        )}
      </div>
    </section>
  );
}
