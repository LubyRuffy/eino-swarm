import { describe, expect, it } from "vitest"

import { autoConnectTarget } from "./gateway-auto-connect"
import type { GatewayHost } from "./gateway-account"
import type { SavedLink } from "./store"

const host = (id: string, patch: Partial<GatewayHost> = {}): GatewayHost =>
  ({ id, name: id, hostname: id, version: "1", online: true, enabled: true, ...patch })
const saved = { fingerprint: "fp" } as SavedLink

describe("opening a computer right after sign-in", () => {
  it("picks the only online computer when the phone has none yet", () => {
    expect(autoConnectTarget([host("a")], [], "")?.id).toBe("a")
    // An offline or disabled sibling is not a choice the person could make.
    expect(autoConnectTarget([host("a"), host("b", { online: false }), host("c", { enabled: false })], [], "")?.id).toBe("a")
  })

  it("never chooses for the person when there is more than one to choose from", () => {
    expect(autoConnectTarget([host("a"), host("b")], [], "")).toBeUndefined()
  })

  it("does nothing without an online computer", () => {
    expect(autoConnectTarget([], [], "")).toBeUndefined()
    expect(autoConnectTarget([host("a", { online: false })], [], "")).toBeUndefined()
  })

  it("leaves a phone that already has a computer alone", () => {
    expect(autoConnectTarget([host("a")], [saved], "")).toBeUndefined()
    expect(autoConnectTarget([host("a")], [], "fp")).toBeUndefined()
  })
})
