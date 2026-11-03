import { describe, expect, it } from "vitest"

import { isOutputBudgetError, OUTPUT_BUDGET_ERROR } from "./output-budget"

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
