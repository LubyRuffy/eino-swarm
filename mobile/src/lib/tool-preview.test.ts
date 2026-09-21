import { describe, expect, it } from "vitest"

import { dropPackedJson, jsonPreview, looksPacked, rosterCounts, rosterLines } from "./tool-preview"

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

  it("counts a wait roster without using ids or elapsed_ms as the preview", () => {
    const raw = JSON.stringify({
      elapsed_ms: 0,
      timed_out: false,
      agents: [
        { agent_id: "w1", role: "worker", status: "done", elapsed_ms: 0 },
        { agent_id: "w2", role: "helper", status: "failed", elapsed_ms: 0 },
        { agent_id: "w3", role: "helper", status: "running", elapsed_ms: 12 },
      ],
    })
    expect(rosterCounts(raw)).toEqual({ done: 1, failed: 1, running: 1, undelivered: 0 })
    expect(rosterLines(raw).join("\n")).toBe("worker done\nhelper failed\nhelper running")
    expect(rosterLines(raw).join(" ")).not.toContain("elapsed_ms")
    expect(rosterLines(raw).join(" ")).not.toContain("w1")
    expect(jsonPreview(raw)).toBe("")
  })

  it("strips a trailing packed envelope from prose", () => {
    const tail = JSON.stringify({
      agents: [{ agent_id: "w1", role: "worker", status: "done", elapsed_ms: 0 }],
    })
    expect(dropPackedJson("keep going\n" + tail)).toBe("keep going")
    expect(dropPackedJson("keep going " + tail)).toBe("keep going")
    expect(dropPackedJson("keep going {\"n\":1}")).toBe("keep going {\"n\":1}")
    expect(dropPackedJson(tail)).toBe("")
    expect(dropPackedJson('{"ok":true}')).toBe('{"ok":true}')
    expect(dropPackedJson("keep going")).toBe("keep going")
  })
})
