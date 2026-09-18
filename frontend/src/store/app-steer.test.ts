import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  preempts: [] as string[],
  retracts: [] as Array<{ id: string; seq: number }>,
  preemptCode: undefined as string | undefined,
  retractStatus: undefined as number | undefined,
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: "New conversation",
    provider_id: "default",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: true,
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
        status: { running: true },
      }),
      turns: async () => [],
      threadLog: async () => ({ events: [], has_more: false }),
      agentLog: async () => ({ events: [] }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => [],
      schedules: async () => ({ schedules: [], unread: 0 }),
      preempt: async (id: string) => {
        if (fake.preemptCode) {
          throw new ApiError("no unread steering", 409, fake.preemptCode)
        }
        fake.preempts.push(id)
        return { preempted: true }
      },
      retractSteer: async (id: string, seq: number) => {
        if (fake.retractStatus) {
          throw new ApiError("gone", fake.retractStatus)
        }
        fake.retracts.push({ id, seq })
      },
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (
    _id: string,
    handlers: { onReady?: (p: unknown) => void },
  ) => {
    handlers.onReady?.({ status: { running: true } })
    return () => undefined
  },
}))

beforeEach(() => {
  fake.preempts.length = 0
  fake.retracts.length = 0
  fake.preemptCode = undefined
  fake.retractStatus = undefined
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    followups: [],
    error: undefined,
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

describe("interrupt inject", () => {
  it("preempts the open conversation so unread steering can land now", async () => {
    await useApp.getState().boot()
    await useApp.getState().preempt()
    expect(fake.preempts).toEqual(["th_old"])
    expect(useApp.getState().error).toBeUndefined()
  })

  it("retracts one unread bubble by event seq", async () => {
    await useApp.getState().boot()
    await useApp.getState().retractSteer(12)
    expect(fake.retracts).toEqual([{ id: "th_old", seq: 12 }])
  })

  it("swallows a race where the inbox already drained", async () => {
    await useApp.getState().boot()
    fake.preemptCode = "no_steer"
    await useApp.getState().preempt()
    expect(useApp.getState().error).toBeUndefined()
    fake.retractStatus = 404
    await useApp.getState().retractSteer(12)
    expect(useApp.getState().error).toBeUndefined()
  })
})
