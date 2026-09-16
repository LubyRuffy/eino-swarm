import { afterEach, describe, expect, it } from "vitest"

import { FIND_HIGHLIGHT, FIND_HIGHLIGHT_CURRENT, FIND_PAINT_CAP } from "./find"
import {
  applyFindHighlights,
  clearFindHighlights,
  collectVisibleText,
  rangeFromOffsets,
  rangesForMatches,
  scrollRangeIntoView,
  supportsCssHighlights,
} from "./find-dom"

function mount(html: string): HTMLElement {
  const root = document.createElement("div")
  root.innerHTML = html
  document.body.appendChild(root)
  return root
}

afterEach(() => {
  document.body.replaceChildren()
})

describe("collectVisibleText", () => {
  it("joins text across tags so a match can span markup", () => {
    const root = mount("<p>hel<strong>lo there</strong></p>")
    const { haystack, slices } = collectVisibleText(root)
    expect(haystack).toBe("hello there")
    expect(slices.length).toBeGreaterThan(1)
    const range = rangeFromOffsets(slices, 0, 5)
    expect(range?.toString()).toBe("hello")
  })

  it("skips aria-hidden copies and data-find-ignore subtrees", () => {
    const root = mount(
      `<p>visible</p><p aria-hidden="true">hidden-copy</p><p data-find-ignore="">clock</p>`,
    )
    expect(collectVisibleText(root).haystack).toBe("visible")
  })

  it("skips script and input values", () => {
    const root = mount(`<p>keep</p><script>drop()</script><textarea>nope</textarea>`)
    expect(collectVisibleText(root).haystack).toBe("keep")
  })
})

describe("applyFindHighlights", () => {
  it("counts hits even when the CSS Highlight API is missing", () => {
    const root = mount("<p>alpha beta alpha</p>")
    expect(supportsCssHighlights()).toBe(false)
    const painted = applyFindHighlights(root, "alpha", 0)
    expect(painted.total).toBe(2)
    expect(painted.current?.toString()).toBe("alpha")
    const second = applyFindHighlights(root, "alpha", 1)
    expect(second.current?.toString()).toBe("alpha")
    expect(second.current?.startOffset).not.toBe(painted.current?.startOffset)
  })

  it("is empty for a query that is not on screen", () => {
    const root = mount("<p>alpha</p>")
    expect(applyFindHighlights(root, "zzz", 0)).toEqual({ total: 0, painted: 0 })
  })

  it("counts every hit but only materialises a window of ranges", () => {
    const root = mount(`<p>${"x".repeat(200)}</p>`)
    const painted = applyFindHighlights(root, "x", 0)
    expect(painted.total).toBe(200)
    expect(painted.painted).toBe(FIND_PAINT_CAP)
    const last = applyFindHighlights(root, "x", 199)
    expect(last.total).toBe(200)
    expect(last.painted).toBe(FIND_PAINT_CAP)
    expect(last.current?.startOffset).toBeGreaterThan(painted.current?.startOffset ?? 0)
  })

  it("maps a hit in a late text node without walking the slice list twice", () => {
    const parts = Array.from({ length: 40 }, (_, i) => `<span>n${i}</span>`)
    const root = mount(`<p>${parts.join("")}</p>`)
    const { haystack, slices } = collectVisibleText(root)
    expect(slices.length).toBe(40)
    const start = haystack.lastIndexOf("n39")
    expect(rangeFromOffsets(slices, start, start + 3)?.toString()).toBe("n39")
  })

  it("does not construct a Highlight with every match as an argument", () => {
    class HighlightSpy {
      static maxArgs = 0
      constructor(...ranges: Range[]) {
        HighlightSpy.maxArgs = Math.max(HighlightSpy.maxArgs, ranges.length)
      }
      add() {}
    }
    const prevHighlight = (window as Window & { Highlight?: unknown }).Highlight
    const prevCSS = Object.getOwnPropertyDescriptor(window, "CSS")
    ;(window as Window & { Highlight: unknown }).Highlight = HighlightSpy
    Object.defineProperty(window, "CSS", {
      configurable: true,
      value: { highlights: { set() {}, delete() {} } },
    })
    try {
      const root = mount(`<p>${"x".repeat(40)}</p>`)
      applyFindHighlights(root, "x", 0)
      expect(HighlightSpy.maxArgs).toBeLessThanOrEqual(1)
    } finally {
      if (prevHighlight) {
        ;(window as Window & { Highlight: unknown }).Highlight = prevHighlight
      } else {
        delete (window as Window & { Highlight?: unknown }).Highlight
      }
      if (prevCSS) Object.defineProperty(window, "CSS", prevCSS)
      else delete (window as Window & { CSS?: unknown }).CSS
    }
  })

  it("builds one range per non-overlapping hit", () => {
    const root = mount("<p>aa aa</p>")
    const { haystack, slices } = collectVisibleText(root)
    const ranges = rangesForMatches(slices, [
      { start: 0, end: 2 },
      { start: 3, end: 5 },
    ])
    expect(haystack).toBe("aa aa")
    expect(ranges.map((r) => r.toString())).toEqual(["aa", "aa"])
  })
})

describe("clearFindHighlights", () => {
  it("does not throw without a highlight registry", () => {
    expect(() => clearFindHighlights()).not.toThrow()
    expect(FIND_HIGHLIGHT).toBe("zwai-find")
    expect(FIND_HIGHLIGHT_CURRENT).toBe("zwai-find-current")
  })
})

describe("scrollRangeIntoView", () => {
  it("moves the scroller so a hit is not left under the find bar", () => {
    const scroller = document.createElement("div")
    Object.defineProperty(scroller, "clientHeight", { value: 200 })
    Object.defineProperty(scroller, "scrollHeight", { value: 800 })
    scroller.scrollTop = 0
    scroller.scrollTo = ((opts: ScrollToOptions) => {
      scroller.scrollTop = Number(opts.top ?? 0)
    }) as typeof scroller.scrollTo
    scroller.getBoundingClientRect = () =>
      ({ top: 0, left: 0, width: 300, height: 200, bottom: 200, right: 300, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

    const root = mount("<p>needle</p>")
    const { slices } = collectVisibleText(root)
    const range = rangeFromOffsets(slices, 0, 6)
    expect(range).toBeDefined()
    range!.getBoundingClientRect = () =>
      ({ top: 500, left: 0, width: 40, height: 16, bottom: 516, right: 40, x: 0, y: 500, toJSON: () => ({}) }) as DOMRect

    scrollRangeIntoView(scroller, range!, window)
    expect(scroller.scrollTop).toBeGreaterThan(0)
  })
})
