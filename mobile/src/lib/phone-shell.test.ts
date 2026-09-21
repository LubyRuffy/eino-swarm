import { describe, expect, it } from "vitest"

import { phoneShell } from "./phone-shell"

describe("phoneShell", () => {
  it("keeps scan for a phone that has never bound", () => {
    expect(phoneShell(0)).toBe("scan")
  })

  it("paints the inbox chrome once any ticket is saved", () => {
    expect(phoneShell(1)).toBe("home")
    expect(phoneShell(2)).toBe("home")
  })
})
