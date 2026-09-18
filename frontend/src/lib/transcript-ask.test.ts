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
