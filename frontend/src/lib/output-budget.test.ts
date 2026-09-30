import { describe, expect, it } from "vitest"

import {
  budgetRetryMessages,
  isOutputBudgetError,
  OUTPUT_BUDGET_ERROR,
} from "./output-budget"

describe("isOutputBudgetError", () => {
  it("matches the stored sentence and nothing nearby", () => {
    // signal.go errOutputBudget. A drifted constant still matches itself
    // and the settings link never appears on a stored turn.
    expect(OUTPUT_BUDGET_ERROR).toBe(
      "the model used its whole output budget before it produced an answer",
    )
    expect(isOutputBudgetError(OUTPUT_BUDGET_ERROR)).toBe(true)
    expect(isOutputBudgetError(`  ${OUTPUT_BUDGET_ERROR}  `)).toBe(true)
    expect(isOutputBudgetError("the endpoint refused the connection")).toBe(false)
    expect(isOutputBudgetError(`${OUTPUT_BUDGET_ERROR} — raise the cap`)).toBe(false)
    expect(isOutputBudgetError("")).toBe(false)
    expect(isOutputBudgetError(undefined)).toBe(false)
  })
})

describe("budgetRetryMessages", () => {
  it("keeps the user message of a turn that still has something to resend", () => {
    const hits = budgetRetryMessages([
      { kind: "user", text: "first", seq: 2, turnId: "t1" },
      { kind: "user", text: "   ", seq: 8, turnId: "t2" },
      { kind: "user", text: "", seq: 9, turnId: "t3", images: [{ id: "img" }] },
      { kind: "steer", text: "later", seq: 4, turnId: "t1" },
      { kind: "user", text: "no seq", seq: 0, turnId: "t4" },
    ])
    expect(hits.get("t1")).toEqual({ text: "first", seq: 2 })
    expect(hits.get("t2")).toBeUndefined()
    expect(hits.get("t3")).toEqual({ text: "", seq: 9 })
    expect(hits.has("t4")).toBe(false)
  })
})
