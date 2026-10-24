import { beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript } from "@/lib/transcript"
import { ApiError } from "@/lib/api"
import type { Turn } from "@/lib/types"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  patches: [] as Array<Record<string, unknown>>,
  answers: [] as Array<Record<string, unknown>>,
  implements: [] as string[],
  steers: [] as Array<{ id: string; text: string }>,
  starts: [] as Array<{ id: string; text: string }>,
  deletedFollowups: [] as string[],
  queued: [] as string[],
  planMode: false,
  planMarkdown: "",
  startFailure: undefined as Error | undefined,
  patchFailure: undefined as Error | undefined,
  acknowledgement: undefined as Promise<Turn> | undefined,
  threadGate: undefined as Promise<void> | undefined,
  probedThreads: [] as string[],
  threadStatuses: [] as Array<{ running: boolean; awaiting_answer?: boolean }>,
}))

function startedPlan(): Turn {
  return {
    id: "tn_plan", thread_id: "th_old", seq: 1, status: "running", user_text: "",
    final: "", provider_id: "default", model: "offline", duration_ms: 0,
    started_at: "2026-01-01T00:00:00.000Z",
  }
}

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
      patchThread: async (id: string, patch: Record<string, unknown>) => {
        fake.patches.push({ id, ...patch })
        if (fake.patchFailure) throw fake.patchFailure
        if (patch.plan_mode !== undefined) fake.planMode = Boolean(patch.plan_mode)
        if (typeof patch.plan_markdown === "string") fake.planMarkdown = patch.plan_markdown
        return { ...thread(id), ...patch }
      },
      steer: async (id: string, text: string) => {
        fake.steers.push({ id, text })
        return { steered: true }
      },
      startTurn: async (id: string, text: string) => {
        fake.starts.push({ id, text })
        if (fake.startFailure) throw fake.startFailure
        return fake.acknowledgement ?? startedPlan()
      },
      deleteFollowup: async (_id: string, followup: string) => { fake.deletedFollowups.push(followup) },
      enqueueFollowup: async (id: string, text: string) => {
        fake.queued.push(text)
        return { id: "fu_new", thread_id: id, seq: 1, text, created_at: "" }
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
  fake.starts.length = 0
  fake.deletedFollowups.length = 0
  fake.queued.length = 0
  fake.planMode = false
  fake.planMarkdown = ""
  fake.startFailure = undefined
  fake.patchFailure = undefined
  fake.acknowledgement = undefined
  fake.threadGate = undefined
  fake.probedThreads.length = 0
  fake.threadStatuses.length = 0
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
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().setPlan("inspect then change")).toBe(true)
    expect(fake.patches).toEqual([])
    expect(fake.starts).toEqual([{ id: "th_old", text: "/plan inspect then change" }])
    expect(fake.steers).toEqual([])
    fake.onEvent?.({
      kind: "plan", seq: 1, thread_id: "th_old", turn_id: "tn_1",
      agent_id: "manager", created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.plan_mode).toBe(true)
  })

  it.each(["Inspect assigned /goal", "/goal Inspect assigned", "  检查目标\n保留 /goal  "])("keeps command literals inside a single plan command: %s", async (text) => {
    // Re-sending only the argument lets goal dispatch steal a planning request.
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().setPlan(text)).toBe(true)
    expect(fake.patches).toEqual([])
    expect(fake.starts).toEqual([{ id: "th_old", text: `/plan ${text.trim()}` }])
    expect(fake.steers).toEqual([])
  })

  it("keeps an explicitly captured plan target after another conversation is selected", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    expect(await useApp.getState().setPlan("Inspect assigned /goal", "th_old")).toBe(true)
    expect(fake.starts).toEqual([{ id: "th_old", text: "/plan Inspect assigned /goal" }])
    expect(fake.steers).toEqual([])
    expect(fake.patches).toEqual([])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("waits for plan acceptance without marking a later selected conversation running", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    let acknowledge!: (reply: Turn) => void
    fake.acknowledgement = new Promise((resolve) => { acknowledge = resolve })
    let accepted = false
    const sending = useApp.getState().setPlan("Inspect acknowledged /goal").then((result) => { accepted = result })
    await vi.waitFor(() => expect(fake.starts).toHaveLength(1))
    expect(accepted).toBe(false)
    await useApp.getState().openThread("th_other")
    acknowledge(startedPlan())
    await sending
    expect(accepted).toBe(true)
    expect(fake.patches).toEqual([])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("returns false without partially entering plan mode when send acknowledgement fails", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.startFailure = new Error("plan send was not acknowledged")
    expect(await useApp.getState().setPlan("Inspect rejected /goal")).toBe(false)
    expect(fake.patches).toEqual([])
    expect(useApp.getState().threads[0]?.plan_mode).toBe(false)
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().error).toBe("plan send was not acknowledged")
  })

  it("refuses a plan when a turn starts during metadata lookup without consuming a matching follow-up", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    let releaseProbe!: () => void
    fake.threadGate = new Promise((resolve) => { releaseProbe = resolve })
    fake.startFailure = new ApiError("conversation is running", 409, "busy")
    const sending = useApp.getState().setPlan("Inspect raced /goal", "th_old")
    await vi.waitFor(() => expect(fake.probedThreads.at(-1)).toBe("th_old"))
    const followups = [{ id: "fu_kept", thread_id: "th_old", seq: 1, text: "/plan Inspect raced /goal", created_at: "" }]
    useApp.setState({ activeId: "th_old", status: { running: true }, followups })
    releaseProbe()
    expect(await sending).toBe(false)
    expect(fake.starts).toEqual([{ id: "th_old", text: "/plan Inspect raced /goal" }])
    expect(fake.steers).toEqual([])
    expect(fake.answers).toEqual([])
    expect(fake.queued).toEqual([])
    expect(fake.deletedFollowups).toEqual([])
    expect(useApp.getState().followups).toEqual(followups)
    expect(useApp.getState().error).toBe("conversation is running")
  })

  it("refuses the plan without answering an ask in the target's second metadata snapshot", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_other")
    fake.probedThreads.length = 0
    fake.threadStatuses.push({ running: false }, { running: true, awaiting_answer: true })
    fake.startFailure = new ApiError("conversation is running", 409, "busy")
    expect(await useApp.getState().setPlan("Inspect pending /goal", "th_old")).toBe(false)
    expect(fake.answers).toEqual([])
    expect(fake.steers).toEqual([])
    expect(fake.starts).toEqual([{ id: "th_old", text: "/plan Inspect pending /goal" }])
    expect(fake.probedThreads).toEqual(["th_old", "th_old"])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
  })

  it("keeps the plan idle when its terminal stream event beats the normalized start acknowledgement", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    let acknowledge!: (reply: Turn) => void
    fake.acknowledgement = new Promise((resolve) => { acknowledge = resolve })
    const sending = useApp.getState().setPlan("Inspect completed /goal")
    await vi.waitFor(() => expect(fake.starts).toHaveLength(1))
    const at = "2026-01-01T00:00:00.010Z"
    fake.onEvent?.({ kind: "user_message", seq: 1, thread_id: "th_old", turn_id: "tn_plan", agent_id: "manager", text: "Inspect completed /goal", created_at: at })
    fake.onEvent?.({ kind: "done", seq: 2, thread_id: "th_old", turn_id: "tn_plan", agent_id: "manager", created_at: at })
    expect(useApp.getState().status.running).toBe(false)
    acknowledge(startedPlan())
    expect(await sending).toBe(true)
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_plan" })
    expect(useApp.getState().transcript.turns.map((turn) => turn.status)).toEqual(["done"])
    expect(fake.steers).toEqual([])
  })

  it.each(["", " \n"])("keeps a bare plan command as an idle mode patch: %j", async (text) => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(await useApp.getState().setPlan(text)).toBe(true)
    expect(fake.patches).toEqual([{ id: "th_old", plan_mode: true }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().threads[0]?.plan_mode).toBe(true)
    expect(useApp.getState().status.running).toBe(false)
  })

  it("keeps the existing plan mode refusal when a turn is already running", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    useApp.setState({ status: { running: true } })
    fake.patchFailure = new Error("conversation is running")
    expect(await useApp.getState().setPlan("Inspect busy /goal")).toBe(false)
    expect(fake.patches).toEqual([{ id: "th_old", plan_mode: true }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().error).toBe("conversation is running")
    expect(useApp.getState().status.running).toBe(true)
  })

  it("saves an edited plan body", async () => {
    fake.planMode = true
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().savePlan("# Plan\n\nDo the work.\n")
    expect(fake.patches[0]).toMatchObject({
      id: "th_old",
      plan_markdown: "# Plan\n\nDo the work.\n",
    })
  })

  it("leaves planning without starting a turn", async () => {
    fake.planMode = true
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().leavePlan()
    expect(fake.patches).toEqual([{ id: "th_old", plan_mode: false }])
    expect(fake.steers).toEqual([])
  })

  it("implements the current plan", async () => {
    fake.planMode = true
    fake.planMarkdown = "# Plan"
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().implementPlan()
    expect(fake.implements).toEqual(["th_old"])
    expect(useApp.getState().threads[0]?.plan_mode).toBe(false)
    expect(useApp.getState().threads[0]?.running).toBe(true)
    expect(useApp.getState().status.running).toBe(true)
  })

  it("posts structured answers for the waiting card", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
    await useApp.getState().openThread("th_old")
    useApp.setState({ status: { running: true, awaiting_answer: true } })
    await useApp.getState().send("do it the other way")
    expect(fake.answers).toEqual([{ id: "th_old", text: "do it the other way" }])
    expect(fake.steers).toEqual([])
  })

  it("updates the banner from plan events", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
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
