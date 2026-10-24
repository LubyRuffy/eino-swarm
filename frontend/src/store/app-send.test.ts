import { beforeEach, describe, expect, it, vi } from "vitest"

import type { SwarmEvent, Thread, Turn } from "@/lib/types"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

type Acknowledgement = { steered: boolean; turn?: Turn }

const fake = vi.hoisted(() => ({
  onEvent: undefined as ((event: SwarmEvent) => void) | undefined,
  acknowledgement: undefined as Promise<Acknowledgement> | undefined,
  failure: undefined as Error | undefined,
  steers: [] as string[],
  followups: [] as string[],
  logs: {} as Record<string, SwarmEvent[]>,
}))

function thread(id: string): Thread {
  return {
    id, title: "A conversation", provider_id: "default", project_id: "", reasoning_effort: "", archived: false,
    created_at: "", last_active_at: "", running: false,
  }
}

function turn(id = "tn_new"): Turn {
  return {
    id, thread_id: "th_old", seq: 1, status: "running", user_text: "",
    final: "", provider_id: "default", model: "offline", duration_ms: 0,
    started_at: "2026-01-01T00:00:00.000Z",
  }
}

vi.mock("@/lib/api", () => ({
  api: {
    thread: async (id: string) => ({ thread: thread(id), status: { running: false } }),
    turns: async () => [],
    threadLog: async (id: string) => ({ events: fake.logs[id] ?? [], has_more: false }),
    followups: async () => [],
    files: async () => ({ workspace: "", files: [] }),
    schedules: async () => ({ schedules: [], unread: 0 }),
    threads: async () => [thread("th_old"), thread("th_other")],
    memory: async () => ({
      dir: "", enabled: false,
      memory: { text: "", entries: [], chars: 0, limit: 2200, rev: "" }, skills: [],
    }),
    steer: async (id: string) => {
      fake.steers.push(id)
      if (fake.failure) throw fake.failure
      return fake.acknowledgement ?? { steered: false, turn: turn() }
    },
    enqueueFollowup: async (id: string, text: string) => {
      fake.followups.push(text)
      return { id: "fu_1", thread_id: id, seq: 1, text, created_at: "" }
    },
  },
}))

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (_id: string, handlers: {
    onEvent: (event: SwarmEvent) => void
    onReady?: (payload: { seq: number; status: { running: boolean } }) => void
  }) => {
    fake.onEvent = handlers.onEvent
    handlers.onReady?.({ seq: 0, status: { running: false } })
    return () => undefined
  },
}))

function event(kind: string, seq: number, turnID = "tn_new"): SwarmEvent {
  return {
    kind, seq, turn_id: turnID, thread_id: "th_old", agent_id: "manager",
    created_at: "2026-01-01T00:00:00.010Z",
    ...(kind === "error" ? { err: "the model request failed" } : {}),
    ...(kind === "agent_message" ? { text: "The check finished." } : {}),
  }
}

function delayedAcknowledgement() {
  let resolve!: (value: Acknowledgement) => void
  fake.acknowledgement = new Promise((done) => { resolve = done })
  return resolve
}

beforeEach(async () => {
  fake.onEvent = undefined
  fake.acknowledgement = undefined
  fake.failure = undefined
  fake.steers.length = 0
  fake.followups.length = 0
  fake.logs = {}
  useApp.setState({
    activeId: undefined, threads: [], status: { running: false },
    followups: [], schedules: [], error: undefined, meta: undefined,
  })
  useProjects.setState({
    selectedId: undefined, memory: undefined, memoryProjectId: undefined,
    error: undefined, reviewing: false, reviewHint: undefined,
  })
  await useApp.getState().openThread("th_old")
})

describe("send acknowledgement ordering", () => {
  // The HTTP response and EventSource can reach the browser in either order.
  // Finishing before the acknowledgement must not leave an idle turn Working.
  it.each(["error", "done"])("keeps a streamed %s terminal idle after a late acknowledgement", async (kind) => {
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("perform a check")
    await vi.waitFor(() => expect(fake.steers).toEqual(["th_old"]))
    fake.onEvent?.(event("user_message", 1))
    if (kind === "done") fake.onEvent?.(event("agent_message", 2))
    fake.onEvent?.(event(kind, kind === "done" ? 3 : 2))
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_new" })
    resolve({ steered: false, turn: turn() })
    await sending
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_new" })
    expect(useApp.getState().status.started_at).toBeUndefined()
    expect(useApp.getState().transcript.running).toBe(false)
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(false)
  })

  it("paints an accepted new turn busy before any live event arrives", async () => {
    await useApp.getState().send("perform a check")
    expect(useApp.getState().status.running).toBe(true)
    expect(useApp.getState().status.started_at).toBeTruthy()
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(true)
  })

  it("starts a new turn after an older turn ended", async () => {
    fake.onEvent?.(event("user_message", 1, "tn_old"))
    fake.onEvent?.(event("done", 2, "tn_old"))
    await useApp.getState().send("perform another check")
    expect(useApp.getState().status.running).toBe(true)
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(true)
  })

  it("preserves a live start and its clock when acknowledgement arrives later", async () => {
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("perform a check")
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    fake.onEvent?.(event("user_message", 1))
    const started = useApp.getState().status.started_at
    resolve({ steered: false, turn: turn() })
    await sending
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_new", started_at: started })
  })

  it("keeps a running steer live and queues the following ordinary send", async () => {
    fake.onEvent?.(event("user_message", 1))
    fake.acknowledgement = Promise.resolve({ steered: true })
    await useApp.getState().send("narrow the check", undefined, { steer: true })
    expect(useApp.getState().status.running).toBe(true)
    await useApp.getState().send("perform a later check")
    expect(fake.steers).toEqual(["th_old"])
    expect(fake.followups).toEqual(["perform a later check"])
  })

  it("does not relight a terminal while an existing-turn steer is acknowledged", async () => {
    fake.onEvent?.(event("user_message", 1))
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("narrow the check", undefined, { steer: true })
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    fake.onEvent?.(event("done", 2))
    resolve({ steered: true })
    await sending
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(false)
  })

  it("keeps a streamed terminal idle when an early busy hint had no turn id", async () => {
    useApp.setState({ status: { running: true } })
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("narrow the check", undefined, { steer: true })
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    fake.onEvent?.(event("user_message", 1))
    fake.onEvent?.(event("error", 2))
    resolve({ steered: true })
    await sending
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_new" })
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(false)
  })

  it("preserves a later live turn when an earlier turn is acknowledged", async () => {
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("perform a check")
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    fake.onEvent?.(event("user_message", 1))
    fake.onEvent?.(event("done", 2))
    fake.onEvent?.(event("user_message", 3, "tn_next"))
    const started = useApp.getState().status.started_at
    resolve({ steered: false, turn: turn() })
    await sending
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "tn_next", started_at: started })
  })

  it("does not relight an older acknowledgement after a later turn also ended", async () => {
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("perform a check")
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    fake.onEvent?.(event("user_message", 1))
    fake.onEvent?.(event("done", 2))
    fake.onEvent?.(event("user_message", 3, "tn_next"))
    fake.onEvent?.(event("done", 4, "tn_next"))
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_next" })
    resolve({ steered: false, turn: turn() })
    await sending
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "tn_next" })
    expect(useApp.getState().transcript.turns.map((row) => row.status)).toEqual(["done", "done"])
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(false)
  })

  it("does not mark another conversation busy after navigating during acknowledgement", async () => {
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().send("perform a check")
    await vi.waitFor(() => expect(fake.steers).toHaveLength(1))
    await useApp.getState().openThread("th_other")
    resolve({ steered: false, turn: turn() })
    await sending
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().threads.find((row) => row.id === "th_other")?.running).toBe(false)
  })

  it.each(["live", "history"])("keeps an inactive goal target idle when its %s terminal beats an acknowledgement from another conversation", async (arrival) => {
    await useApp.getState().openThread("th_other")
    fake.onEvent?.({ ...event("user_message", 99, "tn_other"), thread_id: "th_other" })
    fake.onEvent?.({ ...event("done", 100, "tn_other"), thread_id: "th_other" })
    expect(useApp.getState().transcript.lastSeq).toBe(100)
    const resolve = delayedAcknowledgement()
    const sending = useApp.getState().setGoal("Inspect the existing work", "th_old")
    await vi.waitFor(() => expect(fake.steers).toEqual(["th_old"]))
    const completed = [event("user_message", 9), event("done", 10)]
    if (arrival === "history") fake.logs.th_old = completed
    await useApp.getState().openThread("th_old")
    if (arrival === "live") for (const row of completed) fake.onEvent?.(row)
    expect(useApp.getState().transcript.lastSeq).toBe(10)
    expect(useApp.getState().transcript.turns.map((row) => row.status)).toEqual(["done"])
    expect(useApp.getState().status.running).toBe(false)
    resolve({ steered: true })
    expect(await sending).toBe(true)
    expect(useApp.getState().activeId).toBe("th_old")
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().transcript.running).toBe(false)
    expect(useApp.getState().threads.find((row) => row.id === "th_old")?.running).toBe(false)
  })

  it("uses the visible target's own sequence when its accepted live update has no new event yet", async () => {
    fake.onEvent?.(event("user_message", 9, "tn_previous"))
    fake.onEvent?.(event("done", 10, "tn_previous"))
    fake.acknowledgement = Promise.resolve({ steered: true })
    expect(await useApp.getState().setGoal("Inspect the current work")).toBe(true)
    expect(useApp.getState().transcript.lastSeq).toBe(10)
    expect(useApp.getState().status.running).toBe(true)
  })

  it("keeps failed-request feedback idle", async () => {
    fake.failure = new Error("the request was not accepted")
    await useApp.getState().send("perform a check")
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().error).toBe("the request was not accepted")
  })
})


describe("engine restoration admission", () => {
  it.each([false, true])("keeps a pending draft without sending or queueing while restoring (steer %s)", async (steer) => {
    useApp.setState({ meta: { startup: "restoring", restore: { completed: 1, total: 3 } } as import("@/lib/types").Meta })
    expect(await useApp.getState().send("retained draft", undefined, { steer })).toBe(false)
    expect(fake.steers).toEqual([])
    expect(fake.followups).toEqual([])
    expect(useApp.getState().status.running).toBe(false)
  })
})
