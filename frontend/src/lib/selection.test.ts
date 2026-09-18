import { afterEach, describe, expect, it } from "vitest"

import {
  QUOTE_SOURCE_ATTR,
  holdSelection,
  menuPosition,
  nextMenuSelection,
  normalizeSelectedText,
  readTextSelection,
} from "./selection"

function selectAll(el: HTMLElement) {
  const range = document.createRange()
  range.selectNodeContents(el)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}

function quoteSource(text: string): HTMLElement {
  const el = document.createElement("div")
  el.setAttribute(QUOTE_SOURCE_ATTR, "")
  el.textContent = text
  document.body.appendChild(el)
  return el
}

afterEach(() => {
  document.body.style.userSelect = ""
  document.body.style.webkitUserSelect = ""
  document.body.replaceChildren()
  window.getSelection()?.removeAllRanges()
})

describe("holdSelection", () => {
  // Dragging the panel border is a pointer move over the transcript. Without
  // this, WebKit treats that move as a click-and-drag and paints a selection.
  it("clears an existing selection and blocks a new one until released", () => {
    const p = document.createElement("p")
    p.textContent = "selectable"
    document.body.appendChild(p)
    const range = document.createRange()
    range.selectNodeContents(p)
    window.getSelection()?.addRange(range)
    expect(window.getSelection()?.toString()).toBe("selectable")

    const release = holdSelection()
    expect(window.getSelection()?.toString()).toBe("")
    expect(document.body.style.userSelect).toBe("none")
    expect(document.body.style.webkitUserSelect).toBe("none")

    const blocked = new Event("selectstart", { cancelable: true })
    document.dispatchEvent(blocked)
    expect(blocked.defaultPrevented).toBe(true)

    release()
    expect(document.body.style.userSelect).toBe("")
    const allowed = new Event("selectstart", { cancelable: true })
    document.dispatchEvent(allowed)
    expect(allowed.defaultPrevented).toBe(false)
  })

  it("puts back the inline user-select it found", () => {
    document.body.style.userSelect = "text"
    const release = holdSelection()
    expect(document.body.style.userSelect).toBe("none")
    release()
    expect(document.body.style.userSelect).toBe("text")
  })
})

describe("normalizeSelectedText", () => {
  it("keeps inner newlines and strips nbsp padding", () => {
    expect(normalizeSelectedText("\u00a0alpha\r\nbeta\u00a0")).toBe("alpha\nbeta")
  })
})

describe("nextMenuSelection", () => {
  const live = {
    text: "alpha",
    rect: { top: 10, left: 10, width: 40, height: 12, bottom: 22 },
  }

  it("keeps the snapshot when the native selection collapses mid-stream", () => {
    expect(nextMenuSelection(live, null, false)).toEqual(live)
  })

  it("clears only after the user settles with nothing selected", () => {
    expect(nextMenuSelection(live, null, true)).toBeNull()
  })

  it("replaces the snapshot when a new quote-source range exists", () => {
    const next = { ...live, text: "beta" }
    expect(nextMenuSelection(live, next, false)).toEqual(next)
  })
})

describe("readTextSelection", () => {
  it("returns the text when the range sits inside a quote source", () => {
    const el = quoteSource("alpha beta")
    selectAll(el)
    expect(readTextSelection()?.text).toBe("alpha beta")
  })

  it("ignores a selection that is not in a quote source", () => {
    const el = document.createElement("p")
    el.textContent = "composer draft"
    document.body.appendChild(el)
    selectAll(el)
    expect(readTextSelection()).toBeNull()
  })

  it("ignores a range that starts inside a quote source and ends outside", () => {
    const source = quoteSource("inside")
    const outside = document.createElement("p")
    outside.textContent = "outside"
    document.body.appendChild(outside)
    const range = document.createRange()
    range.setStart(source.firstChild as Text, 0)
    range.setEnd(outside.firstChild as Text, 7)
    const sel = window.getSelection()
    sel?.removeAllRanges()
    sel?.addRange(range)
    expect(readTextSelection()).toBeNull()
  })
})

describe("menuPosition", () => {
  const rect = { top: 120, left: 40, width: 80, height: 20, bottom: 140 }

  it("places the pill above when there is room so it does not cover the words", () => {
    expect(menuPosition(rect, { width: 800, height: 600 })).toEqual({
      top: 112,
      left: 80,
      place: "above",
    })
  })

  it("drops below when the selection is against the top of the window", () => {
    const tight = { top: 4, left: 40, width: 80, height: 20, bottom: 24 }
    const pos = menuPosition(tight, { width: 800, height: 600 })
    expect(pos.place).toBe("below")
    expect(pos.top).toBeGreaterThan(tight.bottom)
  })
})
