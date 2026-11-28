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
  thread: (threadId: string, signal?: AbortSignal) => read(`/api/threads/${encodeURIComponent(threadId)}/graph?preview=1`, signal),
  project: (projectId: string, signal?: AbortSignal) => read(`/api/projects/${encodeURIComponent(projectId)}/graph?preview=1`, signal),
  extract: async (threadId: string, clear = false, ownerId?: string): Promise<{ id: string }> => {
    const query = new URLSearchParams()
    if (clear) {
      query.set("clear", "1")
      if (ownerId) query.set("owner_id", ownerId)
    }
    const endpoint = `/api/threads/${encodeURIComponent(threadId)}/graph/extract`
    const params = query.toString()
    const response = await fetch(params ? `${endpoint}?${params}` : endpoint, { method: "POST" })
    const body = (await response.json().catch(() => ({}))) as { error?: string; code?: string; turn?: { id: string } }
    if (!response.ok) throw new ApiError(body.error || response.statusText, response.status, body.code)
    return { id: body.turn?.id ?? "" }
  },
}
