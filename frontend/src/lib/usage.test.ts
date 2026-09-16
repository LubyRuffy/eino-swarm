import { describe, expect, it } from "vitest"

import {
  contextPercent,
  emptyUsage,
  formatTokens,
  formatTurnBits,
  hasUsage,
  mergeModelContext,
  meterFill,
  setModelWindow,
  parseUsage,
  unknownWindowFill,
  windowForSelection,
} from "./usage"

describe("formatTokens", () => {
  it("matches the compact Cursor counts", () => {
    expect(formatTokens(850)).toBe("850")
    expect(formatTokens(1200)).toBe("1.2K")
    expect(formatTokens(71300)).toBe("71.3K")
    expect(formatTokens(256000)).toBe("256K")
    expect(formatTokens(1_200_000)).toBe("1.2M")
  })
})

describe("contextPercent", () => {
  it("is undefined without a window, not a fake 0%", () => {
    expect(contextPercent(100, 0)).toBeUndefined()
    expect(contextPercent(71300, 256000)).toBe(28)
  })
})

describe("meterFill", () => {
  it("is the percentage when the window is known", () => {
    expect(meterFill(71300, 256000)).toBeCloseTo(0.28)
  })

  it("still grows when the window is unknown so the ring is not a dead circle", () => {
    expect(unknownWindowFill(0, 80000)).toBe(0)
    expect(unknownWindowFill(54100, 0)).toBe(0)
    expect(unknownWindowFill(54100, 80000)).toBeCloseTo(1 - 1 / (1 + 54100 / 80000))
    expect(meterFill(54100, 0, 80000)).toBeGreaterThan(0.3)
    expect(meterFill(54100, 0, 80000)).toBeLessThan(0.5)
  })
})

describe("windowForSelection", () => {
  const rows = [
    {
      provider_id: "a",
      model: "alpha",
      default: true,
      context_window: 128000,
    },
    { provider_id: "a", model: "beta", context_window: 0 },
    {
      provider_id: "b",
      model: "gamma",
      default: true,
      context_window: 32000,
    },
  ]

  it("uses the selected name, not a sibling", () => {
    expect(windowForSelection(rows, "a", "beta")).toBe(0)
    expect(windowForSelection(rows, "a", "alpha")).toBe(128000)
  })

  it("falls back to the provider default when the name is not listed", () => {
    expect(windowForSelection(rows, "a", "typed-in")).toBe(128000)
    expect(windowForSelection(rows, "b")).toBe(32000)
  })
})

describe("parseUsage", () => {
  it("reads a snapshot and ignores garbage", () => {
    const u = parseUsage(
      JSON.stringify({
        context_tokens: 12,
        context_window: 100,
        turn: { prompt_tokens: 8, completion_tokens: 2, total_tokens: 10, calls: 1 },
        thread: { total_tokens: 10, calls: 1 },
      }),
    )
    expect(u?.context_tokens).toBe(12)
    expect(u?.turn.calls).toBe(1)
    expect(parseUsage("{")).toBeUndefined()
    expect(hasUsage(emptyUsage())).toBe(false)
    expect(hasUsage(u)).toBe(true)
  })
})

describe("formatTurnBits", () => {
  it("hides until a call has billed something", () => {
    expect(formatTurnBits(emptyUsage().turn)).toBeUndefined()
    expect(
      formatTurnBits({
        prompt_tokens: 12000,
        completion_tokens: 3100,
        cached_tokens: 4000,
        reasoning_tokens: 800,
        total_tokens: 15100,
        calls: 4,
      }),
    ).toBe("12K in · 3.1K out · 4K cached · 800 thinking")
  })
})

describe("setModelWindow", () => {
  it("writes one name and clears it without inventing a sibling", () => {
    expect(setModelWindow({ alpha: 8 }, "beta", 16)).toEqual({
      alpha: 8,
      beta: 16,
    })
    expect(setModelWindow({ alpha: 8, beta: 16 }, "beta", 0)).toEqual({
      alpha: 8,
    })
    expect(setModelWindow({ alpha: 8 }, "  ", 32)).toEqual({ alpha: 8 })
  })
})

describe("mergeModelContext", () => {
  it("lets a discovered window overwrite that name only", () => {
    expect(
      mergeModelContext({ alpha: 8, beta: 16 }, { alpha: 32, junk: 0 }),
    ).toEqual({ alpha: 32, beta: 16 })
  })
})
