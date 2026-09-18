import type { SwarmEvent, ThreadStatus } from "./types"

export interface StreamHandlers {
  onEvent: (ev: SwarmEvent) => void
  /** Fired once the replay is finished, so the UI can drop its loading state.
   *  Carries the live status because a conversation may already be working
   *  when the tab opens. `started_at` is the current turn, not the standing
   *  objective — that clock lives on `goal_started_at`. */
  onReady?: (payload: { seq: number; status?: ThreadStatus }) => void
  onOpen?: () => void
  onClose?: (reason: "error" | "closed") => void
}

/** The event-stream kinds the app renders. EventSource delivers by name, so
 *  each one has to be subscribed explicitly: a kind missing from this list is
 *  stored, traceable, and invisible until the page is reloaded. */
export const KINDS = [
  "user_message",
  "agent_message",
  "reasoning",
  "reasoning_delta",
  "delta",
  "turn",
  "spawned",
  "finished",
  "tool_call",
  "tool_result",
  "tool_delta",
  "steer",
  "steer_retracted",
  "steer_preempted",
  "cleanup",
  "progress",
  "memory_review",
  "max_iterations",
  "max_iterations_continued",
  "model_retry",
  "title",
  "session_memory",
  "done",
  "error",
  "resumed",
  "goal",
  "goal_complete",
  "goal_continued",
  "goal_capped",
  "goal_idle",
  "goal_blocked",
  "goal_edited",
  "goal_resumed",
  "goal_session",
  "plan",
  "plan_updated",
  "plan_implemented",
  "plan_cancelled",
  "compacted",
  "usage",
  "rewound",
  "schedule",
  "schedule_fired",
  "schedule_skipped",
  "schedule_report",
  "schedule_cancelled",
  "message",
] as const

/** Subscribe to a conversation.
 *
 *  The browser's EventSource reconnects on its own and sends Last-Event-ID,
 *  which the server uses to resume; `since` covers the other case, where the
 *  app already has events in memory from a previous mount and must not render
 *  them twice. */
export function subscribeEvents(
  threadId: string,
  handlers: StreamHandlers,
  since = 0,
): () => void {
  const url = `/api/threads/${threadId}/events${since > 0 ? `?since=${since}` : ""}`
  const source = new EventSource(url)
  let closed = false

  source.onopen = () => handlers.onOpen?.()
  source.onerror = () => {
    // EventSource retries by itself unless the connection was closed for
    // good; only a terminal state is worth telling the UI about.
    if (source.readyState === EventSource.CLOSED && !closed) {
      handlers.onClose?.("error")
    }
  }

  for (const kind of KINDS) {
    source.addEventListener(kind, (e) => {
      const parsed = parse(e as MessageEvent)
      if (parsed) handlers.onEvent(parsed)
    })
  }
  source.addEventListener("ready", (e) => {
    try {
      handlers.onReady?.(JSON.parse((e as MessageEvent).data))
    } catch {
      handlers.onReady?.({ seq: 0 })
    }
  })

  return () => {
    closed = true
    source.close()
    handlers.onClose?.("closed")
  }
}

function parse(e: MessageEvent): SwarmEvent | null {
  try {
    const data = JSON.parse(e.data) as SwarmEvent
    // The event name is the authority: the payload's kind and the SSE event
    // name are written from the same field, but only one of them is what the
    // listener matched on.
    return { ...data, kind: e.type === "message" ? data.kind : e.type }
  } catch {
    return null
  }
}
