import { describe, expect, it } from "vitest"

import { cn } from "./utils"
import { chromeTypeClass, composerPinClass, contentTypeClass } from "./chrome-type"

describe("chromeTypeClass", () => {
  it("pins chrome to the CSS token so Font size cannot balloon the sidebar", () => {
    expect(chromeTypeClass).toContain("--chrome-font-size")
    expect(chromeTypeClass).toContain("font-normal")
    expect(chromeTypeClass).not.toMatch(/\btext-sm\b/)
    expect(chromeTypeClass).not.toMatch(/\bfont-medium\b/)
  })
})

describe("composerPinClass", () => {
  it("is an opaque chip so transcript lines cannot show through", () => {
    expect(composerPinClass).toContain("bg-background")
    expect(composerPinClass).toContain("--chrome-font-size")
    expect(composerPinClass).not.toMatch(/bg-muted/)
  })
})

describe("contentTypeClass", () => {
  it("uses the content face and size tokens, beating Textarea text-sm", () => {
    expect(contentTypeClass).toContain("content-type")
    expect(contentTypeClass).toContain("--ui-font-size")
    expect(contentTypeClass).not.toMatch(/\btext-sm\b/)
    const merged = cn("text-sm shadow-sm", contentTypeClass)
    expect(merged).toContain("--ui-font-size")
    expect(merged).not.toMatch(/\btext-sm\b/)
  })
})
