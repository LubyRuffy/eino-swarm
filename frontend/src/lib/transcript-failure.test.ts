import { describe, expect, it } from "vitest"

import { emptyTranscript, MANAGER_ID, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

function history(): SwarmEvent[] {
  return [
    { kind: "user_message", text: "Perform the assigned work." },
    { kind: "spawned", agent_id: "worker-1", role: "worker" },
    { kind: "spawned", agent_id: "worker-2", role: "reviewer" },
    { kind: "finished", agent_id: "worker-2", err: "worker-specific failure" },
    { kind: "error", err: "the model service is unavailable" },
  ].map((event, index) => ({
    thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID,
    created_at: "2026-10-21T01:00:00Z", seq: index + 1, ...event,
  }))
}

describe("worker failure evidence", () => {
  // A failed parent can stop workers without their own finished events. That
  // reason must survive reload without pretending the worker failed independently.
  it.each(["full", "roster"] as const)("retains parent failure origin during %s replay", (mode) => {
    const events = history()
    const replay = events.reduce((state, ev) => reduceEvent(state, ev, mode), emptyTranscript())
    expect(replay.agents["worker-1"]).toMatchObject({
      status: "failed", error: "the model service is unavailable", errorSource: "turn",
    })
    expect(replay.agents["worker-2"]).toMatchObject({
      status: "failed", error: "worker-specific failure", errorSource: "agent",
    })
    const live = reduceEvents(reduceEvents(emptyTranscript(), events.slice(0, 4)), events.slice(4))
    expect(live.agents["worker-1"]).toEqual(replay.agents["worker-1"])
  })

  it.each(["spawned", "delta"])("clears the prior reason and its origin when the worker resumes via %s", (kind) => {
    const events = history()
    const state = reduceEvents(emptyTranscript(), events)
    const resumed = reduceEvent(state, { ...events[1], kind, text: "Continuing the assigned work.", seq: 6, turn_id: "next-turn" })
    expect(resumed.agents["worker-1"]).toMatchObject({ status: "running" })
    expect(resumed.agents["worker-1"].error).toBeUndefined()
    expect(resumed.agents["worker-1"].errorSource).toBeUndefined()
  })

  it("does not turn cleanup or a human stop into an independent failure", () => {
    const events = history().slice(0, 3)
    for (const terminal of [
      { kind: "cleanup", text: "Stopped unfinished workers." },
      { kind: "error", err: "interrupted" },
    ]) {
      const state = reduceEvents(emptyTranscript(), [...events, { ...events[0], ...terminal, seq: 4 }])
      expect(state.agents["worker-1"].status).toBe("cancelled")
      expect(state.agents["worker-1"].error).toBeUndefined()
      expect(state.agents["worker-1"].errorSource).toBeUndefined()
    }
  })
})
