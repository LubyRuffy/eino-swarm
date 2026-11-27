import { ApiError } from "@/lib/api"
import type { GraphView } from "./types"

async function read(path: string, signal?: AbortSignal): Promise<GraphView> {
  const response = await fetch(path, { signal, cache: "no-store" })
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as { error?: string; code?: string }
    throw new ApiError(body.error || response.statusText, response.status, body.code)
  }
  return response.json() as Promise<GraphView>
}

/** The conversation's graph is its project's when it has one. */
export const graphAPI = {
  thread: (threadId: string, signal?: AbortSignal) => read(`/api/threads/${encodeURIComponent(threadId)}/graph`, signal),
  project: (projectId: string, signal?: AbortSignal) => read(`/api/projects/${encodeURIComponent(projectId)}/graph`, signal),
}
