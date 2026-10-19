import { describe, expect, it } from "vitest"
import { emptyTranscript, lastFailedTurnError, liveWorkers, MANAGER_ID, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

function events(): SwarmEvent[] {
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    { kind: "spawned", agent_id: "worker-1", role: "worker" },
    { kind: "spawned", agent_id: "worker-2", role: "worker" },
    { kind: "spawned", agent_id: "worker-3", role: "worker" },
    { kind: "finished", agent_id: "worker-3", text: "Verified result." },
    { kind: "tool_call", agent_id: "worker-1", text: "exec({})", tool_call_id: "call" },
    { kind: "delta", agent_id: "worker-2", text: "Partial progress" },
  ].map((event, index) => ({
    thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID,
    created_at: "2026-10-19T01:00:00Z", seq: index + 1, ...event,
  }))
}

function terminal(kind: string, seq: number, err?: string): SwarmEvent {
  return { kind, seq, err, text: kind === "cleanup" ? "Stopped unfinished workers." : undefined,
    thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID, created_at: "2026-10-19T01:00:01Z" }
}

describe("stopping unfinished workers", () => {
  // Cleanup is evidence that execution stopped, never evidence that its task succeeded.
  it.each(["error", "done"])("keeps cleanup workers stopped after %s and persisted replay", (kind) => {
    const history = [...events(), terminal("cleanup", 8), terminal(kind, 9, kind === "error" ? "interrupted" : undefined)]
    const live = reduceEvents(reduceEvents(emptyTranscript(), history.slice(0, 7)), history.slice(7))
    const replayed = reduceEvents(emptyTranscript(), history)
    expect(replayed).toEqual(live)
    for (const state of [live, replayed]) {
      for (const id of ["worker-1", "worker-2"]) {
        expect(state.agents[id]).toMatchObject({ status: "cancelled", activity: "stopped" })
        expect(state.agents[id].blocks.every((block) => !block.streaming && !block.tool?.pending)).toBe(true)
      }
      expect(state.agents["worker-3"]).toMatchObject({ status: "done", result: "Verified result." })
      expect(liveWorkers(state)).toBe(0)
      expect(state.running).toBe(false)
    }
    const roster = history.reduce((state, event) => reduceEvent(state, event, "roster"), emptyTranscript())
    expect(roster.agents["worker-1"].status).toBe("cancelled")
    expect(roster.agents["worker-3"].status).toBe("done")
  })

  it("shows an interrupted turn as stopped rather than a model failure even without cleanup", () => {
    const state = reduceEvents(emptyTranscript(), [...events(), terminal("error", 8, "interrupted")])
    expect(state.turns[0].status).toBe("cancelled")
    expect(state.agents["worker-1"].status).toBe("cancelled")
    expect(state.agents["worker-3"].status).toBe("done")
    expect(lastFailedTurnError(state.turns)).toBe("")
    expect(state.agents[MANAGER_ID].blocks.at(-1)).toMatchObject({ kind: "notice", text: "interrupted" })
  })

  it("retains genuine model failures as failures", () => {
    const state = reduceEvents(emptyTranscript(), [...events(), terminal("error", 8, "provider unavailable")])
    expect(state.turns[0].status).toBe("error")
    expect(state.agents["worker-1"].status).toBe("failed")
    expect(state.agents["worker-3"].status).toBe("done")
    expect(lastFailedTurnError(state.turns)).toBe("provider unavailable")
  })
})
