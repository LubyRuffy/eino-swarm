import { afterEach, describe, expect, it } from "vitest"

import {
  APPEARANCE_KEY,
  applyAppearance,
  defaultAppearance,
  normalizeAppearance,
  normalizeContentWidth,
  normalizeFont,
  normalizeFontSize,
  normalizeUISettings,
  readAppearance,
  toggleContentWidth,
  writeAppearance,
} from "./appearance"

describe("normalizeFont", () => {
  it("keeps a known typeface and treats anything else as the UI sans stack", () => {
    expect(normalizeFont("system")).toBe("system")
    expect(normalizeFont("SERIF")).toBe("serif")
    expect(normalizeFont(" mono ")).toBe("mono")
    expect(normalizeFont("")).toBe("system")
    expect(normalizeFont("comic")).toBe("system")
  })
})

describe("normalizeFontSize", () => {
  it("keeps a known size and treats anything else as medium", () => {
    expect(normalizeFontSize("small")).toBe("small")
    expect(normalizeFontSize("LARGE")).toBe("large")
    expect(normalizeFontSize("")).toBe("medium")
    expect(normalizeFontSize("huge")).toBe("medium")
  })
})

describe("normalizeContentWidth", () => {
  it("keeps a known column and treats anything else as the reading width", () => {
    expect(normalizeContentWidth("full")).toBe("full")
    expect(normalizeContentWidth("COMFORTABLE")).toBe("comfortable")
    expect(normalizeContentWidth("wide")).toBe("comfortable")
  })
})

describe("normalizeAppearance", () => {
  it("reads both snake and camel keys from a settings or meta payload", () => {
    expect(
      normalizeAppearance({ font: "mono", font_size: "small", content_width: "full" }),
    ).toEqual({ font: "mono", fontSize: "small", contentWidth: "full" })
    expect(
      normalizeAppearance({ font: "serif", fontSize: "large", contentWidth: "full" }),
    ).toEqual({ font: "serif", fontSize: "large", contentWidth: "full" })
    expect(normalizeAppearance(undefined)).toEqual(defaultAppearance())
  })
})

describe("normalizeUISettings", () => {
  it("fills every chrome field so a PUT cannot drop the typeface", () => {
    expect(normalizeUISettings({ locale: "zh" }, "en")).toEqual({
      locale: "en",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
    expect(
      normalizeUISettings({
        locale: "system",
        font: "serif",
        font_size: "large",
        content_width: "full",
      }),
    ).toEqual({
      locale: "system",
      font: "serif",
      font_size: "large",
      content_width: "full",
    })
  })
})

describe("appearance storage", () => {
  afterEach(() => {
    localStorage.removeItem(APPEARANCE_KEY)
  })

  it("round-trips a pin and ignores junk", () => {
    expect(readAppearance()).toEqual(defaultAppearance())
    writeAppearance({ font: "serif", fontSize: "large", contentWidth: "full" })
    expect(readAppearance()).toEqual({
      font: "serif",
      fontSize: "large",
      contentWidth: "full",
    })
    localStorage.setItem(APPEARANCE_KEY, "nope")
    expect(readAppearance()).toEqual(defaultAppearance())
  })
})

describe("toggleContentWidth", () => {
  it("flips the reading column and the wide fill", () => {
    expect(toggleContentWidth("comfortable")).toBe("full")
    expect(toggleContentWidth("full")).toBe("comfortable")
  })
})

describe("applyAppearance", () => {
  it("writes CSS variables and data attributes the column class reads", () => {
    applyAppearance({ font: "serif", fontSize: "large", contentWidth: "full" })
    const root = document.documentElement
    expect(root.dataset.font).toBe("serif")
    expect(root.dataset.fontSize).toBe("large")
    expect(root.dataset.contentWidth).toBe("full")
    expect(root.style.getPropertyValue("--font-sans")).toMatch(/serif/)
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("16px")
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("")
    expect(root.style.getPropertyValue("--content-max")).toBe("none")
    expect(root.style.getPropertyValue("--content-gutter")).toBe("1rem")

    applyAppearance(defaultAppearance())
    expect(root.dataset.font).toBe("system")
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--content-max")).toBe("48rem")
    expect(root.style.getPropertyValue("--content-gutter")).toBe("2rem")
  })
})
