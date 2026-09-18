import { ApiError, api } from "@/lib/api"
import { resolveLocale, t, type LocalePref } from "@/lib/i18n"
import type { Schedule, ScheduleCreate, SchedulePatch } from "@/lib/types"

/** Active thread wake targeting this conversation — the composer banner. */
export function activeWake(
  schedules: Schedule[] | undefined,
  threadId: string | undefined,
): Schedule | undefined {
  if (!threadId) return undefined
  return (schedules ?? []).find(
    (row) =>
      row.kind === "thread" &&
      row.thread_id === threadId &&
      row.status === "active",
  )
}

export type ScheduleSlice = {
  schedules: Schedule[]
  scheduleUnread: number
  scheduleInboxOpen: boolean
  refreshSchedules: () => Promise<void>
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
}

type SetSchedule = (
  partial:
    | Partial<ScheduleSlice>
    | Partial<Pick<ScheduleHost, "error">>
    | ((
        s: ScheduleSlice & ScheduleHost,
      ) => Partial<ScheduleSlice> | Partial<Pick<ScheduleHost, "error">>),
) => void

/** Inbox actions. Kept out of app.ts so that file stays under 1000 lines. */
export function scheduleActions(
  set: SetSchedule,
  get: () => ScheduleSlice & ScheduleHost,
  deps: { fail: (e: unknown) => string },
): ScheduleSlice {
  return {
    schedules: [],
    scheduleUnread: 0,
    scheduleInboxOpen: false,

    refreshSchedules: async () => {
      try {
        const got = await api.schedules()
        set({ schedules: got.schedules ?? [], scheduleUnread: got.unread ?? 0 })
      } catch (e) {
        set({ error: deps.fail(e) })
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
        await get().refreshSchedules()
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    runScheduleNow: async (id) => {
      try {
        await api.runSchedule(id)
        await get().refreshSchedules()
      } catch (e) {
        if (e instanceof ApiError && e.code === "skipped_busy") {
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

    closeScheduleInbox: () => set({ scheduleInboxOpen: false }),
  }
}
