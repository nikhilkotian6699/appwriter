import { useEffect, useMemo, useReducer, useRef } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type Draft, type DraftDecision } from "../../api/client";
import { keys, useRun, useRunDrafts } from "../../api/hooks";
import { useRunEvents, type DraftPayload, type RunEventMessage, type WriterUsage } from "../../api/events";
import { Badge, Button, ErrorBanner, Spinner } from "../ui";
import { fmtCost, fmtTokens } from "../../lib/format";

type DraftState = {
  id: string;
  name: string;
  slug: string;
  status: "waiting" | "writing" | "retrying" | "done" | "failed";
  text: string;
  retryReason?: string;
  error?: string;
  usage?: WriterUsage;
};

type State = { drafts: Record<string, DraftState>; order: string[]; connectionError?: string };

function reduce(state: State, ev: RunEventMessage | { type: "connection.error"; message: string }): State {
  switch (ev.type) {
    case "draft.started":
    case "draft.delta":
    case "draft.retry":
    case "draft.done":
    case "draft.failed": {
      const p = ev.payload as DraftPayload;
      const cur = state.drafts[p.draft_id] ?? { id: p.draft_id, name: p.name, slug: p.slug, status: "waiting", text: "" };
      let next = cur;
      if (ev.type === "draft.started") next = { ...cur, status: "writing" };
      if (ev.type === "draft.delta") next = { ...cur, status: "writing", text: cur.text + (p.text ?? "") };
      if (ev.type === "draft.retry") next = { ...cur, status: "retrying", retryReason: p.reason, text: "" };
      if (ev.type === "draft.done") next = { ...cur, status: "done", text: p.text ?? cur.text, usage: p.usage };
      if (ev.type === "draft.failed") next = { ...cur, status: "failed", error: p.error, usage: p.usage };
      const order = state.order.includes(p.draft_id) ? state.order : [...state.order, p.draft_id];
      return { ...state, drafts: { ...state.drafts, [p.draft_id]: next }, order };
    }
    case "connection.error":
      return { ...state, connectionError: ev.message };
    default:
      return state;
  }
}

type Props = {
  runId: string;
  /** Puts a draft into the editor; returns false when there is no editor to put it in. */
  onInsert: (draft: Draft, how: "insert" | "replace") => boolean;
  onAskAgain: () => void;
};

/** DraftSection follows a co-write or compare run and lets the author place, discard or take back each draft. */
export function DraftSection({ runId, onInsert, onAskAgain }: Props) {
  const qc = useQueryClient();
  const run = useRun(runId);
  const [state, dispatch] = useReducer(reduce, { drafts: {}, order: [] });
  const live = !!run.data && run.data.status === "running";
  useRunEvents(
    runId,
    live,
    (ev) => {
      dispatch(ev);
      if (ev.type === "run.finished") {
        qc.invalidateQueries({ queryKey: keys.run(runId) });
        qc.invalidateQueries({ queryKey: keys.runDrafts(runId) });
        if (run.data?.chapter_id) qc.invalidateQueries({ queryKey: keys.chapterDrafts(run.data.chapter_id) });
      }
    },
    (message) => dispatch({ type: "connection.error", message }),
  );
  const stored = useRunDrafts(runId, !!run.data && run.data.status !== "running");
  const decide = useMutation({
    mutationFn: (v: { id: string; decision: DraftDecision }) => call(api.PUT("/api/drafts/{draftId}/decision", { params: { path: { draftId: v.id } }, body: { decision: v.decision } })),
    onSuccess: (updated) => {
      qc.setQueryData(keys.runDrafts(runId), (old: Draft[] | undefined) => (old ? old.map((d) => (d.id === updated.id ? updated : d)) : old));
      if (updated.chapter_id) qc.invalidateQueries({ queryKey: keys.chapterDrafts(updated.chapter_id) });
    },
  });

  const place = (draft: Draft, how: "insert" | "replace") => {
    if (!onInsert(draft, how)) return;
    decide.mutate({ id: draft.id, decision: how === "insert" ? "inserted" : "replaced" });
  };

  const mode = (run.data?.params?.mode as string | undefined) ?? stored.data?.[0]?.mode;
  const instruction = run.data?.params?.instruction as string | undefined;
  const compare = run.data?.kind === "compare";
  const cards = useMemo(() => {
    if (stored.data) return stored.data;
    return state.order.map((id) => state.drafts[id]);
  }, [stored.data, state]);

  return (
    <section className="rounded-lg border border-stone-300 bg-white shadow-sm">
      <header className="flex items-center justify-between gap-2 border-b border-stone-200 bg-stone-50 px-3 py-2">
        <div className="min-w-0">
          <div className="font-medium text-stone-900">{compare ? "Compare drafts" : "Co-writer"}</div>
          <div className="truncate text-[11px] text-stone-500" title={instruction}>
            {instruction ? `“${instruction}”` : ""}
            {mode ? ` · ${mode === "selection" ? "for the selected passage" : "from the cursor"}` : ""}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {live ? (
            <Badge tone="amber">
              <Spinner className="mr-1 inline h-3 w-3" />
              drafting…
            </Badge>
          ) : run.data?.status === "failed" ? (
            <Badge tone="red">failed</Badge>
          ) : run.data?.status === "cancelled" ? (
            <Badge tone="stone">cancelled</Badge>
          ) : (
            <Button size="sm" onClick={onAskAgain}>
              Ask again
            </Button>
          )}
        </div>
      </header>
      <div className="px-3 py-2 text-sm">
        {state.connectionError && <p className="mb-2 text-xs text-red-800">{state.connectionError}</p>}
        {run.data?.status === "failed" && run.data.error && <ErrorBanner error={new Error(run.data.error)} />}
        <ErrorBanner error={decide.error ?? stored.error} />
        {cards.length === 0 && <p className="text-xs text-stone-500">Waiting for the writer…</p>}
        <div className={cards.length > 1 ? "grid gap-3 md:grid-cols-2 xl:grid-cols-3" : "space-y-3"}>
          {cards.map((c) =>
            "run_id" in c ? (
              <DraftCard key={c.id} draft={c} busy={decide.isPending && decide.variables?.id === c.id} onPlace={(how) => place(c, how)} onDecide={(d) => decide.mutate({ id: c.id, decision: d })} />
            ) : (
              <LiveDraftCard key={c.id} draft={c} />
            ),
          )}
        </div>
      </div>
    </section>
  );
}

function LiveDraftCard({ draft: d }: { draft: DraftState }) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [d.text]);
  return (
    <article className="rounded-md border border-stone-200 p-2">
      <header className="mb-1 flex items-center justify-between gap-2">
        <span className="font-medium text-stone-900">{d.name}</span>
        <Badge tone={d.status === "failed" ? "red" : d.status === "done" ? "green" : "amber"}>
          {(d.status === "writing" || d.status === "retrying") && <Spinner className="mr-1 inline h-3 w-3" />}
          {d.status === "retrying" ? "asking again…" : d.status === "writing" ? "writing…" : d.status}
        </Badge>
      </header>
      {d.status === "retrying" && d.retryReason && <p className="mb-1 text-xs text-amber-800">The first reply could not be used ({d.retryReason}). Asking once more.</p>}
      {d.error && <p className="rounded border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-800">{d.error}</p>}
      <div ref={ref} className="max-h-72 overflow-y-auto whitespace-pre-wrap font-serif leading-relaxed text-stone-800">
        {d.text || (d.status === "waiting" ? "Waiting for the gateway…" : "…")}
      </div>
    </article>
  );
}

const decisionTone: Record<DraftDecision, "green" | "stone" | "blue"> = { inserted: "green", replaced: "green", discarded: "stone", pending: "blue" };

function DraftCard({ draft: d, busy, onPlace, onDecide }: { draft: Draft; busy: boolean; onPlace: (how: "insert" | "replace") => void; onDecide: (decision: DraftDecision) => void }) {
  const ok = d.status === "succeeded";
  return (
    <article className={`rounded-md border p-2 ${d.decision === "discarded" ? "border-stone-200 bg-stone-50 opacity-70" : d.decision !== "pending" ? "border-green-300 bg-green-50/40" : "border-stone-200"}`}>
      <header className="mb-1 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <span className="font-medium text-stone-900">{d.writer_name}</span>
          <span className="ml-2 font-mono text-[11px] text-stone-400">{d.model_alias}</span>
        </div>
        {d.status !== "succeeded" ? <Badge tone="red">{d.status}</Badge> : d.decision !== "pending" ? <Badge tone={decisionTone[d.decision]}>{d.decision}</Badge> : null}
      </header>
      {d.error && <p className="mb-1 rounded border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-800">{d.error}</p>}
      {ok && (
        <div className="max-h-96 overflow-y-auto whitespace-pre-wrap font-serif leading-relaxed text-stone-800">
          {d.text}
        </div>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        {ok && d.decision === "pending" && (
          <>
            {d.mode === "selection" && (
              <Button size="sm" variant="primary" onClick={() => onPlace("replace")} loading={busy}>
                Replace selection
              </Button>
            )}
            <Button size="sm" variant={d.mode === "selection" ? "secondary" : "primary"} onClick={() => onPlace("insert")} disabled={busy}>
              Insert at cursor
            </Button>
            <Button size="sm" variant="danger" onClick={() => onDecide("discarded")} disabled={busy}>
              Discard
            </Button>
          </>
        )}
        {ok && d.decision === "discarded" && (
          <Button size="sm" variant="ghost" onClick={() => onDecide("pending")} loading={busy}>
            Take back
          </Button>
        )}
        <span className="ml-auto text-[11px] text-stone-400">
          {d.prompt_tokens + d.completion_tokens > 0 ? `${fmtTokens(d.prompt_tokens + d.completion_tokens)} tokens · ${fmtCost(d.cost_usd, d.cost_estimated)}` : ""}
        </span>
      </div>
    </article>
  );
}
