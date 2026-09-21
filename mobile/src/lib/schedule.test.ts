import { describe, expect, it } from "vitest"

import {
  applyWakeDetail,
  isLiveThreadWake,
  laterScheduleDue,
  parseArmedSchedule,
  scheduleHeadline,
} from "./schedule"

describe("parseArmedSchedule", () => {
  it("reads the chip payload and ignores junk", () => {
    expect(parseArmedSchedule("not-json")).toBeUndefined()
    expect(parseArmedSchedule("{}")).toBeUndefined()
    const got = parseArmedSchedule(
      JSON.stringify({
        id: "sch_1",
        kind: "thread",
        title: "wake",
        prompt: "Continue the wait.",
        thread_id: "t1",
        status: "active",
        next_run_at: "2026-09-21T02:00:00Z",
      }),
    )
    expect(got?.id).toBe("sch_1")
    expect(isLiveThreadWake(got)).toBe(true)
    expect(isLiveThreadWake({ id: "sch_1", kind: "standalone" })).toBe(false)
    expect(
      applyWakeDetail(
        { id: "t1", title: "one" },
        { kind: "schedule", text: JSON.stringify({ id: "sch_x", kind: "thread", thread_id: "other" }) },
      ).waiting,
    ).toBeUndefined()
    const live = applyWakeDetail(
      { id: "t1", title: "one", waiting: true, wake: { id: "sch_1" } },
      { kind: "schedule_cancelled", text: "sch_other" },
    )
    expect(live.waiting).toBe(true)
    expect(applyWakeDetail({ id: "t1", title: "one" }, { kind: "user_message", text: "hi" })).toEqual({
      id: "t1",
      title: "one",
    })
  })
})

describe("scheduleHeadline", () => {
  it("falls back to the prompt when the title is empty", () => {
    expect(scheduleHeadline({ title: "wake", prompt: "Continue the wait." })).toBe("wake")
    expect(scheduleHeadline({ title: "  ", prompt: "Continue the wait." })).toBe(
      "Continue the wait.",
    )
  })
})

describe("laterScheduleDue", () => {
  it("does not rewind a later due time", () => {
    expect(laterScheduleDue("2026-09-21T04:00:00Z", "2026-09-21T01:00:00Z")).toBe(
      "2026-09-21T04:00:00Z",
    )
    expect(laterScheduleDue("nope", "2026-09-21T01:00:00Z")).toBe("2026-09-21T01:00:00Z")
    expect(laterScheduleDue("2026-09-21T01:00:00Z", "nope")).toBe("2026-09-21T01:00:00Z")
    expect(laterScheduleDue("", "")).toBe("")
  })
})

describe("applyWakeDetail", () => {
  it("arms, keeps the later clock, and clears on fire or cancel", () => {
    const payload = JSON.stringify({
      id: "sch_1",
      kind: "thread",
      title: "wake",
      prompt: "Continue the wait.",
      thread_id: "t1",
      status: "active",
      next_run_at: "2026-09-21T01:00:00Z",
    })
    let d = applyWakeDetail(
      { id: "t1", title: "one" },
      { kind: "schedule", text: payload },
    )
    expect(d.waiting).toBe(true)
    expect(d.wake?.id).toBe("sch_1")
    d = applyWakeDetail(d, {
      kind: "schedule",
      text: JSON.stringify({
        id: "sch_1",
        kind: "thread",
        status: "active",
        next_run_at: "2026-09-21T04:00:00Z",
      }),
    })
    expect(d.wake?.next_run_at).toBe("2026-09-21T04:00:00Z")
    d = applyWakeDetail(d, { kind: "schedule_cancelled", text: "sch_1" })
    expect(d.waiting).toBe(false)
    expect(d.wake).toBeUndefined()
    d = applyWakeDetail(
      { id: "t1", title: "one", waiting: true, wake: { id: "sch_1" } },
      { kind: "schedule_fired", text: "sch_1" },
    )
    expect(d.waiting).toBe(false)
  })
})
