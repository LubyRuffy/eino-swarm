import { describe, expect, it } from "vitest"

import { applySystemTheme } from "./theme"

describe("theme", () => {
  it("toggles the dark class from prefers-color-scheme", () => {
    const listeners: Array<(e: MediaQueryListEvent) => void> = []
    let matches = false
    const mq = {
      matches,
      media: "(prefers-color-scheme: dark)",
      addEventListener: (_: string, fn: (e: MediaQueryListEvent) => void) => {
        listeners.push(fn)
      },
      removeEventListener: () => undefined,
    }
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: () => mq,
    })
    const stop = applySystemTheme()
    expect(document.documentElement.classList.contains("dark")).toBe(false)
    mq.matches = true
    listeners[0]?.({ matches: true } as MediaQueryListEvent)
    expect(document.documentElement.classList.contains("dark")).toBe(true)
    stop()
  })
})
