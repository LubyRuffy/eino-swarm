import { describe, expect, it, vi } from "vitest"

import { isDark } from "./app-chrome"

describe("isDark", () => {
  it("follows the pin, then the OS preference", () => {
    expect(isDark("dark")).toBe(true)
    expect(isDark("light")).toBe(false)
    const match = vi.fn(() => ({ matches: true }))
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: match,
    })
    expect(isDark("system")).toBe(true)
    expect(match).toHaveBeenCalled()
  })
})
