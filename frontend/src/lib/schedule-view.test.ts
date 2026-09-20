import { describe, expect, it } from "vitest"

import type { Schedule } from "@/lib/types"
import {
  applyArmedSchedule,
  applyCancelledSchedule,
  inboxScheduleOrder,
  parseArmedSchedule,
  scheduleHeadline,
} from "./schedule-view"

function wait(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "thread",
    origin_thread_id: "th_1",
    thread_id: "th_1",
    project_id: "",
    provider_id: "",
    model: "",
    title: "",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 60,
    cron: "",
    status: "done",
    next_run_at: "2026-09-19T01:00:00.000Z",
    run_count: 1,
    max_runs: 0,
    created_by: "manager",
    created_at: "2026-09-19T00:00:00.000Z",
    updated_at: "2026-09-19T00:00:00.000Z",
    ...partial,
  }
}

describe("schedule-view", () => {
  it("uses the prompt when the title is empty", () => {
    expect(scheduleHeadline(wait())).toBe("Continue the wait.")
    expect(scheduleHeadline(wait({ title: "wake" }))).toBe("wake")
    expect(scheduleHeadline(wait({ title: "  " }))).toBe("Continue the wait.")
  })

  it("lists live waits ahead of done ones even when the done due time is earlier", () => {
    const done = wait({ id: "sch_old" })
    const live = wait({
      id: "sch_new",
      status: "active",
      next_run_at: "2026-09-19T04:00:00.000Z",
    })
    expect(inboxScheduleOrder([done, live]).map((row) => row.id)).toEqual([
      "sch_new",
      "sch_old",
    ])
  })

  it("applies an armed chip onto the list so the banner can paint before GET", () => {
    const payload = parseArmedSchedule(
      JSON.stringify({
        id: "sch_new",
        kind: "thread",
        title: "wake",
        prompt: "Continue the wait.",
        thread_id: "th_1",
        status: "active",
        next_run_at: "2026-09-19T04:00:00.000Z",
      }),
    )
    const rows = applyArmedSchedule([wait()], payload, "th_1")
    expect(rows[0]?.id).toBe("sch_new")
    expect(rows[0]?.status).toBe("active")
    expect(rows[0]?.prompt).toBe("Continue the wait.")
    expect(JSON.stringify(rows)).not.toMatch(/CI|deploy|GitHub/)
  })

  it("marks a cancelled chip so the banner drops without waiting for GET", () => {
    const rows = applyCancelledSchedule(
      [wait({ id: "sch_1", status: "active" })],
      "sch_1",
    )
    expect(rows[0]?.status).toBe("cancelled")
  })
})
