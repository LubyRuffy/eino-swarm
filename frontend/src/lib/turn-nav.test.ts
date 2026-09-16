import { describe, expect, it, vi } from "vitest"

import type { Block } from "./transcript"
import {
  TURN_NAV_MIN,
  activeNavId,
  prefersInstantScroll,
  previewText,
  offsetInScroller,
  scrollTurnIntoView,
  turnNavItems,
  turnNavSelector,
} from "./turn-nav"

function user(turnId: string, text: string): Block {
  return {
    id: `${turnId}:user`,
    kind: "user",
    agentId: "manager",
    text,
    turnId,
    seq: 1,
    at: new Date().toISOString(),
  }
}

function steer(turnId: string, text: string): Block {
  return { ...user(turnId, text), id: `${turnId}:steer`, kind: "steer" }
}

describe("turnNavItems", () => {
  it("keeps user turns and drops steering", () => {
    const items = turnNavItems([
      user("tn_a", "first request"),
      steer("tn_a", "nudge"),
      user("tn_b", "second request"),
    ])
    expect(items.map((i) => i.id)).toEqual(["tn_a", "tn_b"])
    expect(items.map((i) => i.text)).toEqual(["first request", "second request"])
  })

  it("skips a blank user row", () => {
    expect(turnNavItems([user("tn_a", "   ")])).toEqual([])
  })

  it("does not take its labels from a hardcoded sample", () => {
    const items = turnNavItems([user("tn_a", "alpha"), user("tn_b", "beta")])
    const blob = JSON.stringify(items)
    expect(blob).not.toMatch(/notes\.md|summary\.md|deadline/i)
  })
})

describe("previewText", () => {
  it("collapses whitespace so a tick stays one line", () => {
    expect(previewText("hello\n\n  world")).toBe("hello world")
  })

  it("truncates with an ellipsis rather than overflowing the rail", () => {
    const long = "word ".repeat(40).trim()
    const preview = previewText(long, 20)
    expect(preview.endsWith("…")).toBe(true)
    expect(preview.length).toBeLessThanOrEqual(20)
  })
})

describe("activeNavId", () => {
  const items = [
    { id: "tn_a", top: 0 },
    { id: "tn_b", top: 400 },
    { id: "tn_c", top: 800 },
  ]

  it("picks the last message whose top has crossed the probe", () => {
    expect(activeNavId(items, 380, 300)).toBe("tn_b")
    expect(activeNavId(items, 0, 300)).toBe("tn_a")
    expect(activeNavId(items, 780, 300)).toBe("tn_c")
  })

  it("pins the latest turn when the scroller is at the bottom", () => {
    // Last row still sits below the top probe because of composer padding.
    expect(activeNavId(items, 900, 300, 1200)).toBe("tn_c")
  })

  it("is empty when there is nothing to jump to", () => {
    expect(activeNavId([], 0, 300)).toBeUndefined()
  })
})

describe("scrollTurnIntoView", () => {
  it("scrolls the matching user row and leaves a missing id alone", () => {
    const root = document.createElement("div")
    const row = document.createElement("div")
    row.setAttribute("data-turn-nav", "tn_a")
    const intoView = vi.fn()
    row.scrollIntoView = intoView
    root.appendChild(row)

    expect(scrollTurnIntoView(root, "tn_a", true)).toBe(true)
    expect(intoView).toHaveBeenCalledWith(
      expect.objectContaining({ behavior: "auto", block: "start" }),
    )
    expect(scrollTurnIntoView(root, "tn_missing", true)).toBe(false)
  })

  it("builds an attribute selector that matches the row", () => {
    const root = document.createElement("div")
    const row = document.createElement("div")
    row.setAttribute("data-turn-nav", "tn_a")
    root.appendChild(row)
    expect(root.querySelector(turnNavSelector("tn_a"))).toBe(row)
  })
})

describe("prefersInstantScroll", () => {
  it("honours reduced motion so a jump does not animate", () => {
    const win = {
      matchMedia: (q: string) => ({
        matches: q.includes("prefers-reduced-motion"),
      }),
    } as unknown as Window
    expect(prefersInstantScroll(win)).toBe(true)
  })

  it("treats a window without matchMedia as animated", () => {
    expect(prefersInstantScroll({} as Window)).toBe(false)
  })
})

describe("offsetInScroller", () => {
  it("adds the scroller's scrollTop to the visible gap", () => {
    const el = document.createElement("div")
    const root = document.createElement("div")
    el.getBoundingClientRect = () =>
      ({ top: 80 } as DOMRect)
    root.getBoundingClientRect = () =>
      ({ top: 40 } as DOMRect)
    root.scrollTop = 200
    expect(offsetInScroller(el, root)).toBe(240)
  })
})

describe("TURN_NAV_MIN", () => {
  it("stays a two-turn threshold", () => {
    expect(TURN_NAV_MIN).toBe(2)
  })
})
