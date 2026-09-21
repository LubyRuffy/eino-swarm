import { describe, expect, it } from "vitest"

import {
  bindingFingerprint,
  bindingTitle,
  bindingUsesLastSeen,
  bindingWhen,
} from "./remote-binding"
import type { RemoteBinding } from "./types"

const base: RemoteBinding = {
  id: "b1",
  device_fp: "aa11bb22cc33dd44",
  created_at: "2026-09-20T16:00:00Z",
  session_id: "s1",
}

describe("bindingTitle", () => {
  it("prefers the reported model and keeps the fingerprint as the subtitle", () => {
    const labeled = { ...base, device: "Phone 1.0 Device", last_seen: "2026-09-20T16:03:32Z" }
    expect(bindingTitle(labeled)).toBe("Phone 1.0 Device")
    expect(bindingFingerprint(labeled)).toBe("aa11bb22cc33dd44")
    expect(bindingWhen(labeled)).toBe("2026-09-20T16:03:32Z")
    expect(bindingUsesLastSeen(labeled)).toBe(true)
    expect(bindingTitle(base)).toBe("aa11bb22cc33dd44")
    expect(bindingFingerprint(base)).toBe("")
    expect(bindingWhen(base)).toBe("2026-09-20T16:00:00Z")
    expect(bindingUsesLastSeen(base)).toBe(false)
  })
})
