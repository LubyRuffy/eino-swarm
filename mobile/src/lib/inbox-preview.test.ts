import { describe, expect, it } from "vitest"

import { inboxPreview, scheduleToolNotice } from "./inbox-preview"

describe("inboxPreview", () => {
  it("pulls findings and drops schedule / memory jargon", () => {
    expect(
      inboxPreview(`schedule_wake({"every_s":900,"id":"sch_x","prompt":"Continue the wait."})`),
    ).toBe("")
    expect(inboxPreview(`report_schedule({"findings":"","keep":true})`)).toBe("")
    expect(inboxPreview(`report_schedule({"findings":"one thing changed","keep":true})`)).toBe(
      "one thing changed",
    )
    expect(inboxPreview(`memory({"action":"add","content":"a durable fact"})`)).toBe("")
    expect(inboxPreview("This turn is a scheduled check.\n\nContinue the wait.")).toBe("")
    expect(inboxPreview(`exec({"command":"echo hi"})`)).toBe("echo hi")
    expect(inboxPreview("read(notes.md)")).toBe("notes.md")
    expect(inboxPreview("working on it")).toBe("working on it")
    expect(inboxPreview("Hello (aside)")).toBe("Hello (aside)")
    expect(inboxPreview("read")).toBe("read")
    expect(inboxPreview("")).toBe("")
  })

  it("strips tool envelopes with spaces in the JSON", () => {
    expect(inboxPreview(`read({"file_path": "pkg/alpha.go"})`)).toBe("pkg/alpha.go")
    expect(inboxPreview(`read({"file_path": "pkg/alpha.go"})`)).not.toContain("read(")
    expect(inboxPreview(`schedule_wake({"every_s": 900, "id": "sch_x"})`)).toBe("")
    expect(
      inboxPreview(`report_schedule({"findings": "one thing changed", "keep": true})`),
    ).toBe("one thing changed")
    expect(inboxPreview(`report_schedule({"findings": "", "keep": true})`)).toBe("")
  })
})

describe("scheduleToolNotice", () => {
  it("hides wakes and surfaces findings as a notice", () => {
    expect(scheduleToolNotice("schedule_wake", `{"every_s":30}`)).toBe("")
    expect(scheduleToolNotice("cancel_schedule", `{"id":"sch_1"}`)).toBe("")
    expect(scheduleToolNotice("report_schedule", `{"findings":""}`)).toBe("")
    expect(scheduleToolNotice("report_schedule", `{"findings":"one thing changed"}`)).toBe(
      "one thing changed",
    )
    expect(scheduleToolNotice("exec", `{"command":"echo hi"}`)).toBeUndefined()
  })
})
