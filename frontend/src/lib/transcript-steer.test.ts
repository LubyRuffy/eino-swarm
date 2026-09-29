import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  splitQueuedSteers,
  type TranscriptState,
} from "./transcript"
import { parseSteerRetractSeq, parseSteerRevision } from "./transcript-steer"
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

describe("parseSteerRevision", () => {
  it("requires a seq and a text field", () => {
    expect(parseSteerRevision(`{"seq":4,"text":"next"}`)).toEqual({ seq: 4, text: "next" })
    expect(parseSteerRevision(`{"seq":4,"text":""}`)).toEqual({ seq: 4, text: "" })
    expect(parseSteerRevision(`{"seq":4}`)).toBeUndefined()
    expect(parseSteerRevision("nope")).toBeUndefined()
    expect(parseSteerRevision("")).toBeUndefined()
  })
})

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

  it("replaces an unread caption and keeps a later revision", () => {
    seq = 0
    const state = fold([
      ev({ kind: "user_message", text: "look into this", seq: 1 }),
      ev({ kind: "steer", text: "same words", seq: 3 }),
      ev({ kind: "steer", text: "same words", seq: 4 }),
      ev({ kind: "steer_revised", text: `{"seq":3,"text":"first edited"}`, seq: 5 }),
      ev({ kind: "steer_revised", text: `{"seq":3,"text":"first final"}`, seq: 6 }),
    ])
    const texts = manager(state)
      .blocks.filter((b) => b.kind === "steer")
      .map((b) => ({ seq: b.seq, text: b.text }))
    expect(texts).toEqual([
      { seq: 3, text: "first final" },
      { seq: 4, text: "same words" },
    ])
    const late = fold(
      [ev({ kind: "steer", text: "original", seq: 8 })],
      fold([ev({ kind: "steer_revised", text: `{"seq":8,"text":"from the earlier page"}`, seq: 9 })]),
    )
    expect(manager(late).blocks.filter((b) => b.kind === "steer").map((b) => b.text)).toEqual([
      "from the earlier page",
    ])
  })

  it("a rewind drops revisions from the cut onward", () => {
    seq = 0
    const state = fold([
      ev({ kind: "user_message", text: "keep", seq: 1 }),
      ev({ kind: "steer", text: "early", seq: 2 }),
      ev({ kind: "steer_revised", text: `{"seq":2,"text":"early edited"}`, seq: 3 }),
      ev({ kind: "user_message", text: "cut", seq: 4 }),
      ev({ kind: "steer", text: "late", seq: 5 }),
      ev({ kind: "steer_revised", text: `{"seq":5,"text":"late edited"}`, seq: 6 }),
    ])
    const after = fold([ev({ kind: "rewound", seq: 0, text: "4" })], state)
    expect(after.steerRevisions).toEqual({ 2: "early edited" })
    expect(manager(after).blocks.filter((b) => b.kind === "steer").map((b) => b.text)).toEqual([
      "early edited",
    ])
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
