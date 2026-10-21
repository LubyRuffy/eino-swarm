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
  describe.each(["full", "roster"] as const)("late worker receipts in %s history", (mode) => {
    // A cancelled model call can finish after cleanup and the turn's terminal
    // event. Neither that receipt nor replay may rewrite the user's Stop.
    it.each([undefined, "swarm: agent worker-1 cancelled: context canceled"])("keeps Stop authoritative after late finished (%s)", (err) => {
      const stopped = [
        ...events(), terminal("user_stop_requested", 8),
        terminal("cleanup", 9), terminal("error", 10, "interrupted"),
      ]
      const late = {
        ...terminal("finished", 11, err), agent_id: "worker-1",
        text: err ? undefined : "Late result.", created_at: "2026-10-19T01:00:02Z",
      }
      const reduce = (history: SwarmEvent[]) => history.reduce((state, event) => reduceEvent(state, event, mode), emptyTranscript())
      const live = reduceEvent(reduce(stopped), late, mode)
      const replay = reduce([...stopped, late])
      expect(replay).toEqual(live)
      for (const state of [live, replay]) {
        expect(state.agents["worker-1"]).toMatchObject({
          status: "cancelled", activity: "stopped", endedAt: "2026-10-19T01:00:01Z",
        })
        expect(state.agents["worker-1"].error).toBeUndefined()
        expect(state.agents["worker-1"].errorSource).toBeUndefined()
        expect(liveWorkers(state)).toBe(0)
      }
    })

    it.each([undefined, "worker-specific failure"])("allows an explicitly resumed worker to finish (%s)", (err) => {
      const history = [
        ...events(), terminal("cleanup", 8), terminal("error", 9, "interrupted"),
        { ...terminal("spawned", 10), agent_id: "worker-1", turn_id: "next-turn", role: "worker" },
        { ...terminal("finished", 11, err), agent_id: "worker-1", turn_id: "next-turn", text: err ? undefined : "Verified after resume." },
      ]
      const state = history.reduce((state, event) => reduceEvent(state, event, mode), emptyTranscript())
      expect(state.agents["worker-1"]).toMatchObject({
        status: err ? "failed" : "done", activity: err ? "failed" : "done",
      })
      expect(state.agents["worker-1"].error).toBe(err)
      expect(state.agents["worker-1"].result).toBe(err ? undefined : "Verified after resume.")
    })

    it("preserves workers that already completed or failed before cleanup", () => {
      const history = [
        ...events(),
        { ...terminal("finished", 8, "worker-specific failure"), agent_id: "worker-2" },
        terminal("cleanup", 9), terminal("error", 10, "interrupted"),
      ]
      const state = history.reduce((state, event) => reduceEvent(state, event, mode), emptyTranscript())
      expect(state.agents["worker-1"].status).toBe("cancelled")
      expect(state.agents["worker-2"]).toMatchObject({ status: "failed", error: "worker-specific failure" })
      expect(state.agents["worker-3"]).toMatchObject({ status: "done", result: "Verified result." })
    })
  })

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
