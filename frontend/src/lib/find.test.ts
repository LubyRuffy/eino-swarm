import { describe, expect, it } from "vitest"

import type { Block } from "./transcript"
import {
  FIND_PAINT_CAP,
  FIND_REVEAL_MIN,
  clampMatch,
  countMatches,
  findCountLabel,
  findMatchWindow,
  findMatches,
  findShortcut,
  hiddenSearchText,
  paintWindow,
  revealBlockIds,
  seedQuery,
  stepMatch,
} from "./find"

function key(
  partial: Partial<Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey">> = {},
): Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey"> {
  return {
    key: "f",
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    ...partial,
  }
}

function block(partial: Partial<Block> & Pick<Block, "id" | "kind">): Block {
  return {
    agentId: "manager",
    text: "",
    turnId: "t1",
    seq: 1,
    at: new Date().toISOString(),
    ...partial,
  }
}

describe("findMatches", () => {
  it("is empty for a blank query", () => {
    expect(findMatches("alpha beta", "")).toEqual([])
    expect(findMatches("alpha beta", "   ")).toEqual([])
  })

  it("matches case-insensitively without overlapping", () => {
    expect(findMatches("Alpha alpha ALPHA", "alpha")).toEqual([
      { start: 0, end: 5 },
      { start: 6, end: 11 },
      { start: 12, end: 17 },
    ])
    expect(findMatches("aaaa", "aa")).toEqual([
      { start: 0, end: 2 },
      { start: 2, end: 4 },
    ])
  })

  it("trims the needle so a trailing space is not a different search", () => {
    expect(findMatches("needle in here", " needle ")).toEqual([{ start: 0, end: 6 }])
  })

  it("counts without building the match list", () => {
    expect(countMatches("Alpha alpha ALPHA", "alpha")).toBe(3)
    expect(countMatches("aaaa", "aa")).toBe(2)
    expect(countMatches("alpha", "")).toBe(0)
  })
})

describe("findMatchWindow", () => {
  it("keeps the full count while only materialising a window around the current hit", () => {
    const hay = "x".repeat(200)
    expect(paintWindow(0, 200, 10)).toEqual({ start: 0, end: 10 })
    expect(paintWindow(199, 200, 10)).toEqual({ start: 190, end: 200 })
    expect(paintWindow(100, 200, 10)).toEqual({ start: 95, end: 105 })
    const first = findMatchWindow(hay, "x", 0, 10)
    expect(first.total).toBe(200)
    expect(first.matches).toHaveLength(10)
    expect(first.current).toBe(0)
    expect(first.matches[0]).toEqual({ start: 0, end: 1 })
    const last = findMatchWindow(hay, "x", 199, 10)
    expect(last.matches).toHaveLength(10)
    expect(last.current).toBe(9)
    expect(last.matches[9]).toEqual({ start: 199, end: 200 })
  })

  it("paints everything when the list fits in the cap", () => {
    const all = findMatchWindow("aaa", "a", 1, FIND_PAINT_CAP)
    expect(all).toEqual({
      total: 3,
      matches: [
        { start: 0, end: 1 },
        { start: 1, end: 2 },
        { start: 2, end: 3 },
      ],
      current: 1,
    })
  })
})

describe("stepMatch and clampMatch", () => {
  it("wraps next and previous around the list", () => {
    expect(stepMatch(0, 3, 1)).toBe(1)
    expect(stepMatch(2, 3, 1)).toBe(0)
    expect(stepMatch(0, 3, -1)).toBe(2)
  })

  it("stays at zero when there is nothing to step", () => {
    expect(stepMatch(4, 0, 1)).toBe(0)
    expect(clampMatch(4, 0)).toBe(0)
  })

  it("pulls an out-of-range index back onto the last hit", () => {
    expect(clampMatch(9, 3)).toBe(2)
    expect(clampMatch(-1, 3)).toBe(0)
  })
})

describe("findCountLabel", () => {
  it("stays quiet until there is a query", () => {
    expect(findCountLabel(0, 0, "")).toBe("")
  })

  it("says when nothing matches, then numbers the current hit", () => {
    expect(findCountLabel(0, 0, "needle")).toBe("No results")
    expect(findCountLabel(0, 8, "needle")).toBe("1 / 8 results")
    expect(findCountLabel(7, 8, "needle")).toBe("8 / 8 results")
  })
})

describe("seedQuery", () => {
  it("uses a short selection and ignores a dump", () => {
    expect(seedQuery("  Needle  ", "")).toBe("Needle")
    expect(seedQuery("first\nsecond", "old")).toBe("first")
    expect(seedQuery("x".repeat(201), "old")).toBe("old")
    expect(seedQuery("   ", "old")).toBe("old")
  })
})

describe("findShortcut", () => {
  it("opens on ⌘F and selects the query when the bar is already up", () => {
    expect(findShortcut(key({ metaKey: true }), false)).toEqual({ action: "open" })
    expect(findShortcut(key({ ctrlKey: true }), false)).toEqual({ action: "open" })
    expect(findShortcut(key({ metaKey: true }), true)).toEqual({ action: "select-query" })
  })

  it("does not steal ⌘⇧F or option-modified F", () => {
    expect(findShortcut(key({ metaKey: true, shiftKey: true }), false)).toBeUndefined()
    expect(findShortcut(key({ metaKey: true, altKey: true }), false)).toBeUndefined()
    expect(findShortcut(key({ key: "f" }), false)).toBeUndefined()
  })

  it("closes on Escape only while the bar is open", () => {
    expect(findShortcut(key({ key: "Escape" }), true)).toEqual({ action: "close" })
    expect(findShortcut(key({ key: "Escape" }), false)).toBeUndefined()
  })

  it("steps with F3 and ⌘G, backwards with shift", () => {
    expect(findShortcut(key({ key: "F3" }), true)).toEqual({ action: "next", delta: 1 })
    expect(findShortcut(key({ key: "F3", shiftKey: true }), true)).toEqual({
      action: "next",
      delta: -1,
    })
    expect(findShortcut(key({ key: "g", metaKey: true }), true)).toEqual({
      action: "next",
      delta: 1,
    })
    expect(findShortcut(key({ key: "g", ctrlKey: true, shiftKey: true }), true)).toEqual({
      action: "next",
      delta: -1,
    })
    expect(findShortcut(key({ key: "F3" }), false)).toBeUndefined()
  })
})

describe("revealBlockIds", () => {
  it("opens a finished thought whose body holds the query", () => {
    const thought = block({
      id: "r1",
      kind: "reasoning",
      text: "the hidden needle sits here",
    })
    expect(revealBlockIds([thought], "needle")).toEqual(["r1"])
    expect(revealBlockIds([thought], "Thought")).toEqual([])
  })

  it("opens a tool row only when the payload matches, not the visible name", () => {
    const tool = block({
      id: "t1",
      kind: "tool",
      text: "read",
      tool: {
        callId: "c1",
        name: "read",
        args: `{"file_path":"alpha.txt"}`,
        result: "payload-unique",
        pending: false,
      },
    })
    expect(revealBlockIds([tool], "payload-unique")).toEqual(["t1"])
    expect(revealBlockIds([tool], "read")).toEqual([])
    expect(hiddenSearchText(tool)).toContain("payload-unique")
  })

  it("leaves answers alone — they are already on screen", () => {
    const answer = block({ id: "a1", kind: "answer", text: "needle in the reply" })
    expect(revealBlockIds([answer], "needle")).toEqual([])
  })

  it("does not explode a tool dump for a one-character query", () => {
    const tool = block({
      id: "t1",
      kind: "tool",
      text: "read",
      tool: {
        callId: "c1",
        name: "read",
        args: "{}",
        result: "e".repeat(80),
        pending: false,
      },
    })
    expect(FIND_REVEAL_MIN).toBe(2)
    expect(revealBlockIds([tool], "e")).toEqual([])
    expect(revealBlockIds([tool], "ee")).toEqual(["t1"])
  })

  it("opens only the first collapsed row that holds the query", () => {
    const first = block({
      id: "t1",
      kind: "tool",
      text: "read",
      tool: {
        callId: "c1",
        name: "read",
        args: "{}",
        result: "shared-hit in the first dump",
        pending: false,
      },
    })
    const second = block({
      id: "t2",
      kind: "tool",
      text: "read",
      tool: {
        callId: "c2",
        name: "read",
        args: "{}",
        result: "shared-hit in the second dump",
        pending: false,
      },
    })
    expect(revealBlockIds([first, second], "shared-hit")).toEqual(["t1"])
  })
})
