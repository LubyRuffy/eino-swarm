import { describe, expect, it } from "vitest"
import { emptyTranscript, MANAGER_ID, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

function event(kind: string, seq: number, text?: string): SwarmEvent {
  return { kind, seq, text, thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID, created_at: "2026-10-19T01:00:00Z" }
}

describe("automatic restart handoff", () => {
  it("keeps a Stop acknowledgement separate from actual turn completion", () => {
    const state = reduceEvents(emptyTranscript(), [
      event("user_message", 1, "continue the task"),
      event("user_stop_requested", 2, "Stop requested by the user."),
    ])
    expect(state.turns[0].status).toBe("running")
    expect(state.agents[MANAGER_ID].blocks.at(-1)).toMatchObject({ kind: "notice", text: "Stop requested by the user." })
  })
  it("keeps raw recovery checkpoints out of the conversation and roster", () => {
    const state = reduceEvents(emptyTranscript(), [event("engine_restart_checkpoint", 1, '{"manager_history":["private checkpoint"]}')])
    expect(JSON.stringify(state)).not.toContain("private checkpoint")
    expect(state.agentOrder).toEqual([])
  })

  it("shows handoff and recovery without completing the task or its workers", () => {
    const state = reduceEvents(emptyTranscript(), [
      event("user_message", 1, "continue the task"),
      { ...event("spawned", 2, "assigned work"), agent_id: "worker-1", role: "worker" },
      { ...event("tool_call", 3, "exec({})"), tool_call_id: "call" },
      event("restart_preparing", 4, "Preparing task handoff before restarting."),
      event("restart_ready", 5, "Task handoff saved; waiting for the replacement engine."),
      event("restart_resumed", 6, "Saved task progress restored; continuing."),
    ])
    expect(state.running).toBe(true)
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.turns[0].status).toBe("running")
    const blocks = state.agents[MANAGER_ID].blocks
    expect(blocks.filter((block) => block.kind === "notice")).toHaveLength(3)
    expect(blocks.find((block) => block.kind === "tool")?.tool?.pending).toBe(false)
  })
})
