import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"

const fake = vi.hoisted(() => ({
  savedUI: undefined as
    | {
        locale?: string
        font?: string
        font_size?: string
        content_width?: string
      }
    | undefined,
}))

vi.mock("@/lib/api", () => ({
  api: {
    saveSettings: async (patch: {
      ui?: {
        locale?: string
        font?: string
        font_size?: string
        content_width?: string
      }
    }) => {
      if (patch.ui) fake.savedUI = patch.ui
      return patch
    },
  },
}))

beforeEach(() => {
  fake.savedUI = undefined
  useApp.setState({
    locale: "en",
    font: "system",
    fontSize: "medium",
    contentWidth: "comfortable",
  })
})

describe("locale preference", () => {
  // Desktop binds a random loopback, so localStorage-only would forget the
  // language on every launch. The pin has to ride PUT /api/settings.
  it("writes the chrome language through settings so the next boot keeps it", async () => {
    await useApp.getState().setLocale("zh")
    expect(useApp.getState().locale).toBe("zh")
    expect(document.documentElement.lang).toBe("zh-CN")
    expect(fake.savedUI).toEqual({
      locale: "zh",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
  })

  it("applies a boot locale without rewriting settings", () => {
    useApp.getState().setLocale("zh", { persist: false })
    expect(useApp.getState().locale).toBe("zh")
    expect(fake.savedUI).toBeUndefined()
  })
})

describe("appearance preference", () => {
  it("writes typeface and column through settings so the next boot keeps them", async () => {
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
      font: "serif",
      font_size: "large",
      content_width: "full",
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
      font: "serif",
      font_size: "medium",
      content_width: "comfortable",
    })
  })
})
