import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/react";
import { api, ApiError, call, type Chapter } from "../api/client";
import { keys, useChapter, useChapterRevisions, useChapterRuns, useProject } from "../api/hooks";
import { ChapterEditor } from "../components/editor/ChapterEditor";
import { highlightQuote } from "../components/editor/issueHighlight";
import { ConveneDialog } from "../components/guild/ConveneDialog";
import { GuildPanel } from "../components/guild/GuildPanel";
import { RevisionSection } from "../components/guild/RevisionSection";
import { VersionsPanel } from "../components/VersionsPanel";
import { Button, ErrorBanner, Spinner } from "../components/ui";
import { wordCount } from "../lib/format";

type SaveStatus = "clean" | "dirty" | "saving" | "saved" | "conflict" | "error";
type Panel = "guild" | "history" | null;

export default function ChapterPage() {
  const { chapterId = "" } = useParams();
  const chapter = useChapter(chapterId);
  if (chapter.isLoading) return <Spinner />;
  if (chapter.error) return <ErrorBanner error={chapter.error} onRetry={() => chapter.refetch()} />;
  return <ChapterWorkspace key={chapter.data!.id} initial={chapter.data!} />;
}

function ChapterWorkspace({ initial }: { initial: Chapter }) {
  const qc = useQueryClient();
  const project = useProject(initial.project_id);
  const [title, setTitle] = useState(initial.title);
  const [status, setStatus] = useState<SaveStatus>("clean");
  const [saveError, setSaveError] = useState<unknown>(null);
  const [panel, setPanel] = useState<Panel>("history");
  const [convening, setConvening] = useState(false);
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [revisionRunId, setRevisionRunId] = useState<string | null>(null);
  const [currentHash, setCurrentHash] = useState(initial.content_hash);
  const [words, setWords] = useState(wordCount(initial.content_md));
  const [editorKey, setEditorKey] = useState(0);
  const [loaded, setLoaded] = useState(initial.content_md);

  const editorRef = useRef<Editor | null>(null);
  const hashRef = useRef(initial.content_hash);
  const lastSavedRef = useRef<string | null>(null);
  const currentRef = useRef<string>(initial.content_md);
  const timerRef = useRef<number | null>(null);
  const savingRef = useRef(false);

  const chapterId = initial.id;

  const save = useCallback(async () => {
    if (savingRef.current) return;
    const md = currentRef.current;
    if (lastSavedRef.current === md) return;
    savingRef.current = true;
    setStatus("saving");
    try {
      const res = await call(
        api.PUT("/api/chapters/{chapterId}/content", { params: { path: { chapterId } }, body: { content_md: md, base_hash: hashRef.current } }),
      );
      hashRef.current = res.chapter.content_hash;
      setCurrentHash(res.chapter.content_hash);
      lastSavedRef.current = md;
      setSaveError(null);
      setStatus(currentRef.current === md ? "saved" : "dirty");
      if (res.snapshot_created) qc.invalidateQueries({ queryKey: keys.versions(chapterId) });
      qc.setQueryData(keys.chapter(chapterId), res.chapter);
      qc.invalidateQueries({ queryKey: keys.chapters(initial.project_id) });
    } catch (e) {
      setSaveError(e);
      setStatus(e instanceof ApiError && e.status === 409 ? "conflict" : "error");
    } finally {
      savingRef.current = false;
    }
  }, [chapterId, initial.project_id, qc]);

  const scheduleSave = useCallback(() => {
    if (timerRef.current) window.clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => void save(), 1500);
  }, [save]);

  const onReady = useCallback((editor: Editor, normalised: string) => {
    editorRef.current = editor;
    // The normalised markdown is the baseline; only real edits count as changes.
    lastSavedRef.current = normalised;
    currentRef.current = normalised;
  }, []);

  const onChange = useCallback(
    (md: string) => {
      currentRef.current = md;
      setWords(wordCount(md));
      if (md === lastSavedRef.current) {
        setStatus("saved");
        return;
      }
      setStatus("dirty");
      scheduleSave();
    },
    [scheduleSave],
  );

  // Warn before leaving with unsaved text, and flush on unmount.
  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (currentRef.current !== lastSavedRef.current) {
        e.preventDefault();
      }
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      if (timerRef.current) window.clearTimeout(timerRef.current);
      if (currentRef.current !== lastSavedRef.current) {
        void fetch(`/api/chapters/${chapterId}/content`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ content_md: currentRef.current, base_hash: hashRef.current }),
          keepalive: true,
        });
      }
    };
  }, [chapterId]);

  const rename = useMutation({
    mutationFn: (t: string) => call(api.PUT("/api/chapters/{chapterId}", { params: { path: { chapterId } }, body: { title: t } })),
    onSuccess: (ch) => {
      qc.setQueryData(keys.chapter(chapterId), (old: Chapter | undefined) => (old ? { ...old, title: ch.title } : old));
      qc.invalidateQueries({ queryKey: keys.chapters(initial.project_id) });
    },
  });

  const reloadFromServer = async () => {
    const fresh = await call(api.GET("/api/chapters/{chapterId}", { params: { path: { chapterId } } }));
    applyServerContent(fresh);
  };

  const applyServerContent = (ch: Chapter) => {
    hashRef.current = ch.content_hash;
    setCurrentHash(ch.content_hash);
    lastSavedRef.current = null;
    currentRef.current = ch.content_md;
    setLoaded(ch.content_md);
    setEditorKey((k) => k + 1);
    setStatus("clean");
    setSaveError(null);
    setWords(wordCount(ch.content_md));
    qc.setQueryData(keys.chapter(chapterId), ch);
  };

  // The latest critique run of this chapter; a run still in session opens the Guild panel at once.
  const critiqueRuns = useChapterRuns(chapterId, "critique", 1);
  const latestRun = critiqueRuns.data?.[0];
  useEffect(() => {
    if (!latestRun || activeRunId) return;
    setActiveRunId(latestRun.id);
    if (latestRun.status === "running") setPanel("guild");
  }, [latestRun, activeRunId]);

  const convene = useMutation({
    mutationFn: async (writerIds: string[]) => {
      // The critics must read what is on screen, so flush any pending edit first.
      if (timerRef.current) window.clearTimeout(timerRef.current);
      await save();
      if (currentRef.current !== lastSavedRef.current) throw new ApiError(0, "unsaved", "The chapter could not be saved; fix that before convening the Guild.");
      return call(api.POST("/api/chapters/{chapterId}/critiques", { params: { path: { chapterId } }, body: { writer_ids: writerIds } }));
    },
    onSuccess: (run) => {
      setActiveRunId(run.id);
      setPanel("guild");
      setConvening(false);
      qc.invalidateQueries({ queryKey: keys.chapterRuns(chapterId, "critique") });
    },
  });

  // A revision proposed earlier (and still applicable) can be reviewed without a new run.
  const proposedRevisions = useChapterRevisions(chapterId, "proposed", 1);
  const pendingRevision = !revisionRunId && proposedRevisions.data?.[0] && !proposedRevisions.data[0].stale ? proposedRevisions.data[0] : null;

  const revise = useMutation({
    mutationFn: async () => {
      if (!activeRunId) throw new ApiError(0, "no_run", "Convene the Guild first.");
      if (timerRef.current) window.clearTimeout(timerRef.current);
      await save();
      if (currentRef.current !== lastSavedRef.current) throw new ApiError(0, "unsaved", "The chapter could not be saved; fix that before asking for a revision.");
      return call(api.POST("/api/chapters/{chapterId}/revisions", { params: { path: { chapterId } }, body: { run_id: activeRunId } }));
    },
    onSuccess: (run) => {
      setRevisionRunId(run.id);
      setPanel("guild");
    },
  });

  const onRevisionApplied = useCallback(
    (ch: Chapter) => {
      applyServerContent(ch);
      qc.invalidateQueries({ queryKey: keys.versions(chapterId) });
      qc.invalidateQueries({ queryKey: keys.chapterRevisions(chapterId, "proposed") });
    },
    // applyServerContent is stable enough: it only touches refs and setters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [chapterId, qc],
  );

  const onHighlight = useCallback((quote: string) => {
    const editor = editorRef.current;
    return editor ? highlightQuote(editor, quote) : false;
  }, []);

  const guildRunning = latestRun?.status === "running" || convene.isPending;

  const statusText: Record<SaveStatus, string> = {
    clean: "Saved",
    dirty: "Unsaved changes",
    saving: "Saving…",
    saved: "Saved",
    conflict: "Conflict: the chapter changed elsewhere",
    error: "Could not save",
  };
  const statusTone = status === "conflict" || status === "error" ? "text-red-700" : status === "dirty" || status === "saving" ? "text-amber-700" : "text-stone-500";

  return (
    <div>
      <div className="mb-2 text-sm text-stone-500">
        <Link to="/projects" className="hover:underline">
          Projects
        </Link>{" "}
        /{" "}
        <Link to={`/projects/${initial.project_id}`} className="hover:underline">
          {project.data?.name ?? "Project"}
        </Link>{" "}
        / {title}
      </div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={() => {
            const t = title.trim();
            if (t && t !== initial.title) rename.mutate(t);
            else if (!t) setTitle(initial.title);
          }}
          className="min-w-[16rem] flex-1 border-0 bg-transparent text-2xl font-semibold text-stone-900 focus:outline-none focus:ring-0"
          aria-label="Chapter title"
          maxLength={300}
        />
        <div className="flex items-center gap-3 text-sm">
          <span className="text-stone-500">{words.toLocaleString()} words</span>
          <span className={statusTone}>{statusText[status]}</span>
          {(status === "dirty" || status === "error") && (
            <Button size="sm" onClick={() => void save()}>
              Save now
            </Button>
          )}
          {status === "conflict" && (
            <Button size="sm" variant="danger" onClick={() => void reloadFromServer()}>
              Reload server version
            </Button>
          )}
          <Button size="sm" onClick={() => setPanel((p) => (p === "history" ? null : "history"))} aria-pressed={panel === "history"}>
            History
          </Button>
          {activeRunId && (
            <Button size="sm" onClick={() => setPanel((p) => (p === "guild" ? null : "guild"))} aria-pressed={panel === "guild"}>
              Guild
            </Button>
          )}
          <Button size="sm" variant="primary" onClick={() => setConvening(true)} disabled={guildRunning} title={guildRunning ? "The Guild is still in session" : "Ask the critics to read this chapter"}>
            Convene the Guild
          </Button>
        </div>
      </div>
      {saveError !== null && status !== "conflict" && <div className="mb-3"><ErrorBanner error={saveError} /></div>}
      {status === "conflict" && (
        <div className="mb-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          Someone (or another tab) saved this chapter after you opened it. Copy anything you want to keep, then reload the server version.
        </div>
      )}
      <ErrorBanner error={rename.error} />
      <div className={`grid gap-4 ${panel === "history" ? "lg:grid-cols-[1fr_20rem]" : panel === "guild" ? "lg:grid-cols-[1fr_26rem]" : ""}`}>
        <div>
          <ChapterEditor key={editorKey} initialMarkdown={loaded} onReady={onReady} onChange={onChange} />
        </div>
        {panel === "history" && <VersionsPanel chapterId={chapterId} onRestored={applyServerContent} />}
        {panel === "guild" && activeRunId && (
          <aside aria-label="The Guild" className="lg:sticky lg:top-4 lg:max-h-[calc(100vh-2rem)] lg:overflow-y-auto">
            <h2 className="mb-2 font-semibold text-stone-900">The Guild</h2>
            <ErrorBanner error={revise.error} />
            <div className="mb-3">
              <RevisionSection key={revisionRunId ?? pendingRevision?.id ?? "none"} runId={revisionRunId} pending={pendingRevision} chapterId={chapterId} onApplied={onRevisionApplied} />
            </div>
            <GuildPanel
              key={activeRunId}
              runId={activeRunId}
              currentHash={currentHash}
              onHighlight={onHighlight}
              onConveneAgain={() => setConvening(true)}
              onRevise={() => revise.mutate()}
              revising={revise.isPending}
            />
          </aside>
        )}
      </div>
      <ConveneDialog open={convening} busy={convene.isPending} error={convene.error} onClose={() => setConvening(false)} onStart={(ids) => convene.mutate(ids)} />
    </div>
  );
}
