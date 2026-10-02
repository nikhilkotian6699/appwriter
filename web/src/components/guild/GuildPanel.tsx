import React, { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type Critique, type CritiqueRecord, type IssueSource, type Run, type RunStatus } from "../../api/client";
import { keys, useRun, useRunCritiques, useRunIssues } from "../../api/hooks";
import { useRunEvents, type EditorPayload, type EventIssue, type FinishedPayload, type PlanPayload, type RunEventMessage, type WriterPayload, type WriterUsage } from "../../api/events";
import { Badge, Button, ErrorBanner, Spinner } from "../ui";
import { fmtCost, fmtTokens, timeAgo } from "../../lib/format";

type WriterStatus = "waiting" | "reading" | "retrying" | "done" | "failed" | "cancelled";

type WriterState = {
  critiqueId: string;
  writerId?: string;
  name: string;
  slug: string;
  alias: string;
  status: WriterStatus;
  text: string;
  retries: number;
  retryReason?: string;
  critique?: Critique;
  error?: string;
  usage?: WriterUsage;
  rawText?: string;
};

type EditorStatus = "idle" | "reading" | "retrying" | "done";

type EditorState = {
  status: EditorStatus;
  text: string;
  retries: number;
  retryReason?: string;
  issues?: EventIssue[];
  warnings?: string[];
  fallback?: boolean;
  error?: string;
  usage?: WriterUsage;
};

type State = {
  plan?: PlanPayload;
  writers: Record<string, WriterState>;
  order: string[];
  editor: EditorState;
  finished?: FinishedPayload;
  connectionError?: string;
};

const initial: State = { writers: {}, order: [], editor: { status: "idle", text: "", retries: 0 } };

function reduce(state: State, ev: RunEventMessage | { type: "connection.error"; message: string }): State {
  switch (ev.type) {
    case "critique.plan": {
      const writers: Record<string, WriterState> = {};
      const order: string[] = [];
      for (const w of ev.payload.writers) {
        writers[w.critique_id] = { critiqueId: w.critique_id, writerId: w.writer_id, name: w.name, slug: w.slug, alias: w.model_alias, status: "waiting", text: "", retries: 0 };
        order.push(w.critique_id);
      }
      return { ...state, plan: ev.payload, writers, order };
    }
    case "writer.started":
    case "writer.delta":
    case "writer.retry":
    case "writer.done":
    case "writer.failed": {
      const p = ev.payload as WriterPayload;
      const cur = state.writers[p.critique_id] ?? { critiqueId: p.critique_id, writerId: p.writer_id, name: p.slug, slug: p.slug, alias: "", status: "waiting", text: "", retries: 0 };
      let next: WriterState = cur;
      if (ev.type === "writer.started") next = { ...cur, status: "reading" };
      if (ev.type === "writer.delta") next = { ...cur, status: cur.status === "retrying" ? "reading" : cur.status, text: cur.text + (p.text ?? "") };
      if (ev.type === "writer.retry") next = { ...cur, status: "retrying", retries: cur.retries + 1, retryReason: p.reason, rawText: (cur.rawText ? cur.rawText + "\n\n" : "") + cur.text, text: "" };
      if (ev.type === "writer.done") next = { ...cur, status: "done", critique: p.critique, usage: p.usage, rawText: (cur.rawText ? cur.rawText + "\n\n" : "") + cur.text };
      if (ev.type === "writer.failed")
        next = { ...cur, status: state.finished?.status === "cancelled" || p.error === "context canceled" ? "cancelled" : "failed", error: p.error, critique: p.critique, usage: p.usage, rawText: (cur.rawText ? cur.rawText + "\n\n" : "") + cur.text };
      const order = state.order.includes(p.critique_id) ? state.order : [...state.order, p.critique_id];
      return { ...state, writers: { ...state.writers, [p.critique_id]: next }, order };
    }
    case "editor.started":
      return { ...state, editor: { ...state.editor, status: "reading" } };
    case "editor.delta":
      return { ...state, editor: { ...state.editor, status: "reading", text: state.editor.text + (ev.payload.text ?? "") } };
    case "editor.retry":
      return { ...state, editor: { ...state.editor, status: "retrying", retries: state.editor.retries + 1, retryReason: ev.payload.reason, text: "" } };
    case "editor.done": {
      const p = ev.payload as EditorPayload;
      return { ...state, editor: { ...state.editor, status: "done", issues: p.issues ?? [], warnings: p.warnings, fallback: p.fallback, error: p.error, usage: p.usage } };
    }
    case "run.finished": {
      const writers = { ...state.writers };
      if (ev.payload.status === "cancelled") {
        for (const id of Object.keys(writers)) {
          if (writers[id].status === "reading" || writers[id].status === "retrying" || writers[id].status === "waiting") writers[id] = { ...writers[id], status: "cancelled" };
        }
      }
      return { ...state, writers, finished: ev.payload };
    }
    case "connection.error":
      return { ...state, connectionError: ev.message };
    default:
      return state;
  }
}

function fromRecords(records: CritiqueRecord[]): WriterState[] {
  return records.map((r) => ({
    critiqueId: r.id,
    writerId: r.writer_id,
    name: r.writer_name,
    slug: r.writer_slug,
    alias: r.model_alias,
    status: r.status === "running" ? "reading" : (r.status as WriterStatus),
    text: "",
    retries: 0,
    critique: r.critique,
    error: r.error || undefined,
    rawText: r.raw_text,
    usage: { prompt_tokens: r.prompt_tokens, completion_tokens: r.completion_tokens, cost_usd: r.cost_usd, cost_estimated: r.cost_estimated },
  }));
}

type Props = {
  runId: string;
  /** Hash of the chapter text as it stands in the editor, to warn when a critique is stale. */
  currentHash: string;
  onHighlight: (quote: string) => boolean;
  onConveneAgain: () => void;
};

/** GuildPanel follows one critique run: live while it runs, from the records afterwards. */
export function GuildPanel({ runId, currentHash, onHighlight, onConveneAgain }: Props) {
  const qc = useQueryClient();
  const run = useRun(runId);
  const [state, dispatch] = useReducer(reduce, initial);
  const runStatus: RunStatus | undefined = state.finished?.status ?? run.data?.status;
  const isLive = run.data?.status === "running" && !state.finished;
  const records = useRunCritiques(runId, !!run.data && run.data.status !== "running");
  const storedIssues = useRunIssues(runId, !!run.data && run.data.status !== "running");

  useRunEvents(
    runId,
    isLive,
    (ev) => {
      dispatch(ev);
      if (ev.type === "run.finished") {
        qc.invalidateQueries({ queryKey: keys.run(runId) });
        qc.invalidateQueries({ queryKey: keys.runCritiques(runId) });
        qc.invalidateQueries({ queryKey: keys.runIssues(runId) });
        if (run.data?.chapter_id) qc.invalidateQueries({ queryKey: keys.chapterRuns(run.data.chapter_id, "critique") });
      }
    },
    (message) => dispatch({ type: "connection.error", message }),
  );

  const cancel = useMutation({
    mutationFn: () => call(api.POST("/api/runs/{runId}/cancel", { params: { path: { runId } } })),
    onSuccess: (r: Run) => qc.setQueryData(keys.run(runId), r),
  });

  const writers: WriterState[] = useMemo(() => {
    if (state.order.length > 0) return state.order.map((id) => state.writers[id]);
    if (records.data) return fromRecords(records.data);
    return [];
  }, [state, records.data]);

  const totals = useMemo(() => {
    let prompt = 0;
    let completion = 0;
    let cost = 0;
    let estimated = false;
    for (const w of writers) {
      if (!w.usage) continue;
      prompt += w.usage.prompt_tokens;
      completion += w.usage.completion_tokens;
      cost += w.usage.cost_usd;
      estimated = estimated || w.usage.cost_estimated;
    }
    if (state.finished) return { prompt: state.finished.prompt_tokens, completion: state.finished.completion_tokens, cost: state.finished.cost_usd, estimated: state.finished.cost_estimated };
    if (run.data && run.data.status !== "running") return { prompt: run.data.prompt_tokens, completion: run.data.completion_tokens, cost: run.data.cost_usd, estimated: run.data.cost_estimated };
    return { prompt, completion, cost, estimated };
  }, [writers, state.finished, run.data]);

  const issues: EventIssue[] | undefined = state.editor.status === "done" ? state.editor.issues : storedIssues.data;
  const synthesis = (run.data?.result?.synthesis as string | undefined) ?? (state.editor.fallback ? "fallback" : undefined);
  const editorDone = state.editor.status === "done" || (!!run.data && run.data.status !== "running");
  const planHash = state.plan?.content_hash ?? (run.data?.params?.content_hash as string | undefined);
  const stale = !!planHash && planHash !== currentHash;
  const sceneCount = state.plan?.scenes.length ?? records.data?.[0]?.scene_count ?? 1;

  if (run.isLoading) return <Spinner />;
  if (run.error) return <ErrorBanner error={run.error} onRetry={() => run.refetch()} />;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <div className="flex items-center gap-2">
          <RunStatusBadge status={runStatus} />
          {run.data && <span className="text-stone-500">{run.data.status === "running" ? "started" : "finished"} {timeAgo(run.data.finished_at ?? run.data.created_at)}</span>}
          {sceneCount > 1 && <Badge tone="blue">{sceneCount} scenes</Badge>}
        </div>
        <div className="flex items-center gap-2">
          {runStatus === "running" && (
            <Button size="sm" variant="danger" onClick={() => cancel.mutate()} loading={cancel.isPending}>
              Cancel
            </Button>
          )}
          {runStatus && runStatus !== "running" && (
            <Button size="sm" variant="primary" onClick={onConveneAgain}>
              Convene again
            </Button>
          )}
        </div>
      </div>
      {stale && (
        <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-900">
          The chapter has changed since this critique. Quotes may no longer be found; convene the Guild again for fresh notes.
        </div>
      )}
      {state.connectionError && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800">{state.connectionError}</div>}
      {run.data?.status === "failed" && run.data.error && <ErrorBanner error={new Error(run.data.error)} />}
      <ErrorBanner error={cancel.error} />
      {records.isLoading && writers.length === 0 && <Spinner />}
      {writers.length === 0 && !records.isLoading && runStatus === "running" && <p className="text-sm text-stone-500">Convening…</p>}
      {(state.editor.status !== "idle" || (issues && issues.length > 0) || synthesis) && (
        <EditorSection editor={state.editor} issues={issues} synthesis={synthesis} loading={storedIssues.isLoading} runError={run.data?.error} onHighlight={onHighlight} />
      )}
      {writers.length > 0 && (
        <details open={!editorDone} className="group">
          <summary className="cursor-pointer select-none text-sm font-medium text-stone-700 hover:text-stone-900">
            Critics' notes ({writers.length})
          </summary>
          <div className="mt-2 flex flex-col gap-3">
            {writers.map((w) => (
              <WriterCard key={w.critiqueId} writer={w} onHighlight={onHighlight} />
            ))}
          </div>
        </details>
      )}
      {(runStatus === "succeeded" || runStatus === "failed" || runStatus === "cancelled" || totals.prompt > 0) && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-stone-200 pt-2 text-xs text-stone-500">
          <span>
            {fmtTokens(totals.prompt)} prompt · {fmtTokens(totals.completion)} completion tokens
          </span>
          <span title={totals.estimated ? "Streamed replies carry no cost header; this is estimated from token counts and gateway prices." : "From the gateway's cost header."}>
            {fmtCost(totals.cost, totals.estimated)}
          </span>
        </div>
      )}
    </div>
  );
}

function RunStatusBadge({ status }: { status?: RunStatus }) {
  switch (status) {
    case "running":
      return (
        <Badge tone="amber">
          <span className="inline-flex items-center gap-1">
            <Spinner className="h-3 w-3" /> in session
          </span>
        </Badge>
      );
    case "succeeded":
      return <Badge tone="green">finished</Badge>;
    case "failed":
      return <Badge tone="red">failed</Badge>;
    case "cancelled":
      return <Badge tone="stone">cancelled</Badge>;
    default:
      return <Badge tone="stone">{status ?? "…"}</Badge>;
  }
}

const statusLabel: Record<WriterStatus, string> = {
  waiting: "waiting",
  reading: "reading…",
  retrying: "asking again…",
  done: "done",
  failed: "failed",
  cancelled: "cancelled",
};

function WriterCard({ writer: w, onHighlight }: { writer: WriterState; onHighlight: (quote: string) => boolean }) {
  const [showRaw, setShowRaw] = useState(false);
  const [notFound, setNotFound] = useState<string | null>(null);
  const streamRef = useRef<HTMLPreElement | null>(null);
  useEffect(() => {
    if (streamRef.current) streamRef.current.scrollTop = streamRef.current.scrollHeight;
  }, [w.text]);

  const tone = w.status === "done" ? "green" : w.status === "failed" ? "red" : w.status === "reading" || w.status === "retrying" ? "amber" : "stone";
  const issues = w.critique?.issues ?? [];
  const conflicts = w.critique?.bible_conflicts ?? [];
  const click = (quote: string) => {
    const ok = onHighlight(quote);
    setNotFound(ok ? null : quote);
  };
  return (
    <section className="rounded-lg border border-stone-200 bg-white shadow-sm">
      <header className="flex items-center justify-between gap-2 border-b border-stone-100 px-3 py-2">
        <div className="min-w-0">
          <div className="truncate font-medium text-stone-900">{w.name}</div>
          <div className="truncate font-mono text-[11px] text-stone-400">{w.alias}</div>
        </div>
        <Badge tone={tone}>
          {(w.status === "reading" || w.status === "retrying") && <Spinner className="mr-1 inline h-3 w-3" />}
          {statusLabel[w.status]}
          {w.retries > 0 && w.status !== "retrying" ? ` · ${w.retries} retry` : ""}
        </Badge>
      </header>
      <div className="px-3 py-2 text-sm">
        {(w.status === "reading" || w.status === "retrying" || w.status === "waiting") && (
          <>
            {w.status === "retrying" && w.retryReason && <p className="mb-1 text-xs text-amber-800">The first reply was not valid ({w.retryReason}). Asking once more.</p>}
            <pre ref={streamRef} className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded bg-stone-50 p-2 font-mono text-[11px] leading-snug text-stone-600">
              {w.text || (w.status === "waiting" ? "Waiting for the gateway…" : "…")}
            </pre>
          </>
        )}
        {w.error && <p className="rounded border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-800">{w.error}</p>}
        {w.critique && (
          <div className={w.error ? "mt-2" : ""}>
            {w.critique.overall && <p className="font-serif italic text-stone-700">{w.critique.overall}</p>}
            {issues.length > 0 ? (
              <ul className="mt-2 space-y-2">
                {issues.map((is) => (
                  <IssueItem key={is.id} issue={is} onClick={() => click(is.quote)} missing={notFound === is.quote} />
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-xs text-stone-500">No issues raised.</p>
            )}
            {conflicts.length > 0 && (
              <div className="mt-3">
                <div className="text-xs font-semibold uppercase tracking-wide text-stone-500">Story bible conflicts</div>
                <ul className="mt-1 space-y-1">
                  {conflicts.map((c, i) => (
                    <li key={i} className="text-xs text-stone-700">
                      <button type="button" className="font-serif italic text-stone-800 hover:underline" onClick={() => click(c.quote)}>
                        “{c.quote}”
                      </button>{" "}
                      conflicts with {c.conflicts_with}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {w.critique.warnings && w.critique.warnings.length > 0 && (
              <details className="mt-2 text-xs text-stone-500">
                <summary className="cursor-pointer">{w.critique.warnings.length} note{w.critique.warnings.length === 1 ? "" : "s"} from validation</summary>
                <ul className="mt-1 list-disc pl-4">
                  {w.critique.warnings.map((m, i) => (
                    <li key={i}>{m}</li>
                  ))}
                </ul>
              </details>
            )}
          </div>
        )}
        {(w.status === "done" || w.status === "failed" || w.status === "cancelled") && (
          <div className="mt-2 flex items-center justify-between text-[11px] text-stone-400">
            <span>{w.usage ? `${fmtTokens(w.usage.prompt_tokens + w.usage.completion_tokens)} tokens · ${fmtCost(w.usage.cost_usd, w.usage.cost_estimated)}` : ""}</span>
            {w.rawText && (
              <button type="button" className="hover:underline" onClick={() => setShowRaw((v) => !v)}>
                {showRaw ? "Hide raw reply" : "Show raw reply"}
              </button>
            )}
          </div>
        )}
        {showRaw && w.rawText && <pre className="mt-1 max-h-60 overflow-y-auto whitespace-pre-wrap break-words rounded bg-stone-50 p-2 font-mono text-[11px] text-stone-600">{w.rawText}</pre>}
      </div>
    </section>
  );
}

const severityTone: Record<string, "red" | "amber" | "stone"> = { high: "red", medium: "amber", low: "stone" };

type IssueLike = { severity: string; quote: string; problem: string; suggested_fix: string };

function IssueItem({ issue, sources, onClick, missing, children }: { issue: IssueLike; sources?: IssueSource[]; onClick: () => void; missing: boolean; children?: React.ReactNode }) {
  return (
    <li className="rounded-md border border-stone-200 p-2">
      <div className="flex items-start gap-2">
        <Badge tone={severityTone[issue.severity] ?? "stone"}>{issue.severity}</Badge>
        <button type="button" onClick={onClick} className="flex-1 text-left font-serif italic leading-snug text-stone-800 hover:underline" title="Show this passage in the editor">
          “{issue.quote}”
        </button>
      </div>
      {missing && <p className="mt-1 text-xs text-amber-800">This passage is no longer in the chapter text.</p>}
      <p className="mt-1.5 text-stone-700">{issue.problem}</p>
      {issue.suggested_fix && (
        <p className="mt-1 text-stone-600">
          <span className="text-xs font-semibold uppercase tracking-wide text-stone-400">Fix </span>
          {issue.suggested_fix}
        </p>
      )}
      {sources && sources.length > 0 && (
        <p className="mt-1.5 flex flex-wrap items-center gap-1 text-[11px] text-stone-500">
          <span>Raised by</span>
          {sources.map((src) => (
            <span key={src.id} className="rounded bg-stone-100 px-1.5 py-0.5 text-stone-700" title={src.id}>
              {src.writer_name}
            </span>
          ))}
        </p>
      )}
      {children}
    </li>
  );
}

function EditorSection({
  editor,
  issues,
  synthesis,
  loading,
  runError,
  onHighlight,
}: {
  editor: EditorState;
  issues?: EventIssue[];
  synthesis?: string;
  loading: boolean;
  runError?: string;
  onHighlight: (quote: string) => boolean;
}) {
  const [notFound, setNotFound] = useState<string | null>(null);
  const streamRef = useRef<HTMLPreElement | null>(null);
  useEffect(() => {
    if (streamRef.current) streamRef.current.scrollTop = streamRef.current.scrollHeight;
  }, [editor.text]);
  const live = editor.status === "reading" || editor.status === "retrying";
  const fallback = synthesis === "fallback" || editor.fallback === true;
  const click = (quote: string) => setNotFound(onHighlight(quote) ? null : quote);
  return (
    <section className="rounded-lg border border-stone-300 bg-white shadow-sm">
      <header className="flex items-center justify-between gap-2 border-b border-stone-200 bg-stone-50 px-3 py-2">
        <div>
          <div className="font-medium text-stone-900">Editor-in-chief</div>
          <div className="text-[11px] text-stone-500">{fallback ? "critics' notes, unmerged" : "the prioritized list"}</div>
        </div>
        <Badge tone={live ? "amber" : fallback ? "amber" : editor.status === "done" || issues ? "green" : "stone"}>
          {live && <Spinner className="mr-1 inline h-3 w-3" />}
          {editor.status === "reading" ? "merging…" : editor.status === "retrying" ? "asking again…" : fallback ? "fallback" : issues ? `${issues.length} issue${issues.length === 1 ? "" : "s"}` : "…"}
        </Badge>
      </header>
      <div className="px-3 py-2 text-sm">
        {live && (
          <>
            {editor.status === "retrying" && editor.retryReason && <p className="mb-1 text-xs text-amber-800">The first reply was not valid ({editor.retryReason}). Asking once more.</p>}
            <pre ref={streamRef} className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded bg-stone-50 p-2 font-mono text-[11px] leading-snug text-stone-600">
              {editor.text || "…"}
            </pre>
          </>
        )}
        {fallback && (
          <p className="mb-2 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-xs text-amber-900">
            The editor-in-chief could not merge the notes{editor.error ? ` (${editor.error})` : runError ? ` (${runError})` : ""}. The critics' issues are listed unmerged, most severe first.
          </p>
        )}
        {loading && !issues && <Spinner />}
        {issues && issues.length === 0 && !live && <p className="text-xs text-stone-500">The editor-in-chief set every note aside: nothing here needs changing.</p>}
        {issues && issues.length > 0 && (
          <ol className="space-y-2">
            {issues.map((is) => (
              <IssueItem key={is.id} issue={is} sources={is.sources} onClick={() => click(is.quote)} missing={notFound === is.quote} />
            ))}
          </ol>
        )}
        {editor.warnings && editor.warnings.length > 0 && (
          <details className="mt-2 text-xs text-stone-500">
            <summary className="cursor-pointer">{editor.warnings.length} note{editor.warnings.length === 1 ? "" : "s"} from validation</summary>
            <ul className="mt-1 list-disc pl-4">
              {editor.warnings.map((m, i) => (
                <li key={i}>{m}</li>
              ))}
            </ul>
          </details>
        )}
        {editor.usage && (editor.status === "done") && (
          <p className="mt-2 text-[11px] text-stone-400">
            {fmtTokens(editor.usage.prompt_tokens + editor.usage.completion_tokens)} tokens · {fmtCost(editor.usage.cost_usd, editor.usage.cost_estimated)}
          </p>
        )}
      </div>
    </section>
  );
}
