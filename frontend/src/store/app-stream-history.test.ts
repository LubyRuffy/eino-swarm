import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript, placePendingEdit, rewindTranscript } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { dropQueued, queueEvent, type StreamSnapshot } from "./app-stream"
import {
  applyAgentLog,
  applyTail,
  loadAgentHistory,
  loadOlderHistory,
  prependOlder,
  rememberRewind,
  resetThreadHistory,
} from "./thread-history"

const fake = vi.hoisted(() => ({ agentLog: vi.fn(), threadLog: vi.fn() }))
vi.mock("@/lib/api", () => ({ api: { turns: async () => [], agentLog: fake.agentLog, threadLog: fake.threadLog } }))

let paint: FrameRequestCallback | undefined
let state: StreamSnapshot & { historyHasMore: boolean; historyLoading: boolean; loadOlder: () => Promise<void> }

function event(seq: number, kind: SwarmEvent["kind"], text: string, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    thread_id: "th_1", turn_id: "tn_1", agent_id: "worker-1", seq, kind, text,
    created_at: "2026-01-01T00:00:00Z", ...extra,
  }
}

function emit(ev: SwarmEvent) {
  queueEvent((patch) => {
    state = { ...state, ...(typeof patch === "function" ? patch(state) : patch) }
  }, () => state, state.activeId!, ev)
  const pending = paint
  paint = undefined
  pending?.(0)
}

function workerThoughts() {
  return state.transcript.agents["worker-1"]?.blocks.filter((b) => b.kind === "reasoning") ?? []
}

beforeEach(() => {
  fake.agentLog.mockReset()
  fake.threadLog.mockReset()
  dropQueued()
  resetThreadHistory()
  paint = undefined
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => { paint = callback; return 1 })
  vi.stubGlobal("cancelAnimationFrame", () => { paint = undefined })
  state = {
    activeId: "th_1", transcript: emptyTranscript(), status: { running: false },
    historyHasMore: true, historyLoading: false, loadOlder: async () => {},
    threads: [], followups: [], schedules: [],
    refreshFiles: vi.fn(async () => {}), refreshThreads: vi.fn(async () => {}),
    refreshFollowups: vi.fn(async () => {}), refreshSchedules: vi.fn(async () => {}),
  }
})

afterEach(() => { dropQueued(); vi.unstubAllGlobals() })

describe("history and live event overlap", () => {
  for (const historyFirst of [true, false]) {
    it(`keeps one worker thought when ${historyFirst ? "agent history" : "SSE"} arrives first`, () => {
      const thought = event(11, "reasoning", "verified the source")
      state.transcript = applyTail("th_1", [event(10, "user_message", "continue", { agent_id: "manager" })])
      if (historyFirst) state.transcript = applyAgentLog("th_1", [thought])!
      emit(thought)
      if (!historyFirst) state.transcript = applyAgentLog("th_1", [thought])!
      expect(workerThoughts()).toHaveLength(1)
      expect(workerThoughts()[0].seq).toBe(11)
    })
  }

  it("ignores a repeated live event before repeating terminal side effects", () => {
    state.transcript = applyTail("th_1", [])
    const done = event(12, "done", "finished", { agent_id: "manager" })
    emit(done)
    emit(done)
    expect(state.refreshFiles).toHaveBeenCalledTimes(1)
    expect(state.refreshThreads).toHaveBeenCalledTimes(1)
    expect(state.refreshFollowups).toHaveBeenCalledTimes(1)
    expect(state.refreshSchedules).toHaveBeenCalledTimes(1)
  })

  it("does not replay terminal side effects already loaded in history", () => {
    const done = event(12, "done", "finished", { agent_id: "manager" })
    state.transcript = applyTail("th_1", [done])
    emit(done)
    expect(state.refreshFiles).not.toHaveBeenCalled()
    expect(state.refreshSchedules).not.toHaveBeenCalled()
  })

  it("deduplicates SSE fallback when the initial history endpoint was unavailable", () => {
    const thought = event(11, "reasoning", "fallback event")
    emit(thought)
    emit(thought)
    expect(workerThoughts()).toHaveLength(1)
  })

  it("loads an unseen lower seq and keeps identical text with different seqs", () => {
    state.transcript = applyTail("th_1", [event(40, "reasoning", "same thought")])
    emit(event(11, "reasoning", "same thought"))
    emit(event(12, "reasoning", "same thought"))
    expect(workerThoughts().map((b) => b.seq)).toEqual([40, 11, 12])
  })

  it("does not repeat a paged thought when its delayed SSE event arrives", () => {
    const thought = event(11, "reasoning", "earlier thought")
    state.transcript = applyTail("th_1", [event(40, "reasoning", "latest thought")])
    state.transcript = prependOlder("th_1", [thought])!
    emit(thought)
    expect(workerThoughts().map((b) => b.seq)).toEqual([11, 40])
  })

  it("promotes roster-only rows into the full page without duplicating worker state", () => {
    const spawned = event(2, "spawned", "assigned work", { role: "worker" })
    const finished = event(3, "finished", "verified")
    state.transcript = applyTail("th_1", [event(40, "user_message", "later", { agent_id: "manager" })], [spawned, finished])
    state.transcript = prependOlder("th_1", [spawned, finished])!
    emit(spawned)
    expect(state.transcript.agents.manager.blocks.filter((b) => b.kind === "spawn")).toHaveLength(1)
    expect(state.transcript.agents["worker-1"].status).toBe("done")
  })

  it("resets event identity when another conversation reuses a seq", () => {
    const thought = event(11, "reasoning", "first conversation")
    state.transcript = applyTail("th_1", [thought])
    resetThreadHistory()
    state.activeId = "th_2"
    state.transcript = applyTail("th_2", [])
    emit({ ...thought, thread_id: "th_2", text: "second conversation" })
    expect(workerThoughts().map((b) => b.text)).toEqual(["second conversation"])
  })

  it("retains rewind exclusion while accepting later replacement events", () => {
    state.transcript = applyTail("th_1", [event(11, "reasoning", "discarded")])
    rememberRewind("th_1", 11, 11)
    state.transcript = prependOlder("th_1", [])!
    emit(event(11, "reasoning", "discarded"))
    emit(event(12, "reasoning", "replacement"))
    expect(workerThoughts().map((b) => b.text)).toEqual(["replacement"])
  })

  for (const fromAgentLog of [false, true]) {
    it(`restores a pre-cut checkpoint from ${fromAgentLog ? "agent history" : "the page"} after a merged thought is rewound`, () => {
      const checkpoints = [
        event(2, "reasoning", "first", { message_id: "output-a" }),
        event(5, "reasoning", "first extended", { message_id: "output-a" }),
      ]
      state.transcript = applyTail("th_1", fromAgentLog ? [] : checkpoints)
      if (fromAgentLog) state.transcript = applyAgentLog("th_1", checkpoints)!
      expect(workerThoughts()).toHaveLength(1)
      expect(workerThoughts()[0].text).toBe("first extended")
      emit(event(0, "rewound", "4", { agent_id: "manager" }))
      expect(workerThoughts()).toHaveLength(1)
      expect(workerThoughts()[0]).toMatchObject({ text: "first", seq: 2, streaming: false })
      expect(state.transcript.lastSeq).toBe(5)
      // The cut spans what the page and SSE delivered before the marker. A
      // worker-log row is not proof of that order: it may be newer than the
      // truncation, and SSE delivers such a row again.
      expect(state.transcript.rewindCut).toEqual({ from: 4, through: fromAgentLog ? 0 : 5, live: true })
      if (!fromAgentLog) {
        emit(event(5, "reasoning", "first extended", { message_id: "output-a" }))
        expect(workerThoughts()[0].text).toBe("first")
      }
    })
  }

  it("keeps an optimistic edited message while restoring the earlier checkpoint", () => {
    state.transcript = applyTail("th_1", [
      event(2, "reasoning", "first", { message_id: "output-a" }),
      event(5, "reasoning", "first extended", { message_id: "output-a" }),
    ])
    rememberRewind("th_1", 4, state.transcript.lastSeq)
    state.transcript = placePendingEdit(rewindTranscript(state.transcript, 4), "revised input")
    emit(event(0, "rewound", "4", { agent_id: "manager" }))
    expect(workerThoughts()[0]).toMatchObject({ text: "first", seq: 2 })
    expect(state.transcript.agents.manager.blocks).toEqual([expect.objectContaining({ id: "pending-edit", text: "revised input" })])
    expect(state.transcript.running).toBe(true)
    expect(state.transcript.agents.manager.status).toBe("running")
    expect(state.transcript.lastSeq).toBe(5)
  })

  for (const fromAgentLog of [true, false]) {
    it(`discards an in-flight ${fromAgentLog ? "agent" : "older page"} response with deleted rows beyond the local rewind cursor`, async () => {
      state.transcript = applyTail("th_1", [event(2, "reasoning", "retained"), event(5, "reasoning", "discarded")])
      let release!: (page: { events: SwarmEvent[]; has_more: boolean }) => void
      const response = new Promise<{ events: SwarmEvent[]; has_more: boolean }>((resolve) => { release = resolve })
      const update = (patch: Partial<typeof state>) => { state = { ...state, ...patch } }
      fake.agentLog.mockReturnValueOnce(response)
      fake.threadLog.mockReturnValueOnce(response)
      const request = fromAgentLog
        ? loadAgentHistory(() => state, update, String, "worker-1")
        : loadOlderHistory(() => state, update, String)
      emit(event(0, "rewound", "4", { agent_id: "manager" }))
      emit(event(7, "user_message", "replacement", { agent_id: "manager", turn_id: "tn_2" }))
      release({ events: [event(6, "reasoning", "unseen deleted checkpoint")], has_more: false })
      await request
      expect(workerThoughts().map((b) => b.text)).toEqual(["retained"])
      expect(state.transcript.agents.manager.blocks).toContainEqual(expect.objectContaining({ seq: 7, text: "replacement" }))
      expect(state.historyLoading).toBe(false)
    })

    it(`keeps a stale ${fromAgentLog ? "agent" : "older page"} response from resurrecting a rewound checkpoint`, () => {
      const first = event(2, "reasoning", "first", { message_id: "output-a" })
      const later = event(5, "reasoning", "first extended", { message_id: "output-a" })
      state.transcript = applyTail("th_1", [first, later])
      emit(event(0, "rewound", "4", { agent_id: "manager" }))
      state.transcript = fromAgentLog ? applyAgentLog("th_1", [later])! : prependOlder("th_1", [later])!
      expect(workerThoughts()).toEqual([expect.objectContaining({ text: "first", seq: 2 })])
    })

    it(`preserves the pending edit and rewind cursor across a late ${fromAgentLog ? "agent" : "older page"} response`, () => {
      state.transcript = applyTail("th_1", [event(2, "reasoning", "first"), event(5, "reasoning", "later")])
      state.transcript = placePendingEdit(state.transcript, "revised input")
      emit(event(0, "rewound", "4", { agent_id: "manager" }))
      state.transcript = fromAgentLog
        ? applyAgentLog("th_1", [], state.transcript)!
        : prependOlder("th_1", [], state.transcript)!
      expect(state.transcript.agents.manager.blocks).toContainEqual(expect.objectContaining({ id: "pending-edit", text: "revised input" }))
      expect(state.transcript.lastSeq).toBe(5)
      expect(state.transcript.rewindCut).toEqual({ from: 4, through: 5, live: true })
      expect(state.transcript.running).toBe(true)
    })
  }
})

describe("stored rewind markers", () => {
  const manager = (seq: number, kind: SwarmEvent["kind"], text: string, turn = "tn_1") =>
    event(seq, kind, text, { agent_id: "manager", turn_id: turn })
  const managerSeqs = () => state.transcript.agents.manager.blocks.map((b) => b.seq)

  // A page already holds the truncated log, so its marker has nothing left
  // to cut; re-applying it would sweep worker statuses and hide live tokens.
  it("leaves a page that contains its marker as stored", () => {
    state.transcript = applyTail("th_1", [manager(1, "user_message", "kept"), manager(6, "rewound", "2", ""), manager(7, "user_message", "replacement", "tn_2")])
    expect(managerSeqs()).toEqual([1, 7])
    expect(state.transcript.rewindCut).toBeUndefined()
    expect(state.transcript.lastSeq).toBe(7)
  })

  // The stream or a reconnect can repeat a marker after a newer cut moved
  // the remembered range past it. The repeat must not cut a second time.
  it("does not cut again when an older marker is repeated after a newer cut", () => {
    state.transcript = applyTail("th_1", [manager(1, "user_message", "first"), manager(2, "agent_message", "answer")])
    emit(manager(3, "rewound", "2", ""))
    emit(manager(4, "user_message", "second", "tn_2"))
    emit(manager(5, "rewound", "4", ""))
    emit(manager(6, "user_message", "third", "tn_3"))
    emit(manager(3, "rewound", "2", ""))
    expect(managerSeqs()).toEqual([1, 6])
  })

  it("applies a marker that arrives through catch-up", () => {
    state.transcript = applyTail("th_1", [manager(1, "user_message", "first"), manager(2, "user_message", "removed", "tn_2")])
    emit(manager(9, "rewound", "2", ""))
    emit(manager(10, "user_message", "replacement", "tn_3"))
    expect(managerSeqs()).toEqual([1, 10])
  })
})

describe("live output during a history request", () => {
  it("does not restore live text from the discarded side of a rewind", () => {
    state.transcript = applyTail("th_1", [
      event(2, "reasoning", "retained", { message_id: "output-a" }),
      event(5, "user_message", "discarded input", { agent_id: "manager" }),
    ])
    emit(event(0, "reasoning_delta", "retained plus discarded evidence", { message_id: "output-a" }))
    emit(event(0, "rewound", "4", { agent_id: "manager" }))
    expect(workerThoughts()).toEqual([expect.objectContaining({ text: "retained", streaming: false })])
  })

  for (const fromAgentLog of [true, false]) {
    for (const kind of ["reasoning", "answer"] as const) {
      it(`keeps a live ${kind} with an id but no checkpoint when ${fromAgentLog ? "agent" : "page"} history arrives`, async () => {
        state.transcript = applyTail("th_1", [event(5, "user_message", "continue", { agent_id: "manager" })], [event(1, "spawned", "worker")])
        let release!: (page: { events: SwarmEvent[]; has_more: boolean }) => void
        const response = new Promise<{ events: SwarmEvent[]; has_more: boolean }>((resolve) => { release = resolve })
        const update = (patch: Partial<typeof state>) => { state = { ...state, ...patch } }
        const read = fromAgentLog ? fake.agentLog : fake.threadLog
        read.mockReturnValueOnce(response)
        const request = fromAgentLog
          ? loadAgentHistory(() => state, update, String, "worker-1")
          : loadOlderHistory(() => state, update, String)
        emit(event(0, kind === "reasoning" ? "reasoning_delta" : "delta", "first live evidence", { message_id: "output-new" }))
        release({ events: [], has_more: false })
        await request
        expect(state.transcript.agents["worker-1"].blocks).toEqual([
          expect.objectContaining({ text: "first live evidence", messageId: "output-new", streaming: true }),
        ])
      })

      it(`keeps longer live ${kind} when an older ${fromAgentLog ? "agent" : "page"} snapshot returns`, async () => {
        const saved = event(2, kind === "reasoning" ? "reasoning" : "agent_message", "old checkpoint", { message_id: "output-a" })
        state.transcript = applyTail("th_1", [event(5, "user_message", "continue", { agent_id: "manager" })])
        state.transcript = applyAgentLog("th_1", [saved])!
        let release!: (page: { events: SwarmEvent[]; has_more: boolean }) => void
        const response = new Promise<{ events: SwarmEvent[]; has_more: boolean }>((resolve) => { release = resolve })
        const update = (patch: Partial<typeof state>) => { state = { ...state, ...patch } }
        ;(fromAgentLog ? fake.agentLog : fake.threadLog).mockReturnValueOnce(response)
        const request = fromAgentLog
          ? loadAgentHistory(() => state, update, String, "worker-1")
          : loadOlderHistory(() => state, update, String)
        emit(event(0, kind === "reasoning" ? "reasoning_delta" : "delta", "old checkpoint with currently streamed evidence", { message_id: "output-a" }))
        release({ events: [saved], has_more: false })
        await request
        expect(state.transcript.agents["worker-1"].blocks).toEqual([
          expect.objectContaining({ text: "old checkpoint with currently streamed evidence", messageId: "output-a", streaming: true }),
        ])
      })
    }

    for (const terminal of ["finished", "cleanup"] as const) {
      it(`keeps ${terminal} authoritative over live output when the ${fromAgentLog ? "agent" : "page"} request finishes`, async () => {
        const saved = event(2, "reasoning", "old checkpoint", { message_id: "output-a" })
        state.transcript = applyTail("th_1", [event(5, "user_message", "continue", { agent_id: "manager" })])
        state.transcript = applyAgentLog("th_1", [saved])!
        let release!: (page: { events: SwarmEvent[]; has_more: boolean }) => void
        const response = new Promise<{ events: SwarmEvent[]; has_more: boolean }>((resolve) => { release = resolve })
        const update = (patch: Partial<typeof state>) => { state = { ...state, ...patch } }
        ;(fromAgentLog ? fake.agentLog : fake.threadLog).mockReturnValueOnce(response)
        const request = fromAgentLog
          ? loadAgentHistory(() => state, update, String, "worker-1")
          : loadOlderHistory(() => state, update, String)
        emit(event(0, "reasoning_delta", "old checkpoint with live evidence", { message_id: "output-a" }))
        emit(event(6, terminal, "finished", { agent_id: terminal === "cleanup" ? "manager" : "worker-1" }))
        release({ events: [saved], has_more: false })
        await request
        expect(state.transcript.agents["worker-1"].status).toBe(terminal === "finished" ? "done" : "cancelled")
        expect(state.transcript.agents["worker-1"].blocks.every((b) => !b.streaming)).toBe(true)
      })
    }

    it(`does not reopen an older output when ${fromAgentLog ? "agent" : "page"} history contains the next output`, () => {
      const saved = event(2, "reasoning", "old checkpoint", { message_id: "output-a" })
      state.transcript = applyTail("th_1", [event(5, "user_message", "continue", { agent_id: "manager" })])
      state.transcript = applyAgentLog("th_1", [saved])!
      emit(event(0, "reasoning_delta", "old checkpoint with late evidence", { message_id: "output-a" }))
      const next = event(3, "agent_message", "next output", { message_id: "output-b" })
      state.transcript = fromAgentLog
        ? applyAgentLog("th_1", [saved, next], state.transcript)!
        : prependOlder("th_1", [saved, next], state.transcript)!
      expect(state.transcript.agents["worker-1"].blocks).toEqual([
        expect.objectContaining({ text: "old checkpoint", streaming: false }),
        expect.objectContaining({ text: "next output", streaming: false }),
      ])
    })

    it(`keeps a newer ${fromAgentLog ? "agent" : "page"} output authoritative when the live output had no checkpoint`, () => {
      state.transcript = applyTail("th_1", [event(5, "user_message", "continue", { agent_id: "manager" })], [event(1, "spawned", "worker")])
      emit(event(0, "reasoning_delta", "first live evidence", { message_id: "output-a" }))
      const next = event(6, "agent_message", "newer output", { message_id: "output-b" })
      state.transcript = fromAgentLog
        ? applyAgentLog("th_1", [next], state.transcript)!
        : prependOlder("th_1", [next], state.transcript)!
      expect(state.transcript.agents["worker-1"].blocks).toEqual([
        expect.objectContaining({ text: "newer output", messageId: "output-b", streaming: false }),
      ])
    })
  }
})

for (const historyFirst of [true, false]) {
  it(`retains the interruption and recovered state when ${historyFirst ? "agent history" : "SSE"} arrives first`, () => {
    const old = event(2, "reasoning", "unfinished", { message_id: "old" })
    const stop = event(3, "generation_interrupted", JSON.stringify({ reason: "engine_restart", recovery: "pending" }), { message_id: "old" })
    const resume = event(4, "generation_recovered", JSON.stringify({ reason: "engine_restart", recovery: "recovered" }), { message_id: "old" })
    state.transcript = applyTail("th_1", [event(1, "spawned", "", { role: "worker" })])
    if (historyFirst) state.transcript = applyAgentLog("th_1", [old, stop, resume])!
    for (const row of [old, stop, resume]) emit(row)
    emit(event(0, "reasoning_delta", "resumed output", { message_id: "new" }))
    if (!historyFirst) state.transcript = applyAgentLog("th_1", [old, stop, resume], state.transcript)!
    state.transcript = prependOlder("th_1", [old, stop], state.transcript)!
    const worker = state.transcript.agents["worker-1"]
    expect(worker.status).toBe("running")
    expect(worker.blocks.filter((b) => b.kind === "reasoning").map((b) => [b.messageId, b.streaming])).toEqual([["old", false], ["new", true]])
    expect(worker.blocks.filter((b) => b.generation)).toHaveLength(1)
    expect(worker.blocks.find((b) => b.generation)?.generation?.recovery).toBe("recovered")
  })
}
