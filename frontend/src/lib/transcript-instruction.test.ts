import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  type TranscriptState,
} from "./transcript"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: "tn_1",
    seq: ++seq,
    agent_id: partial.agent_id ?? MANAGER_ID,
    created_at: new Date(1700000000000 + seq * 1000).toISOString(),
    ...partial,
  }
}

function fold(events: SwarmEvent[], from?: TranscriptState) {
  return reduceEvents(from ?? emptyTranscript(), events)
}

describe("worker instruction", () => {
  // The chrome needs the actual Instruction, not the role name. Older events
  // stored the role in text, so those must not light up a prompt viewer.
  it("records the worker instruction from a spawned event", () => {
    const prompt = "## Environment\n\ndo the assigned work"
    const state = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: prompt,
      }),
    ])
    expect(state.agents["worker-1"].instruction).toBe(prompt)
    const [spawn] = state.agents[MANAGER_ID].blocks.filter((b) => b.kind === "spawn")
    expect(spawn.text).toBe(prompt)
    expect(spawn.spawn).toEqual({ agentId: "worker-1", role: "worker" })
  })

  it("does not treat a legacy role-only spawned event as an instruction", () => {
    const state = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", text: "worker" }),
    ])
    expect(state.agents["worker-1"].instruction).toBeUndefined()
  })

  it("replaces the instruction when the same worker is resumed", () => {
    const state = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", text: "first prompt" }),
      ev({ kind: "finished", agent_id: "worker-1", text: "done" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", text: "second prompt" }),
    ])
    expect(state.agents["worker-1"].instruction).toBe("second prompt")
    expect(state.agents[MANAGER_ID].blocks.filter((b) => b.kind === "spawn")).toHaveLength(1)
  })
})
