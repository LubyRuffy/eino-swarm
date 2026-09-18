import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  splitQueuedSteers,
  type TranscriptState,
} from "./transcript"
import { parseSteerRetractSeq } from "./transcript-steer"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  const stored = partial.kind !== "delta" && partial.kind !== "reasoning_delta"
  return {
    thread_id: "th_1",
    turn_id: partial.turn_id ?? "tn_1",
    seq: partial.seq ?? (stored ? ++seq : 0),
    kind: partial.kind,
    agent_id: partial.agent_id ?? MANAGER_ID,
    role: partial.role,
    text: partial.text,
    tool_call_id: partial.tool_call_id,
    err: partial.err,
    images: partial.images,
    created_at: partial.created_at ?? new Date(1700000000000 + seq * 1000).toISOString(),
  }
}

function fold(events: SwarmEvent[], from?: TranscriptState) {
  return reduceEvents(from ?? emptyTranscript(), events)
}

function manager(state: TranscriptState) {
  return state.agents[MANAGER_ID]
}

describe("parseSteerRetractSeq", () => {
  it("reads JSON seq and ignores junk", () => {
    expect(parseSteerRetractSeq(`{"seq":12}`)).toBe(12)
    expect(parseSteerRetractSeq("7")).toBe(7)
    expect(parseSteerRetractSeq("0")).toBe(0)
    expect(parseSteerRetractSeq("-1")).toBe(0)
    expect(parseSteerRetractSeq("nope")).toBe(0)
    expect(parseSteerRetractSeq("")).toBe(0)
  })
})

describe("retracted steering", () => {
  it("drops the matching unread bubble and leaves a duplicate caption", () => {
    seq = 0
    const state = fold([
      ev({ kind: "user_message", text: "look into this", seq: 1 }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1", seq: 2 }),
      ev({ kind: "steer", text: "same words", seq: 3 }),
      ev({ kind: "steer", text: "same words", seq: 4 }),
      ev({ kind: "steer_retracted", text: `{"seq":3}`, seq: 5 }),
    ])
    const texts = manager(state)
      .blocks.filter((b) => b.kind === "steer")
      .map((b) => ({ seq: b.seq, text: b.text }))
    expect(texts).toEqual([{ seq: 4, text: "same words" }])
    expect(state.retractedSteers).toEqual([3])
    const { queued } = splitQueuedSteers(manager(state).blocks, true)
    expect(queued.map((b) => b.seq)).toEqual([4])
  })

  it("hides a steer that arrives after its retract (history paging)", () => {
    seq = 0
    const retracted = fold([ev({ kind: "steer_retracted", text: `{"seq":9}`, seq: 10 })])
    const state = fold([ev({ kind: "steer", text: "late page", seq: 9 })], retracted)
    expect(manager(state).blocks.some((b) => b.kind === "steer")).toBe(false)
    expect(state.retractedSteers).toEqual([9])
  })

  it("does not paint steer_preempted as a notice", () => {
    seq = 0
    const state = fold([
      ev({ kind: "user_message", text: "look into this", seq: 1 }),
      ev({ kind: "steer", text: "change course", seq: 2 }),
      ev({ kind: "steer_preempted", seq: 3 }),
    ])
    expect(manager(state).blocks.some((b) => b.kind === "notice")).toBe(false)
    expect(manager(state).blocks.some((b) => b.kind === "steer")).toBe(true)
    expect(state.lastSeq).toBe(3)
  })
})
