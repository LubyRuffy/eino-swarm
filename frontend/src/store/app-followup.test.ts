import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  steers: [] as Array<{ id: string; text: string }>,
  enqueued: [] as Array<{ id: string; text: string }>,
  queuedItems: [] as Array<{
    id: string
    thread_id: string
    seq: number
    text: string
    created_at: string
  }>,
  steeredFollowups: [] as Array<{ id: string; fid: string }>,
  enqueueIdle: false,
  steerWait: undefined as Promise<void> | undefined,
  followupsWait: undefined as Promise<void> | undefined,
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: "New conversation",
    title_auto: true,
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
      followups: async () => {
        if (fake.followupsWait) await fake.followupsWait
        return fake.queuedItems
      },
      enqueueFollowup: async (id: string, text: string) => {
        if (fake.enqueueIdle) {
          throw new ApiError("the conversation is not running", 409, "idle")
        }
        const item = {
          id: `fu_${fake.enqueued.length + 1}`,
          thread_id: id,
          seq: fake.enqueued.length + 1,
          text,
          created_at: new Date().toISOString(),
        }
        fake.enqueued.push({ id, text })
        fake.queuedItems.push(item)
        return item
      },
      deleteFollowup: async (_id: string, fid: string) => {
        fake.queuedItems = fake.queuedItems.filter((f) => f.id !== fid)
      },
      steerFollowup: async (id: string, fid: string) => {
        fake.steeredFollowups.push({ id, fid })
        fake.queuedItems = fake.queuedItems.filter((f) => f.id !== fid)
        return { steered: true }
      },
      steer: async (id: string, text: string) => {
        if (fake.steerWait) await fake.steerWait
        fake.steers.push({ id, text })
        return { steered: false }
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

beforeEach(() => {
  fake.steers.length = 0
  fake.enqueued.length = 0
  fake.queuedItems.length = 0
  fake.steeredFollowups.length = 0
  fake.enqueueIdle = false
  fake.steerWait = undefined
  fake.followupsWait = undefined
  fake.onEvent = undefined
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
})

describe("follow-up tray", () => {
  it("does not queue a second send of the same draft while the first is starting", async () => {
    let release!: () => void
    fake.steerWait = new Promise<void>((r) => {
      release = r
    })
    useApp.setState({
      activeId: "th_old",
      status: { running: false },
      followups: [],
    })
    const first = useApp.getState().send("same words")
    const second = useApp.getState().send("same words")
    release()
    await Promise.all([first, second])
    expect(fake.steers).toEqual([{ id: "th_old", text: "same words" }])
    expect(fake.enqueued).toEqual([])
    expect(useApp.getState().followups).toEqual([])
  })

  it("does not queue a follow-up that is already the live turn", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true, turn_id: "tn_1" },
      followups: [],
      transcript: {
        agentOrder: ["manager"],
        agents: {
          manager: {
            id: "manager",
            role: "manager",
            status: "running",
            activity: "",
            blocks: [
              {
                id: "b1",
                kind: "user",
                agentId: "manager",
                text: "same words",
                turnId: "tn_1",
                seq: 1,
                at: "",
              },
            ],
          },
        },
        turns: [{ id: "tn_1", userText: "same words", status: "running", agentIds: [] }],
        lastSeq: 1,
        running: true,
      },
    })
    await useApp.getState().send("same words")
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([])
  })

  it("drops a queued copy when the same text is injected", async () => {
    const item = {
      id: "fu_1",
      thread_id: "th_old",
      seq: 1,
      text: "same words",
      created_at: "",
    }
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [item],
    })
    fake.queuedItems = [item]
    await useApp.getState().send("same words", undefined, { steer: true })
    expect(fake.steers).toEqual([{ id: "th_old", text: "same words" }])
    expect(fake.queuedItems).toEqual([])
    expect(useApp.getState().followups).toEqual([])
  })

  it("removes a follow-up from the tray after steering it", async () => {
    const item = {
      id: "fu_1",
      thread_id: "th_old",
      seq: 1,
      text: "narrow it",
      created_at: "",
    }
    useApp.setState({
      activeId: "th_old",
      followups: [item],
    })
    fake.queuedItems = [item]
    await useApp.getState().steerFollowup("fu_1")
    expect(fake.steeredFollowups).toEqual([{ id: "th_old", fid: "fu_1" }])
    expect(useApp.getState().followups).toEqual([])
  })

  it("does not restore a steered follow-up from a stale list", async () => {
    const item = {
      id: "fu_1",
      thread_id: "th_old",
      seq: 1,
      text: "narrow it",
      created_at: "",
    }
    let release!: () => void
    fake.followupsWait = new Promise<void>((r) => {
      release = r
    })
    useApp.setState({ activeId: "th_old", followups: [item] })
    fake.queuedItems = [item]
    const refreshing = useApp.getState().refreshFollowups()
    await useApp.getState().steerFollowup("fu_1")
    expect(useApp.getState().followups).toEqual([])
    fake.queuedItems = [item]
    release()
    await refreshing
    expect(useApp.getState().followups).toEqual([])
  })

  it("drops a queued copy when that text becomes the live turn", async () => {
    await useApp.getState().boot()
    const item = {
      id: "fu_1",
      thread_id: "th_old",
      seq: 1,
      text: "same words",
      created_at: "",
    }
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [item],
    })
    fake.queuedItems = [item]
    fake.onEvent?.({
      kind: "user_message",
      seq: 1,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      text: "same words",
      created_at: new Date().toISOString(),
    })
    await Promise.resolve()
    await Promise.resolve()
    expect(fake.queuedItems).toEqual([])
    expect(useApp.getState().followups).toEqual([])
  })
})
