import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { subscribeEvents } from "@/lib/stream"
import { emptyTranscript } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { collectionDecision, collectionEvent, stoppedCollectionEvents } from "@/test/worker-collection-events"
import { dropQueued, queueEvent, type StreamSnapshot } from "./app-stream"
import { applyAgentLog, applyTail, prependOlder, resetThreadHistory } from "./thread-history"

vi.mock("@/lib/api", () => ({ api: { turns: async () => [] } }))

class FakeEventSource {
  static last: FakeEventSource
  static readonly CLOSED = 2
  readyState = 1
  listeners = new Map<string, (event: MessageEvent) => void>()
  constructor() { FakeEventSource.last = this }
  addEventListener(kind: string, listener: (event: MessageEvent) => void) { this.listeners.set(kind, listener) }
  close() {}
  deliver(event: SwarmEvent) {
    this.listeners.get(event.kind)?.({ type: event.kind, data: JSON.stringify(event) } as MessageEvent)
  }
}

let state: StreamSnapshot
let paint: FrameRequestCallback | undefined
let stop: () => void
function emit(event: SwarmEvent) {
  FakeEventSource.last.deliver(event)
  const pending = paint
  paint = undefined
  pending?.(0)
}
function rawDecisionVisible() {
  return Object.values(state.transcript.agents).some((agent) => agent.blocks.some((block) => block.kind === "notice" && block.text === collectionDecision))
}

beforeEach(() => {
  dropQueued()
  resetThreadHistory()
  paint = undefined
  vi.stubGlobal("EventSource", FakeEventSource)
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => { paint = callback; return 1 })
  vi.stubGlobal("cancelAnimationFrame", () => { paint = undefined })
  state = {
    activeId: "thread", transcript: emptyTranscript(), status: { running: true, turn_id: "turn", awaiting_continue: true },
    threads: [], followups: [], schedules: [],
    refreshFiles: vi.fn(async () => {}), refreshThreads: vi.fn(async () => {}),
    refreshFollowups: vi.fn(async () => {}), refreshSchedules: vi.fn(async () => {}),
  }
  stop = subscribeEvents("thread", { onEvent: (event) => queueEvent(
    (patch) => { state = { ...state, ...(typeof patch === "function" ? patch(state) : patch) } },
    () => state, "thread", event,
  ) })
})

afterEach(() => { stop(); dropQueued(); vi.unstubAllGlobals() })

describe("named worker collection decisions through the live store", () => {
  it("receives the named SSE decision, clears its matching wait, and leaves lifecycle changes to real events", () => {
    const events = stoppedCollectionEvents()
    state.transcript = applyTail("thread", events.slice(0, 5))
    emit(events[5])
    expect(state.status).toEqual({ running: true, turn_id: "turn", awaiting_continue: false })
    expect(state.transcript.agents.manager.blocks.find((block) => block.confirm)?.confirm).toMatchObject({ pending: false, continued: false })
    expect(state.transcript.agents["worker-1"].status).toBe("running")
    expect(state.transcript.lastSeq).toBe(14)
    expect(rawDecisionVisible()).toBe(false)
  })

  it.each([true, false])("keeps a Stop receipt and late cancellation once when history arrives first=%s", (historyFirst) => {
    const events = stoppedCollectionEvents()
    state.transcript = applyTail("thread", events.slice(0, 5))
    for (const event of events.slice(5)) {
      if (historyFirst) state.transcript = applyAgentLog("thread", [event], state.transcript)!
      emit(event)
      if (!historyFirst) state.transcript = applyAgentLog("thread", [event], state.transcript)!
      emit(event)
    }
    expect(rawDecisionVisible()).toBe(false)
    expect(state.transcript.agents.manager.blocks.filter((block) => block.confirm)).toHaveLength(1)
    expect(state.transcript.agents["worker-1"]).toMatchObject({ status: "cancelled", activity: "stopped" })
    expect(state.transcript.agents["worker-1"].error).toBeUndefined()
    expect(state.transcript.running).toBe(false)
    expect(state.transcript.lastSeq).toBe(19)
    expect(state.refreshFiles).toHaveBeenCalledTimes(historyFirst ? 0 : 1)
    expect(state.refreshThreads).toHaveBeenCalledTimes(historyFirst ? 0 : 1)
  })

  it("keeps the receipt hidden through cold and older-page replay without reviving stopped workers", () => {
    const events = stoppedCollectionEvents()
    const cold = applyTail("thread", events)
    state.transcript = applyTail("thread", events.slice(6), events.slice(0, 5).filter((event) => event.kind === "spawned"))
    state.transcript = prependOlder("thread", events.slice(0, 6), state.transcript)!
    expect(state.transcript).toEqual(cold)
    expect(rawDecisionVisible()).toBe(false)
    emit(events[5])
    emit(events[8])
    expect(state.transcript).toEqual(cold)
    expect(state.refreshFiles).not.toHaveBeenCalled()
  })

  it.each([
    { text: '{"wait_id":"old-wait"}' },
    { turn_id: "old-turn", text: collectionDecision },
    { agent_id: "worker-1", text: collectionDecision },
    { role: "verifier", text: collectionDecision },
    { text: "{" },
  ])("does not clear a different current wait: %j", (extra) => {
    state.transcript = applyTail("thread", stoppedCollectionEvents().slice(0, 5))
    emit(collectionEvent(14, "workers_collection_declined", extra))
    expect(state.status).toMatchObject({ running: true, awaiting_continue: true })
    expect(state.transcript.agents.manager.blocks.find((block) => block.confirm)?.confirm?.pending).toBe(true)
    expect(rawDecisionVisible()).toBe(false)
  })

  it("never sets running true when the decision arrives after terminal status", () => {
    const events = stoppedCollectionEvents()
    const receipt = events.splice(5, 1)[0]
    state.transcript = applyTail("thread", events)
    state.status = { running: false, turn_id: "turn", awaiting_continue: false }
    const before = state.transcript.agents["worker-1"]
    emit({ ...receipt, seq: 20 })
    expect(state.status).toEqual({ running: false, turn_id: "turn", awaiting_continue: false })
    expect(state.transcript.agents["worker-1"]).toEqual(before)
    expect(state.transcript.lastSeq).toBe(20)
    expect(rawDecisionVisible()).toBe(false)
  })

  it("clears only awaiting_continue when a matching receipt follows a stopped status snapshot", () => {
    state.transcript = applyTail("thread", stoppedCollectionEvents().slice(0, 5))
    state.status = { running: false, turn_id: "turn", awaiting_continue: true }
    emit(collectionEvent(14, "workers_collection_declined", { text: collectionDecision }))
    expect(state.status).toEqual({ running: false, turn_id: "turn", awaiting_continue: false })
    expect(state.transcript.agents["worker-1"].status).toBe("running")
  })
})
