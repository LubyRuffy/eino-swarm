import { describe, expect, it } from "vitest"

import { rosterGenerationReset } from "./roster-generation"

describe("rosterGenerationReset", () => {
  it("keeps the first observation and resets only after a change", () => {
    expect(rosterGenerationReset(null, undefined)).toEqual({ seen: null, reset: false })
    expect(rosterGenerationReset(null, 0)).toEqual({ seen: 0, reset: false })
    expect(rosterGenerationReset(0, 0)).toEqual({ seen: 0, reset: false })
    expect(rosterGenerationReset(0, 1)).toEqual({ seen: 1, reset: true })
  })
})
