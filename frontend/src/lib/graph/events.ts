type GraphEvent =
  | { kind: "revision"; revision: number }
  | { kind: "connection"; connected: boolean }

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
export function parseGraphRevision(data: string, threadId: string): number | undefined {
  try {
    const body = JSON.parse(data) as { thread_id?: string; revision?: number }
    if (body.thread_id !== threadId || !Number.isSafeInteger(body.revision) || (body.revision as number) < 0) return undefined
    return body.revision
  } catch {
    return undefined
  }
}
