import { describe, expect, it } from "vitest"

import { ASK_OTHER_ID, ASK_TOOL, askCardFromEvent, parseAskResult, parseAskToolArgs } from "./transcript-ask"
import type { SwarmEvent } from "./types"

describe("parseAskToolArgs", () => {
  it("keeps the model's ids and injects host Other", () => {
    const qs = parseAskToolArgs(
      JSON.stringify({
        questions: [
          {
            id: "approach",
            header: "Tradeoff",
            prompt: "Which approach should this work take?",
            options: [
              { id: "safer", label: "Prefer the safer path" },
              { id: "faster", label: "Prefer the faster path" },
            ],
          },
        ],
      }),
    )
    expect(qs).toHaveLength(1)
    expect(qs?.[0]?.id).toBe("approach")
    expect(qs?.[0]?.options.map((o) => o.id)).toEqual(["safer", "faster", ASK_OTHER_ID])
  })

  it("matches the host's normalized question id for a hyphenated model id", () => {
    const qs = parseAskToolArgs(JSON.stringify({ questions: [{
      id: "test-window", prompt: "When?", options: [
        { id: "now", label: "Now" }, { id: "later", label: "Later" },
      ],
    }] }))
    expect(qs?.[0]?.id).toBe("test_window")
  })

  it("drops a non-space separator as the host does", () => {
    const qs = parseAskToolArgs(JSON.stringify({ questions: [{
      id: "test\uFEFF-window", prompt: "When?", options: [
        { id: "now", label: "Now" }, { id: "later", label: "Later" },
      ],
    }] }))
    expect(qs?.[0]?.id).toBe("test_window")
  })

  it("keeps a question whose options contain labels without ids", () => {
    const qs = parseAskToolArgs(JSON.stringify({ questions: [{
      id: "timing", prompt: "When should the review happen?", options: [
        { label: "Review now" }, { label: "Review later" },
      ],
    }] }))
    expect(qs?.[0]?.options.map((o) => o.id)).toEqual(["review_now", "review_later", ASK_OTHER_ID])
  })

  it("shows every option accepted by the host when model ids are absent or unusable", () => {
    const qs = parseAskToolArgs(JSON.stringify({ questions: [{
      id: "review-window", prompt: "Which review window?", options: [
        { label: "  现在审阅（推荐）  ", description: "Keep the review brief" },
        { id: "  !!!  ", label: "Review later" },
        { id: "  Explicit-ID  ", label: "Another window" },
      ],
    }] }))
    expect(qs).toEqual([{
      id: "review_window", prompt: "Which review window?", header: undefined,
      options: [
        { id: "现在审阅推荐", label: "现在审阅（推荐）", description: "Keep the review brief" },
        { id: "review_later", label: "Review later", description: undefined },
        { id: "explicit_id", label: "Another window", description: undefined },
        { id: ASK_OTHER_ID, label: "Other" },
      ],
    }])
  })

  it("rejects junk so a malformed tool_call does not paint a card", () => {
    expect(parseAskToolArgs("")).toBeNull()
    expect(parseAskToolArgs("{}")).toBeNull()
    expect(parseAskToolArgs(`{"questions":[{"id":"a","prompt":"p","options":[{"id":"x","label":"X"}]}]}`)).toBeNull()
  })
})

describe("parseAskResult", () => {
  it("reads the first label per question id", () => {
    expect(
      parseAskResult(`{"answers":{"approach":{"answers":["Prefer the safer path"]}}}`),
    ).toEqual({ approach: "Prefer the safer path" })
    expect(parseAskResult("not json")).toEqual({})
  })
})

describe("askCardFromEvent", () => {
  it("only special-cases ask_user", () => {
    const ev = {
      kind: "tool_call",
      tool_call_id: "tc_1",
      text: `${ASK_TOOL}({})`,
    } as SwarmEvent
    expect(askCardFromEvent(ev, "read", "{}")).toBeNull()
    const card = askCardFromEvent(
      ev,
      ASK_TOOL,
      `{"questions":[{"id":"approach","prompt":"Which approach?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}`,
    )
    expect(card?.callId).toBe("tc_1")
    expect(card?.pending).toBe(true)
    expect(card?.questions).toHaveLength(1)
  })
})
