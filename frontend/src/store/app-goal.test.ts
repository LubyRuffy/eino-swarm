import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

/** Goal/compact stream tests. Kept out of app.test.ts so that file stays
 *  under 1000 lines — the mock here is only what those actions touch. */
const fake = vi.hoisted(() => ({
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  goals: [] as Array<{ id: string; goal?: string; goal_edit?: boolean; goal_resume?: boolean }>,
  steers: [] as Array<{ id: string; text: string }>,
  compacts: [] as string[],
  goalText: "",
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
    goal: fake.goalText,
  })
  return {
    ApiError: class ApiError extends Error {
      constructor(
        message: string,
        readonly status: number,
        readonly code?: string,
      ) {
        super(message)
      }
    },
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
      patchThread: async (
        id: string,
        patch: {
          goal?: string
          goal_edit?: boolean
          goal_resume?: boolean
        },
      ) => {
        fake.goals.push({
          id,
          goal: patch.goal,
          goal_edit: patch.goal_edit,
          goal_resume: patch.goal_resume,
        })
        if (patch.goal !== undefined) fake.goalText = patch.goal
        return {
          ...thread(id),
          ...patch,
          running: Boolean(patch.goal_resume),
          goal_blocked: patch.goal_resume ? false : undefined,
        }
      },
      steer: async (id: string, text: string) => {
        fake.steers.push({ id, text })
        return { steered: true }
      },
      compactThread: async (id: string) => {
        fake.compacts.push(id)
        return {
          thread: { ...thread(id), compacted: true },
          status: { running: false },
        }
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
  fake.onEvent = undefined
  fake.goals.length = 0
  fake.steers.length = 0
  fake.compacts.length = 0
  fake.goalText = ""
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

describe("goal and compact", () => {
  it("patches the standing objective onto the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().setGoal("keep going")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "keep going" }])
    expect(useApp.getState().threads[0]?.goal).toBe("keep going")
    expect(fake.steers).toEqual([{ id: "th_old", text: "keep going" }])
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not start a second turn when a standing objective is set during a run", async () => {
    await useApp.getState().boot()
    useApp.setState({ status: { running: true, started_at: new Date().toISOString() } })
    await useApp.getState().setGoal("keep going")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "keep going" }])
    expect(fake.steers).toEqual([])
  })

  it("does not start a turn when the standing objective is cleared", async () => {
    await useApp.getState().boot()
    await useApp.getState().setGoal("")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "" }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().status.running).toBe(false)
  })

  it("edits the standing objective in place", async () => {
    await useApp.getState().boot()
    await useApp.getState().editGoal("keep going, tighter")
    expect(fake.goals).toEqual([
      { id: "th_old", goal: "keep going, tighter", goal_edit: true },
    ])
  })

  it("resumes a standing objective on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([{ id: "th_old", goal_resume: true }])
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not edit or resume when nothing is open", async () => {
    await useApp.getState().editGoal("x")
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([])
  })

  it("folds replay on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().compactThread()
    expect(fake.compacts).toEqual(["th_old"])
    expect(useApp.getState().threads[0]?.compacted).toBe(true)
  })

  it("does not compact when nothing is open", async () => {
    await useApp.getState().compactThread()
    expect(fake.compacts).toEqual([])
  })

  it("ignores a live compressing pulse when marking the conversation compacted", async () => {
    await useApp.getState().boot()
    const at = new Date().toISOString()
    fake.onEvent?.({
      kind: "compacted",
      seq: 0,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "compact-summarizer",
      text: JSON.stringify({ auto: true, phase: "start", tokens_before: 90000 }),
      created_at: at,
    })
    expect(useApp.getState().threads[0]?.compacted).toBeFalsy()
    fake.onEvent?.({
      kind: "compacted",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "compact-summarizer",
      text: JSON.stringify({
        auto: true,
        tokens_before: 90000,
        tokens_after: 1200,
      }),
      created_at: at,
    })
    expect(useApp.getState().threads[0]?.compacted).toBe(true)
  })

  it("updates the banner from a goal event", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal).toBe("keep going")
    expect(useApp.getState().threads[0]?.goal_complete).toBe(false)
  })

  it("marks the objective complete from the stream", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_complete",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "{}",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_complete).toBe(true)
  })

  it("marks the objective blocked from the stream", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_blocked",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: '{"reason":"needs an external change"}',
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_blocked).toBe(true)
    expect(useApp.getState().threads[0]?.goal_block_reason).toBe("needs an external change")
  })

  it("idles the composer when a pursuing turn crashes", async () => {
    await useApp.getState().boot()
    const at = "2026-01-01T00:00:00.000Z"
    fake.onEvent?.({ kind: "goal_continued", seq: 11, thread_id: "th_old", turn_id: "tn_2", agent_id: "manager", created_at: at })
    expect(useApp.getState().status.running).toBe(true)
    fake.onEvent?.({
      kind: "error",
      seq: 12,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      err: "chat model refused the request",
      created_at: at,
    })
    fake.onEvent?.({
      kind: "goal_blocked",
      seq: 13,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      text: '{"reason":"the last turn failed"}',
      created_at: at,
    })
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().threads[0]?.goal_blocked).toBe(true)
    expect(useApp.getState().threads[0]?.goal_block_reason).toBe("the last turn failed")
  })

  it("keeps status flags when the objective is edited", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_blocked",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: '{"reason":"needs an external change"}',
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_edited",
      seq: 52,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going, tighter",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal).toBe("keep going, tighter")
    expect(useApp.getState().threads[0]?.goal_blocked).toBe(true)
  })

  it("keeps counting working time after the standing objective auto-continues", async () => {
    // done wiped started_at; goal_continued used to leave the header frozen at 1s.
    await useApp.getState().boot()
    const at = "2026-01-01T00:00:00.000Z"
    fake.onEvent?.({ kind: "done", seq: 10, thread_id: "th_old", turn_id: "tn_1", agent_id: "manager", created_at: at })
    fake.onEvent?.({ kind: "goal_continued", seq: 11, thread_id: "th_old", turn_id: "tn_2", agent_id: "manager", created_at: at })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_2", started_at: at })
  })

  it("starts the working clock when a standing objective is resumed", async () => {
    await useApp.getState().boot()
    const at = "2026-01-01T02:00:00.000Z"
    fake.onEvent?.({ kind: "goal_resumed", seq: 12, thread_id: "th_old", turn_id: "tn_3", agent_id: "manager", created_at: at })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_3", started_at: at })
  })

  it("starts the working clock when a scheduled check fires", async () => {
    await useApp.getState().boot()
    const at = "2026-01-01T00:00:00.000Z"
    fake.onEvent?.({ kind: "done", seq: 10, thread_id: "th_old", turn_id: "tn_1", agent_id: "manager", created_at: at })
    fake.onEvent?.({
      kind: "schedule_fired",
      seq: 11,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      text: "Scheduled check.",
      created_at: at,
    })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_2", started_at: at })
  })

  it("holds auto-continue after a no-progress continuation", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_idle",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      text: "Stopped auto-continuing: the last continuation made no progress.",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_idle).toBe(true)
  })
})
