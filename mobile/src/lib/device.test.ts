import { describe, expect, it } from "vitest"

import { clipDeviceLabel, DEVICE_LABEL_MAX, deviceFacts, deviceLabel } from "./device"

describe("deviceLabel", () => {
  it("builds a one-line model from platform and UA, not a marketing name", () => {
    expect(
      deviceLabel({
        platform: "android",
        userAgent: "Mozilla/5.0 (Linux; Android 14; Device Build/TEST) AppleWebKit/537.36",
      }),
    ).toBe("Android 14 Device")
    expect(
      deviceLabel({
        platform: "ios",
        userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_2 like Mac OS X) AppleWebKit/605.1.15",
      }),
    ).toBe("iOS 18.2 iPhone")
    expect(
      deviceLabel({
        platform: "ios",
        userAgent: "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15",
      }),
    ).toBe("iOS 17.5 iPad")
    expect(deviceLabel({ platform: "web", userAgent: "Mozilla/5.0" })).toBe("Web")
    expect(deviceLabel({ platform: "android", userAgent: "Mozilla/5.0 (Linux; Android 10; wv)" })).toBe(
      "Android 10",
    )
  })

  it("reports the hardware model beside the name and does not split the name to get it", () => {
    const android = "Mozilla/5.0 (Linux; Android 14; Handset Build/TEST) AppleWebKit/537.36"
    expect(deviceFacts({ platform: "android", userAgent: android })).toEqual({
      name: "Android 14 Handset",
      model: "Handset",
    })
    expect(deviceFacts({ platform: "ios", userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_2 like Mac OS X)" })).toEqual({
      name: "iOS 18.2 iPhone",
      model: "iPhone",
    })
    const unnamed = deviceFacts({
      platform: "android",
      userAgent: "Mozilla/5.0 (Linux; Android 10; wv)",
    })
    expect(unnamed).toEqual({ name: "Android 10", model: "" })
    expect(unnamed.model).not.toBe("10")
  })

  it("does not invent a sample phrase when the UA is empty", () => {
    const got = deviceLabel({ platform: "web", userAgent: "" })
    expect(got).toBe("Web")
    expect(got.toLowerCase()).not.toMatch(/notes\.md|make a table/)
  })

  it("clips control junk and a novel-length UA", () => {
    expect(clipDeviceLabel("  Phone\n1.0\tDevice  ")).toBe("Phone 1.0 Device")
    const long = "x".repeat(DEVICE_LABEL_MAX + 12)
    expect([...clipDeviceLabel(long)].length).toBe(DEVICE_LABEL_MAX)
  })
})
