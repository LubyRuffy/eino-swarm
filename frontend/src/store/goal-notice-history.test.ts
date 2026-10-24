import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { visibleGoalNotices } from "@/lib/goal-notice"
import { emptyTranscript, type TranscriptState } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { dropQueued, queueEvent, type StreamSnapshot } from "./app-stream"
import { applyAgentLog, applyTail, prependOlder, resetThreadHistory, rewindStoredTranscript } from "./thread-history"

let state: StreamSnapshot

function event(kind: SwarmEvent["kind"], seq: number, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    kind, seq, thread_id: "thread", turn_id: "previous", agent_id: "manager",
    created_at: `2026-01-01T00:00:${String(seq).padStart(2, "0")}.000Z`,
    ...extra,
  }
}

const resumed = () => event("goal_resumed", 2, { role: "human" })
const continued = () => event("goal_continued", 5, { turn_id: "next" })

function emit(ev: SwarmEvent) {
  queueEvent((patch) => {
    state = { ...state, ...(typeof patch === "function" ? patch(state) : patch) }
  }, () => state, "thread", ev)
}

function raw(transcript: TranscriptState = state.transcript) {
  return transcript.agents.manager.blocks
}

function shown() {
  return visibleGoalNotices(raw()).filter((block) => block.goalNoticeKind).map((block) => block.seq)
}

beforeEach(() => {
  dropQueued()
  resetThreadHistory()
  state = {
    activeId: "thread", transcript: emptyTranscript(), status: { running: false },
    threads: [], followups: [], schedules: [],
    refreshFiles: vi.fn(async () => {}), refreshThreads: vi.fn(async () => {}),
    refreshFollowups: vi.fn(async () => {}), refreshSchedules: vi.fn(async () => {}),
  }
})

afterEach(() => { dropQueued(); resetThreadHistory() })

describe("goal notice projection through stored history", () => {
  for (const outcome of [{ changed: true, notes: { add: 1 }, notify: "on" },
                         { changed: false, err: "review failed", notify: "on" }]) {
    const review = () => event("memory_review", 3, { turn_id: "older", agent_id: "memory-reviewer", text: JSON.stringify(outcome) })
    for (const historyFirst of [true, false]) {
      it(`retains the late ${outcome.err ? "failed" : "changed"} review across ${historyFirst ? "history-first" : "SSE-first"} replay with one resume`, () => {
        const events = [resumed(), review(), continued()]
        state.transcript = applyTail("thread", historyFirst ? events : [])
        for (const ev of events) emit(ev)
        if (!historyFirst) state.transcript = applyAgentLog("thread", events, state.transcript)!
        for (const ev of events) emit(ev)
        expect(shown()).toEqual([5])
        expect(raw().map((row) => row.seq)).toEqual([2, 3, 5])
        expect(visibleGoalNotices(raw()).map((row) => row.seq)).toEqual([3, 5])
        expect(raw().find((row) => row.review)?.quiet).toBe(false)
      })
    }
    it(`keeps the late ${outcome.err ? "failed" : "changed"} review while pairing a resume loaded on an older page`, () => {
      state.transcript = applyTail("thread", [continued()])
      state.transcript = prependOlder("thread", [review()], state.transcript)!
      state.transcript = prependOlder("thread", [resumed()], state.transcript)!
      expect(shown()).toEqual([5])
      expect(raw().map((row) => row.seq)).toEqual([2, 3, 5])
      expect(visibleGoalNotices(raw()).map((row) => row.seq)).toEqual([3, 5])
      for (const ev of [resumed(), review(), continued()]) emit(ev)
      expect(raw().map((row) => row.seq)).toEqual([2, 3, 5])
    })
  }

  for (const historyFirst of [true, false]) {
    it(`keeps one notice when ${historyFirst ? "history" : "live events"} arrive first and replay`, () => {
      const events = [resumed(), continued()]
      state.transcript = applyTail("thread", historyFirst ? events : [])
      for (const ev of events) emit(ev)
      if (!historyFirst) state.transcript = applyAgentLog("thread", events, state.transcript)!
      for (const ev of events) emit(ev)
      expect(shown()).toEqual([5])
      expect(raw().map((block) => block.seq)).toEqual([2, 5])
      expect(state.transcript.turns.find((turn) => turn.id === "next")?.status).toBe("running")
    })
  }

  it("merges across pages only once the older human resume has actually loaded", () => {
    state.transcript = applyTail("thread", [continued()])
    expect(shown()).toEqual([5])
    state.transcript = prependOlder("thread", [resumed()], state.transcript)!
    expect(shown()).toEqual([5])
    expect(raw().map((block) => block.seq)).toEqual([2, 5])
    emit(resumed())
    expect(raw().map((block) => block.seq)).toEqual([2, 5])
  })

  it("reconstructs source order when the older resume arrives later through the agent log", () => {
    state.transcript = applyTail("thread", [continued()])
    state.transcript = applyAgentLog("thread", [resumed()], state.transcript)!
    expect(shown()).toEqual([5])
    expect(raw().map((block) => block.seq)).toEqual([2, 5])
  })

  for (const kind of ["title", "session_memory"] as const) {
    it(`does not let actual ${kind} metadata or an already-running worker split a human resume`, () => {
      const worker = event("spawned", 1, { agent_id: "worker", role: "worker", text: "assigned work" })
      const thought = event("reasoning", 3, { agent_id: "worker", text: "working" })
      const metadata = event(kind, 4, { text: "conversation metadata" })
      state.transcript = applyTail("thread", [worker, resumed(), thought, metadata, continued()])
      expect(shown()).toEqual([5])
      expect(state.transcript.agents.worker.blocks[0].text).toBe("working")
      expect(raw().filter((block) => block.goalNoticeKind).map((block) => block.seq)).toEqual([2, 5])
      expect(raw().find((block) => block.seq === 4)?.quiet).toBe(true)
    })
  }

  it("restores an independent resume when a rewind removes its paired continuation", () => {
    state.transcript = applyTail("thread", [resumed(), continued()])
    expect(shown()).toEqual([5])
    state.transcript = rewindStoredTranscript("thread", 5, state.transcript)
    expect(shown()).toEqual([2])
    emit(continued())
    expect(shown()).toEqual([2])
    emit(event("goal_continued", 6, { turn_id: "replacement" }))
    expect(shown()).toEqual([6])
    expect(raw().map((block) => block.seq)).toEqual([2, 6])
  })

  it("keeps a later independent continuation with the same wording", () => {
    state.transcript = applyTail("thread", [resumed(), continued()])
    emit(event("goal_continued", 6, { turn_id: "later" }))
    expect(shown()).toEqual([5, 6])
    expect(raw().map((block) => block.seq)).toEqual([2, 5, 6])
  })
})
