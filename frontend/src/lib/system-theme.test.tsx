import { act, cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { App } from "@/App"
import { api } from "@/lib/api"
import { useApp, type Theme } from "@/store/app"

vi.mock("@/components/app/terminal-panel", () => ({ TerminalPanel: () => null }))

const initialState = useApp.getState()

describe("system theme changes while the app is open", () => {
  let media: EventTarget & { matches: boolean }

  beforeEach(() => {
    media = Object.assign(new EventTarget(), { matches: false })
    vi.stubGlobal("matchMedia", vi.fn(() => media as unknown as MediaQueryList))
    vi.spyOn(api, "clients").mockResolvedValue({ enabled: false, tools: [] })
    useApp.setState({ boot: vi.fn(async () => {}), status: { running: false } })
    useApp.getState().setTheme("system")
  })

  afterEach(() => {
    cleanup()
    useApp.setState(initialState)
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.documentElement.classList.remove("dark")
  })

  function changeSystemTheme(dark: boolean) {
    act(() => {
      media.matches = dark
      media.dispatchEvent(new Event("change"))
    })
  }

  it("updates the mounted app for both OS color scheme changes", () => {
    render(<App />)
    expect(document.documentElement).not.toHaveClass("dark")

    changeSystemTheme(true)
    expect(document.documentElement).toHaveClass("dark")
    expect(useApp.getState().theme).toBe("system")

    changeSystemTheme(false)
    expect(document.documentElement).not.toHaveClass("dark")
    expect(localStorage.getItem("zwai.theme")).toBe("system")
  })

  it.each<Theme>(["light", "dark"])("keeps the explicit %s preference", (theme) => {
    useApp.getState().setTheme(theme)
    const add = vi.spyOn(media, "addEventListener")
    render(<App />)

    changeSystemTheme(true)
    changeSystemTheme(false)
    expect(document.documentElement.classList.contains("dark")).toBe(theme === "dark")
    expect(add).not.toHaveBeenCalled()
  })

  it("stops following on an explicit preference and resumes when system is selected", () => {
    const remove = vi.spyOn(media, "removeEventListener")
    render(<App />)
    act(() => useApp.getState().setTheme("light"))
    expect(remove).toHaveBeenCalledWith("change", expect.any(Function))

    changeSystemTheme(true)
    expect(document.documentElement).not.toHaveClass("dark")
    act(() => useApp.getState().setTheme("system"))
    expect(document.documentElement).toHaveClass("dark")
    changeSystemTheme(false)
    expect(document.documentElement).not.toHaveClass("dark")
  })

  it("removes its OS listener when the app unmounts", () => {
    const remove = vi.spyOn(media, "removeEventListener")
    const view = render(<App />)
    view.unmount()
    expect(remove).toHaveBeenCalledWith("change", expect.any(Function))

    changeSystemTheme(true)
    expect(document.documentElement).not.toHaveClass("dark")
  })

  it("still mounts when color scheme observation is unavailable", () => {
    vi.stubGlobal("matchMedia", undefined)
    const view = render(<App />)
    expect(document.documentElement).not.toHaveClass("dark")
    view.unmount()
  })
})
