import { describe, expect, it } from "vitest"

import { chromeTypeClass, composerPinClass } from "./chrome-type"

describe("chromeTypeClass", () => {
  it("pins chrome to the CSS token so Font size cannot balloon the sidebar", () => {
    expect(chromeTypeClass).toContain("--chrome-font-size")
    expect(chromeTypeClass).toContain("font-normal")
    expect(chromeTypeClass).not.toMatch(/\btext-sm\b/)
    expect(chromeTypeClass).not.toMatch(/\bfont-medium\b/)
  })
})

describe("composerPinClass", () => {
  it("is a muted chip, not a card", () => {
    expect(composerPinClass).toContain("bg-muted/40")
    expect(composerPinClass).toContain("--chrome-font-size")
    expect(composerPinClass).not.toMatch(/\bbg-card\b/)
  })
})
