import { describe, expect, it } from "vitest"

import type { Block } from "./transcript"
import {
  TURN_NAV_LIST_PREVIEW,
  TURN_NAV_MIN,
  TURN_NAV_TICK_PACK,
  activeNavId,
  packTurnNavTicks,
  prefersInstantScroll,
  previewText,
  offsetInScroller,
  isHumanNavTurn,
  resolveTurnNavItems,
  scrollTurnIntoView,
  turnNavItems,
  turnNavItemsFromTurns,
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

  it("labels a quoted send by the request, not the wire tags", () => {
    const wrapped =
      "<selected_text>\nalpha\n</selected_text>\n\n<user_request>\ndo this\n</user_request>"
    const items = turnNavItems([user("tn_a", wrapped)])
    expect(items.map((i) => i.text)).toEqual(["do this"])
    expect(JSON.stringify(items)).not.toMatch(/selected_text|user_request/)
  })
})

describe("turnNavItemsFromTurns", () => {
  it("skips auto-continue sessions that have no human request", () => {
    const items = turnNavItemsFromTurns([
      { id: "tn_a", user_text: "first request" },
      { id: "tn_b", user_text: "kept going", goal_continue: true },
      { id: "tn_c", user_text: "second request" },
    ])
    expect(items.map((i) => i.id)).toEqual(["tn_a", "tn_c"])
  })

  it("skips a wait fire so a /goal park is still one jump", () => {
    const items = turnNavItemsFromTurns([
      { id: "tn_a", user_text: "first request" },
      { id: "tn_b", user_text: "scheduled wrapper", schedule_continue: true },
      { id: "tn_c", user_text: "scheduled wrapper", schedule_continue: true },
      { id: "tn_d", user_text: "second request" },
    ])
    expect(items.map((i) => i.id)).toEqual(["tn_a", "tn_d"])
    expect(items.map((i) => i.text)).not.toEqual(
      expect.arrayContaining(["scheduled wrapper"]),
    )
  })
})

describe("isHumanNavTurn", () => {
  it("keeps a typed send and drops engine-started rows", () => {
    expect(isHumanNavTurn({ id: "tn_a", user_text: "first request" })).toBe(true)
    expect(isHumanNavTurn({ id: "tn_b", user_text: "kept going", goal_continue: true })).toBe(false)
    expect(
      isHumanNavTurn({ id: "tn_c", user_text: "scheduled wrapper", schedule_continue: true }),
    ).toBe(false)
    expect(isHumanNavTurn({ id: "tn_d", user_text: "   " })).toBe(false)
  })
})

describe("resolveTurnNavItems", () => {
  it("uses the turn list when the loaded slice is only a tail", () => {
    const items = resolveTurnNavItems([user("tn_c", "second request")], [
      { id: "tn_a", user_text: "first request" },
      { id: "tn_c", user_text: "second request" },
    ])
    expect(items.map((i) => i.id)).toEqual(["tn_a", "tn_c"])
  })

  it("does not let engine-started turns outvote the loaded user rows", () => {
    const items = resolveTurnNavItems([user("tn_a", "first request")], [
      { id: "tn_a", user_text: "first request" },
      { id: "tn_b", user_text: "kept going", goal_continue: true },
      { id: "tn_c", user_text: "scheduled wrapper", schedule_continue: true },
    ])
    expect(items.map((i) => i.id)).toEqual(["tn_a"])
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

  it("keeps a longer slice for the hover list than a tick label", () => {
    const long = "word ".repeat(40).trim()
    const tick = previewText(long)
    const list = previewText(long, TURN_NAV_LIST_PREVIEW)
    expect(list.length).toBeGreaterThan(tick.length)
    expect(list.length).toBeLessThanOrEqual(TURN_NAV_LIST_PREVIEW)
  })
})

describe("activeNavId", () => {
  const items = [
    { id: "tn_a", top: 0 },
    { id: "tn_b", top: 400 },
    { id: "tn_c", top: 800 },
  ]

  it("picks the last message that has crossed the reading line", () => {
    expect(activeNavId(items, 380, 300)).toBe("tn_b")
    expect(activeNavId(items, 0, 300)).toBe("tn_a")
    expect(activeNavId(items, 780, 300)).toBe("tn_c")
  })

  it("keeps the first send while a later one has only entered the bottom", () => {
    // A long first answer still fills the pane. The next bubble peeking
    // at the bottom used to steal the tick, so the rail and the scrollbar
    // disagreed about which turn the reader was on.
    const longFirst = [
      { id: "tn_a", top: 0 },
      { id: "tn_b", top: 700 },
      { id: "tn_c", top: 1400 },
    ]
    expect(activeNavId(longFirst, 0, 800, 4000)).toBe("tn_a")
    expect(activeNavId(longFirst, 200, 800, 4000)).toBe("tn_a")
  })

  it("stays on the first send at the top even when the next input is visible", () => {
    // The pane is at scrollTop 0. The next input sits lower in the same
    // viewport. Lighting it disagreed with the scrollbar.
    const onScreen = [
      { id: "tn_a", top: 0 },
      { id: "tn_b", top: 640 },
    ]
    expect(activeNavId(onScreen, 0, 800, 2000)).toBe("tn_a")
  })

  it("lights a later send once that row reaches the top of the pane", () => {
    const tight = [
      { id: "tn_a", top: 0 },
      { id: "tn_b", top: 40 },
      { id: "tn_c", top: 80 },
      { id: "tn_d", top: 200 },
    ]
    expect(activeNavId(tight, 180, 800, 2000)).toBe("tn_d")
  })

  it("pins the latest turn when the scroller is at the bottom", () => {
    // Last row still sits below the top probe because of composer padding.
    expect(activeNavId(items, 900, 300, 1200)).toBe("tn_c")
  })

  it("pins the latest turn while following the live edge", () => {
    expect(activeNavId(items, 0, 300, 1200, true)).toBe("tn_c")
  })

  it("skips turns that have not been mounted yet", () => {
    expect(
      activeNavId(
        [{ id: "tn_a" }, { id: "tn_b" }, { id: "tn_c", top: 800 }],
        700,
        300,
        1200,
      ),
    ).toBe("tn_c")
  })

  it("does not light a turn that is still below the fold", () => {
    expect(activeNavId(items, 0, 300, 1200)).toBe("tn_a")
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
    root.appendChild(row)
    root.getBoundingClientRect = () => ({ top: 0 } as DOMRect)
    row.getBoundingClientRect = () => ({ top: 120 } as DOMRect)

    expect(scrollTurnIntoView(root, "tn_a", true)).toBe(true)
    expect(root.scrollTop).toBe(120)
    expect(scrollTurnIntoView(root, "tn_missing", true)).toBe(false)
  })

  it("pins the send at the pane top so earlier work is not left peeking", () => {
    // Native scrollIntoView honours scroll-margin and also yanks the window,
    // so a thought fold sitting above the first tick stayed on screen.
    const root = document.createElement("div")
    const earlier = document.createElement("div")
    const row = document.createElement("div")
    row.setAttribute("data-turn-nav", "tn_a")
    root.appendChild(earlier)
    root.appendChild(row)
    root.scrollTop = 0
    root.getBoundingClientRect = () => ({ top: 0 } as DOMRect)
    earlier.getBoundingClientRect = () => ({ top: 0 } as DOMRect)
    row.getBoundingClientRect = () => ({ top: 480 } as DOMRect)

    expect(scrollTurnIntoView(root, "tn_a", true)).toBe(true)
    expect(root.scrollTop).toBe(480)
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

describe("packTurnNavTicks", () => {
  it("packs only after a compact cluster would overflow", () => {
    expect(packTurnNavTicks(TURN_NAV_TICK_PACK)).toBe(false)
    expect(packTurnNavTicks(TURN_NAV_TICK_PACK + 1)).toBe(true)
  })
})
