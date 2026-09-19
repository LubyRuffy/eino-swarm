import { describe, expect, it } from "vitest"

import { isMathFence } from "./math-fence"

describe("isMathFence", () => {
  it("treats math dialects as formulas, not source", () => {
    expect(isMathFence("math")).toBe(true)
    expect(isMathFence("LaTeX")).toBe(true)
    expect(isMathFence("tex")).toBe(true)
    expect(isMathFence("katex")).toBe(true)
  })

  it("leaves ordinary languages as code", () => {
    expect(isMathFence("go")).toBe(false)
    expect(isMathFence("chart")).toBe(false)
    expect(isMathFence("")).toBe(false)
  })
})
