import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Editor } from "@tiptap/react";
import { api, ApiError, call, type Chapter } from "../api/client";
import { keys, useChapter, useProject } from "../api/hooks";
import { ChapterEditor } from "../components/editor/ChapterEditor";
import { VersionsPanel } from "../components/VersionsPanel";
import { Button, ErrorBanner, Spinner } from "../components/ui";
import { wordCount } from "../lib/format";

type SaveStatus = "clean" | "dirty" | "saving" | "saved" | "conflict" | "error";

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
  const [showHistory, setShowHistory] = useState(true);
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
    lastSavedRef.current = null;
    currentRef.current = ch.content_md;
    setLoaded(ch.content_md);
    setEditorKey((k) => k + 1);
    setStatus("clean");
    setSaveError(null);
    setWords(wordCount(ch.content_md));
    qc.setQueryData(keys.chapter(chapterId), ch);
  };

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
          <Button size="sm" onClick={() => setShowHistory((v) => !v)}>
            {showHistory ? "Hide history" : "History"}
          </Button>
          <Button size="sm" variant="primary" disabled title="Convene the Guild arrives in milestone 2">
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
      <div className={`grid gap-4 ${showHistory ? "lg:grid-cols-[1fr_20rem]" : ""}`}>
        <div>
          <ChapterEditor key={editorKey} initialMarkdown={loaded} onReady={onReady} onChange={onChange} />
        </div>
        {showHistory && <VersionsPanel chapterId={chapterId} onRestored={applyServerContent} />}
      </div>
    </div>
  );
}
