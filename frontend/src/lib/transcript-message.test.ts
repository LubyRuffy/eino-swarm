import { describe, expect, it } from "vitest"
import { collapseLiveEvents, emptyTranscript, reduceEvent, type TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

function event(kind: string, seq: number, text: string, messageId?: string, turn = "check-1", agent = "reader-1"): SwarmEvent {
  return {
    thread_id: "thread", turn_id: turn, agent_id: agent, kind, seq, text,
    created_at: new Date(1700000000000 + seq * 1000).toISOString(),
    ...(messageId ? { message_id: messageId } : {}),
  }
}

function fold(events: SwarmEvent[]): TranscriptState {
  return events.reduce((state, ev) => reduceEvent(state, ev), emptyTranscript())
}

describe("model output identity", () => {
  // A manager check can end while the same worker is still inside one model
  // request. Its durable snapshots must not become independent thoughts.
  it("updates one worker thought across manager checks and full replay", () => {
    const rows = [
      event("reasoning_delta", 0, "Inspect", "output-a"),
      event("reasoning", 2, "Inspect the input", "output-a"),
      event("done", 3, "Check complete", undefined, "check-1", "manager"),
      event("user_message", 4, "Continue the check", undefined, "check-2", "manager"),
      event("reasoning_delta", 0, "Inspect the input and evidence", "output-a", "check-2"),
      event("reasoning", 5, "Inspect the input and evidence", "output-a", "check-2"),
      event("reasoning", 7, "Inspect the input and evidence again", "output-a", "check-3"),
    ]
    for (const state of [fold(rows), fold(rows.filter((row) => row.seq > 0))]) {
      const thoughts = state.agents["reader-1"].blocks.filter((b) => b.kind === "reasoning")
      expect(thoughts).toHaveLength(1)
      expect(thoughts[0]).toMatchObject({ text: "Inspect the input and evidence again", streaming: false })
    }
  })

  it("keeps a checkpoint identity while its worker continues streaming", () => {
    const saved = event("reasoning", 2, "Inspect", "output-a")
    const state = fold([saved, event("reasoning_delta", 0, "Inspect more", "output-a", "check-2")])
    expect(state.agents["reader-1"].blocks).toHaveLength(1)
    expect(state.agents["reader-1"].blocks[0]).toMatchObject({ text: "Inspect more", streaming: true, seq: 2 })
  })

  it("preserves independent model outputs with identical text even if a prior stream is open", () => {
    const state = fold([
      event("reasoning_delta", 0, "Inspect", "output-a"),
      event("reasoning_delta", 0, "Inspect", "output-b"),
      event("reasoning", 2, "Inspect", "output-b"),
    ])
    expect(state.agents["reader-1"].blocks.map((b) => [b.text, Boolean(b.streaming)]))
      .toEqual([["Inspect", false], ["Inspect", false]])
  })

  it("does not let a stale snapshot reopen or shorten a stored thought", () => {
    const state = fold([
      event("reasoning", 8, "Inspect the evidence", "output-a"),
      event("reasoning_delta", 0, "Inspect", "output-a"),
      event("reasoning", 3, "Inspect the", "output-a"),
      event("reasoning_delta", 0, "Inspect the evidence", "output-a"),
    ])
    expect(state.agents["reader-1"].blocks).toHaveLength(1)
    expect(state.agents["reader-1"].blocks[0]).toMatchObject({ text: "Inspect the evidence", seq: 8, streaming: false })
  })

  it("keeps reasoning and commentary separate while preserving tool input and result", () => {
    const call = { ...event("tool_call", 4, 'read_file({"path":"input.txt"})', "output-a"), tool_call_id: "read-1" }
    const result = { ...event("tool_result", 5, "Complete evidence"), tool_call_id: "read-1" }
    const state = fold([
      event("reasoning", 1, "Inspect", "output-a"),
      event("agent_message", 2, "Reading", "output-a"),
      event("reasoning", 3, "Inspect the input", "output-a", "check-2"), call, result,
      event("reasoning_delta", 0, "Inspect the input", "output-a", "check-2"),
      event("reasoning", 6, "Inspect the input", "output-b", "check-2"),
    ])
    const blocks = state.agents["reader-1"].blocks
    expect(blocks.map((b) => b.kind)).toEqual(["reasoning", "answer", "tool", "reasoning"])
    expect(blocks[0].text).toBe("Inspect the input")
    expect(blocks[2].tool).toMatchObject({ name: "read_file", args: '{"path":"input.txt"}', result: "Complete evidence", pending: false })
  })

  it("keeps each worker and each output kind independent", () => {
    const state = fold([
      event("reasoning", 1, "First", "output-a"),
      event("agent_message", 2, "Answer", "output-a"),
      event("reasoning", 3, "Second", "output-a", "check-1", "reader-2"),
      event("agent_message", 4, "Answer continued", "output-a", "check-2"),
    ])
    expect(state.agents["reader-1"].blocks.map((b) => b.text)).toEqual(["First", "Answer continued"])
    expect(state.agents["reader-2"].blocks.map((b) => b.text)).toEqual(["Second"])
  })

  it("does not infer identity for old snapshots without a message id", () => {
    const state = fold([
      event("reasoning", 1, "Inspect"),
      event("reasoning", 2, "Inspect more", undefined, "check-2"),
    ])
    expect(state.agents["reader-1"].blocks).toHaveLength(2)
  })

  it("coalesces only deltas belonging to the same model output", () => {
    const rows = [
      event("reasoning_delta", 0, "First", "output-a"),
      event("reasoning_delta", 0, "First continued", "output-a"),
      event("reasoning_delta", 0, "Second", "output-b"),
      event("delta", 0, "Answer", "output-b"),
    ]
    expect(collapseLiveEvents(rows).map((ev) => ev.text)).toEqual(["First continued", "Second", "Answer"])
  })

  it("does not reopen an earlier output when a delayed delta follows a newer one", () => {
    const state = fold([
      event("reasoning", 1, "First", "output-a"),
      event("reasoning_delta", 0, "Second", "output-b"),
      event("reasoning_delta", 0, "First extended", "output-a"),
    ])
    expect(state.agents["reader-1"].blocks.map((b) => [b.text, Boolean(b.streaming)]))
      .toEqual([["First", false], ["Second", true]])
  })
})
