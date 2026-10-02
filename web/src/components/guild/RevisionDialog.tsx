import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type Chapter, type DiffHunk, type DiffOp, type Revision } from "../../api/client";
import { keys } from "../../api/hooks";
import { Badge, Button, Dialog, ErrorBanner } from "../ui";

type Props = {
  revision: Revision | null;
  open: boolean;
  onClose: () => void;
  onApplied: (chapter: Chapter, revision: Revision, bibleRunId: string | null) => void;
  onDiscarded: (revision: Revision) => void;
};

/** RevisionDialog shows the lead writer's changes side by side and lets the author apply all, some, or none. */
export function RevisionDialog({ revision, open, onClose, onApplied, onDiscarded }: Props) {
  const qc = useQueryClient();
  const [chosen, setChosen] = useState<Set<number>>(new Set());
  useEffect(() => {
    if (revision && open) setChosen(new Set(revision.hunks.map((h) => h.index)));
  }, [revision, open]);

  const apply = useMutation({
    mutationFn: (indexes: number[] | null) =>
      call(api.POST("/api/revisions/{revisionId}/apply", { params: { path: { revisionId: revision!.id } }, body: indexes ? { hunk_indexes: indexes } : {} })),
    onSuccess: (res) => {
      qc.setQueryData(keys.revision(res.revision.id), res.revision);
      qc.invalidateQueries({ queryKey: keys.chapterRevisions(res.chapter.id) });
      qc.invalidateQueries({ queryKey: keys.versions(res.chapter.id) });
      onApplied(res.chapter, res.revision, res.bible_run_id ?? null);
    },
  });
  const discard = useMutation({
    mutationFn: () => call(api.POST("/api/revisions/{revisionId}/discard", { params: { path: { revisionId: revision!.id } } })),
    onSuccess: (rev) => {
      qc.setQueryData(keys.revision(rev.id), rev);
      qc.invalidateQueries({ queryKey: keys.chapterRevisions(rev.chapter_id) });
      onDiscarded(rev);
    },
  });

  const busy = apply.isPending || discard.isPending;
  const canApply = !!revision && revision.status === "proposed" && !revision.stale && revision.hunks.length > 0;
  const allChosen = !!revision && chosen.size === revision.hunks.length;

  if (!revision) return null;
  const toggle = (i: number) =>
    setChosen((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });

  return (
    <Dialog open={open} title="Review the revision" onClose={onClose} wide>
      <div className="flex flex-wrap items-center gap-2 text-sm text-stone-600">
        <RevisionStatusBadge revision={revision} />
        <span>
          {revision.stats.hunks} change{revision.stats.hunks === 1 ? "" : "s"} · {revision.stats.words_added} words added · {revision.stats.words_removed} removed
        </span>
      </div>
      {revision.stale && revision.status === "proposed" && (
        <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          The chapter changed after this revision was proposed, so it cannot be applied any more. Discard it and ask for a new revision.
        </div>
      )}
      {revision.skipped.length > 0 && (
        <div className="mt-3 rounded-md border border-stone-200 bg-stone-50 px-3 py-2 text-xs text-stone-600">
          <div className="font-medium text-stone-700">Not applied</div>
          <ul className="mt-1 list-disc pl-4">
            {revision.skipped.map((s) => (
              <li key={s.issue_id}>
                <span className="font-serif italic">“{s.quote}”</span>: {s.reason}.
              </li>
            ))}
          </ul>
        </div>
      )}
      {revision.hunks.length === 0 && <p className="mt-4 text-sm text-stone-600">The lead writer changed nothing.</p>}
      <ol className="mt-4 space-y-3">
        {revision.hunks.map((h) => (
          <HunkView key={h.index} hunk={h} checked={chosen.has(h.index)} selectable={canApply} onToggle={() => toggle(h.index)} />
        ))}
      </ol>
      <ErrorBanner error={apply.error ?? discard.error} />
      <div className="mt-5 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {revision.status === "proposed" && (
            <Button variant="danger" onClick={() => discard.mutate()} loading={discard.isPending} disabled={busy}>
              Discard
            </Button>
          )}
          <Button onClick={onClose} disabled={busy}>
            Close
          </Button>
        </div>
        {canApply && (
          <div className="flex items-center gap-2">
            {!allChosen && (
              <Button variant="primary" onClick={() => apply.mutate([...chosen])} loading={apply.isPending} disabled={busy || chosen.size === 0}>
                Accept selected ({chosen.size})
              </Button>
            )}
            <Button variant="primary" onClick={() => apply.mutate(null)} loading={apply.isPending} disabled={busy}>
              Accept all
            </Button>
          </div>
        )}
      </div>
    </Dialog>
  );
}

export function RevisionStatusBadge({ revision }: { revision: Revision }) {
  if (revision.status === "applied") return <Badge tone="green">applied</Badge>;
  if (revision.status === "discarded") return <Badge tone="stone">discarded</Badge>;
  if (revision.stale) return <Badge tone="amber">stale</Badge>;
  return <Badge tone="blue">proposed</Badge>;
}

function HunkView({ hunk, checked, selectable, onToggle }: { hunk: DiffHunk; checked: boolean; selectable: boolean; onToggle: () => void }) {
  const before = useMemo(() => hunk.ops.filter((op) => op.kind !== "insert"), [hunk.ops]);
  const after = useMemo(() => hunk.ops.filter((op) => op.kind !== "delete"), [hunk.ops]);
  return (
    <li className={`rounded-md border ${checked && selectable ? "border-stone-300" : "border-stone-200"} bg-white`}>
      <div className="flex items-center gap-2 border-b border-stone-100 px-3 py-1.5 text-xs text-stone-500">
        {selectable && <input type="checkbox" checked={checked} onChange={onToggle} className="h-4 w-4 accent-stone-900" aria-label={`Apply change ${hunk.index + 1}`} />}
        <span>Change {hunk.index + 1}</span>
      </div>
      <div className="grid gap-0 md:grid-cols-2">
        <Side label="Before" ops={before} context={hunk} />
        <Side label="After" ops={after} context={hunk} right />
      </div>
    </li>
  );
}

function Side({ label, ops, context, right = false }: { label: string; ops: DiffOp[]; context: DiffHunk; right?: boolean }) {
  return (
    <div className={`px-3 py-2 ${right ? "md:border-l md:border-stone-100" : ""}`}>
      <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-stone-400">{label}</div>
      <p className="whitespace-pre-wrap font-serif leading-relaxed text-stone-800">
        <span className="text-stone-400">{context.context_before}</span>
        {ops.map((op, i) => (
          <Mark key={i} op={op} />
        ))}
        <span className="text-stone-400">{context.context_after}</span>
      </p>
    </div>
  );
}

function Mark({ op }: { op: DiffOp }) {
  if (op.kind === "insert") return <mark className="rounded-sm bg-green-100 text-green-900">{op.text}</mark>;
  if (op.kind === "delete") return <del className="rounded-sm bg-red-100 text-red-900">{op.text}</del>;
  return <>{op.text}</>;
}
