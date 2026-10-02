import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type BibleProposal } from "../../api/client";
import { keys } from "../../api/hooks";
import { Badge, Button, ErrorBanner, Field, Input, Textarea } from "../ui";
import { SECTION_LABELS } from "../../lib/format";

const actionTone: Record<string, "green" | "blue" | "red"> = { add: "green", update: "blue", delete: "red" };
const actionLabel: Record<string, string> = { add: "new entry", update: "update", delete: "delete" };

/** BibleProposalsList shows the bible keeper's proposals with approve, edit-and-approve and reject. */
export function BibleProposalsList({ proposals, projectId, compact = false }: { proposals: BibleProposal[]; projectId: string; compact?: boolean }) {
  const qc = useQueryClient();
  const decide = useMutation({
    mutationFn: (v: { id: string; decision: "approved" | "rejected"; title?: string; fields?: Record<string, string> }) =>
      call(api.PUT("/api/bible-proposals/{proposalId}", { params: { path: { proposalId: v.id } }, body: { decision: v.decision, title: v.title, fields: v.fields } })),
    onSuccess: (updated) => {
      qc.setQueryData(keys.runProposals(updated.run_id), (old: BibleProposal[] | undefined) => (old ? old.map((p) => (p.id === updated.id ? updated : p)) : old));
      qc.invalidateQueries({ queryKey: ["projects", projectId, "proposals"] });
      qc.invalidateQueries({ queryKey: keys.bible(projectId) });
    },
  });
  if (proposals.length === 0) return null;
  return (
    <div>
      <ErrorBanner error={decide.error} />
      <ul className={`space-y-2 ${compact ? "" : "mt-2"}`}>
        {proposals.map((p) => (
          <ProposalCard key={p.id} proposal={p} busy={decide.isPending && decide.variables?.id === p.id} onDecide={(decision, title, fields) => decide.mutate({ id: p.id, decision, title, fields })} />
        ))}
      </ul>
    </div>
  );
}

function ProposalCard({ proposal: p, busy, onDecide }: { proposal: BibleProposal; busy: boolean; onDecide: (decision: "approved" | "rejected", title?: string, fields?: Record<string, string>) => void }) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(p.title);
  const [fields, setFields] = useState<Record<string, string>>(p.fields);
  useEffect(() => {
    if (!editing) {
      setTitle(p.title);
      setFields(p.fields);
    }
  }, [p.title, p.fields, editing]);
  const pending = p.status === "pending";
  const keys = Object.keys(p.fields).sort();
  const before = p.current;
  return (
    <li className={`rounded-md border p-2 text-sm ${p.status === "approved" ? "border-green-300 bg-green-50/40" : p.status === "rejected" ? "border-stone-200 bg-stone-50 opacity-70" : "border-amber-300 bg-amber-50/30"}`}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={actionTone[p.action] ?? "blue"}>{actionLabel[p.action] ?? p.action}</Badge>
        <span className="text-xs text-stone-500">{SECTION_LABELS[p.section] ?? p.section}</span>
        <span className="font-medium text-stone-900">{p.title || "(untitled)"}</span>
        {p.status !== "pending" && (
          <Badge tone={p.status === "approved" ? "green" : "stone"} >
            {p.status}
          </Badge>
        )}
      </div>
      {!editing && (
        <dl className="mt-1.5 space-y-0.5">
          {keys.map((k) => {
            const was = before?.fields?.[k];
            const changed = p.action === "update" && was !== undefined && was !== p.fields[k];
            return (
              <div key={k} className="text-stone-700">
                <dt className="inline text-xs font-semibold uppercase tracking-wide text-stone-400">{k.replace(/_/g, " ")} </dt>
                <dd className="inline">
                  {changed && <span className="mr-1 text-stone-400 line-through">{was}</span>}
                  <span className={p.action === "delete" ? "line-through" : changed ? "text-green-900" : ""}>{p.fields[k]}</span>
                </dd>
              </div>
            );
          })}
          {p.action === "update" && before && before.title !== p.title && (
            <div className="text-xs text-stone-500">
              Title was <span className="line-through">{before.title}</span>
            </div>
          )}
        </dl>
      )}
      <p className="mt-1.5 text-xs italic text-stone-600">Why: {p.rationale}</p>
      {editing && (
        <div className="mt-2 space-y-2">
          <Field label="Title">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={300} />
          </Field>
          {Object.keys(fields)
            .sort()
            .map((k) => (
              <Field key={k} label={k.replace(/_/g, " ")}>
                <Textarea value={fields[k]} onChange={(e) => setFields({ ...fields, [k]: e.target.value })} rows={2} />
              </Field>
            ))}
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="primary"
              loading={busy}
              onClick={() => {
                onDecide("approved", title.trim(), fields);
                setEditing(false);
              }}
            >
              Save and approve
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setEditing(false)} disabled={busy}>
              Cancel
            </Button>
          </div>
        </div>
      )}
      {pending && !editing && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Button size="sm" variant="primary" loading={busy} onClick={() => onDecide("approved")}>
            Approve
          </Button>
          {p.action !== "delete" && (
            <Button size="sm" onClick={() => setEditing(true)} disabled={busy}>
              Edit and approve
            </Button>
          )}
          <Button size="sm" variant="danger" onClick={() => onDecide("rejected")} disabled={busy}>
            Reject
          </Button>
        </div>
      )}
    </li>
  );
}
