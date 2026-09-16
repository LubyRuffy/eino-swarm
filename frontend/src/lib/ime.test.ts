import { afterEach, describe, expect, it, vi } from "vitest"

import { afterImeSettles, enterSendsMessage, IME_KEYCODE } from "./ime"

function key(
  partial: Partial<Pick<KeyboardEvent, "key" | "shiftKey" | "isComposing" | "keyCode">> = {},
): Pick<KeyboardEvent, "key" | "shiftKey" | "isComposing" | "keyCode"> {
  return {
    key: "Enter",
    shiftKey: false,
    isComposing: false,
    keyCode: 13,
    ...partial,
  }
}

describe("enterSendsMessage", () => {
  it("sends on a settled Enter", () => {
    expect(enterSendsMessage(key(), false)).toBe(true)
  })

  it("leaves Shift+Enter as a newline", () => {
    expect(enterSendsMessage(key({ shiftKey: true }), false)).toBe(false)
  })

  it("does not send other keys", () => {
    expect(enterSendsMessage(key({ key: "a" }), false)).toBe(false)
  })

  // The IME is still composing: Enter confirms leftover Latin (keep English
  // as typed) instead of sending the draft.
  it("does not send while the IME is composing", () => {
    expect(enterSendsMessage(key({ isComposing: true }), false)).toBe(false)
    expect(enterSendsMessage(key(), true)).toBe(false)
  })

  it("does not send the IME Process key", () => {
    expect(enterSendsMessage(key({ keyCode: IME_KEYCODE }), false)).toBe(false)
  })
})

describe("afterImeSettles", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("runs after this frame so a confirming Enter can land first", () => {
    const raf = vi.fn((cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("requestAnimationFrame", raf)
    const settled = vi.fn()
    afterImeSettles(settled)
    expect(raf).toHaveBeenCalledTimes(1)
    expect(settled).toHaveBeenCalledTimes(1)
  })

  it("can cancel before the frame", () => {
    const cancel = vi.fn()
    vi.stubGlobal("requestAnimationFrame", vi.fn(() => 7))
    vi.stubGlobal("cancelAnimationFrame", cancel)
    const settled = vi.fn()
    const abort = afterImeSettles(settled)
    abort()
    expect(cancel).toHaveBeenCalledWith(7)
    expect(settled).not.toHaveBeenCalled()
  })
})
