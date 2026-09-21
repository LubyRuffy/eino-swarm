import { ApiError, api } from "@/lib/api"
import { resolveLocale, t, type LocalePref } from "@/lib/i18n"
import { setThreadRunning } from "@/lib/thread-title"
import type {
  Schedule,
  ScheduleCreate,
  SchedulePatch,
  Thread,
  ThreadStatus,
} from "@/lib/types"
import {
  activeWake,
  applyCancelledSchedule,
  keepArmedWakes,
} from "@/lib/schedule-view"
import { withRunningClock } from "./app-stream"

export { activeWake } from "@/lib/schedule-view"

/** Conversations whose next turn is a parked wait, not a live tool call.
 *  Standalone jobs mint their own thread on fire; they do not mark origin.
 *  Listing `waiting` covers a wake the schedule list has not caught yet. */
export function waitingThreadIds(
  schedules: Schedule[] | undefined,
  threads?: Array<{ id: string; waiting?: boolean }>,
): Set<string> {
  const ids = new Set<string>()
  for (const row of schedules ?? []) {
    if (row.kind !== "thread" || row.status !== "active" || !row.thread_id) continue
    ids.add(row.thread_id)
  }
  for (const thread of threads ?? []) {
    if (thread.waiting && thread.id) ids.add(thread.id)
  }
  return ids
}

/** Run now on this conversation must paint Working before the first SSE. */
export function wakeTargetsOpenThread(
  row: Schedule | undefined,
  threadId: string | undefined,
): boolean {
  return Boolean(
    row && row.kind === "thread" && threadId && row.thread_id === threadId,
  )
}

export type ScheduleSlice = {
  schedules: Schedule[]
  scheduleUnread: number
  scheduleInboxOpen: boolean
  refreshSchedules: (opts?: { silent?: boolean }) => Promise<void>
  createSchedule: (body: ScheduleCreate) => Promise<void>
  patchSchedule: (id: string, patch: SchedulePatch) => Promise<void>
  deleteSchedule: (id: string) => Promise<void>
  runScheduleNow: (id: string) => Promise<void>
  readScheduleRun: (rid: string) => Promise<void>
  openScheduleInbox: () => void
  closeScheduleInbox: () => void
}

type ScheduleHost = {
  locale: LocalePref
  error?: string
  activeId?: string
  status: ThreadStatus
  threads: Thread[]
  refreshThreads: () => Promise<void>
}

type ScheduleStatePatch = Partial<ScheduleSlice> &
  Partial<Pick<ScheduleHost, "error" | "status" | "threads">>

type SetSchedule = (
  partial: ScheduleStatePatch | ((s: ScheduleSlice & ScheduleHost) => ScheduleStatePatch),
) => void

/** Inbox actions. Kept out of app.ts so that file stays under 1000 lines. */
let scheduleFetch = 0

export function scheduleActions(
  set: SetSchedule,
  get: () => ScheduleSlice & ScheduleHost,
  deps: { fail: (e: unknown) => string },
): ScheduleSlice {
  return {
    schedules: [],
    scheduleUnread: 0,
    scheduleInboxOpen: false,

    refreshSchedules: async (opts) => {
      const n = ++scheduleFetch
      try {
        const got = await api.schedules()
        if (n !== scheduleFetch) return
        set((s) => {
          const incoming = got.schedules ?? []
          const schedules = keepArmedWakes(s.schedules, incoming, {
            waiting: Boolean(s.status.waiting),
            threadId: s.activeId,
          })
          return {
            schedules,
            scheduleUnread: got.unread ?? 0,
            status: {
              ...s.status,
              waiting: Boolean(s.status.waiting || activeWake(schedules, s.activeId)),
            },
          }
        })
      } catch (e) {
        if (n !== scheduleFetch) return
        // The sidebar tick is not a user action; a dropped GET must not toast.
        if (!opts?.silent) set({ error: deps.fail(e) })
      }
    },

    createSchedule: async (body) => {
      try {
        await api.createSchedule(body)
        await get().refreshSchedules()
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    patchSchedule: async (id, patch) => {
      try {
        await api.patchSchedule(id, patch)
        await get().refreshSchedules()
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    deleteSchedule: async (id) => {
      try {
        await api.deleteSchedule(id)
        set((s) => {
          const schedules = applyCancelledSchedule(s.schedules, id)
          return {
            schedules,
            status: {
              ...s.status,
              waiting: Boolean(activeWake(schedules, s.activeId)),
            },
          }
        })
        await get().refreshSchedules()
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    runScheduleNow: async (id) => {
      const row = get().schedules.find((s) => s.id === id)
      const activeId = get().activeId
      const already = get().status.running
      const painted = wakeTargetsOpenThread(row, activeId) && !already
      try {
        if (painted && activeId) {
          set((s) => ({
            status: withRunningClock(s.status),
            threads: setThreadRunning(s.threads, activeId, true),
            error: undefined,
          }))
        }
        await api.runSchedule(id)
        await get().refreshSchedules()
        void get().refreshThreads()
      } catch (e) {
        const busy = e instanceof ApiError && e.code === "skipped_busy"
        if (painted && !busy && activeId) {
          set((s) => ({
            status: { running: false },
            threads: setThreadRunning(s.threads, activeId, false),
          }))
        }
        if (busy) {
          set({
            error: t(resolveLocale(get().locale), "schedule.skippedBusy"),
          })
          return
        }
        set({ error: deps.fail(e) })
      }
    },

    readScheduleRun: async (rid) => {
      try {
        await api.markScheduleRunRead(rid)
        await get().refreshSchedules()
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    openScheduleInbox: () => {
      set({ scheduleInboxOpen: true })
      void get().refreshSchedules()
    },

    closeScheduleInbox: () => set({ scheduleInboxOpen: false, error: undefined }),
  }
}
