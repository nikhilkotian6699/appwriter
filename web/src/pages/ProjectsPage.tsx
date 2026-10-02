import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { keys, useProjects } from "../api/hooks";
import { Button, Dialog, EmptyState, ErrorBanner, Field, Input, PageHeader, Pending, Textarea } from "../components/ui";
import { timeAgo } from "../lib/format";

export default function ProjectsPage() {
  const projects = useProjects();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const create = useMutation({
    mutationFn: () => call(api.POST("/api/projects", { body: { name, description } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.projects });
      setOpen(false);
      setName("");
      setDescription("");
    },
  });

  return (
    <div>
      <PageHeader
        title="Projects"
        subtitle="One project is one novel. Each holds its chapters and its story bible."
        actions={
          <Button variant="primary" onClick={() => setOpen(true)}>
            New project
          </Button>
        }
      />
      <ErrorBanner error={projects.error} onRetry={() => projects.refetch()} />
      {projects.isPending && <Pending paused={projects.isPaused} />}
      {projects.data && projects.data.length === 0 && (
        <EmptyState title="No projects yet">Start with "New project", then add a chapter and paste or write your text.</EmptyState>
      )}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {projects.data?.map((p) => (
          <Link key={p.id} to={`/projects/${p.id}`} className="block rounded-lg border border-stone-200 bg-white p-4 shadow-sm transition hover:border-stone-400">
            <div className="font-semibold text-stone-900">{p.name}</div>
            {p.description && <div className="mt-1 line-clamp-2 text-sm text-stone-600">{p.description}</div>}
            <div className="mt-3 text-xs text-stone-500">
              {p.chapter_count} chapter{p.chapter_count === 1 ? "" : "s"} · updated {timeAgo(p.updated_at)}
            </div>
          </Link>
        ))}
      </div>

      <Dialog open={open} title="New project" onClose={() => setOpen(false)}>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus required maxLength={200} placeholder="The working title" />
          </Field>
          <Field label="Description" help="Optional. A line or two about the book.">
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} maxLength={5000} />
          </Field>
          <ErrorBanner error={create.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={() => setOpen(false)}>Cancel</Button>
            <Button variant="primary" type="submit" loading={create.isPending} disabled={!name.trim()}>
              Create
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}
