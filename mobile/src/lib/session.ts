import { ASK_TOOL, splitToolCall } from "./ask"
import {
  OpEvent,
  OpReady,
  type RemoteEvent,
  type RemoteResponse,
  type ThreadDetail,
} from "./rpc"
import { applyEvent, pendingAsk, type CompactBlock } from "./transcript"

/** One open thread on the phone. threadId is set before watch returns,
 *  because catch-up events can beat React's setState. */
export type PhoneView = {
  threadId: string | null
  lastSeq: number
  blocks: CompactBlock[]
  detail: ThreadDetail | null
}

export function emptyView(): PhoneView {
  return { threadId: null, lastSeq: 0, blocks: [], detail: null }
}

export function openView(detail: ThreadDetail): PhoneView {
  return { threadId: detail.id, lastSeq: 0, blocks: [], detail }
}

export function markRunning(view: PhoneView): PhoneView {
  if (!view.detail || view.detail.running) return view
  return {
    ...view,
    detail: {
      ...view.detail,
      running: { thread_id: view.detail.id, title: view.detail.title },
    },
  }
}

export function applyPush(view: PhoneView, resp: RemoteResponse): PhoneView {
  if (!view.threadId) return view
  if (resp.thread_id && resp.thread_id !== view.threadId) return view
  if (resp.op === OpReady) return applyReady(view, resp)
  if (resp.op === OpEvent && resp.event) return applyLiveEvent(view, resp.event)
  return view
}

function applyReady(view: PhoneView, resp: RemoteResponse): PhoneView {
  const st = resp.status
  if (!st || !view.detail) {
    return { ...view, lastSeq: Math.max(view.lastSeq, resp.seq ?? 0) }
  }
  return {
    ...view,
    lastSeq: Math.max(view.lastSeq, resp.seq ?? 0),
    detail: {
      ...view.detail,
      running: st.running
        ? {
            thread_id: view.detail.id,
            title: view.detail.title,
            turn_id: st.turn_id,
            ask_user: st.awaiting_answer,
          }
        : undefined,
    },
  }
}

function applyLiveEvent(view: PhoneView, ev: RemoteEvent): PhoneView {
  if (ev.seq > 0 && ev.seq <= view.lastSeq) return view
  const blocks = applyEvent(view.blocks, ev)
  let detail = view.detail
  if (detail) {
    detail = patchDetail(detail, ev, blocks)
  }
  return {
    ...view,
    lastSeq: ev.seq > view.lastSeq ? ev.seq : view.lastSeq,
    blocks,
    detail,
  }
}

function patchDetail(
  detail: ThreadDetail,
  ev: RemoteEvent,
  blocks: CompactBlock[],
): ThreadDetail {
  let next = detail
  if (ev.kind === "title" && ev.text) {
    next = { ...next, title: ev.text }
  }
  if (ev.kind === "goal" && ev.text) {
    next = { ...next, goal: ev.text, goal_on: true }
  }
  if (ev.kind === "goal_resumed" || ev.kind === "goal_continued") {
    next = { ...next, goal_on: true }
  }
  if (ev.kind === "goal_complete" || ev.kind === "goal_blocked") {
    next = { ...next, goal_on: false }
  }
  if (ev.kind === "plan") {
    next = { ...next, plan_on: true }
  }
  if (ev.kind === "plan_cancelled" || ev.kind === "plan_implemented") {
    next = { ...next, plan_on: false }
  }
  if (ev.kind === "turn") {
    next = {
      ...next,
      running: {
        thread_id: next.id,
        title: next.title,
        turn_id: ev.turn_id,
        ask_user: false,
      },
    }
  }
  if (ev.kind === "done" || ev.kind === "error") {
    next = { ...next, running: undefined }
  }
  const asking =
    Boolean(pendingAsk(blocks)) ||
    (ev.kind === "tool_call" && splitToolCall(ev.text).name === ASK_TOOL)
  if (next.running && asking !== Boolean(next.running.ask_user)) {
    next = { ...next, running: { ...next.running, ask_user: asking } }
  }
  return next
}
