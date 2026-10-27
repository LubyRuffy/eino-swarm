import { api } from "@/lib/api"
import { askStatusFromTranscript } from "@/lib/transcript-ask"
import { deskDest, listDestForThread, setDeskDest } from "@/lib/desk-nav"
import { LIBRARY_ID } from "@/lib/library"
import { subscribeEvents } from "@/lib/stream"
import type { TranscriptState } from "@/lib/transcript"
import { setThreadRunning, upsertThread } from "@/lib/thread-title"
import { logPageSize, type ThreadLog } from "@/lib/thread-log"
import type { SwarmEvent, Thread, ThreadStatus } from "@/lib/types"
import { queueEvent } from "./app-stream"
import { bumpFollowups, isFollowupGeneration } from "./followup-sync"
import { useProjects } from "./projects"
import { applyTail, historyNewestSeq, loadUntilVisibleHistory } from "./thread-history"
import type { AppState } from "./app"
import { beginTurnsRead, isCurrentTurnsRead } from "./turns-sync"

type SetApp = (patch: Partial<AppState> | ((state: AppState) => Partial<AppState>)) => void

/** A decline owns only its matching waiting flag, never the snapshot's running
 *  state. A newer wait or ready watermark can describe different work. */
function afterCollectionDecline(status: ThreadStatus, transcript: TranscriptState, through: number): ThreadStatus {
  const card = transcript.agents.manager?.blocks.filter((block) => block.confirm).at(-1)
  const pause = card?.confirm
  if (!pause || pause.reason !== "workers_uncollected" || !pause.waitId || pause.pending ||
    pause.continued !== false || pause.declinedSeq === undefined || status.turn_id !== card.turnId ||
    !(through <= pause.declinedSeq)) return status
  return { ...status, awaiting_continue: false }
}

/** An opening snapshot may fill unchanged metadata, but cannot undo a live
 *  title, goal, completion or context mutation that arrived during its read. */
function mergeOpenedThread(incoming: Thread, current: Thread | undefined, baseline: Thread | undefined, advanced: boolean): Thread {
  if (!current) return incoming
  const merged = { ...current }
  for (const key of Object.keys(incoming) as Array<keyof Thread>) {
    if (current[key] !== baseline?.[key] || (advanced && key === "context_chars")) continue
    Object.assign(merged, { [key]: incoming[key] })
  }
  return merged
}

/** Each HTTP response owns one slice. The tail controls rendering and SSE;
 *  replay-derived metadata never gates either of them. `current` includes the
 *  visit identity, since thread id alone cannot reject an A → B → A response. */
export async function loadOpenedThread(
  id: string,
  get: () => AppState,
  set: SetApp,
  current: () => boolean,
  attach: (unsubscribe: () => void) => void,
  fail: (error: unknown) => string,
  guess: string,
  keepList?: boolean,
): Promise<void> {
  const baseline = get()
  const originalThread = baseline.threads.find((thread) => thread.id === id)
  const followupToken = bumpFollowups()
  let contextAdvanced = false
  let liveStatus = false
  const failed = (error: unknown) => {
    if (current()) set({ error: fail(error) })
  }

  const detail = api.thread(id).then(({ thread, status, usage }) => {
    if (!current()) return
    if (!keepList && deskDest() === guess) setDeskDest(listDestForThread(thread.project_id))
    set((s) => {
      const rewound = s.transcript.rewindCut !== baseline.transcript.rewindCut
      const stale = contextAdvanced || rewound
      const unchangedStatus = !liveStatus && !rewound &&
        s.status.running === baseline.status.running && s.status.turn_id === baseline.status.turn_id
      const openedStatus = unchangedStatus ? afterCollectionDecline(
        { ...status, waiting: s.status.waiting ?? status.waiting }, s.transcript, 0) : s.status
      const effectiveStatus = askStatusFromTranscript(s.transcript, openedStatus)
      const merged = mergeOpenedThread(thread, s.threads.find((row) => row.id === id), originalThread, stale)
      return {
        status: effectiveStatus,
        ...(s.usage === baseline.usage && !rewound ? { usage } : {}),
        ...(unchangedStatus && status.running ? { transcript: { ...s.transcript, running: true } } : {}),
        threads: setThreadRunning(upsertThread(s.threads, merged), id, effectiveStatus.running, Boolean(effectiveStatus.awaiting_answer)),
      }
    })
    // Memory belongs to the project; it can load independently of the log.
    useProjects.getState().select(thread.project_id || undefined)
    void useProjects.getState().loadMemory(thread.project_id || LIBRARY_ID)
  }).catch(failed)
  const turnsToken = beginTurnsRead()
  const turns = api.turns(id).then((rows) => {
    if (current() && isCurrentTurnsRead(turnsToken) && get().transcript.rewindCut === baseline.transcript.rewindCut && get().turns === baseline.turns) set({ turns: rows })
  }).catch(failed)
  const followups = api.followups(id).then((rows) => {
    if (current() && isFollowupGeneration(followupToken) && get().followups === baseline.followups) set({ followups: rows })
  }).catch(failed)
  const tail = (async () => {
    let log: ThreadLog | undefined
    try {
      log = await api.threadLog(id, { limit: logPageSize(0) })
    } catch {
      // A missing log endpoint falls back to SSE replay from the start.
    }
    if (!current()) return
    let since = 0
    // A local rewind/send may already have replaced the empty opening state.
    // Status-only metadata changes do not give the tail ownership of that draft.
    if (log && !get().transcript.rewindCut && get().transcript.lastSeq === baseline.transcript.lastSeq) {
      const transcript = applyTail(id, log.events ?? [], log.roster ?? [])
      if (get().status.running) transcript.running = true
      since = historyNewestSeq()
      set({ connected: true, transcript, loaded: true, historyHasMore: Boolean(log.has_more) })
    }
    void useProjects.getState().restoreReview(id, log?.events ?? [])
    void get().refreshSchedules()
    attach(subscribeEvents(id, {
      onEvent: (ev) => {
        if (!current()) return
        if (ev.seq === 0 || ev.seq > since) {
          if (changesReplay(ev)) contextAdvanced = true
          if (changesStatus(ev)) liveStatus = true
        }
        queueEvent(set, get, id, ev)
      },
      onReady: ({ seq, status: live }) => {
        if (!current()) return
        if (live) liveStatus = true
        set((s) => {
          const effective = live ? askStatusFromTranscript(s.transcript,
            afterCollectionDecline({ ...live, waiting: live.waiting ?? s.status.waiting }, s.transcript, seq ?? Infinity)) : s.status
          return {
            loaded: true,
            connected: true,
            status: effective,
            transcript: live ? { ...s.transcript, running: live.running } : s.transcript,
            historyHasMore: since > 0 ? s.historyHasMore : false,
            threads: live ? setThreadRunning(s.threads, id, Boolean(live.running), Boolean(effective.awaiting_answer)) : s.threads,
          }
        })
        void get().refreshFiles()
      },
      onClose: (reason) => {
        if (current()) set({ connected: reason !== "error" })
      },
    }, since))
    // Worker/tool-only tails still page toward the latest visible manager row,
    // after rendering and subscribing so neither UI nor live events wait.
    if (log?.has_more && current()) await loadUntilVisibleHistory(get)
  })().catch(failed)
  await Promise.all([detail, turns, followups, tail])
}

function changesReplay(event: SwarmEvent): boolean {
  return event.kind === "user_message" || event.kind === "steer" ||
    event.kind === "steer_retracted" || event.kind === "steer_revised" ||
    event.kind === "rewound" || event.kind === "compacted" || event.kind === "goal" ||
    event.kind === "goal_edited" || (event.kind === "agent_message" && (!event.agent_id || event.agent_id === "manager"))
}

function changesStatus(event: SwarmEvent): boolean {
  return event.kind === "user_message" || event.kind === "resumed" || event.kind === "done" ||
    event.kind === "error" || event.kind === "max_iterations" || event.kind === "max_iterations_continued" || event.kind === "workers_collection_resumed" ||
    event.kind === "tool_call" || event.kind === "tool_result" || event.kind === "ask_recovered" || event.kind === "compacted" ||
    event.kind === "goal_resumed" || event.kind === "goal_continued" || event.kind === "plan_implemented" ||
    event.kind === "schedule" || event.kind === "schedule_fired" || event.kind === "schedule_cancelled"
}
