import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import type { WriterStats } from "../api/client";
import { useProjects, useWriterStats, type StatsPeriod } from "../api/hooks";
import { Badge, Button, EmptyState, ErrorBanner, PageHeader, Select, Spinner } from "../components/ui";
import { fmtCost, fmtTokens, RUN_KIND_LABELS } from "../lib/format";

type SortKey = "cost" | "issues" | "drafts" | "name";

const PERIODS: { value: StatsPeriod; label: string }[] = [
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
  { value: "all", label: "All time" },
];

function pct(rate: number | undefined): string {
  return rate === undefined ? "—" : `${Math.round(rate * 100)}%`;
}

/** StatsPage shows each writer's acceptance rates and cost for a period and project. */
export default function StatsPage() {
  const [period, setPeriod] = useState<StatsPeriod>("30d");
  const [projectId, setProjectId] = useState("");
  const [sort, setSort] = useState<SortKey>("cost");
  const projects = useProjects();
  const stats = useWriterStats(period, projectId || undefined);

  const rows = useMemo(() => {
    const list = [...(stats.data?.writers ?? [])];
    const by: Record<SortKey, (a: WriterStats, b: WriterStats) => number> = {
      cost: (a, b) => b.cost_usd - a.cost_usd,
      issues: (a, b) => (b.issues.acceptance_rate ?? -1) - (a.issues.acceptance_rate ?? -1) || b.issues.listed - a.issues.listed,
      drafts: (a, b) => (b.drafts.acceptance_rate ?? -1) - (a.drafts.acceptance_rate ?? -1) || b.drafts.count - a.drafts.count,
      name: (a, b) => a.name.localeCompare(b.name),
    };
    return list.sort(by[sort]);
  }, [stats.data, sort]);
  const active = rows.filter((w) => w.calls > 0 || w.issues.listed > 0 || w.drafts.count > 0);
  const idle = rows.filter((w) => !active.includes(w));

  return (
    <div>
      <PageHeader
        title="Writer stats"
        subtitle="How often you accept each writer's notes and drafts, and what each one costs. An issue on the editor-in-chief's list counts for every critic who raised it."
        actions={
          <Link to="/writers">
            <Button size="sm">Writers</Button>
          </Link>
        }
      />
      <div className="mb-4 flex flex-wrap items-end gap-3">
        <label className="block text-sm">
          <span className="mb-1 block font-medium text-stone-700">Period</span>
          <Select value={period} onChange={(e) => setPeriod(e.target.value as StatsPeriod)} className="w-44">
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium text-stone-700">Project</span>
          <Select value={projectId} onChange={(e) => setProjectId(e.target.value)} className="w-56">
            <option value="">All projects</option>
            {(projects.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        </label>
        <label className="block text-sm">
          <span className="mb-1 block font-medium text-stone-700">Sort by</span>
          <Select value={sort} onChange={(e) => setSort(e.target.value as SortKey)} className="w-48">
            <option value="cost">Cost</option>
            <option value="issues">Issue acceptance</option>
            <option value="drafts">Draft acceptance</option>
            <option value="name">Name</option>
          </Select>
        </label>
      </div>
      <ErrorBanner error={stats.error} onRetry={() => stats.refetch()} />
      {stats.isLoading && <Spinner />}
      {stats.data && (
        <>
          <div className="mb-4 grid gap-3 sm:grid-cols-3">
            <Stat label="Gateway calls" value={stats.data.totals.calls.toLocaleString()} />
            <Stat label="Cost" value={fmtCost(stats.data.totals.cost_usd, stats.data.totals.cost_estimated)} hint={stats.data.totals.cost_estimated ? "Part of this was estimated from token counts and gateway prices." : "From the gateway's cost header."} />
            <Stat label="Tokens" value={`${fmtTokens(stats.data.totals.prompt_tokens)} in · ${fmtTokens(stats.data.totals.completion_tokens)} out`} />
          </div>
          {active.length === 0 && <EmptyState title="Nothing in this period">Convene the Guild or ask for a draft and the numbers appear here.</EmptyState>}
          {active.length > 0 && <StatsTable rows={active} />}
          {idle.length > 0 && (
            <details className="mt-4 text-sm text-stone-500">
              <summary className="cursor-pointer">
                {idle.length} writer{idle.length === 1 ? "" : "s"} with nothing in this period
              </summary>
              <p className="mt-1">{idle.map((w) => w.name).join(", ")}</p>
            </details>
          )}
        </>
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

function StatsTable({ rows }: { rows: WriterStats[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-stone-200 bg-white shadow-sm">
      <table className="w-full text-sm">
        <thead className="bg-stone-50 text-left text-xs uppercase tracking-wide text-stone-500">
          <tr>
            <th className="px-3 py-2 font-medium">Writer</th>
            <th className="px-3 py-2 text-right font-medium" title="Issues on the editor-in-chief's list that cite this writer">
              Issues
            </th>
            <th className="px-3 py-2 text-right font-medium">Accepted</th>
            <th className="px-3 py-2 text-right font-medium">Rejected</th>
            <th className="px-3 py-2 text-right font-medium" title="accepted / (accepted + rejected)">
              Rate
            </th>
            <th className="px-3 py-2 text-right font-medium">Drafts</th>
            <th className="px-3 py-2 text-right font-medium" title="Inserted or replaced">
              Used
            </th>
            <th className="px-3 py-2 text-right font-medium" title="used / (used + discarded)">
              Rate
            </th>
            <th className="px-3 py-2 text-right font-medium">Cost</th>
            <th className="px-3 py-2 font-medium">By kind</th>
            <th className="px-3 py-2 text-right font-medium">Tokens</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((w) => (
            <tr key={w.writer_id} className="border-t border-stone-100 align-top">
              <td className="px-3 py-2">
                <div className="font-medium text-stone-900">{w.name}</div>
                <div className="flex flex-wrap items-center gap-1 text-[11px] text-stone-400">
                  <span className="font-mono">{w.model_alias}</span>
                  {w.is_system && <Badge tone="stone">system</Badge>}
                  {!w.enabled && <Badge tone="stone">disabled</Badge>}
                </div>
              </td>
              <td className="px-3 py-2 text-right tabular-nums">{w.issues.listed || "—"}</td>
              <td className="px-3 py-2 text-right tabular-nums text-green-800">{w.issues.listed ? w.issues.accepted : "—"}</td>
              <td className="px-3 py-2 text-right tabular-nums text-red-800">{w.issues.listed ? w.issues.rejected : "—"}</td>
              <td className="px-3 py-2 text-right tabular-nums font-medium">{pct(w.issues.acceptance_rate)}</td>
              <td className="px-3 py-2 text-right tabular-nums">{w.drafts.count || "—"}</td>
              <td className="px-3 py-2 text-right tabular-nums text-green-800">{w.drafts.count ? w.drafts.used : "—"}</td>
              <td className="px-3 py-2 text-right tabular-nums font-medium">{pct(w.drafts.acceptance_rate)}</td>
              <td className="px-3 py-2 text-right tabular-nums">{w.calls ? fmtCost(w.cost_usd, w.cost_estimated) : "—"}</td>
              <td className="px-3 py-2">
                <div className="flex flex-wrap gap-1">
                  {w.by_kind.map((k) => (
                    <span key={k.kind} className="rounded bg-stone-100 px-1.5 py-0.5 text-[11px] text-stone-700" title={`${k.calls} call${k.calls === 1 ? "" : "s"}`}>
                      {RUN_KIND_LABELS[k.kind] ?? k.kind} {fmtCost(k.cost_usd, k.cost_estimated)}
                    </span>
                  ))}
                </div>
              </td>
              <td className="px-3 py-2 text-right tabular-nums text-stone-600">{w.calls ? `${fmtTokens(w.prompt_tokens)} / ${fmtTokens(w.completion_tokens)}` : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
