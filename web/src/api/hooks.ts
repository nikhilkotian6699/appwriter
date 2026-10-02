import { useQuery } from "@tanstack/react-query";
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
