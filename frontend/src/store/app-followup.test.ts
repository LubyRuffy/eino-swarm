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
  steerFollowupWait: undefined as Promise<void> | undefined,
  steerFollowupError: undefined as { status: number; code?: string } | undefined,
  followupsError: false,
  followupReads: [] as string[],
  followupsWait: undefined as Promise<void> | undefined,
  deleteStatus: undefined as number | undefined,
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
      schedules: async () => ({ schedules: [], unread: 0 }),
      thread: async (id: string) => ({
        thread: thread(id),
        status: { running: false },
      }),
      turns: async () => [],
      threadLog: async () => ({ events: [], has_more: false }),
      agentLog: async () => ({ events: [] }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async (id: string) => {
        fake.followupReads.push(id)
        if (fake.followupsError) throw new Error("queue unavailable")
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
        if (fake.deleteStatus) throw new ApiError("still queued", fake.deleteStatus)
        fake.queuedItems = fake.queuedItems.filter((f) => f.id !== fid)
      },
      steerFollowup: async (id: string, fid: string) => {
        fake.steeredFollowups.push({ id, fid })
        if (fake.steerFollowupWait) await fake.steerFollowupWait
        if (fake.steerFollowupError) {
          const { status, code } = fake.steerFollowupError
          throw new ApiError("store: not found", status, code)
        }
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
  fake.steerFollowupWait = undefined
  fake.steerFollowupError = undefined
  fake.followupsError = false
  fake.followupReads.length = 0
  fake.followupsWait = undefined
  fake.deleteStatus = undefined
  fake.onEvent = undefined
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    followups: [],
    error: undefined,
    locale: "en",
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
  it("sends a queued row only once while its injection is in flight", async () => {
    let release!: () => void
    fake.steerFollowupWait = new Promise<void>((r) => { release = r })
    const item = { id: "fu_1", thread_id: "th_old", seq: 1, text: "continue", created_at: "" }
    useApp.setState({ activeId: "th_old", followups: [item] })
    fake.queuedItems = [item]
    const first = useApp.getState().steerFollowup(item.id)
    const duplicate = useApp.getState().steerFollowup(item.id)
    expect(fake.steeredFollowups).toEqual([{ id: "th_old", fid: "fu_1" }])
    release()
    await Promise.all([first, duplicate])
    expect(useApp.getState().followups).toEqual([])
    expect(useApp.getState().error).toBeUndefined()
  })

  it("allows retrying the same row after a failed injection", async () => {
    const item = { id: "fu_1", thread_id: "th_old", seq: 1, text: "retry this message", created_at: "" }
    useApp.setState({ activeId: "th_old", followups: [item] })
    fake.queuedItems = [item]
    fake.steerFollowupError = { status: 500 }
    await useApp.getState().steerFollowup(item.id)
    expect(useApp.getState().followups).toEqual([item])
    expect(useApp.getState().error).toBeTruthy()
    fake.steerFollowupError = undefined
    await useApp.getState().steerFollowup(item.id)
    expect(fake.steeredFollowups).toHaveLength(2)
    expect(useApp.getState().followups).toEqual([])
    expect(useApp.getState().error).toBeUndefined()
  })

  it("preserves a newer error from another action when an injection succeeds", async () => {
    let release!: () => void
    fake.steerFollowupWait = new Promise<void>((r) => { release = r })
    useApp.setState({ activeId: "th_old", followups: [], error: "old injection error" })
    const injecting = useApp.getState().steerFollowup("fu_1")
    useApp.setState({ error: "new upload error" })
    release()
    await injecting
    expect(useApp.getState().error).toBe("new upload error")
  })

  it("does not deduplicate different rows or the same row id in different conversations", async () => {
    let release!: () => void
    fake.steerFollowupWait = new Promise<void>((r) => { release = r })
    useApp.setState({ activeId: "th_old", followups: [] })
    const first = useApp.getState().steerFollowup("fu_1")
    const differentRow = useApp.getState().steerFollowup("fu_2")
    useApp.setState({ activeId: "th_new", followups: [] })
    const differentConversation = useApp.getState().steerFollowup("fu_1")
    expect(fake.steeredFollowups).toEqual([
      { id: "th_old", fid: "fu_1" }, { id: "th_old", fid: "fu_2" },
      { id: "th_new", fid: "fu_1" },
    ])
    release()
    await Promise.all([first, differentRow, differentConversation])
  })

  it.each([false, true])("does not apply an old conversation's late injection response (failure: %s)", async (fails) => {
    let release!: () => void
    fake.steerFollowupWait = new Promise<void>((r) => { release = r })
    if (fails) fake.steerFollowupError = { status: 404, code: "followup_unavailable" }
    useApp.setState({ activeId: "th_old", followups: [] })
    const sending = useApp.getState().steerFollowup("fu_1")
    const current = { id: "fu_1", thread_id: "th_new", seq: 1, text: "different message", created_at: "" }
    useApp.setState({ activeId: "th_new", followups: [current], error: "current error" })
    release()
    await sending
    expect(useApp.getState().followups).toEqual([current])
    expect(useApp.getState().error).toBe("current error")
    expect(fake.followupReads).toEqual([])
  })

  it.each([undefined, "followup_unavailable"])("refreshes a missing queued row without resending its text (code: %s)", async (code) => {
    const stale = { id: "fu_1", thread_id: "th_old", seq: 1, text: "do not duplicate", created_at: "" }
    const kept = { ...stale, id: "fu_2", seq: 2, text: "waiting" }
    fake.steerFollowupError = { status: 404, code }
    fake.queuedItems = [kept]
    useApp.setState({ activeId: "th_old", followups: [stale, kept] })
    await useApp.getState().steerFollowup(stale.id)
    expect(fake.followupReads).toEqual(["th_old"])
    expect(useApp.getState().followups).toEqual([kept])
    expect(useApp.getState().error).toContain("no longer in the waiting queue")
    expect(useApp.getState().error).toContain("queue has been refreshed")
    expect(fake.steers).toEqual([])
    expect(fake.enqueued).toEqual([])
  })

  it("does not show an old error when navigation occurs as the refreshed queue lands", async () => {
    fake.steerFollowupError = { status: 404, code: "followup_unavailable" }
    const kept = { id: "fu_2", thread_id: "th_old", seq: 2, text: "waiting", created_at: "" }
    fake.queuedItems = [kept]
    useApp.setState({ activeId: "th_old", followups: [] })
    const unsubscribe = useApp.subscribe((state) => {
      if (state.followups === fake.queuedItems) {
        useApp.setState({ activeId: "th_new", followups: [], error: "current error" })
      }
    })
    try {
      await useApp.getState().steerFollowup("fu_1")
      expect(useApp.getState().activeId).toBe("th_new")
      expect(useApp.getState().error).toBe("current error")
    } finally {
      unsubscribe()
    }
  })

  it("does not apply an old queue refresh or error after navigating while it is pending", async () => {
    let release!: () => void
    fake.followupsWait = new Promise<void>((r) => { release = r })
    fake.steerFollowupError = { status: 404, code: "followup_unavailable" }
    useApp.setState({ activeId: "th_old", followups: [] })
    const sending = useApp.getState().steerFollowup("fu_1")
    await Promise.resolve()
    expect(fake.followupReads).toEqual(["th_old"])
    useApp.setState({ activeId: "th_new", followups: [], error: "current error" })
    release()
    await sending
    expect(useApp.getState().followups).toEqual([])
    expect(useApp.getState().error).toBe("current error")
  })

  it("clears a previous conversation's error when opening another conversation", async () => {
    useApp.setState({ activeId: "th_old", error: "old queue error", followups: [] })
    await useApp.getState().openThread("th_new")
    expect(useApp.getState().activeId).toBe("th_new")
    expect(useApp.getState().error).toBeUndefined()
  })

  it("does not claim the queue refreshed if its refresh failed", async () => {
    fake.steerFollowupError = { status: 404, code: "followup_unavailable" }
    fake.followupsError = true
    useApp.setState({ activeId: "th_old", followups: [] })
    await useApp.getState().steerFollowup("fu_1")
    expect(useApp.getState().error).toContain("Could not refresh the waiting queue")
    expect(useApp.getState().error).not.toContain("queue has been refreshed")
  })

  it("shows the Chinese stale-message explanation in Chinese", async () => {
    fake.steerFollowupError = { status: 404, code: "followup_unavailable" }
    useApp.setState({ activeId: "th_old", followups: [], locale: "zh" })
    await useApp.getState().steerFollowup("fu_1")
    expect(useApp.getState().error).toContain("已不在待发送队列中")
    expect(useApp.getState().error).toContain("队列已刷新")
    expect(useApp.getState().error).not.toContain("store:")
  })

  it("keeps a missing conversation distinct from an unavailable queued message", async () => {
    fake.steerFollowupError = { status: 404, code: "thread_not_found" }
    useApp.setState({ activeId: "th_missing", followups: [] })
    await useApp.getState().steerFollowup("fu_1")
    expect(useApp.getState().error).not.toContain("queue has been refreshed")
    expect(fake.followupReads).toEqual([])
  })

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

  it("reports whether a queued row left so the composer can take it", async () => {
    const item = {
      id: "fu_1",
      thread_id: "th_old",
      seq: 1,
      text: "after this",
      created_at: "",
    }
    useApp.setState({ activeId: "th_old", followups: [item] })
    await expect(useApp.getState().deleteFollowup("fu_1")).resolves.toBe(true)
    expect(useApp.getState().followups).toEqual([])

    useApp.setState({ activeId: "th_old", followups: [item], error: undefined })
    fake.queuedItems = [item]
    fake.deleteStatus = 500
    await expect(useApp.getState().deleteFollowup("fu_1")).resolves.toBe(false)
    expect(useApp.getState().followups).toEqual([item])
    expect(useApp.getState().error).toBeTruthy()
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
    await useApp.getState().openThread("th_old")
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
