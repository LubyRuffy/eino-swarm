import { describe, expect, it } from "vitest"

import {
  clearLastThreadId,
  clearLink,
  loadLastThreadId,
  saveLastThreadId,
  saveLink,
} from "./store"

describe("last thread", () => {
  it("remembers the id the phone last opened and forgets it on unlink", () => {
    expect(loadLastThreadId()).toBe("")
    saveLastThreadId("  t1  ")
    expect(loadLastThreadId()).toBe("t1")
    saveLastThreadId("")
    expect(loadLastThreadId()).toBe("t1")
    saveLink({
      hubURL: "https://hub.example",
      ticket: "tick",
      hostPub: "pub",
      sessionID: "sid",
      fingerprint: "fp",
    })
    clearLink()
    expect(loadLastThreadId()).toBe("")
  })

  it("clears an explicit last id without touching a missing link", () => {
    saveLastThreadId("t2")
    clearLastThreadId()
    expect(loadLastThreadId()).toBe("")
  })
})
