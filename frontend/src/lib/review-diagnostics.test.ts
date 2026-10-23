import { describe, expect, it } from "vitest"

import { reviewTraceMetadata } from "./review-diagnostics"
import { parseReview, reviewNotice, reviewPanelHint } from "./transcript-review"
import type { SwarmEvent } from "./types"

const event = (body: unknown) => ({ text: JSON.stringify(body), kind: "memory_review" }) as SwarmEvent
const failure = {
  changed: false,
  err: "The review ended without submitting a complete memory proposal.",
  stop_reason: "iteration_limit",
  attempt_count: 8,
  refused: [
    { index: 0, reason: "private quote token=secret", code: "evidence", attempt: 1, sources: [2] },
    { index: 0, reason: "number 53 not grounded", code: "numeric", attempt: 2 },
    { index: 0, reason: "notes over budget", code: "capacity", attempt: 8, usage: "2200/2200", over_by: 129 },
  ],
  attempts: [{ number: 8, operations: [{ target: "memory", action: "add", sources: [2] }], status: "refused" }],
}

describe("review diagnostics", () => {
  it("keeps the tidy's stale candidates visible in the turn Trace", () => {
    const got = parseReview(event({ changed: false, stale_candidates: ["older-procedure", "", 1] }))
    expect(got?.stale_candidates).toEqual(["older-procedure"])
    expect(JSON.parse(reviewTraceMetadata(got!) ?? "{}")).toMatchObject({ stale_candidates: ["older-procedure"] })
  })

  it("retains structured refusal and bounded attempt metadata through replay", () => {
    const got = parseReview(event(failure))
    expect(got).toMatchObject({ stop_reason: "iteration_limit", attempt_count: 8, refused: failure.refused, attempts: failure.attempts })
  })

  it("shows all rejection categories, capacity gap and the bounded stop without exposing proposal text", () => {
    const got = parseReview(event(failure))
    const en = reviewPanelHint(got)
    expect(en).toContain("8 attempts")
    expect(en).toContain("Evidence reference did not pass validation")
    expect(en).toContain("Number not supported by the cited evidence")
    expect(en).toContain("2200/2200")
    expect(en).toContain("129 characters over")
    expect(en).toContain("Notes were not saved")
    expect(en).not.toMatch(/secret|53|private quote/)
    const zh = reviewPanelHint(got, "zh")
    expect(zh).toContain("8 次提案")
    expect(zh).toContain("证据引用未通过校验")
    expect(zh).toContain("超出 129 字符")
    expect(reviewNotice(got, "zh")).toBe(zh)
  })

  it("does not turn a repaired proposal into a failure", () => {
    const got = parseReview(event({ ...failure, changed: true, err: undefined, stop_reason: "completed", notes: { add: 1 } }))
    expect(reviewNotice(got)).toBe("Memory updated: 1 note stored.")
    expect(reviewPanelHint(got)).toBe("Review finished.")
  })

  it("states that committed skill work survives notes refusal", () => {
    const got = parseReview(event({ ...failure, changed: true, skills: [{ target: "skill_manage", action: "patch", name: "retained-skill" }] }))
    expect(reviewPanelHint(got)).toContain("Skill changes were saved")
    expect(reviewPanelHint(got)).toContain("Notes were not saved")
  })

  it("reads old refusal events without reflecting untrusted legacy reasons", () => {
    const got = parseReview(event({ changed: false, err: "token=secret", refused: [{ index: 0, reason: "evidence quote secret does not match" }] }))
    expect(reviewPanelHint(got)).toContain("Evidence reference did not pass validation")
    expect(reviewPanelHint(got)).not.toContain("secret")
    expect(reviewPanelHint(parseReview(event({ changed: false, err: "provider token=secret" })))).toBe("Memory review failed. Open the turn Trace for details.\nNotes were not saved.")
  })

  it("drops malformed optional diagnostics without dropping a valid review", () => {
    const got = parseReview(event({ changed: false, stop_reason: "unsafe", attempt_count: -2, refused: [{ index: "bad", reason: 1 }, null], attempts: [null] }))
    expect(got).toMatchObject({ changed: false })
    expect(got?.refused).toEqual([])
    expect(got?.attempts).toEqual([])
    expect(got?.stop_reason).toBeUndefined()
    expect(got?.attempt_count).toBeUndefined()
  })
})

describe("legacy review limits", () => {
  it("keeps whole-batch refusals and extracts only safe capacity numbers", () => {
    const got = parseReview(event({ changed: false, err: "failed", refused: [{ index: -1, reason: "memory is at 140/150 characters; this write exceeds the limit by 24. secret body" }] }))
    expect(got?.refused?.[0]).toMatchObject({ index: -1, usage: "140/150", over_by: 24 })
    expect(reviewPanelHint(got)).toContain("Notes capacity exceeded (140/150), 24 characters over")
    expect(reviewPanelHint(got)).not.toContain("secret body")
  })

  it("keeps per-note cap failures separate from the total notes budget", () => {
    const got = parseReview(event({ changed: false, err: "failed", refused: [{ index: 0, reason: "memory: a note is 80 characters; the per-note cap is 64. source text secret" }] }))
    expect(reviewPanelHint(got)).toContain("Proposal did not pass validation")
    expect(reviewPanelHint(got)).not.toContain("secret")
  })
})

describe("localized successful review notices", () => {
  it("keeps the pre-existing Chinese memory and shared-library chrome", () => {
    expect(reviewNotice({ changed: true }, "zh")).toBe("记忆已更新。")
    expect(reviewNotice({ changed: true, library: true }, "zh")).toBe("已写入共享技能库。")
    expect(reviewNotice({ changed: true, library: true, skills: [{ target: "skill_manage", action: "create", name: "a-procedure" }] }, "zh")).toBe('已写入共享技能库：skill "a-procedure" recorded。')
  })
})
