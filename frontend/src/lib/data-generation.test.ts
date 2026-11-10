import { describe, expect, it } from "vitest"

import { dataGenerationChanged } from "./data-generation"

describe("dataGenerationChanged", () => {
  it("treats the first observation as a baseline", () => {
    expect(dataGenerationChanged(undefined, 0)).toBe(false)
    expect(dataGenerationChanged(null, 4)).toBe(false)
    expect(dataGenerationChanged(1, undefined)).toBe(false)
  })

  it("notices a restore and ignores the same generation", () => {
    expect(dataGenerationChanged(0, 0)).toBe(false)
    expect(dataGenerationChanged(1, 2)).toBe(true)
  })
})
