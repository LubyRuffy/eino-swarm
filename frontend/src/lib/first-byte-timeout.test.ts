import { describe, expect, it } from "vitest"

import { firstByteTimeoutDuration, firstByteTimeoutSettingsKey } from "./first-byte-timeout"

describe("first-byte timeout guidance", () => {
  it.each(["30s", "2m0s", "1h2m3.5s", "250ms", "1µs"])("keeps the actual Go duration %s", (duration) => {
    expect(firstByteTimeoutDuration(`the model sent no first byte within ${duration}`)).toBe(duration)
  })

  it.each([
    "the model sent no data for 30s; raise the provider request timeout in Settings, or lower thinking",
    "timeout awaiting response headers",
    "the model sent no first byte within later",
    "the model sent no first byte within 30s extra text",
    "the model service is unavailable",
  ])("does not mistake another failure for a first-byte timeout: %s", (text) => {
    expect(firstByteTimeoutDuration(text)).toBeUndefined()
  })

  it("scopes the settings anchor by provider and leaves an unknown provider unselected", () => {
    expect(firstByteTimeoutSettingsKey("provider:2")).toBe("first_byte_timeout_seconds:provider:2")
    expect(firstByteTimeoutSettingsKey("")).toBe("")
  })
})
