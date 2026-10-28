import { describe, expect, it } from "vitest"

import { emptyTranscript, reduceEvents, rewindTranscript, type TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

// A worker log fetched over HTTP can already hold rows whose SSE copies are
// still queued behind their live-only broadcasts. Those broadcasts carry no
// seq, so identity is the only way to see that they are already superseded.

function row(seq: number, kind: string, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    thread_id: "th_1", turn_id: "tn_1", agent_id: "worker-1", role: "worker", seq, kind,
    created_at: new Date(1700000000000 + seq * 1000).toISOString(), ...extra,
  }
}

function fold(events: SwarmEvent[], from = emptyTranscript()): TranscriptState {
  return reduceEvents(from, events)
}

const finishedWorker = [
  row(1, "user_message", { agent_id: "manager", role: "user", text: "start" }),
  row(2, "spawned", { text: "task" }),
  row(3, "reasoning", { text: "think it through", message_id: "m_think" }),
  row(4, "tool_call", { text: 'exec({"n":1})', tool_call_id: "c1" }),
  row(5, "tool_result", { text: "ok complete", tool_call_id: "c1" }),
  row(6, "agent_message", { text: "result text", message_id: "m_answer" }),
  row(7, "finished", { text: "result text" }),
]

describe("late live broadcasts", () => {
  for (const late of [
    row(0, "reasoning_delta", { text: "think", message_id: "m_think" }),
    row(0, "delta", { text: "res", message_id: "m_answer" }),
  ]) {
    it(`keeps a finished worker done when a stale ${late.kind} arrives`, () => {
      const state = fold([late], fold(finishedWorker))
      expect(state.agents["worker-1"].status).toBe("done")
      expect(state.agents["worker-1"].blocks.some((b) => b.streaming)).toBe(false)
      // A turn failure afterwards must not repaint the finished worker as failed.
      const failed = fold([row(8, "error", { agent_id: "manager", role: undefined, err: "provider failed" })], state)
      expect(failed.agents["worker-1"].status).toBe("done")
    })
  }

  it("still revives a worker when the same output carries new text", () => {
    const state = fold([row(0, "delta", { text: "result text and more", message_id: "m_answer" })], fold(finishedWorker))
    expect(state.agents["worker-1"].status).toBe("running")
  })

  for (const late of [
    row(0, "tool_call_delta", { text: "exec(7)", tool_call_id: "c1" }),
    row(0, "tool_delta", { text: "ok", tool_call_id: "c1" }),
  ]) {
    it(`does not reopen a completed tool call when a stale ${late.kind} arrives`, () => {
      const state = fold([late], fold(finishedWorker))
      const tool = state.agents["worker-1"].blocks.find((b) => b.tool?.callId === "c1")?.tool
      expect(tool).toMatchObject({ pending: false, result: "ok complete" })
      expect(state.agents["worker-1"].status).toBe("done")
    })
  }

  it("keeps a tool row streamed live when a later message is edited", () => {
    const live = fold([
      ...finishedWorker.slice(0, 3),
      row(0, "tool_call_delta", { text: "exec(7)", tool_call_id: "c1" }),
      ...finishedWorker.slice(3),
      row(8, "done", { agent_id: "manager", role: undefined, text: "done" }),
      row(9, "user_message", { turn_id: "tn_2", agent_id: "manager", role: "user", text: "next" }),
    ])
    const tool = live.agents["worker-1"].blocks.find((b) => b.tool?.callId === "c1")
    expect(tool?.seq).toBe(4)
    expect(rewindTranscript(live, 9).agents["worker-1"].blocks.some((b) => b.tool?.callId === "c1")).toBe(true)
  })
})
