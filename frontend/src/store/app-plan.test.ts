import { beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript } from "@/lib/transcript"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  patches: [] as Array<Record<string, unknown>>,
  answers: [] as Array<Record<string, unknown>>,
  implements: [] as string[],
  steers: [] as Array<{ id: string; text: string }>,
  planMode: false,
  planMarkdown: "",
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
    plan_mode: fake.planMode,
    plan_markdown: fake.planMarkdown,
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
      schedules: async () => ({ schedules: [], unread: 0 }),
      patchThread: async (id: string, patch: Record<string, unknown>) => {
        fake.patches.push({ id, ...patch })
        if (patch.plan_mode !== undefined) fake.planMode = Boolean(patch.plan_mode)
        if (typeof patch.plan_markdown === "string") fake.planMarkdown = patch.plan_markdown
        return { ...thread(id), ...patch }
      },
      steer: async (id: string, text: string) => {
        fake.steers.push({ id, text })
        return { steered: true }
      },
      answerTurn: async (id: string, body: Record<string, unknown>) => {
        fake.answers.push({ id, ...body })
        return { answered: true }
      },
      implementPlan: async (id: string) => {
        fake.implements.push(id)
        fake.planMode = false
        return {
          turn: {
            id: "tn_imp",
            thread_id: id,
            seq: 1,
            status: "running",
            user_text: "The human accepted the plan. Execute it.",
            final: "",
            provider_id: "default",
            model: "m",
            started_at: new Date().toISOString(),
            duration_ms: 0,
          },
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
  fake.patches.length = 0
  fake.answers.length = 0
  fake.implements.length = 0
  fake.steers.length = 0
  fake.planMode = false
  fake.planMarkdown = ""
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    transcript: emptyTranscript(),
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

describe("plan and ask store", () => {
  it("enters plan mode and starts the task when idle", async () => {
    await useApp.getState().boot()
    await useApp.getState().setPlan("inspect then change")
    expect(fake.patches).toEqual([{ id: "th_old", plan_mode: true }])
    expect(fake.steers).toEqual([{ id: "th_old", text: "inspect then change" }])
    expect(useApp.getState().threads[0]?.plan_mode).toBe(true)
  })

  it("saves an edited plan body", async () => {
    fake.planMode = true
    await useApp.getState().boot()
    await useApp.getState().savePlan("# Plan\n\nDo the work.\n")
    expect(fake.patches[0]).toMatchObject({
      id: "th_old",
      plan_markdown: "# Plan\n\nDo the work.\n",
    })
  })

  it("leaves planning without starting a turn", async () => {
    fake.planMode = true
    await useApp.getState().boot()
    await useApp.getState().leavePlan()
    expect(fake.patches).toEqual([{ id: "th_old", plan_mode: false }])
    expect(fake.steers).toEqual([])
  })

  it("implements the current plan", async () => {
    fake.planMode = true
    fake.planMarkdown = "# Plan"
    await useApp.getState().boot()
    await useApp.getState().implementPlan()
    expect(fake.implements).toEqual(["th_old"])
    expect(useApp.getState().threads[0]?.plan_mode).toBe(false)
    expect(useApp.getState().status.running).toBe(true)
  })

  it("posts structured answers for the waiting card", async () => {
    await useApp.getState().boot()
    await useApp.getState().answerAsk("tc_1", {
      approach: { answers: ["Prefer the safer path"] },
    })
    expect(fake.answers).toEqual([
      {
        id: "th_old",
        call_id: "tc_1",
        answers: { approach: { answers: ["Prefer the safer path"] } },
      },
    ])
  })

  it("treats composer text as Other while a question is waiting", async () => {
    await useApp.getState().boot()
    useApp.setState({ status: { running: true, awaiting_answer: true } })
    await useApp.getState().send("do it the other way")
    expect(fake.answers).toEqual([{ id: "th_old", text: "do it the other way" }])
    expect(fake.steers).toEqual([])
  })

  it("updates the banner from plan events", async () => {
    await useApp.getState().boot()
    const at = new Date().toISOString()
    fake.onEvent?.({
      kind: "plan",
      seq: 1,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      created_at: at,
    })
    expect(useApp.getState().threads[0]?.plan_mode).toBe(true)
    fake.onEvent?.({
      kind: "plan_updated",
      seq: 2,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "# Plan\n\nSteps.\n",
      created_at: at,
    })
    expect(useApp.getState().threads[0]?.plan_markdown).toBe("# Plan\n\nSteps.\n")
    fake.onEvent?.({
      kind: "plan_implemented",
      seq: 3,
      thread_id: "th_old",
      turn_id: "tn_imp",
      agent_id: "manager",
      created_at: at,
    })
    expect(useApp.getState().status.running).toBe(true)
  })
})
