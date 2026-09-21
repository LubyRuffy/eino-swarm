import { beforeEach, describe, expect, it, vi } from "vitest"

import { defaultAppearance } from "@/lib/appearance"
import { useApp } from "@/store/app"

const fake = vi.hoisted(() => ({
  savedUI: undefined as Record<string, string> | undefined,
}))

vi.mock("@/lib/api", () => ({
  api: {
    saveSettings: async (patch: { ui?: Record<string, string> }) => {
      if (patch.ui) fake.savedUI = patch.ui
      return patch
    },
  },
}))

beforeEach(() => {
  fake.savedUI = undefined
  useApp.setState({
    locale: "en",
    ...defaultAppearance(),
  })
})

const chrome = {
  font: "system",
  ui_font_size: "medium",
  content_font: "ui",
  font_size: "ui",
  code_font: "mono",
  code_font_size: "content",
  content_width: "comfortable",
  transcript_mode: "user",
  palette: "zwai",
}

describe("locale preference", () => {
  // Desktop binds a random loopback, so localStorage-only would forget the
  // language on every launch. The pin has to ride PUT /api/settings.
  it("writes the chrome language through settings so the next boot keeps it", async () => {
    await useApp.getState().setLocale("zh")
    expect(useApp.getState().locale).toBe("zh")
    expect(document.documentElement.lang).toBe("zh-CN")
    expect(fake.savedUI).toEqual({ locale: "zh", ...chrome })
  })

  it("applies a boot locale without rewriting settings", () => {
    useApp.getState().setLocale("zh", { persist: false })
    expect(useApp.getState().locale).toBe("zh")
    expect(fake.savedUI).toBeUndefined()
  })
})

describe("appearance preference", () => {
  it("writes transcript mode through settings so the next boot keeps it", async () => {
    await useApp.getState().setAppearance({ transcriptMode: "developer" })
    expect(useApp.getState().transcriptMode).toBe("developer")
    expect(document.documentElement.dataset.transcriptMode).toBe("developer")
    expect(fake.savedUI).toEqual({
      locale: "en",
      ...chrome,
      transcript_mode: "developer",
    })
  })

  it("writes the typeface and conversation width through settings", async () => {
    await useApp.getState().setAppearance({
      font: "serif",
      fontSize: "large",
      contentWidth: "full",
    })
    expect(useApp.getState().font).toBe("serif")
    expect(useApp.getState().fontSize).toBe("large")
    expect(useApp.getState().contentWidth).toBe("full")
    expect(document.documentElement.dataset.contentWidth).toBe("full")
    expect(fake.savedUI).toEqual({
      locale: "en",
      ...chrome,
      font: "serif",
      font_size: "large",
      content_width: "full",
    })
  })

  it("writes the named color set through settings so the next boot keeps it", async () => {
    await useApp.getState().setAppearance({ palette: "fofa" })
    expect(useApp.getState().palette).toBe("fofa")
    expect(document.documentElement.dataset.palette).toBe("fofa")
    expect(fake.savedUI).toEqual({
      locale: "en",
      ...chrome,
      palette: "fofa",
    })
  })

  it("applies boot chrome without rewriting settings", () => {
    useApp.getState().setAppearance({ font: "mono" }, { persist: false })
    expect(useApp.getState().font).toBe("mono")
    expect(fake.savedUI).toBeUndefined()
  })

  it("a language write keeps the typeface already in the store", async () => {
    useApp.getState().setAppearance({ font: "serif" }, { persist: false })
    await useApp.getState().setLocale("zh")
    expect(fake.savedUI).toEqual({
      locale: "zh",
      ...chrome,
      font: "serif",
    })
  })
})
