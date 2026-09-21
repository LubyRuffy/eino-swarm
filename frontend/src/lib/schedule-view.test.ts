import { describe, expect, it } from "vitest"

import type { Schedule, ScheduleRun } from "@/lib/types"
import {
  applyArmedSchedule,
  applyCancelledSchedule,
  applyFiredThreadWake,
  inboxHiddenEndedCount,
  inboxScheduleOrder,
  inboxVisibleSchedules,
  isLiveSchedule,
  isLiveThreadWake,
  keepArmedWakes,
  laterScheduleDue,
  parseArmedSchedule,
  scheduleHeadline,
  unreadFindings,
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

  it("hides ended waits from the inbox unless they still have unread findings", () => {
    const done = wait({ id: "sch_old" })
    const cancelled = wait({ id: "sch_cancel", status: "cancelled" })
    const paused = wait({
      id: "sch_paused",
      status: "paused",
      next_run_at: "2026-09-19T03:00:00.000Z",
    })
    const live = wait({
      id: "sch_new",
      status: "active",
      next_run_at: "2026-09-19T04:00:00.000Z",
    })
    const rows = [done, cancelled, paused, live]
    expect(isLiveSchedule(live)).toBe(true)
    expect(isLiveSchedule(paused)).toBe(true)
    expect(isLiveSchedule(done)).toBe(false)
    expect(isLiveSchedule(cancelled)).toBe(false)
    expect(inboxVisibleSchedules(rows, [], false).map((row) => row.id)).toEqual([
      "sch_paused",
      "sch_new",
    ])
    expect(inboxHiddenEndedCount(rows, [])).toBe(2)
    const unread = [
      run({ id: "srun_old", schedule_id: "sch_old", thread_id: "th_old" }),
    ]
    expect(inboxVisibleSchedules(rows, unread, false).map((row) => row.id)).toEqual([
      "sch_paused",
      "sch_new",
      "sch_old",
    ])
    expect(inboxHiddenEndedCount(rows, unread)).toBe(1)
    expect(inboxVisibleSchedules(rows, unread, true).map((row) => row.id)).toEqual([
      "sch_paused",
      "sch_new",
      "sch_old",
      "sch_cancel",
    ])
  })

  it("does not rewind next_run_at when a replayed arm chip is older", () => {
    const live = wait({
      id: "sch_new",
      status: "active",
      next_run_at: "2026-09-19T04:00:00.000Z",
    })
    const payload = parseArmedSchedule(
      JSON.stringify({
        id: "sch_new",
        kind: "thread",
        status: "active",
        thread_id: "th_1",
        next_run_at: "2026-09-19T01:00:00.000Z",
      }),
    )
    const rows = applyArmedSchedule([live], payload, "th_1")
    expect(rows.find((row) => row.id === "sch_new")?.next_run_at).toBe(
      "2026-09-19T04:00:00.000Z",
    )
    expect(laterScheduleDue("2026-09-19T04:00:00.000Z", "2026-09-19T01:00:00.000Z")).toBe(
      "2026-09-19T04:00:00.000Z",
    )
    expect(laterScheduleDue("", "2026-09-19T01:00:00.000Z")).toBe(
      "2026-09-19T01:00:00.000Z",
    )
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

  it("treats a thread chip without kind/status as still live", () => {
    expect(isLiveThreadWake(parseArmedSchedule('{"id":"sch_1"}'))).toBe(true)
    expect(
      isLiveThreadWake(parseArmedSchedule('{"id":"sch_1","kind":"standalone"}')),
    ).toBe(false)
    expect(
      isLiveThreadWake(parseArmedSchedule('{"id":"sch_1","status":"cancelled"}')),
    ).toBe(false)
  })

  it("marks a cancelled chip so the banner drops without waiting for GET", () => {
    const rows = applyCancelledSchedule(
      [wait({ id: "sch_1", status: "active" })],
      "sch_1",
    )
    expect(rows[0]?.status).toBe("cancelled")
  })

  it("marks this conversation's live wake done when it fires", () => {
    const rows = applyFiredThreadWake(
      [
        wait({ id: "sch_live", status: "active" }),
        wait({ id: "sch_other", thread_id: "th_other", status: "active" }),
      ],
      "th_1",
    )
    expect(rows.find((row) => row.id === "sch_live")?.status).toBe("done")
    expect(rows.find((row) => row.id === "sch_other")?.status).toBe("active")
    expect(applyFiredThreadWake(rows, "")).toBe(rows)
  })

  it("keeps the open conversation's armed wait when GET is still empty", () => {
    const armed = wait({ id: "sch_live", status: "active" })
    expect(
      keepArmedWakes([armed], [], { waiting: true, threadId: "th_1" }).map((row) => row.id),
    ).toEqual(["sch_live"])
    expect(keepArmedWakes([armed], [], { waiting: false, threadId: "th_1" })).toEqual([])
    expect(keepArmedWakes([armed], [], { waiting: true, threadId: "th_other" })).toEqual([])
    expect(
      keepArmedWakes([armed], [wait({ id: "sch_live", status: "active" })], {
        waiting: true,
        threadId: "th_1",
      }).map((row) => row.id),
    ).toEqual(["sch_live"])
  })
})

function run(partial: Partial<ScheduleRun> = {}): ScheduleRun {
  return {
    id: "srun_1",
    schedule_id: "sch_1",
    thread_id: "th_1",
    turn_id: "tn_1",
    status: "findings",
    summary: "one thing changed",
    unread: true,
    created_at: "2026-09-19T03:00:00.000Z",
    updated_at: "2026-09-19T03:00:00.000Z",
    ...partial,
  }
}

describe("unreadFindings", () => {
  it("keeps unread findings and errors that still have a conversation", () => {
    const rows = unreadFindings([
      run({ id: "srun_old", created_at: "2026-09-19T03:00:00.000Z" }),
      run({
        id: "srun_new",
        created_at: "2026-09-19T04:00:00.000Z",
        summary: "later change",
      }),
      run({ id: "srun_quiet", status: "quiet", unread: false }),
      run({ id: "srun_read", unread: false }),
      run({ id: "srun_notarget", thread_id: "" }),
      run({ id: "srun_err", status: "error", created_at: "2026-09-19T03:30:00.000Z" }),
    ])
    expect(rows.map((row) => row.id)).toEqual(["srun_new", "srun_err", "srun_old"])
  })

  it("does not treat a sample body as the filter", () => {
    expect(JSON.stringify(unreadFindings([run()]))).not.toMatch(/CI|deploy|GitHub/)
  })
})
