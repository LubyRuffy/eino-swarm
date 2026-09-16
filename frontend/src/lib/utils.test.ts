import { describe, expect, it } from "vitest"

import { formatMessageTime, formatTime } from "./utils"

describe("formatMessageTime", () => {
  it("is empty when the clock cannot read the stamp", () => {
    expect(formatMessageTime("nope")).toBe("")
  })

  // Codex/Hermes sit a calendar time under the bubble. Seconds make every
  // message look like it just happened; the timeline already has those.
  it("names the day and the clock, not the seconds", () => {
    const at = "2026-09-09T12:34:56.789Z"
    const got = formatMessageTime(at)
    expect(got.length).toBeGreaterThan(0)
    expect(got).not.toMatch(/:\d{2}:\d{2}/)
    expect(got).not.toBe(formatTime(at))
  })
})
