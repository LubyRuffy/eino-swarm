import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"
import { api } from "@/lib/api"

/** Goal/compact stream tests. Kept out of app.test.ts so that file stays
 *  under 1000 lines — the mock here is only what those actions touch. */
const fake = vi.hoisted(() => ({
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  goals: [] as Array<{ id: string; goal?: string; goal_edit?: boolean; goal_resume?: boolean }>,
  steers: [] as Array<{ id: string; text: string }>,
  compacts: [] as string[],
  goalText: "",
  steerFailure: undefined as Error | undefined,
  acknowledgement: undefined as Promise<{ steered: boolean }> | undefined,
  threadGate: undefined as Promise<void> | undefined,
  probedThreads: [] as string[],
  followups: [] as string[],
  answers: [] as Array<{ id: string; text: string }>,
  threadStatuses: [] as Array<{ running: boolean; awaiting_answer?: boolean }>,
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
      thread: async (id: string) => {
        fake.probedThreads.push(id)
        await fake.threadGate
        return { thread: thread(id), status: fake.threadStatuses.shift() ?? { running: false } }
      },
      turns: async () => [],
      threadLog: async () => ({ events: [], has_more: false }),
      agentLog: async () => ({ events: [] }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => [],
      schedules: async () => ({ schedules: [], unread: 0 }),
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
        if (fake.steerFailure) throw fake.steerFailure
        return fake.acknowledgement ?? { steered: true }
      },
      enqueueFollowup: async (id: string, text: string) => {
        fake.followups.push(text)
        return { id: "fu_1", thread_id: id, seq: 1, text, created_at: "" }
      },
      answerTurn: async (id: string, body: { text: string }) => {
        fake.answers.push({ id, text: body.text })
        return { answered: true }
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
  fake.steerFailure = undefined
  fake.acknowledgement = undefined
  fake.threadGate = undefined
  fake.probedThreads.length = 0
  fake.followups.length = 0
  fake.answers.length = 0
  fake.threadStatuses.length = 0
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
  it("starts pursuit with one goal command and receives the standing objective from the stream", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().setGoal("keep going")).toBe(true)
    expect(fake.goals).toEqual([])
    expect(fake.steers).toEqual([{ id: "th_old", text: "/goal keep going" }])
    fake.onEvent?.({
      kind: "goal", seq: 50, thread_id: "th_old", turn_id: "tn_1",
      agent_id: "manager", text: "keep going", created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal).toBe("keep going")
    expect(useApp.getState().status.running).toBe(true)
  })

  it.each(["Inspect assigned /goal", "校验当前目标\n保留末尾 /goal"])("keeps a command literal in the objective for one server interpretation: %s", async (objective) => {
    // Sending only the parsed argument lets the engine consume its last /goal again.
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().setGoal(objective)).toBe(true)
    expect(fake.steers).toEqual([{ id: "th_old", text: `/goal ${objective}` }])
    expect(fake.goals).toEqual([])
  })

  it("keeps an explicitly captured goal target when another conversation is selected", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    expect(await useApp.getState().setGoal("Inspect assigned /goal", "th_old")).toBe(true)
    expect(fake.steers).toEqual([{ id: "th_old", text: "/goal Inspect assigned /goal" }])
    expect(fake.goals).toEqual([])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("waits for the goal send acknowledgement and keeps navigation ownership", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    let acknowledge!: (reply: { steered: boolean }) => void
    fake.acknowledgement = new Promise((resolve) => { acknowledge = resolve })
    let accepted = false
    const sending = useApp.getState().setGoal("Inspect assigned /goal").then((result) => { accepted = result })
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    expect(accepted).toBe(false)
    await useApp.getState().openThread("th_other")
    acknowledge({ steered: true })
    await sending
    expect(accepted).toBe(true)
    expect(fake.goals).toEqual([])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("steers the goal command when a turn starts during the captured target's metadata read", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    let releaseProbe!: () => void
    fake.threadGate = new Promise((resolve) => { releaseProbe = resolve })
    const sending = useApp.getState().setGoal("Inspect raced /goal", "th_old")
    await vi.waitFor(() => expect(fake.probedThreads.at(-1)).toBe("th_old"))
    // A live update/navigation can expose the target's new turn while its old snapshot arrives.
    useApp.setState({ activeId: "th_old", status: { running: true }, followups: [] })
    releaseProbe()
    expect(await sending).toBe(true)
    expect(fake.steers).toEqual([{ id: "th_old", text: "/goal Inspect raced /goal" }])
    expect(fake.goals).toEqual([])
    expect(fake.followups).toEqual([])
  })

  it("returns false without a partial goal patch when the send acknowledgement fails", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.steerFailure = new Error("goal send was not acknowledged")
    expect(await useApp.getState().setGoal("Inspect assigned /goal")).toBe(false)
    expect(fake.goals).toEqual([])
    expect(useApp.getState().error).toBe("goal send was not acknowledged")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("keeps the goal command out of an ask that appears in the target's second metadata snapshot", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    fake.probedThreads.length = 0
    fake.threadStatuses.push({ running: false }, { running: true, awaiting_answer: true })
    expect(await useApp.getState().setGoal("Inspect pending /goal", "th_old")).toBe(true)
    expect(fake.answers).toEqual([])
    expect(fake.followups).toEqual([])
    expect(fake.steers).toEqual([{ id: "th_old", text: "/goal Inspect pending /goal" }])
    expect(fake.probedThreads).toEqual(["th_old", "th_old"])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("starts a new human goal after an earlier objective completed", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.({
      kind: "goal_complete", seq: 50, thread_id: "th_old", turn_id: "tn_old",
      agent_id: "manager", text: "{}", created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_complete).toBe(true)
    expect(await useApp.getState().setGoal("Inspect assigned /goal")).toBe(true)
    expect(fake.goals).toEqual([])
    expect(fake.steers).toEqual([{ id: "th_old", text: "/goal Inspect assigned /goal" }])
    fake.onEvent?.({
      kind: "goal", seq: 51, thread_id: "th_old", turn_id: "tn_new",
      agent_id: "manager", text: "Inspect assigned /goal", created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]).toMatchObject({ goal: "Inspect assigned /goal", goal_complete: false })
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not start a second turn when a standing objective is set during a run", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    useApp.setState({ status: { running: true, started_at: new Date().toISOString() } })
    await useApp.getState().setGoal("keep going")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "keep going" }])
    expect(fake.steers).toEqual([])
  })

  it("does not start a turn when the standing objective is cleared", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().setGoal("")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "" }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().status.running).toBe(false)
  })

  it("edits the standing objective in place", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().editGoal("keep going, tighter")).toBe(true)
    expect(fake.goals).toEqual([
      { id: "th_old", goal: "keep going, tighter", goal_edit: true },
    ])
  })

  it("keeps a refused goal edit unacknowledged and preserves the original objective", async () => {
    fake.goalText = "Original objective"
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    const patch = vi.spyOn(api, "patchThread").mockRejectedValueOnce(new Error("Engine is restoring unfinished tasks."))
    expect(await useApp.getState().editGoal("Unacknowledged edit")).toBe(false)
    expect(useApp.getState().error).toBe("Engine is restoring unfinished tasks.")
    expect(fake.goalText).toBe("Original objective")
    expect(useApp.getState().threads.find((thread) => thread.id === "th_old")?.goal).toBe("Original objective")
    patch.mockRestore()
    useApp.setState({ meta: { startup: "restoring" } as import("@/lib/types").Meta })
    expect(await useApp.getState().editGoal("Blocked edit")).toBe(false)
    expect(fake.goals).toEqual([])
  })

  it("resumes a standing objective on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([{ id: "th_old", goal_resume: true }])
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not edit or resume when nothing is open", async () => {
    expect(await useApp.getState().editGoal("x")).toBe(false)
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([])
  })

  it("folds replay on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
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
    expect(useApp.getState().status.compressing).toBe(true)
    expect(useApp.getState().status.running).toBe(true)
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
    expect(useApp.getState().status.compressing).toBe(false)
  })

  it("clears compressing when the turn ends even if compact never finished", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.({
      kind: "compacted",
      seq: 0,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "compact-summarizer",
      text: JSON.stringify({ auto: true, phase: "start", tokens_before: 90000 }),
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().status.compressing).toBe(true)
    fake.onEvent?.({
      kind: "done",
      seq: 52,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "ok",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().status.compressing).toBeFalsy()
    expect(useApp.getState().status.running).toBe(false)
  })

  it("updates the banner from a goal event", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
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

  it("reopens a completed objective from a resume event", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.({
      kind: "goal_complete",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "{}",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_resumed",
      seq: 52,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_complete).toBe(false)
  })

  it("marks the objective blocked from the stream", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
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

  it("keeps pursuing after a recoverable model error auto-continues", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    const at = "2026-01-01T00:00:00.000Z"
    fake.onEvent?.({
      kind: "goal",
      seq: 10,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: at,
    })
    fake.onEvent?.({
      kind: "error",
      seq: 11,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      err: "the model request failed because a tool call was not valid JSON",
      created_at: at,
    })
    fake.onEvent?.({
      kind: "goal_continued",
      seq: 12,
      thread_id: "th_old",
      turn_id: "tn_2",
      agent_id: "manager",
      created_at: at,
    })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_2", started_at: at })
    expect(useApp.getState().threads[0]?.goal_blocked).toBeFalsy()
  })

  it("keeps status flags when the objective is edited", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
    const at = "2026-01-01T00:00:00.000Z"
    fake.onEvent?.({ kind: "done", seq: 10, thread_id: "th_old", turn_id: "tn_1", agent_id: "manager", created_at: at })
    fake.onEvent?.({ kind: "goal_continued", seq: 11, thread_id: "th_old", turn_id: "tn_2", agent_id: "manager", created_at: at })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_2", started_at: at })
  })

  it("starts the working clock when a standing objective is resumed", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    const at = "2026-01-01T02:00:00.000Z"
    fake.onEvent?.({ kind: "goal_resumed", seq: 12, thread_id: "th_old", turn_id: "tn_3", agent_id: "manager", created_at: at })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_3", started_at: at })
  })

  it("starts the working clock when a scheduled check fires", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
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
