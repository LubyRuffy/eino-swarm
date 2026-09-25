import { afterEach, describe, expect, it } from "vitest"

import { androidBackLayer, installAndroidBack } from "./android-back"

describe("android system back", () => {
  afterEach(() => {
    delete window.__zwaiAndroidBack
  })

  it("pops the add-PC sheet before a conversation", () => {
    expect(androidBackLayer({ sheet: true, compose: true, thread: true })).toBe("sheet")
    expect(androidBackLayer({ sheet: true, compose: false, thread: false })).toBe("sheet")
  })

  // New conversation is opened from the inbox and paints over a resumed
  // thread, so back leaves the compose screen before it leaves that thread.
  it("pops the new-conversation screen before a conversation", () => {
    expect(androidBackLayer({ sheet: false, compose: true, thread: true })).toBe("compose")
    expect(androidBackLayer({ sheet: false, compose: true, thread: false })).toBe("compose")
  })

  it("pops a conversation back to the inbox", () => {
    expect(androidBackLayer({ sheet: false, compose: false, thread: true })).toBe("thread")
  })

  it("pops a direct-model thread back to the chat list", () => {
    expect(androidBackLayer({ sheet: false, compose: false, thread: false, chat: true })).toBe("chat")
    expect(androidBackLayer({ sheet: true, compose: false, thread: false, chat: true })).toBe("sheet")
  })

  it("finishes only from the inbox or the unbound scan screen", () => {
    expect(androidBackLayer({ sheet: false, compose: false, thread: false })).toBe("root")
  })

  it("installs one hook and removes it on unmount", () => {
    const first = () => true
    const stop = installAndroidBack(first)
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    const second = () => false
    const stopSecond = installAndroidBack(second)
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    stop()
    expect(window.__zwaiAndroidBack?.()).toBe(false)
    stopSecond()
    expect(window.__zwaiAndroidBack).toBeUndefined()
  })

  it("lets an open overlay consume Back before the app root", () => {
    let rootCalls = 0
    const stopRoot = installAndroidBack(() => { rootCalls++; return false })
    const stopOverlay = installAndroidBack(() => true)
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    expect(rootCalls).toBe(0)
    stopOverlay()
    expect(window.__zwaiAndroidBack?.()).toBe(false)
    expect(rootCalls).toBe(1)
    stopRoot()
    expect(window.__zwaiAndroidBack).toBeUndefined()
  })
})
