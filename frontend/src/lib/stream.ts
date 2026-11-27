import type { SwarmEvent, ThreadStatus } from "./types"
import { publishTaskPanelEvent } from "./task-panel-events"
import type { PanelChange, TaskPanel } from "@/task-panels/types"

import { parseGraphRevision, publishGraphEvent } from "./graph/events"

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

/** The event-stream kinds shared with the phone's remote PushKinds list.
 *  EventSource delivers by name, so each one has to be subscribed explicitly:
 *  stored control receipts must reach the reducer through the same contract. */
export const KINDS = [
  "user_message",
  "agent_message",
  "reasoning",
  "reasoning_delta",
  "generation_started",
  "generation_completed",
  "generation_interrupted",
  "generation_recovered",
  "desktop_recovery",
  "delta",
  "turn",
  "spawned",
  "finished",
  "tool_call",
  "builtin_tool",
  "tool_result",
  "image_generation",
  "ask_recovered",
  "tool_delta",
  "tool_call_delta",
  "steer",
  "task_message",
  "steer_retracted",
  "steer_revised",
  "steer_preempted",
  "steer_consumed",
  "cleanup",
  "workers_retained",
  "progress",
  "memory_review",
  "max_iterations",
  "max_iterations_continued",
  "workers_collection_resumed",
  "workers_collection_declined",
  "model_retry",
  "model_backoff",
  "title",
  "decision",
  "composer_prediction",
  "session_memory",
  "done",
  "error",
  "failure_continued",
  "resumed",
  "restart_preparing",
  "engine_restore",
  "engine_restart_checkpoint",
  "restart_ready",
  "restart_resumed",
  "user_stop_requested",
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
  const query = new URLSearchParams()
  if (since > 0) query.set("since", String(since))
  query.set("panels", "1")
  query.set("graph", "1")
  const url = `/api/threads/${threadId}/events?${query}`
  const source = new EventSource(url)
  let closed = false

  source.onopen = () => {
    if (closed) return
    publishTaskPanelEvent(threadId, { kind: "connection", connected: true })
    publishGraphEvent(threadId, { kind: "connection", connected: true })
    handlers.onOpen?.()
  }
  source.onerror = (event) => {
    // A named application `event: error` also invokes this property. It says
    // nothing about the HTTP connection and still reaches onEvent below.
    if (event instanceof MessageEvent) return
    if (!closed) {
      publishTaskPanelEvent(threadId, { kind: "connection", connected: false })
      publishGraphEvent(threadId, { kind: "connection", connected: false })
    }
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
  // Sharing this connection prevents panels from consuming a third browser
  // HTTP/1 connection per tab and starving draft writes in a second window.
  source.addEventListener("panel_ready", (event) => {
    if (closed) return
    try {
      const body = JSON.parse((event as MessageEvent).data) as { panels: TaskPanel[]; cursor: number }
      if (Array.isArray(body.panels) && Number.isSafeInteger(body.cursor) && body.cursor >= 0)
        publishTaskPanelEvent(threadId, { kind: "ready", ...body })
    } catch { /* Initial GET and the next reconnect can repair a bad frame. */ }
  })
  source.addEventListener("panel_update", (event) => {
    if (closed) return
    try {
      const change = JSON.parse((event as MessageEvent).data) as PanelChange
      if (change.thread_id === threadId) publishTaskPanelEvent(threadId, { kind: "update", change })
    } catch { /* A malformed notification cannot enter chat replay. */ }
  })
  source.addEventListener("panel_error", () => {
    if (!closed) publishTaskPanelEvent(threadId, { kind: "error", error: "Panel state unavailable" })
  })

  source.addEventListener("graph_updated", (event) => {
    if (closed) return
    const revision = parseGraphRevision((event as MessageEvent).data, threadId)
    if (revision !== undefined) publishGraphEvent(threadId, { kind: "revision", revision })
  })

  return () => {
    closed = true
    source.close()
    publishTaskPanelEvent(threadId, { kind: "connection", connected: false })
    publishGraphEvent(threadId, { kind: "connection", connected: false })
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
