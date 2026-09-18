import { beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript } from "@/lib/transcript"
import type { Schedule } from "@/lib/types"
import { useApp } from "@/store/app"
import { activeWake } from "@/store/app-schedule"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  listed: 0,
  rows: [] as Schedule[],
  unread: 0,
  created: [] as Array<Record<string, unknown>>,
  patched: [] as Array<{ id: string; patch: Record<string, unknown> }>,
  deleted: [] as string[],
  ran: [] as string[],
  runBusy: false,
  marked: [] as string[],
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: "New conversation",
    provider_id: "default",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: false,
  })
  class ApiError extends Error {
    constructor(
      message: string,
      readonly status: number,
      readonly code?: string,
    ) {
      super(message)
    }
  }
  return {
    ApiError,
    api: {
      meta: async () => ({ mode: "web", configured: true, capabilities: {} }),
      models: async () => ({ models: [], default: "default", mock: true }),
      threads: async () => [thread("th_old")],
      projects: async () => [],
      thread: async (id: string) => ({
        thread: thread(id),
        status: { running: false },
      }),
      turns: async () => [],
      threadLog: async () => ({ events: [], has_more: false }),
      agentLog: async () => ({ events: [] }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => [],
      schedules: async () => {
        fake.listed += 1
        return { schedules: fake.rows, unread: fake.unread }
      },
      createSchedule: async (body: Record<string, unknown>) => {
        fake.created.push(body)
        return { id: "sch_new", ...body, status: "active" }
      },
      patchSchedule: async (id: string, patch: Record<string, unknown>) => {
        fake.patched.push({ id, patch })
        return { id, ...patch }
      },
      deleteSchedule: async (id: string) => {
        fake.deleted.push(id)
      },
      runSchedule: async (id: string) => {
        if (fake.runBusy) {
          throw new ApiError("the conversation is already running", 409, "skipped_busy")
        }
        fake.ran.push(id)
        return { id: "tn_1", schedule_continue: true }
      },
      markScheduleRunRead: async (rid: string) => {
        fake.marked.push(rid)
      },
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (
    _id: string,
    handlers: {
      onEvent?: (ev: Record<string, unknown>) => void
      onReady?: (p: unknown) => void
    },
  ) => {
    fake.onEvent = handlers.onEvent
    handlers.onReady?.({ status: { running: false } })
    return () => undefined
  },
}))

function wait(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "thread",
    origin_thread_id: "th_old",
    thread_id: "th_old",
    project_id: "",
    provider_id: "",
    model: "",
    title: "Periodic check",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 60,
    cron: "",
    status: "active",
    next_run_at: "2026-09-19T04:00:00.000Z",
    run_count: 0,
    max_runs: 0,
    created_by: "manager",
    created_at: "2026-09-19T00:00:00.000Z",
    updated_at: "2026-09-19T00:00:00.000Z",
    ...partial,
  }
}

beforeEach(() => {
  fake.onEvent = undefined
  fake.listed = 0
  fake.rows = [wait()]
  fake.unread = 2
  fake.created = []
  fake.patched = []
  fake.deleted = []
  fake.ran = []
  fake.runBusy = false
  fake.marked = []
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    transcript: emptyTranscript(),
    followups: [],
    error: undefined,
    locale: "en",
    schedules: [],
    scheduleUnread: 0,
    scheduleInboxOpen: false,
  })
  useProjects.setState({
    projects: [],
    selectedId: undefined,
    memory: undefined,
    memoryProjectId: undefined,
    error: undefined,
    reviewing: false,
    reviewHint: undefined,
    pendingReviewTurnId: undefined,
  })
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    cb(0)
    return 1
  })
  vi.stubGlobal("cancelAnimationFrame", () => undefined)
})

describe("schedule store", () => {
  it("refreshes the inbox list and unread count", async () => {
    await useApp.getState().refreshSchedules()
    expect(useApp.getState().schedules).toEqual([
      expect.objectContaining({ id: "sch_1", title: "Periodic check" }),
    ])
    expect(useApp.getState().scheduleUnread).toBe(2)
  })

  it("loads waits on boot and when the thread list refreshes", async () => {
    await useApp.getState().boot()
    expect(fake.listed).toBeGreaterThanOrEqual(1)
    const afterBoot = fake.listed
    await useApp.getState().refreshThreads()
    expect(fake.listed).toBeGreaterThan(afterBoot)
  })

  it("sets a localized error when run-now hits a busy conversation", async () => {
    fake.runBusy = true
    await expect(useApp.getState().runScheduleNow("sch_1")).resolves.toBeUndefined()
    expect(fake.ran).toEqual([])
    expect(useApp.getState().error).toBe(
      "The conversation is already running a turn.",
    )
  })

  it("refreshes waits when the live stream arms, reports, or cancels one", async () => {
    await useApp.getState().boot()
    const afterBoot = fake.listed
    fake.onEvent?.({
      kind: "schedule",
      seq: 11,
      thread_id: "th_old",
      agent_id: "manager",
      text: '{"id":"sch_1"}',
      created_at: "2026-09-19T00:00:00.000Z",
    })
    await Promise.resolve()
    expect(fake.listed).toBeGreaterThan(afterBoot)
  })
})

describe("activeWake", () => {
  it("finds the active thread wake for that conversation", () => {
    const rows = [
      wait({ id: "sch_other", thread_id: "th_other" }),
      wait({ id: "sch_paused", status: "paused" }),
      wait({ id: "sch_job", kind: "standalone", thread_id: "" }),
      wait({ id: "sch_wake" }),
    ]
    expect(activeWake(rows, "th_old")?.id).toBe("sch_wake")
    expect(activeWake(rows, "th_missing")).toBeUndefined()
    expect(activeWake(rows, undefined)).toBeUndefined()
  })
})
