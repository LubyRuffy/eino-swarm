import { api } from "@/lib/api"
import { ASK_TOOL } from "@/lib/transcript-ask"
import { compactIsStarting } from "@/lib/transcript-notices"
import {
  collapseLiveEvents,
  parseReview,
  reduceEvent,
  splitToolCall,
  toolNameOf,
  type TranscriptState,
} from "@/lib/transcript"
import { MEMORY_WRITE_TOOLS, memoryWriteLanded } from "@/lib/tool-view"
import { parseUsage } from "@/lib/usage"
import type {
  Followup,
  Schedule,
  SwarmEvent,
  Thread,
  ThreadStatus,
  Turn,
  UsageSnapshot,
} from "@/lib/types"
import { setThreadRunning } from "@/lib/thread-title"
import { applyGoalThreadFlags } from "./goal-events"
import { applyPlanThreadFlags } from "./plan-events"
import { useProjects } from "./projects"
import { rememberRewind, rememberStored } from "./thread-history"
import { bumpFollowups, dropMatchingFollowups } from "./followup-sync"
import {
  applyArmedSchedule,
  applyCancelledSchedule,
  parseArmedSchedule,
} from "@/lib/schedule-view"

/** The slice of the store the live event flush reads and writes. */
export type StreamSnapshot = {
  activeId?: string
  transcript: TranscriptState
  status: ThreadStatus
  threads: Thread[]
  usage?: UsageSnapshot
  followups: Followup[]
  schedules: Schedule[]
  refreshFiles: () => Promise<void>
  refreshThreads: () => Promise<void>
  refreshFollowups: () => Promise<void>
  refreshSchedules: () => Promise<void>
}

type StreamSet = (
  partial:
    | Partial<StreamSnapshot>
    | { turns: Turn[] }
    | ((s: StreamSnapshot) => Partial<StreamSnapshot>),
) => void

type StreamGet = () => StreamSnapshot

/** Streamed events that have arrived since the last animation frame. One
 *  rAF applies them together, so a burst of tokens is one React render. */
let queued: SwarmEvent[] = []
let queuedThread = ""
let raf = 0

/** Fold one event into the transcript, ignoring anything for a conversation
 *  the user has already navigated away from. Streamed tokens are queued and
 *  applied on the next animation frame so a burst of deltas is one render,
 *  not one render per token. Terminal events flush immediately: a `done` that
 *  sat behind a rAF would leave the composer looking busy after the turn ended. */
export function queueEvent(
  set: StreamSet,
  get: StreamGet,
  threadId: string,
  ev: SwarmEvent,
) {
  if (get().activeId !== threadId) return
  if (queuedThread !== threadId) {
    flushQueued(set, get)
    queuedThread = threadId
  }
  queued.push(ev)
  if (ev.kind === "done" || ev.kind === "error" || ev.kind === "user_message" ||
      ev.kind === "max_iterations" || ev.kind === "max_iterations_continued" ||
      ev.kind === "model_retry" || ev.kind === "title" ||
      ev.kind === "resumed" || ev.kind === "goal" || ev.kind === "goal_complete" ||
      ev.kind === "goal_continued" || ev.kind === "goal_capped" || ev.kind === "goal_blocked" ||
      ev.kind === "goal_edited" || ev.kind === "goal_resumed" || ev.kind === "goal_idle" || ev.kind === "goal_session" ||
      ev.kind === "compacted" ||
      ev.kind === "plan" || ev.kind === "plan_updated" ||
      ev.kind === "plan_implemented" || ev.kind === "plan_cancelled" ||
      ev.kind === "schedule" || ev.kind === "schedule_fired" ||
      ev.kind === "schedule_skipped" || ev.kind === "schedule_report" ||
      ev.kind === "schedule_cancelled" ||
      ev.kind === "rewound" ||
      ev.kind === "tool_call" || ev.kind === "tool_result" ||
      ev.kind === "steer" || ev.kind === "steer_retracted" || ev.kind === "steer_preempted") {
    flushQueued(set, get)
    return
  }
  if (!raf) {
    raf = requestAnimationFrame(() => {
      raf = 0
      flushQueued(set, get)
    })
  }
}

export function dropQueued() {
  queued = []
  queuedThread = ""
  if (raf) {
    cancelAnimationFrame(raf)
    raf = 0
  }
}

function flushQueued(set: StreamSet, get: StreamGet) {
  if (raf) {
    cancelAnimationFrame(raf)
    raf = 0
  }
  if (queued.length === 0) return
  const threadId = queuedThread
  const events = collapseLiveEvents(queued)
  queued = []
  const state = get()
  if (state.activeId !== threadId) return

  let transcript = state.transcript
  let status = state.status
  let threads = state.threads
  let usage = state.usage
  let followups = state.followups ?? []
  let droppedFollowups: Followup[] = []
  let closed = false
  let schedules = state.schedules ?? []
  let schedulesDirty = false
  for (const ev of events) {
    rememberStored(threadId, ev)
    if (ev.kind === "rewound") {
      const from = Number.parseInt(String(ev.text ?? ""), 10)
      if (Number.isFinite(from) && from > 0) rememberRewind(threadId, from)
    }
    transcript = reduceEvent(transcript, ev)
    if (ev.kind === "user_message" || ev.kind === "resumed") {
      status = withRunningClock(status, {
        turn_id: ev.turn_id,
        started_at: ev.created_at,
        awaiting_continue: ev.kind === "resumed" ? false : status.awaiting_continue,
      })
    }
    if (ev.kind === "user_message") {
      const next = dropMatchingFollowups(followups, ev.text ?? "")
      if (next.length !== followups.length) {
        droppedFollowups = followups.filter((f) => !next.includes(f))
        followups = next
      }
    }
    if (ev.kind === "max_iterations") {
      status = { ...status, running: true, awaiting_continue: true, turn_id: ev.turn_id }
    }
    if (ev.kind === "max_iterations_continued") {
      status = { ...status, running: true, awaiting_continue: false }
    }
    if (ev.kind === "tool_call") {
      const { name } = splitToolCall(ev.text ?? "")
      if (name === ASK_TOOL) {
        status = withRunningClock(status, {
          awaiting_answer: true,
          turn_id: ev.turn_id,
        })
      }
    }
    if (ev.kind === "tool_result" && toolNameOf(transcript, ev.tool_call_id) === ASK_TOOL) {
      status = { ...status, awaiting_answer: false }
    }
    if (ev.kind === "done" || ev.kind === "error") {
      status = { running: false }
      closed = true
    }
    if (ev.kind === "title" && ev.text && !ev.err) {
      const title = ev.text
      threads = threads.map((t) =>
        t.id === threadId ? { ...t, title, title_auto: false } : t,
      )
    }
    threads = applyGoalThreadFlags(threads, threadId, ev)
    threads = applyPlanThreadFlags(threads, threadId, ev)
    if (ev.kind === "goal_resumed" || ev.kind === "goal_continued" || ev.kind === "plan_implemented" || ev.kind === "schedule_fired") {
      status = withRunningClock(status, {
        turn_id: ev.turn_id,
        started_at: ev.created_at,
      })
    }
    if (ev.kind === "compacted") {
      if (compactIsStarting(ev)) {
        status = withRunningClock(status, {
          compressing: true,
          turn_id: ev.turn_id,
        })
      } else {
        status = { ...status, compressing: false }
      }
      if (!ev.err && (ev.seq ?? 0) > 0) {
        threads = threads.map((t) =>
          t.id === threadId ? { ...t, compacted: true } : t,
        )
      }
    }
    if (
      ev.kind === "schedule" ||
      ev.kind === "schedule_fired" ||
      ev.kind === "schedule_cancelled" ||
      ev.kind === "schedule_report"
    ) {
      schedulesDirty = true
    }
    if (ev.kind === "schedule") {
      schedules = applyArmedSchedule(schedules, parseArmedSchedule(ev.text), threadId)
    }
    if (ev.kind === "schedule_cancelled") {
      schedules = applyCancelledSchedule(schedules, ev.text ?? "")
    }
    if (ev.kind === "usage") {
      const next = parseUsage(ev.text)
      if (next) usage = next
    }
    if (ev.kind === "memory_review") {
      // The review wrote the files directly, so the panel has to re-read them
      // rather than derive the new state from the event.
      const outcome = parseReview(ev)
      void useProjects.getState().loadMemory()
      if (outcome?.changed) useProjects.getState().noteMemoryWrite()
      useProjects.getState().finishReview(ev.turn_id, outcome)
    }
    if (ev.kind === "tool_result") {
      const name = toolNameOf(transcript, ev.tool_call_id)
      if (name && MEMORY_WRITE_TOOLS.has(name) && memoryWriteLanded(ev.text)) {
        void useProjects.getState().loadMemory()
        useProjects.getState().noteMemoryWrite()
      }
    }
  }
  threads = setThreadRunning(
    threads,
    threadId,
    status.running,
    Boolean(status.awaiting_answer),
  )
  const followupsDirty = droppedFollowups.length > 0
  set({
    transcript,
    status,
    threads,
    usage,
    ...(followupsDirty ? { followups } : {}),
    ...(schedulesDirty ? { schedules } : {}),
  })
  if (followupsDirty) {
    bumpFollowups()
    const id = threadId
    void Promise.all(
      droppedFollowups.map((f) => api.deleteFollowup(id, f.id).catch(() => undefined)),
    ).then(() => get().refreshFollowups())
  }
  if (schedulesDirty) void get().refreshSchedules()
  if (closed) {
    void get().refreshFiles()
    void get().refreshThreads()
    void get().refreshFollowups()
    void api
      .turns(threadId)
      .then((turns) => set({ turns }))
      .catch(() => undefined)
  }
}

/** Claim the conversation is mid-turn. `done` wipes `started_at`; a later
 *  `goal_continued` that only sets `running` leaves the header clamped at 1s
 *  for the whole auto-continue, which is how a 10-hour pursuit reads as a
 *  one-second job. */
export function withRunningClock(
  status: ThreadStatus,
  extra: Partial<ThreadStatus> = {},
): ThreadStatus {
  return {
    ...status,
    ...extra,
    running: true,
    started_at: extra.started_at ?? status.started_at ?? new Date().toISOString(),
  }
}
