export function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const s = Math.round(diff / 1000);
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  if (d < 30) return `${d} d ago`;
  return fmtDateTime(iso);
}

export function fmtCost(cost: number | undefined | null, estimated: boolean, known = true): string {
  if (!known || cost === undefined || cost === null) return "cost unknown";
  const s = cost < 0.01 ? `$${cost.toFixed(5)}` : `$${cost.toFixed(4)}`;
  return estimated ? `${s} est.` : s;
}

export function fmtTokens(n: number): string {
  return n.toLocaleString();
}

export function wordCount(text: string): number {
  const t = text.trim();
  return t ? t.split(/\s+/).length : 0;
}

/** slugify mirrors the server's rule so the form can preview the slug. */
export function slugify(name: string): string {
  const s = name
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return s || "writer";
}

export const SECTION_LABELS: Record<string, string> = {
  premise: "Premise",
  character: "Characters",
  setting: "Setting and world rules",
  timeline: "Timeline",
  style: "Tone and style rules",
  chapter_summary: "Chapter summaries",
};

export const VERSION_KIND_LABELS: Record<string, string> = {
  autosave: "Autosave",
  manual: "Snapshot",
  pre_revision: "Before revision",
  pre_restore: "Before restore",
};

export const RUN_KIND_LABELS: Record<string, string> = {
  critique: "Critique",
  revision: "Revision",
  bible_update: "Bible update",
  cowrite: "Co-write",
  compare: "Compare",
  writer_test: "Writer test",
};

export function fmtDuration(startIso: string | undefined, endIso: string | undefined): string {
  if (!startIso || !endIso) return "";
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (ms < 0) return "";
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)} s`;
  const m = Math.floor(s / 60);
  return `${m} min ${Math.round(s - m * 60)} s`;
}
