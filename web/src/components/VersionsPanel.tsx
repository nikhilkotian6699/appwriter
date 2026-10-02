import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type Chapter, type ChapterVersion, type ChapterVersionSummary } from "../api/client";
import { keys, useVersions } from "../api/hooks";
import { Badge, Button, ConfirmDialog, Dialog, ErrorBanner, Input, Pending } from "./ui";
import { fmtDateTime, timeAgo, VERSION_KIND_LABELS } from "../lib/format";

type Props = {
  chapterId: string;
  onRestored: (chapter: Chapter) => void;
};

/** VersionsPanel lists snapshots and lets the author view or restore one. */
export function VersionsPanel({ chapterId, onRestored }: Props) {
  const versions = useVersions(chapterId);
  const qc = useQueryClient();
  const [label, setLabel] = useState("");
  const [viewing, setViewing] = useState<ChapterVersion | null>(null);
  const [restoring, setRestoring] = useState<ChapterVersionSummary | null>(null);

  const snapshot = useMutation({
    mutationFn: () => call(api.POST("/api/chapters/{chapterId}/versions", { params: { path: { chapterId } }, body: { label } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.versions(chapterId) });
      setLabel("");
    },
  });

  const view = useMutation({
    mutationFn: (v: ChapterVersionSummary) =>
      call(api.GET("/api/chapters/{chapterId}/versions/{versionId}", { params: { path: { chapterId, versionId: v.id } } })),
    onSuccess: (full) => setViewing(full),
  });

  const restore = useMutation({
    mutationFn: (v: ChapterVersionSummary) =>
      call(api.POST("/api/chapters/{chapterId}/versions/{versionId}/restore", { params: { path: { chapterId, versionId: v.id } } })),
    onSuccess: (chapter) => {
      qc.invalidateQueries({ queryKey: keys.versions(chapterId) });
      setRestoring(null);
      onRestored(chapter);
    },
  });

  const tone = (kind: string) => (kind === "manual" ? "amber" : kind === "autosave" ? "stone" : "blue");

  return (
    <aside className="rounded-lg border border-stone-200 bg-white shadow-sm">
      <div className="border-b border-stone-200 px-4 py-3">
        <h2 className="font-semibold text-stone-900">Versions</h2>
        <p className="mt-1 text-xs text-stone-500">Autosave keeps a snapshot at most every few minutes; take one by hand before a big change.</p>
        <form
          className="mt-3 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            snapshot.mutate();
          }}
        >
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label, e.g. before rewrite" maxLength={200} />
          <Button variant="primary" size="sm" type="submit" loading={snapshot.isPending}>
            Snapshot
          </Button>
        </form>
        <div className="mt-2">
          <ErrorBanner error={snapshot.error || view.error || restore.error} />
        </div>
      </div>
      {versions.isPending && <Pending paused={versions.isPaused} className="p-4" />}
      <ul className="max-h-[70vh] divide-y divide-stone-100 overflow-y-auto">
        {versions.data?.length === 0 && <li className="px-4 py-3 text-sm text-stone-500">No snapshots yet. The first save takes one.</li>}
        {versions.data?.map((v) => (
          <li key={v.id} className="px-4 py-2.5 text-sm">
            <div className="flex items-center justify-between gap-2">
              <span className="flex items-center gap-2">
                <Badge tone={tone(v.kind)}>{VERSION_KIND_LABELS[v.kind] ?? v.kind}</Badge>
                <span className="text-stone-700" title={fmtDateTime(v.created_at)}>
                  {timeAgo(v.created_at)}
                </span>
              </span>
              <span className="text-xs text-stone-400">{v.content_length.toLocaleString()} chars</span>
            </div>
            {v.label && <div className="mt-0.5 text-stone-800">{v.label}</div>}
            <div className="mt-1 flex gap-1">
              <Button size="sm" variant="ghost" onClick={() => view.mutate(v)}>
                View
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setRestoring(v)}>
                Restore
              </Button>
            </div>
          </li>
        ))}
      </ul>

      <Dialog open={viewing !== null} title={viewing?.label || (viewing ? VERSION_KIND_LABELS[viewing.kind] : "")} onClose={() => setViewing(null)} wide>
        {viewing && (
          <div>
            <div className="mb-2 text-xs text-stone-500">{fmtDateTime(viewing.created_at)}</div>
            <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-stone-50 p-3 font-serif text-sm text-stone-800">{viewing.content_md || "(empty)"}</pre>
          </div>
        )}
      </Dialog>

      <ConfirmDialog
        open={restoring !== null}
        title="Restore this snapshot?"
        message={
          <p>
            The current text is saved as a "Before restore" snapshot first, so nothing is lost. The editor then shows the snapshot from{" "}
            {restoring && fmtDateTime(restoring.created_at)}.
          </p>
        }
        confirmLabel="Restore"
        danger={false}
        busy={restore.isPending}
        onConfirm={() => restoring && restore.mutate(restoring)}
        onClose={() => setRestoring(null)}
      />
    </aside>
  );
}
