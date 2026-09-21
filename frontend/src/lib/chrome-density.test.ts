import { describe, expect, it } from "vitest"

import { chromeListDensity, writeChromeListDensity } from "./chrome-density"

describe("chromeListDensity", () => {
  it("grows directory rows with chrome size, not with a fixed cell", () => {
    expect(chromeListDensity(12).rowHeight).toBe("26px")
    expect(chromeListDensity(13).rowHeight).toBe("28px")
    expect(chromeListDensity(16).rowHeight).toBe("34px")
    expect(Number.parseInt(chromeListDensity(12).rowHeight, 10)).toBeLessThan(
      Number.parseInt(chromeListDensity(13).rowHeight, 10),
    )
    expect(Number.parseInt(chromeListDensity(13).sectionGap, 10)).toBeLessThan(
      Number.parseInt(chromeListDensity(16).sectionGap, 10),
    )
  })

  it("falls back to medium chrome when the size is junk", () => {
    expect(chromeListDensity(0)).toEqual(chromeListDensity(13))
    expect(chromeListDensity(Number.NaN)).toEqual(chromeListDensity(13))
  })
})

describe("writeChromeListDensity", () => {
  it("writes the directory tokens, not rem", () => {
    const wrote: Record<string, string> = {}
    writeChromeListDensity(
      { setProperty: (name, value) => { wrote[name] = value } },
      13,
    )
    expect(wrote["--sidebar-row-height"]).toBe("28px")
    expect(wrote["--sidebar-kind"]).toBe("16px")
    expect(wrote["--sidebar-section-gap"]).toBe("10px")
    expect(JSON.stringify(wrote)).not.toMatch(/rem/)
  })
})
