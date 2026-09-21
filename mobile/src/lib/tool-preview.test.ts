import { describe, expect, it } from "vitest"

import { jsonPreview, looksPacked } from "./tool-preview"

describe("tool preview", () => {
  it("pulls findings out of a packed result instead of the envelope", () => {
    const preview = jsonPreview(JSON.stringify({ findings: "one thing changed", quiet: false }))
    expect(preview).toBe("one thing changed")
    expect(preview.startsWith("{")).toBe(false)
  })

  it("does not use a prompt blob as the one-line summary", () => {
    const preview = jsonPreview(
      JSON.stringify({
        id: "sch_x",
        kind: "thread",
        prompt: "do the long check then cat status.json",
        title: "periodic",
      }),
    )
    expect(preview).toBe("periodic")
    expect(preview).not.toContain("status.json")
  })

  it("treats braces as packed", () => {
    expect(looksPacked('{"ok":true}')).toBe(true)
    expect(looksPacked("ok")).toBe(false)
  })
})
