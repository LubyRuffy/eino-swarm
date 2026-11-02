import { ApiError } from "./api"
import type { NuwaSnapshot } from "./nuwa"

export const nuwaAPI = {
  async snapshot(projectId: string): Promise<NuwaSnapshot> {
    const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/nuwa`)
    if (!response.ok) {
      const body = (await response.json().catch(() => ({}))) as { error?: string; code?: string }
      throw new ApiError(body.error || response.statusText, response.status, body.code)
    }
    return response.json() as Promise<NuwaSnapshot>
  },
}

/** The project stream only invalidates the snapshot. A revision is not a
 * conversation seq and must never be folded into transcript replay. */
export function subscribeNuwa(
  projectId: string,
  handlers: {
    onRevision: (revision: number) => void
    onOpen: () => void
    onError: () => void
  }
): () => void {
  const source = new EventSource(`/api/projects/${encodeURIComponent(projectId)}/nuwa/events`)
  let closed = false
  source.onopen = () => {
    if (!closed) handlers.onOpen()
  }
  source.onerror = () => {
    if (!closed) handlers.onError()
  }
  source.addEventListener("nuwa_updated", (event) => {
    if (closed) return
    try {
      const body = JSON.parse((event as MessageEvent).data) as {
        project_id: string
        revision: number
      }
      if (
        body.project_id === projectId &&
        Number.isSafeInteger(body.revision) &&
        body.revision >= 0
      )
        handlers.onRevision(body.revision)
    } catch {
      /* A malformed notification cannot replace the last snapshot. */
    }
  })
  return () => {
    closed = true
    source.close()
  }
}
