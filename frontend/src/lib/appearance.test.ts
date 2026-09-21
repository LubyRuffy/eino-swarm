import { afterEach, describe, expect, it } from "vitest"

import {
  APPEARANCE_KEY,
  applyAppearance,
  defaultAppearance,
  normalizeAppearance,
  normalizeContentWidth,
  normalizeFont,
  normalizeFontSize,
  normalizeTranscriptMode,
  normalizeUISettings,
  normalizePalette,
  readAppearance,
  toggleContentWidth,
  toggleTranscriptMode,
  writeAppearance,
} from "./appearance"

const filled = {
  locale: "system",
  font: "system",
  ui_font_size: "medium",
  content_font: "ui",
  font_size: "ui",
  code_font: "mono",
  code_font_size: "content",
  content_width: "comfortable",
  transcript_mode: "user",
  palette: "zwai",
} as const

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
  it("keeps a known size, ui, and treats junk as medium", () => {
    expect(normalizeFontSize("small")).toBe("small")
    expect(normalizeFontSize("LARGE")).toBe("large")
    expect(normalizeFontSize("ui")).toBe("ui")
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

describe("normalizeTranscriptMode", () => {
  it("keeps a known view and treats anything else as the compact transcript", () => {
    expect(normalizeTranscriptMode("user")).toBe("user")
    expect(normalizeTranscriptMode("DEVELOPER")).toBe("developer")
    expect(normalizeTranscriptMode("verbose")).toBe("user")
    expect(normalizeTranscriptMode("")).toBe("user")
  })
})

describe("normalizePalette", () => {
  it("keeps a named set and treats junk as the current chrome", () => {
    expect(normalizePalette("zwai")).toBe("zwai")
    expect(normalizePalette("FOFA")).toBe("fofa")
    expect(normalizePalette(" fofa ")).toBe("fofa")
    expect(normalizePalette("")).toBe("zwai")
    expect(normalizePalette("codex")).toBe("zwai")
  })
})

describe("normalizeAppearance", () => {
  it("reads both snake and camel keys from a settings or meta payload", () => {
    expect(
      normalizeAppearance({ font: "mono", font_size: "small", content_width: "full", palette: "fofa" }),
    ).toEqual({
      ...defaultAppearance(),
      font: "mono",
      fontSize: "small",
      contentWidth: "full",
      palette: "fofa",
    })
    expect(
      normalizeAppearance({
        font: "serif",
        contentFont: "mono",
        fontSize: "large",
        contentWidth: "full",
      }),
    ).toEqual({
      ...defaultAppearance(),
      font: "serif",
      contentFont: "mono",
      fontSize: "large",
      contentWidth: "full",
    })
    expect(normalizeAppearance(undefined)).toEqual(defaultAppearance())
  })
})

describe("normalizeUISettings", () => {
  it("fills every chrome field so a PUT cannot drop the typeface", () => {
    expect(normalizeUISettings({ locale: "zh" }, "en")).toEqual({
      ...filled,
      locale: "en",
    })
    expect(
      normalizeUISettings({
        locale: "system",
        font: "serif",
        font_size: "large",
        content_width: "full",
      }),
    ).toEqual({
      ...filled,
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
    writeAppearance({
      ...defaultAppearance(),
      font: "serif",
      fontSize: "large",
      contentWidth: "full",
    })
    expect(readAppearance()).toEqual({
      ...defaultAppearance(),
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

describe("toggleTranscriptMode", () => {
  it("flips the compact transcript and the full tool log", () => {
    expect(toggleTranscriptMode("user")).toBe("developer")
    expect(toggleTranscriptMode("developer")).toBe("user")
  })
})

describe("applyAppearance", () => {
  it("writes CSS variables and data attributes the column class reads", () => {
    applyAppearance({
      ...defaultAppearance(),
      font: "serif",
      fontSize: "large",
      contentWidth: "full",
    })
    const root = document.documentElement
    expect(root.dataset.font).toBe("serif")
    expect(root.dataset.fontSize).toBe("large")
    expect(root.dataset.contentWidth).toBe("full")
    expect(root.dataset.transcriptMode).toBe("user")
    expect(root.dataset.palette).toBe("zwai")
    expect(root.style.getPropertyValue("--font-sans")).toMatch(/serif/)
    expect(root.style.getPropertyValue("--font-content")).toMatch(/serif/)
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("16px")
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--content-max")).toBe("none")
    expect(root.style.getPropertyValue("--content-gutter")).toBe("1rem")

    applyAppearance(defaultAppearance())
    expect(root.dataset.font).toBe("system")
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--sidebar-row-height")).toBe("28px")
    expect(root.style.getPropertyValue("--content-max")).toBe("48rem")
    expect(root.style.getPropertyValue("--content-gutter")).toBe("2rem")
  })

  it("pins the named color set so CSS can swap the token sheet", () => {
    const root = document.documentElement
    applyAppearance({ ...defaultAppearance(), palette: "fofa" })
    expect(root.dataset.palette).toBe("fofa")
    applyAppearance(defaultAppearance())
    expect(root.dataset.palette).toBe("zwai")
  })

  it("grows the directory with UI size and ignores conversation size", () => {
    const root = document.documentElement
    applyAppearance({
      ...defaultAppearance(),
      fontSize: "large",
    })
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("16px")
    expect(root.style.getPropertyValue("--sidebar-row-height")).toBe("28px")

    applyAppearance({
      ...defaultAppearance(),
      uiFontSize: "small",
    })
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("12px")
    expect(root.style.getPropertyValue("--sidebar-row-height")).toBe("26px")

    applyAppearance({
      ...defaultAppearance(),
      uiFontSize: "large",
      fontSize: "medium",
    })
    expect(root.style.getPropertyValue("--chrome-font-size")).toBe("16px")
    expect(root.style.getPropertyValue("--ui-font-size")).toBe("13px")
    expect(root.style.getPropertyValue("--sidebar-row-height")).toBe("34px")
    expect(root.style.getPropertyValue("--sidebar-section-gap")).toBe("12px")
  })
})
