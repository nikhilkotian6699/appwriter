import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type ChapterSummary } from "../api/client";
import { keys, useChapters, useProject } from "../api/hooks";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorBanner, Field, Input, PageHeader, Spinner, Textarea } from "../components/ui";
import { timeAgo } from "../lib/format";

export default function ProjectPage() {
  const { projectId = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const project = useProject(projectId);
  const chapters = useChapters(projectId);

  const [newTitle, setNewTitle] = useState("");
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [chapterToDelete, setChapterToDelete] = useState<ChapterSummary | null>(null);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: keys.chapters(projectId) });
    qc.invalidateQueries({ queryKey: keys.projects });
  };

  const createChapter = useMutation({
    mutationFn: () => call(api.POST("/api/projects/{projectId}/chapters", { params: { path: { projectId } }, body: { title: newTitle } })),
    onSuccess: (ch) => {
      invalidate();
      setNewTitle("");
      navigate(`/chapters/${ch.id}`);
    },
  });

  const updateProject = useMutation({
    mutationFn: () => call(api.PUT("/api/projects/{projectId}", { params: { path: { projectId } }, body: { name: editName, description: editDesc } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.project(projectId) });
      qc.invalidateQueries({ queryKey: keys.projects });
      setEditOpen(false);
    },
  });

  const deleteProject = useMutation({
    mutationFn: () => call(api.DELETE("/api/projects/{projectId}", { params: { path: { projectId } } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.projects });
      navigate("/projects");
    },
  });

  const moveChapter = useMutation({
    mutationFn: ({ ch, position }: { ch: ChapterSummary; position: number }) =>
      call(api.PUT("/api/chapters/{chapterId}", { params: { path: { chapterId: ch.id } }, body: { title: ch.title, position } })),
    onSuccess: invalidate,
  });

  const deleteChapter = useMutation({
    mutationFn: (ch: ChapterSummary) => call(api.DELETE("/api/chapters/{chapterId}", { params: { path: { chapterId: ch.id } } })),
    onSuccess: () => {
      invalidate();
      setChapterToDelete(null);
    },
  });

  if (project.isLoading) return <Spinner />;
  if (project.error) return <ErrorBanner error={project.error} onRetry={() => project.refetch()} />;
  const p = project.data!;
  const list = chapters.data ?? [];

  // Swap positions with the neighbour so both stay unique.
  const move = (index: number, dir: -1 | 1) => {
    const a = list[index];
    const b = list[index + dir];
    if (!a || !b) return;
    moveChapter.mutate({ ch: a, position: b.position });
    moveChapter.mutate({ ch: b, position: a.position });
  };

  return (
    <div>
      <div className="mb-2 text-sm text-stone-500">
        <Link to="/projects" className="hover:underline">
          Projects
        </Link>{" "}
        / {p.name}
      </div>
      <PageHeader
        title={p.name}
        subtitle={p.description || "No description yet."}
        actions={
          <>
            <Link to={`/projects/${projectId}/bible`}>
              <Button>Story bible</Button>
            </Link>
            <Button
              onClick={() => {
                setEditName(p.name);
                setEditDesc(p.description);
                setEditOpen(true);
              }}
            >
              Edit
            </Button>
            <Button variant="danger" onClick={() => setDeleteOpen(true)}>
              Delete project
            </Button>
          </>
        }
      />

      <section className="rounded-lg border border-stone-200 bg-white shadow-sm">
        <div className="flex items-center justify-between border-b border-stone-200 px-4 py-3">
          <h2 className="font-semibold text-stone-900">Chapters</h2>
          <form
            className="flex items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (newTitle.trim()) createChapter.mutate();
            }}
          >
            <Input value={newTitle} onChange={(e) => setNewTitle(e.target.value)} placeholder="New chapter title" className="w-64" maxLength={300} />
            <Button variant="primary" type="submit" loading={createChapter.isPending} disabled={!newTitle.trim()}>
              Add chapter
            </Button>
          </form>
        </div>
        <ErrorBanner error={createChapter.error || chapters.error || moveChapter.error} />
        {chapters.isLoading && (
          <div className="p-4">
            <Spinner />
          </div>
        )}
        {list.length === 0 && !chapters.isLoading && (
          <div className="p-4">
            <EmptyState title="No chapters yet">Add a chapter, then write or paste your text into the editor.</EmptyState>
          </div>
        )}
        <ul className="divide-y divide-stone-100">
          {list.map((ch, i) => (
            <li key={ch.id} className="flex items-center gap-3 px-4 py-3">
              <span className="w-8 text-right text-sm tabular-nums text-stone-400">{i + 1}.</span>
              <Link to={`/chapters/${ch.id}`} className="flex-1 font-medium text-stone-900 hover:underline">
                {ch.title}
              </Link>
              <span className="text-xs text-stone-500">
                {ch.content_length.toLocaleString()} characters · {timeAgo(ch.updated_at)}
              </span>
              <div className="flex items-center gap-1">
                <Button size="sm" variant="ghost" onClick={() => move(i, -1)} disabled={i === 0} aria-label="Move up">
                  ↑
                </Button>
                <Button size="sm" variant="ghost" onClick={() => move(i, 1)} disabled={i === list.length - 1} aria-label="Move down">
                  ↓
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setChapterToDelete(ch)} aria-label="Delete chapter">
                  Delete
                </Button>
              </div>
            </li>
          ))}
        </ul>
      </section>

      <Dialog open={editOpen} title="Edit project" onClose={() => setEditOpen(false)}>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            updateProject.mutate();
          }}
        >
          <Field label="Name">
            <Input value={editName} onChange={(e) => setEditName(e.target.value)} required maxLength={200} />
          </Field>
          <Field label="Description">
            <Textarea value={editDesc} onChange={(e) => setEditDesc(e.target.value)} rows={3} maxLength={5000} />
          </Field>
          <ErrorBanner error={updateProject.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={() => setEditOpen(false)}>Cancel</Button>
            <Button variant="primary" type="submit" loading={updateProject.isPending}>
              Save
            </Button>
          </div>
        </form>
      </Dialog>

      <ConfirmDialog
        open={deleteOpen}
        title="Delete this project?"
        message={
          <p>
            This removes <strong>{p.name}</strong> with every chapter, version and story bible entry in it. Runs and their cost stay in the history.
          </p>
        }
        confirmLabel="Delete project"
        typeToConfirm={p.name}
        busy={deleteProject.isPending}
        onConfirm={() => deleteProject.mutate()}
        onClose={() => setDeleteOpen(false)}
      />
      <ConfirmDialog
        open={chapterToDelete !== null}
        title="Delete this chapter?"
        message={
          <p>
            <strong>{chapterToDelete?.title}</strong> and its version history will be removed.
          </p>
        }
        confirmLabel="Delete chapter"
        busy={deleteChapter.isPending}
        onConfirm={() => chapterToDelete && deleteChapter.mutate(chapterToDelete)}
        onClose={() => setChapterToDelete(null)}
      />
    </div>
  );
}
