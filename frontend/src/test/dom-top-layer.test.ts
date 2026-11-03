import { describe, expect, it } from "vitest"

describe("jsdom top-layer matching", () => {
  it.each([":modal", ":fullscreen"])("does not recursively re-enter the native %s match", (selector) => {
    const node = document.createElement("div")
    const native = Element.prototype.matches
    let calls = 0
    // Bound the old nwsapi recursion so this regression observes its cause
    // without waiting for a stack overflow or delaying every Radix fixture.
    Object.defineProperty(node, "matches", {
      value(query: string) {
        calls++
        return calls > 4 ? false : native.call(this, query)
      },
    })
    expect(node.matches(selector)).toBe(false)
    expect(calls).toBe(1)
  })

  it("preserves class, compound, disabled and real dialog attribute matching", () => {
    const button = document.createElement("button")
    button.className = "selected"
    button.disabled = true
    expect(button.matches(".selected")).toBe(true)
    expect(button.matches("button.selected:disabled")).toBe(true)
    expect(button.matches("button:not(:disabled)")).toBe(false)
    const dialog = document.createElement("dialog")
    expect(dialog.matches("dialog[open]")).toBe(false)
    dialog.open = true
    expect(dialog.matches("dialog[open]")).toBe(true)
  })

  it("preserves invalid-selector rejection", () => {
    expect(() => document.createElement("div").matches("[")).toThrow()
  })
})
