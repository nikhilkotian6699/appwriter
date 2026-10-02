import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type Me = Schemas["Me"];
export type Project = Schemas["Project"];
export type ProjectSummary = Schemas["ProjectSummary"];
export type ProjectInput = Schemas["ProjectInput"];
export type Chapter = Schemas["Chapter"];
export type ChapterSummary = Schemas["ChapterSummary"];
export type ChapterVersion = Schemas["ChapterVersion"];
export type ChapterVersionSummary = Schemas["ChapterVersionSummary"];
export type BibleEntry = Schemas["BibleEntry"];
export type BibleEntryInput = Schemas["BibleEntryInput"];
export type BibleSection = Schemas["BibleSection"];
export type Writer = Schemas["Writer"];
export type WriterInput = Schemas["WriterInput"];
export type WriterRole = Schemas["WriterRole"];
export type WriterTestInput = Schemas["WriterTestInput"];
export type WriterTestResult = Schemas["WriterTestResult"];
export type GatewayModels = Schemas["GatewayModels"];
export type Settings = Schemas["Settings"];
export type Run = Schemas["Run"];
export type RunKind = Schemas["RunKind"];
export type RunStatus = Schemas["RunStatus"];
export type RunEvent = Schemas["RunEvent"];
export type Critique = Schemas["Critique"];
export type CritiqueIssue = Schemas["CritiqueIssue"];
export type CritiqueRecord = Schemas["CritiqueRecord"];
export type CritiqueStatus = Schemas["CritiqueStatus"];
export type IssueSeverity = Schemas["IssueSeverity"];

/** ApiError carries the server's stable error code alongside the message. */
export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export const api = createClient<paths>({ baseUrl: "/" });

type Result<T> = { data?: T; error?: unknown; response: Response };

/** call resolves an openapi-fetch request to its data, or throws ApiError. */
export async function call<T>(p: Promise<Result<T>>): Promise<T> {
  let res: Result<T>;
  try {
    res = await p;
  } catch (e) {
    throw new ApiError(0, "network", e instanceof Error ? e.message : "network error");
  }
  if (!res.response.ok) {
    const err = res.error as Schemas["ErrorResponse"] | undefined;
    throw new ApiError(
      res.response.status,
      err?.error?.code ?? "error",
      err?.error?.message ?? `${res.response.status} ${res.response.statusText}`,
    );
  }
  return res.data as T;
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}
