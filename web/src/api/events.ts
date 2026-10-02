import { useEffect, useRef } from "react";
import type { BibleProposal, Critique, DiffStats, Issue, RunStatus, SkippedIssue } from "./client";

/** Payloads of the run events the server emits (see internal/runs and internal/guild). */
export type FinishedPayload = {
  status: RunStatus;
  error?: string;
  cost_usd: number;
  cost_estimated: boolean;
  prompt_tokens: number;
  completion_tokens: number;
};

export type PlanPayload = {
  writers: { critique_id: string; writer_id: string; name: string; slug: string; model_alias: string }[];
  scenes: { index: number; title: string; start: number; end: number }[];
  content_hash: string;
};

export type WriterUsage = { prompt_tokens: number; completion_tokens: number; cost_usd: number; cost_estimated: boolean };

export type WriterPayload = {
  critique_id: string;
  writer_id: string;
  slug: string;
  scene: number;
  text?: string;
  reason?: string;
  error?: string;
  critique?: Critique;
  usage?: WriterUsage;
};

/** An issue as carried by editor.done: the stored row without run-level fields. */
export type EventIssue = Omit<Issue, "run_id" | "content_hash" | "created_at" | "chapter_id" | "decided_at">;

export type EditorPayload = {
  writer_id: string;
  slug: string;
  text?: string;
  reason?: string;
  error?: string;
  fallback: boolean;
  issues?: EventIssue[];
  warnings?: string[];
  usage?: WriterUsage;
};

export type RevisionPayload = {
  writer_id: string;
  slug: string;
  scene: number;
  scenes?: number;
  notes?: number;
  text?: string;
  reason?: string;
  revision_id?: string;
  stats?: DiffStats;
  skipped?: SkippedIssue[];
  warnings?: string[];
  usage?: WriterUsage;
};

export type BiblePayload = {
  writer_id: string;
  slug: string;
  text?: string;
  reason?: string;
  proposals?: Omit<BibleProposal, "created_at" | "current">[];
  warnings?: string[];
  usage?: WriterUsage;
};

export type RunEventMessage =
  | { type: "run.started"; seq: number; payload: { kind: string } }
  | { type: "critique.plan"; seq: number; payload: PlanPayload }
  | { type: "writer.started" | "writer.delta" | "writer.retry" | "writer.done" | "writer.failed"; seq: number; payload: WriterPayload }
  | { type: "editor.started" | "editor.delta" | "editor.retry" | "editor.done"; seq: number; payload: EditorPayload }
  | { type: "revision.started" | "revision.delta" | "revision.retry" | "revision.done"; seq: number; payload: RevisionPayload }
  | { type: "bible.started" | "bible.delta" | "bible.retry" | "bible.done"; seq: number; payload: BiblePayload }
  | { type: "run.finished"; seq: number; payload: FinishedPayload }
  | { type: "end"; seq: number; payload: Record<string, never> };

const EVENT_TYPES = [
  "run.started",
  "critique.plan",
  "writer.started",
  "writer.delta",
  "writer.retry",
  "writer.done",
  "writer.failed",
  "editor.started",
  "editor.delta",
  "editor.retry",
  "editor.done",
  "revision.started",
  "revision.delta",
  "revision.retry",
  "revision.done",
  "bible.started",
  "bible.delta",
  "bible.retry",
  "bible.done",
  "run.finished",
  "end",
] as const;

/**
 * useRunEvents follows a run's Server-Sent Events. The browser's EventSource
 * reconnects by itself and sends Last-Event-ID, so a dropped connection
 * resumes where it stopped; the stream is closed after run.finished or end.
 * onEvent is read through a ref so the caller may pass a fresh closure each render.
 */
export function useRunEvents(runId: string | null, active: boolean, onEvent: (ev: RunEventMessage) => void, onError?: (message: string) => void) {
  const handler = useRef(onEvent);
  handler.current = onEvent;
  const errorHandler = useRef(onError);
  errorHandler.current = onError;

  useEffect(() => {
    if (!runId || !active) return;
    const es = new EventSource(`/api/runs/${runId}/events`);
    let closed = false;
    const close = () => {
      if (!closed) {
        closed = true;
        es.close();
      }
    };
    for (const type of EVENT_TYPES) {
      es.addEventListener(type, (raw) => {
        const msg = raw as MessageEvent<string>;
        let payload: unknown = {};
        try {
          payload = msg.data ? JSON.parse(msg.data) : {};
        } catch {
          payload = {};
        }
        const seq = Number(msg.lastEventId) || 0;
        handler.current({ type, seq, payload } as RunEventMessage);
        if (type === "run.finished" || type === "end") close();
      });
    }
    es.onerror = () => {
      // EventSource retries on its own; only a closed stream is final.
      if (es.readyState === EventSource.CLOSED && !closed) {
        closed = true;
        errorHandler.current?.("The connection to the run was lost. Reload to catch up.");
      }
    };
    return close;
  }, [runId, active]);
}
