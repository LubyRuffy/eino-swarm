import { beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import type { SwarmEvent, Thread, ThreadStatus } from "@/lib/types"
import { emptyTranscript } from "@/lib/transcript"
import { collectionDecision, stoppedCollectionEvents } from "@/test/worker-collection-events"
import { useApp } from "./app"

const fake = vi.hoisted(() => ({ subscriptions: [] as Array<{ id: string; since: number; handlers: {
  onEvent: (event: SwarmEvent) => void
  onReady: (ready: { seq?: number; status?: ThreadStatus }) => void
  onClose: (reason: string) => void
} }> }))
vi.mock("@/lib/api", () => ({ api: {
  thread: vi.fn(), turns: vi.fn(), followups: vi.fn(), threadLog: vi.fn(), startTurn: vi.fn(),
  files: vi.fn(async () => ({ workspace: "", files: [] })),
  schedules: vi.fn(async () => ({ schedules: [], unread: 0 })), threads: vi.fn(async () => []),
} }))
vi.mock("@/lib/stream", () => ({ subscribeEvents: (id: string, handlers: typeof fake.subscriptions[number]["handlers"], since = 0) => {
  fake.subscriptions.push({ id, handlers, since })
  return () => undefined
} }))
vi.mock("./projects", () => ({ useProjects: { getState: () => ({
  restoreReview: async () => undefined, select: () => undefined, loadMemory: async () => undefined,
}) } }))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const thread = (id: string, extra: Partial<Thread> = {}): Thread => ({
  id, title: id, project_id: "", provider_id: "offline", reasoning_effort: "",
  archived: false, running: false, created_at: "", last_active_at: "", ...extra,
})
const event = (id: string, seq = 10, kind = "user_message", text = "Visible tail"): SwarmEvent => ({
  thread_id: id, turn_id: "turn-1", seq, kind, text, agent_id: "manager", created_at: "",
})
const tick = () => new Promise((resolve) => setTimeout(resolve, 0))
type Detail = Awaited<ReturnType<typeof api.thread>>

beforeEach(() => {
  vi.resetAllMocks()
  fake.subscriptions.length = 0
  vi.mocked(api.files).mockResolvedValue({ workspace: "", files: [] })
  vi.mocked(api.schedules).mockResolvedValue({ schedules: [], unread: 0 })
  vi.mocked(api.thread).mockImplementation(async (id) => ({ thread: thread(id), status: { running: false } }))
  vi.mocked(api.threads).mockImplementation(async () => useApp.getState().threads)
  vi.mocked(api.turns).mockResolvedValue([])
  vi.mocked(api.followups).mockResolvedValue([])
  vi.mocked(api.threadLog).mockImplementation(async (id) => ({ events: [event(id)], has_more: false }))
  useApp.setState({ activeId: undefined, threads: [thread("a"), thread("b")], transcript: emptyTranscript(),
    status: { running: false }, turns: [], followups: [], loaded: false, usage: undefined, error: undefined,
    scheduleInboxOpen: false,
  })
})

describe("opening a conversation without waiting for context replay", () => {
  it("starts all reads together and paints the tail while other reads are pending", async () => {
    const detail = deferred<Detail>()
    const turns = deferred<Awaited<ReturnType<typeof api.turns>>>()
    const followups = deferred<Awaited<ReturnType<typeof api.followups>>>()
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.turns).mockReturnValue(turns.promise)
    vi.mocked(api.followups).mockReturnValue(followups.promise)
    const opening = useApp.getState().openThread("a")
    await tick()
    expect(api.threadLog).toHaveBeenCalledWith("a", { limit: 80 })
    expect(useApp.getState().loaded).toBe(true)
    expect(useApp.getState().transcript.agents.manager.blocks[0].text).toBe("Visible tail")
    expect(fake.subscriptions.at(-1)?.since).toBe(10)
    detail.resolve({ thread: thread("a", { context_chars: 3124, context_budget: 8000 }), status: { running: true } })
    await tick()
    expect(useApp.getState().threads[0].context_chars).toBe(3124)
    expect(useApp.getState().status.running).toBe(true)
    expect(useApp.getState().transcript.agents.manager.blocks[0].text).toBe("Visible tail")
    turns.resolve([])
    followups.resolve([])
    await opening
  })

  it("keeps newer SSE terminal and title when opening detail lands late", async () => {
    const detail = deferred<Detail>()
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    const opening = useApp.getState().openThread("a")
    await tick()
    expect(fake.subscriptions).toHaveLength(1)
    fake.subscriptions[0].handlers.onReady({ status: { running: true, turn_id: "turn-1" } })
    fake.subscriptions[0].handlers.onEvent(event("a", 11, "title", "Live title"))
    fake.subscriptions[0].handlers.onEvent(event("a", 12, "done", "Final result"))
    const transcript = useApp.getState().transcript
    detail.resolve({ thread: thread("a", { context_chars: 123 }), status: { running: true, turn_id: "old-turn" } })
    await opening
    expect(useApp.getState().status).toMatchObject({ running: false, turn_id: "turn-1" })
    expect(useApp.getState().threads.find((t) => t.id === "a")?.title).toBe("Live title")
    expect(useApp.getState().transcript).toBe(transcript)
  })

  it("does not restore an opening snapshot's wait after a live Stop decision", async () => {
    const detail = deferred<Detail>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    const opening = useApp.getState().openThread("a")
    await tick()
    // The tail and its live subscription are ready before the stale detail.
    // Do not send onReady: that separate status witness would hide this race.
    fake.subscriptions.at(-1)!.handlers.onEvent(events[5])
    detail.resolve({ thread: thread("a"), status: { running: false, turn_id: "turn", awaiting_continue: true } })
    await opening
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().status.awaiting_continue).not.toBe(true)
    expect(useApp.getState().transcript.agents.manager.blocks.find((block) => block.confirm)?.confirm).toMatchObject({ pending: false, continued: false })
  })

  it.each([
    { name: "a different wait", text: '{"wait_id":"other-wait"}', declined: false },
    { name: "a different turn", turn_id: "another-turn", text: collectionDecision, declined: false },
    { name: "a worker actor", agent_id: "worker-1", text: collectionDecision, declined: false },
    { name: "malformed metadata", text: "{", declined: false },
    { name: "the current wait", text: collectionDecision, declined: true },
  ])("preserves the opening detail's running state after a decline for $name", async ({ name: _name, declined, ...extra }) => {
    const detail = deferred<Detail>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    const opening = useApp.getState().openThread("a")
    await tick()
    fake.subscriptions.at(-1)!.handlers.onEvent({ ...events[5], ...extra })
    detail.resolve({ thread: thread("a"), status: { running: true, turn_id: "turn", awaiting_continue: true, started_at: "2026-01-01T00:00:00Z" } })
    await opening
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: !declined, started_at: "2026-01-01T00:00:00Z" })
    expect(useApp.getState().transcript.running).toBe(true)
    expect(useApp.getState().threads.find((thread) => thread.id === "a")?.running).toBe(true)
    expect(useApp.getState().transcript.agents.manager.blocks.find((block) => block.confirm)?.confirm?.pending).toBe(!declined)
  })

  it.each([13, 14])("does not restore a declined wait from a late ready snapshot at seq %s", async (seq) => {
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    await useApp.getState().openThread("a")
    const handlers = fake.subscriptions.at(-1)!.handlers
    handlers.onEvent(events[5])
    handlers.onReady({ seq, status: { running: true, turn_id: "turn", awaiting_continue: true, workers: 1 } })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: false, workers: 1 })
    expect(useApp.getState().transcript.running).toBe(true)
    expect(useApp.getState().transcript.agents.manager.blocks.find((block) => block.confirm)?.confirm?.pending).toBe(false)
  })

  it.each([15, undefined])("keeps a later or unknown ready watermark authoritative: %s", async (seq) => {
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    await useApp.getState().openThread("a")
    const handlers = fake.subscriptions.at(-1)!.handlers
    handlers.onEvent(events[5])
    handlers.onReady({ seq, status: { running: true, turn_id: "turn", awaiting_continue: true } })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: true })
  })

  it.each([13, 15, undefined])("keeps ready at seq %s authoritative when opening detail arrives afterwards", async (seq) => {
    const detail = deferred<Detail>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    const opening = useApp.getState().openThread("a")
    await tick()
    const handlers = fake.subscriptions.at(-1)!.handlers
    handlers.onEvent(events[5])
    handlers.onReady({ seq, status: { running: true, turn_id: "turn", awaiting_continue: true, workers: 1 } })
    detail.resolve({ thread: thread("a"), status: { running: false, turn_id: "turn", awaiting_continue: true, workers: 0 } })
    await opening
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: seq !== 13, workers: 1 })
  })

  it.each(["next-turn", undefined])("does not apply a prior decline to opening detail with a different or unknown turn: %s", async (turn_id) => {
    const detail = deferred<Detail>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    const opening = useApp.getState().openThread("a")
    await tick()
    fake.subscriptions.at(-1)!.handlers.onEvent(events[5])
    detail.resolve({ thread: thread("a"), status: { running: true, turn_id, awaiting_continue: true } })
    await opening
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id, awaiting_continue: true })
  })

  it("merges the declined waiting flag already present in the opening tail", async () => {
    const detail = deferred<Detail>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 6), has_more: false })
    const opening = useApp.getState().openThread("a")
    await tick()
    expect(fake.subscriptions.at(-1)?.since).toBe(14)
    detail.resolve({ thread: thread("a"), status: { running: true, turn_id: "turn", awaiting_continue: true } })
    await opening
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: false })
  })

  it.each([13, 15])("keeps a newer wait pending when ready at seq %s follows an older decline", async (seq) => {
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.threadLog).mockResolvedValue({ events: events.slice(0, 5), has_more: false })
    await useApp.getState().openThread("a")
    const handlers = fake.subscriptions.at(-1)!.handlers
    handlers.onEvent(events[5])
    handlers.onEvent({ ...events[4], seq: 15, text: events[4].text!.replace("wait-1", "wait-2") })
    handlers.onReady({ seq, status: { running: true, turn_id: "turn", awaiting_continue: true } })
    expect(useApp.getState().status).toMatchObject({ running: true, turn_id: "turn", awaiting_continue: true })
    expect(useApp.getState().transcript.agents.manager.blocks.filter((block) => block.confirm).at(-1)?.confirm)
      .toMatchObject({ waitId: "wait-2", pending: true })
  })

  it("continues paging past a tail containing only an internal Stop decision", async () => {
    const older = deferred<Awaited<ReturnType<typeof api.threadLog>>>()
    const events = stoppedCollectionEvents().map((event) => ({ ...event, thread_id: "a" }))
    vi.mocked(api.threadLog)
      .mockResolvedValueOnce({ events: [events[5]], has_more: true })
      .mockReturnValueOnce(older.promise)
    const opening = useApp.getState().openThread("a")
    await tick()
    expect(api.threadLog).toHaveBeenLastCalledWith("a", { before: 14, limit: 80 })
    expect(useApp.getState().transcript.agents.manager?.blocks ?? []).toHaveLength(0)
    older.resolve({ events: [events[0]], has_more: false })
    await opening
    expect(useApp.getState().transcript.agents.manager.blocks.map((block) => block.text)).toEqual([events[0].text])
  })

  it("discards first visit responses after A to B to A", async () => {
    const detail = deferred<Detail>()
    const tail = deferred<Awaited<ReturnType<typeof api.threadLog>>>()
    vi.mocked(api.thread).mockImplementationOnce(() => detail.promise)
    vi.mocked(api.threadLog).mockImplementationOnce(() => tail.promise)
    const first = useApp.getState().openThread("a")
    await useApp.getState().openThread("b")
    const oldSubscription = fake.subscriptions.at(-1)!
    await useApp.getState().openThread("a")
    const transcript = useApp.getState().transcript
    detail.resolve({ thread: thread("a", { context_chars: 999 }), status: { running: true } })
    tail.resolve({ events: [event("a", 99, "user_message", "Obsolete tail")], has_more: false })
    await first
    oldSubscription.handlers.onReady({ status: { running: true } })
    oldSubscription.handlers.onEvent(event("b", 20, "user_message", "Wrong conversation"))
    expect(useApp.getState().transcript).toBe(transcript)
    expect(useApp.getState().threads.find((t) => t.id === "a")?.context_chars).toBeUndefined()
    expect(useApp.getState().status.running).toBe(false)
  })

  it("does not replace rewound state, newer turns or followups with opening responses", async () => {
    const detail = deferred<Detail>()
    const turns = deferred<Awaited<ReturnType<typeof api.turns>>>()
    const followups = deferred<Awaited<ReturnType<typeof api.followups>>>()
    vi.mocked(api.thread).mockReturnValue(detail.promise)
    vi.mocked(api.turns).mockReturnValue(turns.promise)
    vi.mocked(api.followups).mockReturnValue(followups.promise)
    const opening = useApp.getState().openThread("a")
    await tick()
    expect(fake.subscriptions).toHaveLength(1)
    fake.subscriptions[0].handlers.onEvent(event("a", 11, "rewound", "10"))
    const transcript = useApp.getState().transcript
    const currentTurns = [{ id: "new-turn" }] as ReturnType<typeof useApp.getState>["turns"]
    const currentQueue = [{ id: "new-queue" }] as ReturnType<typeof useApp.getState>["followups"]
    useApp.setState({ turns: currentTurns, followups: currentQueue })
    detail.resolve({ thread: thread("a", { context_chars: 999 }), status: { running: true } })
    turns.resolve([{ id: "old-turn" }] as typeof currentTurns)
    followups.resolve([{ id: "old-queue" }] as typeof currentQueue)
    await opening
    expect(useApp.getState().transcript).toBe(transcript)
    expect(useApp.getState().turns).toBe(currentTurns)
    expect(useApp.getState().followups).toBe(currentQueue)
    expect(useApp.getState().status.running).toBe(false)
  })

  it("replays from zero if tail fails and ignores an abandoned detail failure", async () => {
    vi.mocked(api.threadLog).mockRejectedValueOnce(new Error("tail failed"))
    await useApp.getState().openThread("a")
    expect(fake.subscriptions.at(-1)?.since).toBe(0)
    fake.subscriptions.at(-1)!.handlers.onReady({ status: { running: false } })
    expect(useApp.getState().loaded).toBe(true)
    const detail = deferred<Detail>()
    vi.mocked(api.thread).mockReturnValueOnce(detail.promise)
    const opening = useApp.getState().openThread("b")
    await tick()
    await useApp.getState().openThread("a")
    detail.reject(new Error("abandoned request"))
    await opening
    expect(useApp.getState().error).toBeUndefined()
  })
})

it("retains detail's running flag when the tail arrives later", async () => {
  const tail = deferred<Awaited<ReturnType<typeof api.threadLog>>>()
  vi.mocked(api.threadLog).mockReturnValueOnce(tail.promise)
  vi.mocked(api.thread).mockResolvedValueOnce({ thread: thread("a"), status: { running: true } })
  const opening = useApp.getState().openThread("a")
  await tick()
  expect(useApp.getState().loaded).toBe(false)
  expect(useApp.getState().status.running).toBe(true)
  tail.resolve({ events: [event("a")], has_more: false })
  await opening
  expect(useApp.getState().transcript.running).toBe(true)
  expect(fake.subscriptions.at(-1)?.since).toBe(10)
})

it("paints a tool-only tail and attaches SSE before its older manager page arrives", async () => {
  const older = deferred<Awaited<ReturnType<typeof api.threadLog>>>()
  vi.mocked(api.threadLog)
    .mockResolvedValueOnce({ events: [{ ...event("a", 40, "tool_call", "exec({})"), agent_id: "worker-1" }], has_more: true })
    .mockReturnValueOnce(older.promise)
  const opening = useApp.getState().openThread("a")
  await tick()
  expect(useApp.getState().loaded).toBe(true)
  expect(fake.subscriptions.at(-1)?.since).toBe(40)
  expect(api.threadLog).toHaveBeenLastCalledWith("a", { before: 40, limit: 80 })
  older.resolve({ events: [event("a", 2, "user_message", "Earlier manager input")], has_more: false })
  await opening
  expect(useApp.getState().transcript.agents.manager.blocks.some((block) => block.text === "Earlier manager input")).toBe(true)
})

it("ignores callbacks from an earlier visit to the same conversation", async () => {
  await useApp.getState().openThread("a")
  const old = fake.subscriptions.at(-1)!
  await useApp.getState().openThread("b")
  await useApp.getState().openThread("a")
  const transcript = useApp.getState().transcript
  old.handlers.onReady({ status: { running: true, turn_id: "obsolete" } })
  old.handlers.onEvent(event("a", 20, "user_message", "Obsolete revisit"))
  old.handlers.onClose("error")
  expect(useApp.getState().status.running).toBe(false)
  expect(useApp.getState().transcript).toBe(transcript)
  expect(useApp.getState().connected).toBe(true)
})

it("preserves an updated usage snapshot while filling unrelated detail", async () => {
  const detail = deferred<Detail>()
  vi.mocked(api.thread).mockReturnValueOnce(detail.promise)
  const opening = useApp.getState().openThread("a")
  await tick()
  const usage = { context_tokens: 400, context_window: 1000,
    turn: { prompt_tokens: 400, completion_tokens: 30, cached_tokens: 0, reasoning_tokens: 0, total_tokens: 430, calls: 1 },
    thread: { prompt_tokens: 400, completion_tokens: 30, cached_tokens: 0, reasoning_tokens: 0, total_tokens: 430, calls: 1 } }
  fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 11, "usage", JSON.stringify(usage)))
  fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 12, "title", "Updated title"))
  detail.resolve({ thread: thread("a", { context_budget: 8000, context_chars: 123 }), status: { running: false } })
  await opening
  expect(useApp.getState().usage).toEqual(usage)
  expect(useApp.getState().threads.find((row) => row.id === "a")).toMatchObject({ title: "Updated title", context_budget: 8000 })
})

it("still fills context characters after a progress delta that changes no replay content", async () => {
  const detail = deferred<Detail>()
  vi.mocked(api.thread).mockReturnValueOnce(detail.promise)
  const opening = useApp.getState().openThread("a")
  await tick()
  fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 0, "progress", "Processing"))
  detail.resolve({ thread: thread("a", { context_chars: 2000, context_budget: 8000 }), status: { running: false } })
  await opening
  expect(useApp.getState().threads.find((row) => row.id === "a")?.context_chars).toBe(2000)
})

it("keeps a local rewind before its SSE receipt when detail and turns arrive", async () => {
  const detail = deferred<Detail>()
  const turns = deferred<Awaited<ReturnType<typeof api.turns>>>()
  const sendReceipt = deferred<Awaited<ReturnType<typeof api.startTurn>>>()
  vi.mocked(api.thread).mockReturnValueOnce(detail.promise)
  vi.mocked(api.turns).mockReturnValueOnce(turns.promise)
  vi.mocked(api.startTurn).mockReturnValueOnce(sendReceipt.promise)
  const opening = useApp.getState().openThread("a")
  await tick()
  const sending = useApp.getState().send("Revised input", undefined, { fromEventSeq: 10 })
  const transcript = useApp.getState().transcript
  expect(transcript.rewindCut).toBeDefined()
  detail.resolve({ thread: thread("a", { context_chars: 9999 }), status: { running: false } })
  turns.resolve([{ id: "deleted-turn" }] as ReturnType<typeof useApp.getState>["turns"])
  await opening
  expect(useApp.getState().transcript).toBe(transcript)
  expect(useApp.getState().status.running).toBe(true)
  expect(useApp.getState().turns).toEqual([])
  expect(useApp.getState().threads.find((row) => row.id === "a")?.context_chars).toBeUndefined()
  sendReceipt.resolve({} as Awaited<ReturnType<typeof api.startTurn>>)
  await sending
})

it("does not paint a stale tail over a local rewind while the tail request is pending", async () => {
  const tail = deferred<Awaited<ReturnType<typeof api.threadLog>>>()
  const sendReceipt = deferred<Awaited<ReturnType<typeof api.startTurn>>>()
  vi.mocked(api.threadLog).mockReturnValueOnce(tail.promise)
  vi.mocked(api.startTurn).mockReturnValueOnce(sendReceipt.promise)
  const opening = useApp.getState().openThread("a")
  await tick()
  const sending = useApp.getState().send("Revised before tail", undefined, { fromEventSeq: 10 })
  const transcript = useApp.getState().transcript
  tail.resolve({ events: [event("a", 10, "user_message", "Obsolete input")], has_more: false })
  await opening
  expect(useApp.getState().transcript).toBe(transcript)
  expect(fake.subscriptions.at(-1)?.since).toBe(0)
  sendReceipt.resolve({} as Awaited<ReturnType<typeof api.startTurn>>)
  await sending
})

it("replaces an opening running stamp with the newer idle SSE ready status", async () => {
  vi.mocked(api.thread).mockResolvedValueOnce({ thread: thread("a"), status: { running: true } })
  await useApp.getState().openThread("a")
  expect(useApp.getState().transcript.running).toBe(true)
  fake.subscriptions.at(-1)!.handlers.onReady({ status: { running: false } })
  expect(useApp.getState().status.running).toBe(false)
  expect(useApp.getState().transcript.running).toBe(false)
})

it("does not install an earlier visit's terminal refresh into the newly selected conversation", async () => {
  const turns = deferred<Awaited<ReturnType<typeof api.turns>>>()
  await useApp.getState().openThread("a")
  vi.mocked(api.turns).mockReturnValueOnce(turns.promise)
  fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 11, "done", "Complete"))
  await useApp.getState().openThread("b")
  await useApp.getState().openThread("a")
  const currentTurns = useApp.getState().turns
  turns.resolve([{ id: "obsolete-refresh" }] as typeof currentTurns)
  await tick()
  expect(useApp.getState().turns).toBe(currentTurns)
})

for (const initialFirst of [true, false]) {
  it(`keeps the terminal turn snapshot when opening rows arrive ${initialFirst ? "before" : "after"} it`, async () => {
    const initial = deferred<Awaited<ReturnType<typeof api.turns>>>()
    const terminal = deferred<Awaited<ReturnType<typeof api.turns>>>()
    vi.mocked(api.turns).mockReturnValueOnce(initial.promise).mockReturnValueOnce(terminal.promise)
    const opening = useApp.getState().openThread("a")
    await tick()
    fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 11, "done", "Complete"))
    const running = [{ id: "turn-1", status: "running" }] as ReturnType<typeof useApp.getState>["turns"]
    const done = [{ id: "turn-1", status: "done" }] as typeof running
    if (initialFirst) {
      initial.resolve(running)
      await tick()
      expect(useApp.getState().turns).toEqual([])
      terminal.resolve(done)
    } else {
      terminal.resolve(done)
      await tick()
      expect(useApp.getState().turns).toEqual(done)
      initial.resolve(running)
    }
    await opening
    await tick()
    expect(useApp.getState().turns).toEqual(done)
  })
}

it("keeps the terminal turns refresh when opening the already active conversation", async () => {
  const terminal = deferred<Awaited<ReturnType<typeof api.turns>>>()
  await useApp.getState().openThread("a")
  vi.mocked(api.turns).mockReturnValueOnce(terminal.promise)
  fake.subscriptions.at(-1)!.handlers.onEvent(event("a", 11, "done", "Complete"))
  await useApp.getState().openThread("a")
  const done = [{ id: "turn-1", status: "done" }] as ReturnType<typeof useApp.getState>["turns"]
  terminal.resolve(done)
  await tick()
  expect(useApp.getState().turns).toEqual(done)
})
