type GraphFrame = { kind: "revision" | "preview"; revision: number; ownerId?: string }
type GraphEvent = GraphFrame | { kind: "connection"; connected: boolean }

const subscribers = new Map<string, Set<(event: GraphEvent) => void>>()
const connected = new Set<string>()

/** Graph revisions ride the conversation's own connection; this fans them out
 *  to whoever is looking at the panel without a second EventSource. */
export function publishGraphEvent(threadId: string, event: GraphEvent) {
  if (event.kind === "connection") {
    if (event.connected) connected.add(threadId)
    else connected.delete(threadId)
  }
  for (const listener of subscribers.get(threadId) ?? []) listener(event)
}

export function subscribeGraphEvents(threadId: string, listener: (event: GraphEvent) => void): () => void {
  const group = subscribers.get(threadId) ?? new Set()
  group.add(listener)
  subscribers.set(threadId, group)
  listener({ kind: "connection", connected: connected.has(threadId) })
  return () => {
    group.delete(listener)
    if (!group.size) subscribers.delete(threadId)
  }
}

/** A frame is only a hint to refetch; anything malformed is ignored. */
export function parseGraphFrame(data: string, threadId: string): GraphFrame | undefined {
  try {
    const body = JSON.parse(data) as { thread_id?: string; revision?: number; owner_id?: unknown; preview?: unknown }
    const { revision, owner_id: ownerId } = body
    if (body.thread_id !== threadId || typeof revision !== "number" || !Number.isSafeInteger(revision) || revision < 0) return undefined
    if (ownerId !== undefined && (typeof ownerId !== "string" || !ownerId)) return undefined
    return { kind: body.preview === true ? "preview" : "revision", revision, ...(ownerId ? { ownerId } : {}) }
  } catch {
    return undefined
  }
}
