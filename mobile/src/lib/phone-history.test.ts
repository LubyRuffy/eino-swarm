import { describe, expect, it } from "vitest"
import { hasPhoneHistoryContent } from "./phone-history"
import type { CompactBlock } from "./transcript"

describe("phone opening content", () => {
  const block = (kind: CompactBlock["kind"], text = "", extra = {}): CompactBlock => ({ id: "b", kind, text, ...extra })
  it("ignores separators, blank bubbles, worker output and omitted bookkeeping", () => {
    expect(hasPhoneHistoryContent([
      block("turn_end", "2026-01-01T00:00:00Z"), block("answer", "  "),
      block("answer", "Worker output", { agentId: "worker" }), block("spawn"),
    ])).toBe(false)
  })
  it.each([
    block("user", "", { hasImages: true }), block("answer", "Answer"),
    block("reasoning", "Checking"), block("notice", "Paused"),
    block("error", "Failed"), block("question", "Choose"),
  ])("keeps visible $kind content without an older read", (content) => {
    expect(hasPhoneHistoryContent([content])).toBe(true)
  })
})
