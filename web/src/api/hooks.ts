import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api, call } from "./client";

export const keys = {
  me: ["me"] as const,
  settings: ["settings"] as const,
  gatewayModels: ["gateway-models"] as const,
  projects: ["projects"] as const,
  project: (id: string) => ["projects", id] as const,
  chapters: (projectId: string) => ["projects", projectId, "chapters"] as const,
  chapter: (id: string) => ["chapters", id] as const,
  versions: (chapterId: string) => ["chapters", chapterId, "versions"] as const,
  bible: (projectId: string) => ["projects", projectId, "bible"] as const,
  writers: ["writers"] as const,
  run: (id: string) => ["runs", id] as const,
  runCritiques: (id: string) => ["runs", id, "critiques"] as const,
  runIssues: (id: string) => ["runs", id, "issues"] as const,
  revision: (id: string) => ["revisions", id] as const,
  projectProposals: (projectId: string, status?: string) => ["projects", projectId, "proposals", status ?? "all"] as const,
  runProposals: (runId: string) => ["runs", runId, "proposals"] as const,
  runDrafts: (runId: string) => ["runs", runId, "drafts"] as const,
  chapterDrafts: (chapterId: string) => ["chapters", chapterId, "drafts"] as const,
  chapterHistory: (chapterId: string, kind?: string) => ["chapters", chapterId, "history", kind ?? "all"] as const,
  runCalls: (runId: string) => ["runs", runId, "calls"] as const,
  chapterRevisions: (chapterId: string, status?: string) => ["chapters", chapterId, "revisions", status ?? "all"] as const,
  chapterRuns: (chapterId: string, kind?: string) => ["chapters", chapterId, "runs", kind ?? "all"] as const,
};

export function useMe() {
  return useQuery({ queryKey: keys.me, queryFn: () => call(api.GET("/api/me")), staleTime: 60_000 });
}

export function useSettings() {
  return useQuery({ queryKey: keys.settings, queryFn: () => call(api.GET("/api/settings")) });
}

export function useGatewayModels() {
  return useQuery({
    queryKey: keys.gatewayModels,
    queryFn: () => call(api.GET("/api/gateway/models")),
    retry: false,
    staleTime: 30_000,
  });
}

export function useProjects() {
  return useQuery({ queryKey: keys.projects, queryFn: () => call(api.GET("/api/projects")) });
}

export function useProject(id: string) {
  return useQuery({
    queryKey: keys.project(id),
    queryFn: () => call(api.GET("/api/projects/{projectId}", { params: { path: { projectId: id } } })),
  });
}

export function useChapters(projectId: string) {
  return useQuery({
    queryKey: keys.chapters(projectId),
    queryFn: () => call(api.GET("/api/projects/{projectId}/chapters", { params: { path: { projectId } } })),
  });
}

export function useChapter(id: string) {
  return useQuery({
    queryKey: keys.chapter(id),
    queryFn: () => call(api.GET("/api/chapters/{chapterId}", { params: { path: { chapterId: id } } })),
    staleTime: Infinity,
  });
}

export function useVersions(chapterId: string) {
  return useQuery({
    queryKey: keys.versions(chapterId),
    queryFn: () => call(api.GET("/api/chapters/{chapterId}/versions", { params: { path: { chapterId } } })),
  });
}

export function useBible(projectId: string) {
  return useQuery({
    queryKey: keys.bible(projectId),
    queryFn: () => call(api.GET("/api/projects/{projectId}/bible", { params: { path: { projectId } } })),
  });
}

export function useWriters() {
  return useQuery({ queryKey: keys.writers, queryFn: () => call(api.GET("/api/writers")) });
}

export function useRun(id: string | null) {
  return useQuery({
    queryKey: keys.run(id ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}", { params: { path: { runId: id! } } })),
    enabled: !!id,
  });
}

export function useRunCritiques(id: string | null, enabled = true) {
  return useQuery({
    queryKey: keys.runCritiques(id ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}/critiques", { params: { path: { runId: id! } } })),
    enabled: !!id && enabled,
  });
}

export function useChapterRuns(chapterId: string, kind?: "critique" | "revision" | "bible_update" | "cowrite" | "compare" | "writer_test", limit = 20) {
  return useQuery({
    queryKey: keys.chapterRuns(chapterId, kind),
    queryFn: () => call(api.GET("/api/chapters/{chapterId}/runs", { params: { path: { chapterId }, query: { kind, limit } } })),
  });
}

export function useRunIssues(id: string | null, enabled = true) {
  return useQuery({
    queryKey: keys.runIssues(id ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}/issues", { params: { path: { runId: id! } } })),
    enabled: !!id && enabled,
  });
}

export function useRevision(id: string | null) {
  return useQuery({
    queryKey: keys.revision(id ?? ""),
    queryFn: () => call(api.GET("/api/revisions/{revisionId}", { params: { path: { revisionId: id! } } })),
    enabled: !!id,
  });
}

export function useChapterRevisions(chapterId: string, status?: "proposed" | "applied" | "discarded", limit = 5) {
  return useQuery({
    queryKey: keys.chapterRevisions(chapterId, status),
    queryFn: () => call(api.GET("/api/chapters/{chapterId}/revisions", { params: { path: { chapterId }, query: { status, limit } } })),
  });
}

export function useProjectBibleProposals(projectId: string, status?: "pending" | "approved" | "rejected", limit = 50) {
  return useQuery({
    queryKey: keys.projectProposals(projectId, status),
    queryFn: () => call(api.GET("/api/projects/{projectId}/bible/proposals", { params: { path: { projectId }, query: { status, limit } } })),
  });
}

export function useRunBibleProposals(runId: string | null, enabled = true) {
  return useQuery({
    queryKey: keys.runProposals(runId ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}/bible-proposals", { params: { path: { runId: runId! } } })),
    enabled: !!runId && enabled,
  });
}

export function useRunDrafts(runId: string | null, enabled = true) {
  return useQuery({
    queryKey: keys.runDrafts(runId ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}/drafts", { params: { path: { runId: runId! } } })),
    enabled: !!runId && enabled,
  });
}

export function useChapterDrafts(chapterId: string, limit = 10) {
  return useQuery({
    queryKey: keys.chapterDrafts(chapterId),
    queryFn: () => call(api.GET("/api/chapters/{chapterId}/drafts", { params: { path: { chapterId }, query: { limit } } })),
  });
}

export type HistoryKind = "critique" | "revision" | "bible_update" | "cowrite" | "compare" | "writer_test";

export function useChapterHistory(chapterId: string, kind?: HistoryKind, limit = 30) {
  return useInfiniteQuery({
    queryKey: keys.chapterHistory(chapterId, kind),
    queryFn: ({ pageParam }) =>
      call(api.GET("/api/chapters/{chapterId}/history", { params: { path: { chapterId }, query: { kind, limit, before: pageParam || undefined } } })),
    initialPageParam: "" as string,
    getNextPageParam: (last) => last.next_before ?? undefined,
  });
}

export function useRunCalls(runId: string | null) {
  return useQuery({
    queryKey: keys.runCalls(runId ?? ""),
    queryFn: () => call(api.GET("/api/runs/{runId}/calls", { params: { path: { runId: runId! } } })),
    enabled: !!runId,
  });
}
