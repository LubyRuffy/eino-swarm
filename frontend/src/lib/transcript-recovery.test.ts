import { describe, expect, it } from "vitest"

import { emptyTranscript, liveWorkers, placePendingEdit, reduceEvent, reduceEvents, rewindTranscript } from "./transcript"
import type { SwarmEvent } from "./types"

function event(kind: string, seq: number, patch: Partial<SwarmEvent> = {}): SwarmEvent {
  return { kind, seq, thread_id: "th_recovery", turn_id: "tn_recovery", agent_id: "manager", created_at: `2026-10-22T00:00:0${seq}Z`, ...patch }
}

const diagnostic = event("desktop_recovery", 3, { agent_id: "desktop", text: '{"session_id":"session-1","phase":"recovered"}' })

describe("desktop recovery diagnostics", () => {
  it.each([false, true])("retains Trace evidence without changing a running=%s task or creating a worker", (running) => {
    const before = reduceEvents(emptyTranscript(), [
      event("user_message", 1, { text: "task" }),
      ...(!running ? [event("done", 2, { text: "finished" })] : []),
    ])
    const after = reduceEvent(before, diagnostic)
    expect(after.running).toBe(before.running)
    expect(after.turns).toEqual(before.turns)
    expect(after.agents).toEqual(before.agents)
    expect(after.agentOrder).toEqual(before.agentOrder)
    expect(liveWorkers(after)).toBe(0)
    expect(after.desktopRecoveryEvents).toEqual([diagnostic])
    expect(after.lastSeq).toBe(3)
  })

  it("deduplicates stored identity without discarding an older unseen diagnostic", () => {
    const newer = { ...diagnostic, seq: 9 }
    const after = reduceEvents(emptyTranscript(), [newer, diagnostic, diagnostic])
    expect(after.desktopRecoveryEvents).toEqual([newer, diagnostic])
    expect(after.lastSeq).toBe(9)
    expect(after.agents).toEqual({})
  })

  it("preserves diagnostic evidence across later events and pending input edits", () => {
    const after = reduceEvents(emptyTranscript(), [diagnostic, event("user_message", 4, { text: "task" })])
    expect(placePendingEdit(after, "updated task").desktopRecoveryEvents).toEqual([diagnostic])
  })

  it("removes rewound diagnostics and rejects their late replay", () => {
    const removed = { ...diagnostic, seq: 6 }
    const after = rewindTranscript(reduceEvents(emptyTranscript(), [diagnostic, removed]), 4)
    expect(after.desktopRecoveryEvents).toEqual([diagnostic])
    expect(reduceEvent(after, removed).desktopRecoveryEvents).toEqual([diagnostic])
  })

  it("keeps desktop evidence separate from one worker's interrupted and recovered generation", () => {
    const worker = { agent_id: "worker", message_id: "output-1" }
    const before = reduceEvents(emptyTranscript(), [
      event("user_message", 1, { text: "task" }),
      event("spawned", 2, worker),
      event("reasoning_delta", 0, { ...worker, text: "partial output" }),
      event("generation_interrupted", 3, { ...worker, text: '{"reason":"output_limit","recovery":"pending"}' }),
    ])
    const desktop = { ...diagnostic, seq: 4, agent_id: "" }
    const after = reduceEvents(before, [desktop,
      event("generation_recovered", 5, { ...worker, text: '{"reason":"output_limit","recovery":"recovered"}' }), desktop])
    expect(after.desktopRecoveryEvents).toEqual([desktop])
    expect(after.agents.worker.status).toBe("running")
    expect(after.agents.worker.blocks.filter((block) => block.generation)).toHaveLength(1)
    expect(after.agents.worker.blocks.find((block) => block.generation)?.generation).toMatchObject({ reason: "output_limit", recovery: "recovered" })
    expect(after.agents.worker.blocks.some((block) => block.text.includes("session-1"))).toBe(false)
    expect(after.agentOrder).toEqual(before.agentOrder)
    expect(liveWorkers(after)).toBe(1)
    expect(rewindTranscript(after, 4).desktopRecoveryEvents).toEqual([])
  })
})
