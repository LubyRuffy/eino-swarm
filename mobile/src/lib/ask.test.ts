import { describe, expect, it } from "vitest"

import { parseAskToolArgs, splitToolCall, structuredAnswers } from "./ask"

describe("ask", () => {
  it("parses questions and injects Other", () => {
    const qs = parseAskToolArgs(
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}',
    )
    expect(qs).toHaveLength(1)
    expect(qs?.[0].options.map((o) => o.id)).toEqual(["a", "b", "other"])
    expect(splitToolCall("ask_user({x:1})")).toEqual({
      name: "ask_user",
      args: "{x:1}",
    })
  })

  it("builds structured answers from picks", () => {
    const qs = parseAskToolArgs(
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}',
    )!
    expect(structuredAnswers(qs, { q1: "a" }, {})).toEqual({
      q1: { answers: ["A"] },
    })
    expect(structuredAnswers(qs, { q1: "other" }, { q1: "typed" })).toEqual({
      q1: { answers: ["typed"] },
    })
    expect(structuredAnswers(qs, {}, {})).toBeNull()
  })
})
