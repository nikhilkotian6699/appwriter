import { useEffect, useMemo, useState } from "react";
import type { Writer } from "../../api/client";
import { useWriters } from "../../api/hooks";
import { Badge, Button, Dialog, ErrorBanner, Field, Spinner, Textarea } from "../ui";

export type CowriteContext = {
  /** The selected passage, or empty when continuing from the cursor. */
  selection: string;
  selectionWords: number;
};

const PRESETS = ["Draft this scene", "Continue from here", "Rewrite this dialogue", "Tighten this passage", "Give me 3 alternative openings"];

type Props = {
  open: boolean;
  busy: boolean;
  error: unknown;
  context: CowriteContext;
  /** How many writers may be chosen: 1 to co-write, up to 3 to compare. */
  maxWriters: number;
  onClose: () => void;
  onStart: (writerIds: string[], instruction: string, notes: string) => void;
};

/** CowriteDialog picks the co-writer(s), the instruction and optional scene notes. */
export function CowriteDialog({ open, busy, error, context, maxWriters, onClose, onStart }: Props) {
  const writers = useWriters();
  const cowriters = useMemo(() => (writers.data ?? []).filter((w) => !w.is_system && w.roles.includes("co-writer")), [writers.data]);
  const [chosen, setChosen] = useState<string[]>([]);
  const [instruction, setInstruction] = useState("");
  const [notes, setNotes] = useState("");

  useEffect(() => {
    if (open) {
      const first = cowriters.find((w) => w.enabled);
      setChosen(first ? [first.id] : []);
      setInstruction(context.selection ? "Tighten this passage" : "Continue from here");
      setNotes("");
    }
  }, [open, cowriters, context.selection]);

  const toggle = (w: Writer) =>
    setChosen((prev) => {
      if (prev.includes(w.id)) return prev.filter((id) => id !== w.id);
      if (maxWriters === 1) return [w.id];
      if (prev.length >= maxWriters) return prev;
      return [...prev, w.id];
    });

  const compare = chosen.length > 1;
  return (
    <Dialog open={open} title={maxWriters > 1 ? "Co-write or compare" : "Co-write"} onClose={onClose}>
      <p className="text-sm text-stone-600">
        {context.selection ? (
          <>
            The writer works on your <strong>selected passage</strong> ({context.selectionWords} word{context.selectionWords === 1 ? "" : "s"}) and proposes text to take its place.
          </>
        ) : (
          <>
            Nothing is selected, so the writer <strong>continues from the cursor</strong>, reading what comes before and after it.
          </>
        )}{" "}
        The story bible goes along either way.
      </p>
      <ErrorBanner error={writers.error} onRetry={() => writers.refetch()} />
      {writers.isLoading && <Spinner />}
      <div className="mt-4">
        <span className="mb-1 block text-sm font-medium text-stone-700">{maxWriters > 1 ? `Writers (one to co-write, two or three to compare)` : "Writer"}</span>
        <ul className="max-h-48 divide-y divide-stone-100 overflow-y-auto rounded-md border border-stone-200">
          {cowriters.map((w) => (
            <li key={w.id} className={`flex items-center gap-3 px-3 py-2 ${w.enabled ? "" : "opacity-50"}`}>
              <input
                type={maxWriters === 1 ? "radio" : "checkbox"}
                name="cowriter"
                id={`cowriter-${w.id}`}
                checked={chosen.includes(w.id)}
                disabled={!w.enabled || busy || (!chosen.includes(w.id) && chosen.length >= maxWriters && maxWriters > 1)}
                onChange={() => toggle(w)}
                className="h-4 w-4 accent-stone-900"
              />
              <label htmlFor={`cowriter-${w.id}`} className="flex flex-1 cursor-pointer items-center justify-between gap-2 text-sm">
                <span className="font-medium text-stone-800">{w.name}</span>
                <span className="flex items-center gap-2 text-xs text-stone-500">
                  <span className="font-mono">{w.model_alias}</span>
                  {!w.enabled && <Badge tone="stone">disabled</Badge>}
                </span>
              </label>
            </li>
          ))}
        </ul>
        {!writers.isLoading && cowriters.filter((w) => w.enabled).length === 0 && (
          <p className="mt-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">No enabled writer has the co-writer role. Give a writer that role on the Writers page first.</p>
        )}
      </div>
      <div className="mt-4">
        <Field label="Instruction">
          <Textarea value={instruction} onChange={(e) => setInstruction(e.target.value)} rows={2} maxLength={2000} placeholder="What should the writer do?" />
        </Field>
        <div className="mt-1.5 flex flex-wrap gap-1.5">
          {PRESETS.map((p) => (
            <button key={p} type="button" onClick={() => setInstruction(p)} className="rounded-full border border-stone-300 px-2.5 py-0.5 text-xs text-stone-700 hover:bg-stone-100" disabled={busy}>
              {p}
            </button>
          ))}
        </div>
      </div>
      <div className="mt-3">
        <Field label="Scene notes (optional)" help="What happens, who is there, what the reader should feel.">
          <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={3} maxLength={4000} />
        </Field>
      </div>
      <ErrorBanner error={error} />
      <div className="mt-5 flex justify-end gap-2">
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button variant="primary" onClick={() => onStart(chosen, instruction.trim(), notes.trim())} disabled={chosen.length === 0 || instruction.trim() === ""} loading={busy}>
          {compare ? `Compare ${chosen.length} drafts` : "Ask for a draft"}
        </Button>
      </div>
    </Dialog>
  );
}
