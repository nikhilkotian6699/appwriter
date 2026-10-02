import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import type { RunHistoryItem } from "../api/client";
import { useChapter, useChapterHistory, useRunCalls, type HistoryKind } from "../api/hooks";
import { Badge, Button, EmptyState, ErrorBanner, PageHeader, Spinner } from "../components/ui";
import { fmtCost, fmtDateTime, fmtDuration, fmtTokens, RUN_KIND_LABELS, timeAgo } from "../lib/format";

const KINDS: HistoryKind[] = ["critique", "revision", "bible_update", "cowrite", "compare", "writer_test"];

const statusTone: Record<string, "green" | "red" | "amber" | "stone"> = { succeeded: "green", failed: "red", running: "amber", queued: "amber", cancelled: "stone" };

/** HistoryPage lists every run of a chapter, newest first, with totals and a kind filter. */
export default function HistoryPage() {
  const { chapterId = "" } = useParams();
  const chapter = useChapter(chapterId);
  const [kind, setKind] = useState<HistoryKind | undefined>(undefined);
  const history = useChapterHistory(chapterId, kind);
  const first = history.data?.pages[0];
  const items = useMemo(() => history.data?.pages.flatMap((p) => p.items) ?? [], [history.data]);
  const kindCount = (k: HistoryKind) => first?.kinds.find((c) => c.kind === k)?.count ?? 0;
  const allCount = first?.kinds.reduce((n, c) => n + c.count, 0) ?? 0;

  return (
    <div>
      <div className="mb-2 text-sm text-stone-500">
        <Link to="/projects" className="hover:underline">
          Projects
        </Link>{" "}
        /{" "}
        {chapter.data && (
          <>
            <Link to={`/projects/${chapter.data.project_id}`} className="hover:underline">
              Project
            </Link>{" "}
            /{" "}
            <Link to={`/chapters/${chapterId}`} className="hover:underline">
              {chapter.data.title}
            </Link>{" "}
            /{" "}
          </>
        )}
        History
      </div>
      <PageHeader
        title="History"
        subtitle="Every run of this chapter, newest first: critiques, revisions, drafts, story bible updates and writer tests, with what came of each."
        actions={
          <Link to={`/chapters/${chapterId}`}>
            <Button size="sm">Back to the chapter</Button>
          </Link>
        }
      />
      <ErrorBanner error={history.error} onRetry={() => history.refetch()} />
      {first && (
        <div className="mb-4 grid gap-3 sm:grid-cols-3">
          <Stat label={kind ? `${RUN_KIND_LABELS[kind]} runs` : "Runs"} value={first.totals.runs.toLocaleString()} />
          <Stat
            label="Cost"
            value={fmtCost(first.totals.cost_usd, first.totals.cost_estimated)}
            hint={first.totals.cost_estimated ? "Part of this was estimated from token counts and gateway prices; streamed replies carry no cost header." : "From the gateway's cost header."}
          />
          <Stat label="Tokens" value={`${fmtTokens(first.totals.prompt_tokens)} in · ${fmtTokens(first.totals.completion_tokens)} out`} />
        </div>
      )}
      <div className="mb-4 flex flex-wrap items-center gap-1.5">
        <Chip active={!kind} onClick={() => setKind(undefined)}>
          All ({allCount})
        </Chip>
        {KINDS.map((k) => (
          <Chip key={k} active={kind === k} onClick={() => setKind(k)} disabled={kindCount(k) === 0}>
            {RUN_KIND_LABELS[k]} ({kindCount(k)})
          </Chip>
        ))}
      </div>
      {history.isLoading && <Spinner />}
      {!history.isLoading && items.length === 0 && (
        <EmptyState title={kind ? `No ${RUN_KIND_LABELS[kind].toLowerCase()} runs yet` : "No runs yet"}>Convene the Guild or ask a co-writer from the chapter page; every run lands here.</EmptyState>
      )}
      <ol className="space-y-2">
        {items.map((it) => (
          <HistoryRow key={it.run.id} item={it} />
        ))}
      </ol>
      {history.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <Button onClick={() => history.fetchNextPage()} loading={history.isFetchingNextPage}>
            Load older runs
          </Button>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-stone-200 bg-white px-4 py-3 shadow-sm" title={hint}>
      <div className="text-xs font-semibold uppercase tracking-wide text-stone-500">{label}</div>
      <div className="mt-1 text-lg font-semibold text-stone-900">{value}</div>
    </div>
  );
}

function Chip({ active, disabled, onClick, children }: { active: boolean; disabled?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${active ? "border-stone-900 bg-stone-900 text-white" : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"}`}
    >
      {children}
    </button>
  );
}

function HistoryRow({ item }: { item: RunHistoryItem }) {
  const [open, setOpen] = useState(false);
  const run = item.run;
  const calls = useRunCalls(open ? run.id : null);
  return (
    <li className="rounded-lg border border-stone-200 bg-white shadow-sm">
      <button type="button" onClick={() => setOpen((v) => !v)} className="flex w-full flex-wrap items-start gap-x-3 gap-y-1 px-4 py-3 text-left hover:bg-stone-50" aria-expanded={open}>
        <Badge tone="blue">{RUN_KIND_LABELS[run.kind] ?? run.kind}</Badge>
        <Badge tone={statusTone[run.status] ?? "stone"}>{run.status}</Badge>
        <span className="min-w-0 flex-1 text-sm text-stone-800">{item.summary}</span>
        <span className="text-xs text-stone-500" title={fmtDateTime(run.created_at)}>
          {timeAgo(run.created_at)}
        </span>
      </button>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-stone-100 px-4 py-1.5 text-xs text-stone-500">
        {item.writers.length > 0 && <span>{item.writers.join(", ")}</span>}
        <span>
          {item.model_calls} call{item.model_calls === 1 ? "" : "s"}
        </span>
        <span>
          {fmtTokens(run.prompt_tokens)} in · {fmtTokens(run.completion_tokens)} out
        </span>
        <span>{fmtCost(run.cost_usd, run.cost_estimated)}</span>
        {run.started_at && run.finished_at && <span>{fmtDuration(run.started_at, run.finished_at)}</span>}
      </div>
      {open && (
        <div className="border-t border-stone-100 px-4 py-3 text-sm">
          <ErrorBanner error={calls.error} />
          {calls.isLoading && <Spinner />}
          {calls.data && calls.data.length === 0 && <p className="text-xs text-stone-500">This run made no gateway calls.</p>}
          {calls.data && calls.data.length > 0 && (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-stone-500">
                  <th className="py-1 pr-3 font-medium">Writer</th>
                  <th className="py-1 pr-3 font-medium">Generation</th>
                  <th className="py-1 pr-3 font-medium">Alias</th>
                  <th className="py-1 pr-3 text-right font-medium">Tokens</th>
                  <th className="py-1 pr-3 text-right font-medium">Cost</th>
                  <th className="py-1 pr-3 text-right font-medium">Time</th>
                  <th className="py-1 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {calls.data.map((c) => (
                  <tr key={c.id} className="border-t border-stone-100 text-stone-700">
                    <td className="py-1 pr-3">{c.writer_name ?? "—"}</td>
                    <td className="py-1 pr-3 font-mono">{c.generation_name}</td>
                    <td className="py-1 pr-3 font-mono">{c.model_alias}</td>
                    <td className="py-1 pr-3 text-right">
                      {fmtTokens(c.prompt_tokens)} / {fmtTokens(c.completion_tokens)}
                    </td>
                    <td className="py-1 pr-3 text-right">{fmtCost(c.cost_usd, c.cost_estimated)}</td>
                    <td className="py-1 pr-3 text-right">{c.latency_ms.toLocaleString()} ms</td>
                    <td className="py-1">{c.status === "ok" ? <Badge tone="green">ok</Badge> : <Badge tone="red">{c.error || "error"}</Badge>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {run.error && <p className="mt-2 rounded border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-800">{run.error}</p>}
        </div>
      )}
    </li>
  );
}
