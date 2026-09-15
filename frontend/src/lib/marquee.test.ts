import { describe, expect, it } from "vitest"

import { marqueeDuration, measureOverflow } from "./marquee"

describe("measureOverflow", () => {
  it("is true only when the line does not fit the slot", () => {
    expect(measureOverflow({ clientWidth: 80 }, { scrollWidth: 200 })).toBe(true)
    expect(measureOverflow({ clientWidth: 200 }, { scrollWidth: 80 })).toBe(false)
    expect(measureOverflow({ clientWidth: 80 }, { scrollWidth: 80 })).toBe(false)
  })
})

describe("marqueeDuration", () => {
  it("gives a longer loop to a longer line, inside a floor and a ceiling", () => {
    const short = Number.parseInt(marqueeDuration("wait"), 10)
    const long = Number.parseInt(marqueeDuration("x".repeat(400)), 10)
    expect(short).toBe(8)
    expect(long).toBe(40)
    expect(long).toBeGreaterThan(short)
  })
})
