import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type BibleEntry, type BibleSection } from "../api/client";
import { keys, useBible, useChapters, useProject, useProjectBibleProposals } from "../api/hooks";
import { BibleProposalsList } from "../components/guild/BibleProposals";
import { Button, ConfirmDialog, Dialog, ErrorBanner, Field, Input, PageHeader, Pending, Select, Textarea } from "../components/ui";

type FieldSpec = { key: string; label: string; multiline?: boolean; help?: string };
type SectionSpec = { key: BibleSection; title: string; blurb: string; titleLabel: string; fields: FieldSpec[]; chapterLink?: boolean };

const SECTIONS: SectionSpec[] = [
  { key: "premise", title: "Premise", blurb: "What the book is about, in a paragraph.", titleLabel: "Title", fields: [{ key: "text", label: "Premise", multiline: true }] },
  {
    key: "character",
    title: "Characters",
    blurb: "Everyone who matters, with how they sound and where they are going.",
    titleLabel: "Name",
    fields: [
      { key: "role", label: "Role", help: "Protagonist, antagonist, mentor, minor…" },
      { key: "voice", label: "Voice", help: "How they speak and think." },
      { key: "arc", label: "Arc", multiline: true },
      { key: "key_facts", label: "Key facts", multiline: true, help: "Age, looks, habits, secrets: what the writers must not contradict." },
    ],
  },
  { key: "setting", title: "Setting and world rules", blurb: "Places, period, technology, magic, and the rules that hold.", titleLabel: "Place or rule", fields: [{ key: "text", label: "Details", multiline: true }] },
  { key: "timeline", title: "Timeline", blurb: "What happened when, before and during the story.", titleLabel: "When", fields: [{ key: "text", label: "What happens", multiline: true }] },
  { key: "style", title: "Tone and style rules", blurb: "Tense, point of view, register, things to avoid.", titleLabel: "Rule", fields: [{ key: "text", label: "Detail", multiline: true }] },
  { key: "chapter_summary", title: "Chapter summaries", blurb: "A few lines per chapter so every writer knows the shape of the whole.", titleLabel: "Chapter", fields: [{ key: "text", label: "Summary", multiline: true }], chapterLink: true },
];

type Draft = { section: BibleSection; title: string; fields: Record<string, string>; chapter_id?: string; id?: string };

export default function BiblePage() {
  const { projectId = "" } = useParams();
  const pendingProposals = useProjectBibleProposals(projectId, "pending");
  const qc = useQueryClient();
  const project = useProject(projectId);
  const bible = useBible(projectId);
  const chapters = useChapters(projectId);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [toDelete, setToDelete] = useState<BibleEntry | null>(null);

  const bySection = useMemo(() => {
    const m = new Map<string, BibleEntry[]>();
    for (const e of bible.data ?? []) {
      const list = m.get(e.section) ?? [];
      list.push(e);
      m.set(e.section, list);
    }
    return m;
  }, [bible.data]);

  const invalidate = () => qc.invalidateQueries({ queryKey: keys.bible(projectId) });

  const save = useMutation({
    mutationFn: (d: Draft) => {
      const body = { section: d.section, title: d.title, fields: d.fields, chapter_id: d.chapter_id || undefined };
      return d.id
        ? call(api.PUT("/api/bible-entries/{entryId}", { params: { path: { entryId: d.id } }, body }))
        : call(api.POST("/api/projects/{projectId}/bible", { params: { path: { projectId } }, body }));
    },
    onSuccess: () => {
      invalidate();
      setDraft(null);
    },
  });

  const remove = useMutation({
    mutationFn: (e: BibleEntry) => call(api.DELETE("/api/bible-entries/{entryId}", { params: { path: { entryId: e.id } } })),
    onSuccess: () => {
      invalidate();
      setToDelete(null);
    },
  });

  const chapterTitle = (id?: string) => chapters.data?.find((c) => c.id === id)?.title;

  return (
    <div>
      <div className="mb-2 text-sm text-stone-500">
        <Link to="/projects" className="hover:underline">
          Projects
        </Link>{" "}
        /{" "}
        <Link to={`/projects/${projectId}`} className="hover:underline">
          {project.data?.name ?? "Project"}
        </Link>{" "}
        / Story bible
      </div>
      <PageHeader title="Story bible" subtitle="Every writer reads the relevant parts of this before critiquing or drafting. Keep it true." />
      {pendingProposals.data && pendingProposals.data.length > 0 && (
        <section className="mb-6 rounded-lg border border-amber-300 bg-amber-50/40 p-4" aria-label="Pending story bible proposals">
          <h2 className="font-semibold text-stone-900">
            {pendingProposals.data.length} proposal{pendingProposals.data.length === 1 ? "" : "s"} from the bible keeper
          </h2>
          <p className="mt-1 text-xs text-stone-600">After a revision, the bible keeper suggests what the chapter changed. Nothing is saved until you approve it.</p>
          <BibleProposalsList proposals={pendingProposals.data} projectId={projectId} />
        </section>
      )}
      <ErrorBanner error={bible.error} onRetry={() => bible.refetch()} />
      {bible.isPending && <Pending paused={bible.isPaused} />}

      <div className="space-y-6">
        {SECTIONS.map((spec) => {
          const entries = bySection.get(spec.key) ?? [];
          return (
            <section key={spec.key} className="rounded-lg border border-stone-200 bg-white shadow-sm">
              <div className="flex items-start justify-between gap-3 border-b border-stone-200 px-4 py-3">
                <div>
                  <h2 className="font-semibold text-stone-900">{spec.title}</h2>
                  <p className="text-xs text-stone-500">{spec.blurb}</p>
                </div>
                <Button size="sm" onClick={() => setDraft({ section: spec.key, title: "", fields: {} })}>
                  Add
                </Button>
              </div>
              {entries.length === 0 ? (
                <div className="px-4 py-3 text-sm text-stone-400">Nothing here yet.</div>
              ) : (
                <ul className="divide-y divide-stone-100">
                  {entries.map((e) => (
                    <li key={e.id} className="px-4 py-3">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0 flex-1">
                          <div className="font-medium text-stone-900">
                            {e.title || (spec.chapterLink ? chapterTitle(e.chapter_id) : "") || <span className="text-stone-400">Untitled</span>}
                            {spec.chapterLink && e.chapter_id && e.title !== chapterTitle(e.chapter_id) && (
                              <span className="ml-2 text-xs text-stone-500">({chapterTitle(e.chapter_id) ?? "chapter removed"})</span>
                            )}
                          </div>
                          <dl className="mt-1 space-y-1 text-sm">
                            {spec.fields
                              .filter((f) => e.fields[f.key])
                              .map((f) => (
                                <div key={f.key} className="flex gap-2">
                                  {spec.fields.length > 1 && <dt className="w-20 shrink-0 text-stone-500">{f.label}</dt>}
                                  <dd className="whitespace-pre-wrap text-stone-800">{e.fields[f.key]}</dd>
                                </div>
                              ))}
                          </dl>
                        </div>
                        <div className="flex shrink-0 gap-1">
                          <Button size="sm" variant="ghost" onClick={() => setDraft({ id: e.id, section: e.section, title: e.title, fields: { ...e.fields }, chapter_id: e.chapter_id })}>
                            Edit
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setToDelete(e)}>
                            Delete
                          </Button>
                        </div>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>

      <Dialog open={draft !== null} title={draft?.id ? "Edit entry" : "New entry"} onClose={() => setDraft(null)}>
        {draft && (
          <EntryForm
            draft={draft}
            spec={SECTIONS.find((s) => s.key === draft.section)!}
            chapters={chapters.data ?? []}
            busy={save.isPending}
            error={save.error}
            onChange={setDraft}
            onSubmit={() => save.mutate(draft)}
            onCancel={() => setDraft(null)}
          />
        )}
      </Dialog>

      <ConfirmDialog
        open={toDelete !== null}
        title="Delete this entry?"
        message={<p>"{toDelete?.title || "Untitled"}" will be removed from the story bible.</p>}
        confirmLabel="Delete"
        busy={remove.isPending}
        onConfirm={() => toDelete && remove.mutate(toDelete)}
        onClose={() => setToDelete(null)}
      />
    </div>
  );
}

function EntryForm({
  draft,
  spec,
  chapters,
  busy,
  error,
  onChange,
  onSubmit,
  onCancel,
}: {
  draft: Draft;
  spec: SectionSpec;
  chapters: { id: string; title: string }[];
  busy: boolean;
  error: unknown;
  onChange: (d: Draft) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  return (
    <form
      className="space-y-3"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      {spec.chapterLink && (
        <Field label="Chapter">
          <Select
            value={draft.chapter_id ?? ""}
            onChange={(e) => {
              const id = e.target.value || undefined;
              const t = chapters.find((c) => c.id === id)?.title ?? "";
              onChange({ ...draft, chapter_id: id, title: draft.title || t });
            }}
          >
            <option value="">No chapter</option>
            {chapters.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </Select>
        </Field>
      )}
      <Field label={spec.titleLabel}>
        <Input value={draft.title} onChange={(e) => onChange({ ...draft, title: e.target.value })} maxLength={300} autoFocus={!spec.chapterLink} />
      </Field>
      {spec.fields.map((f) => (
        <Field key={f.key} label={f.label} help={f.help}>
          {f.multiline ? (
            <Textarea rows={4} value={draft.fields[f.key] ?? ""} onChange={(e) => onChange({ ...draft, fields: { ...draft.fields, [f.key]: e.target.value } })} />
          ) : (
            <Input value={draft.fields[f.key] ?? ""} onChange={(e) => onChange({ ...draft, fields: { ...draft.fields, [f.key]: e.target.value } })} />
          )}
        </Field>
      ))}
      <ErrorBanner error={error} />
      <div className="flex justify-end gap-2">
        <Button onClick={onCancel}>Cancel</Button>
        <Button variant="primary" type="submit" loading={busy}>
          Save
        </Button>
      </div>
    </form>
  );
}
