import { afterEach, describe, expect, it } from "vitest"

import { androidBackLayer, installAndroidBack } from "./android-back"

describe("android system back", () => {
  afterEach(() => {
    delete window.__zwaiAndroidBack
  })

  it("pops the add-PC sheet before a conversation", () => {
    expect(androidBackLayer({ sheet: true, thread: true })).toBe("sheet")
    expect(androidBackLayer({ sheet: true, thread: false })).toBe("sheet")
  })

  it("pops a conversation back to the inbox", () => {
    expect(androidBackLayer({ sheet: false, thread: true })).toBe("thread")
  })

  it("finishes only from the inbox or the unbound scan screen", () => {
    expect(androidBackLayer({ sheet: false, thread: false })).toBe("root")
  })

  it("installs one hook and removes it on unmount", () => {
    const first = () => true
    const stop = installAndroidBack(first)
    expect(window.__zwaiAndroidBack).toBe(first)
    const second = () => false
    const stopSecond = installAndroidBack(second)
    stop()
    expect(window.__zwaiAndroidBack).toBe(second)
    stopSecond()
    expect(window.__zwaiAndroidBack).toBeUndefined()
  })
})
