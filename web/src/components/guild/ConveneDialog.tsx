import { useEffect, useMemo, useState } from "react";
import type { Writer } from "../../api/client";
import { useWriters } from "../../api/hooks";
import { Badge, Button, Dialog, ErrorBanner, Pending } from "../ui";

type Props = {
  open: boolean;
  busy: boolean;
  error: unknown;
  onClose: () => void;
  onStart: (writerIds: string[]) => void;
};

/** ConveneDialog lets the author choose which critics attend. Default: every enabled critic. */
export function ConveneDialog({ open, busy, error, onClose, onStart }: Props) {
  const writers = useWriters();
  const critics = useMemo(() => (writers.data ?? []).filter((w) => !w.is_system && w.roles.includes("critic")), [writers.data]);
  const [chosen, setChosen] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (open) setChosen(new Set(critics.filter((w) => w.enabled).map((w) => w.id)));
  }, [open, critics]);

  const toggle = (w: Writer) =>
    setChosen((prev) => {
      const next = new Set(prev);
      if (next.has(w.id)) next.delete(w.id);
      else next.add(w.id);
      return next;
    });

  const enabledCritics = critics.filter((w) => w.enabled);
  return (
    <Dialog open={open} title="Convene the Guild" onClose={onClose}>
      <p className="text-sm text-stone-600">
        The chosen critics read the chapter and the story bible at the same time, each in their own voice, and return at most three issues apiece. Disabled writers
        cannot attend; enable them on the Writers page.
      </p>
      <ErrorBanner error={writers.error} onRetry={() => writers.refetch()} />
      {writers.isPending && <Pending paused={writers.isPaused} />}
      {critics.length > 0 && (
        <ul className="mt-4 max-h-80 divide-y divide-stone-100 overflow-y-auto rounded-md border border-stone-200">
          {critics.map((w) => (
            <li key={w.id} className={`flex items-center gap-3 px-3 py-2 ${w.enabled ? "" : "opacity-50"}`}>
              <input
                type="checkbox"
                id={`critic-${w.id}`}
                checked={chosen.has(w.id)}
                disabled={!w.enabled || busy}
                onChange={() => toggle(w)}
                className="h-4 w-4 accent-stone-900"
              />
              <label htmlFor={`critic-${w.id}`} className="flex flex-1 cursor-pointer items-center justify-between gap-2 text-sm">
                <span className="font-medium text-stone-800">{w.name}</span>
                <span className="flex items-center gap-2 text-xs text-stone-500">
                  <span className="font-mono">{w.model_alias}</span>
                  {!w.enabled && <Badge tone="stone">disabled</Badge>}
                </span>
              </label>
            </li>
          ))}
        </ul>
      )}
      {!writers.isPending && enabledCritics.length === 0 && (
        <p className="mt-4 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          No enabled writer has the critic role. Give a writer the critic role on the Writers page first.
        </p>
      )}
      <div className="mt-3 flex items-center gap-3 text-xs text-stone-500">
        <button type="button" className="hover:underline" onClick={() => setChosen(new Set(enabledCritics.map((w) => w.id)))} disabled={busy}>
          Select all
        </button>
        <button type="button" className="hover:underline" onClick={() => setChosen(new Set())} disabled={busy}>
          Select none
        </button>
        <span className="ml-auto">
          {chosen.size} of {enabledCritics.length} attending
        </span>
      </div>
      <ErrorBanner error={error} />
      <div className="mt-5 flex justify-end gap-2">
        <Button onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button variant="primary" onClick={() => onStart([...chosen])} disabled={chosen.size === 0} loading={busy}>
          Convene {chosen.size > 0 ? `${chosen.size} critic${chosen.size === 1 ? "" : "s"}` : ""}
        </Button>
      </div>
    </Dialog>
  );
}
