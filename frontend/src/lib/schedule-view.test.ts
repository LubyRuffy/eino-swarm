import { describe, expect, it } from "vitest"

import type { Schedule, ScheduleRun } from "@/lib/types"
import {
  applyArmedSchedule,
  applyCancelledSchedule,
  applyFiredThreadWake,
  cadenceAmount,
  inboxFilterMatch,
  inboxFilteredSchedules,
  inboxScheduleOrder,
  inboxSearchMatch,
  inboxStatusTab,
  isLiveSchedule,
  isLiveThreadWake,
  isScheduleDue,
  keepArmedWakes,
  laterScheduleDue,
  parseArmedSchedule,
  scheduleCadenceSpec,
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

  it("splits the inbox into All / Active / Paused / Completed", () => {
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
    expect(inboxStatusTab("")).toBe("active")
    expect(inboxStatusTab("paused")).toBe("paused")
    expect(inboxStatusTab("done")).toBe("completed")
    expect(inboxStatusTab("cancelled")).toBe("completed")
    expect(inboxFilterMatch(live, "active")).toBe(true)
    expect(inboxFilterMatch(paused, "active")).toBe(false)
    expect(inboxFilteredSchedules(rows, "active", "").map((row) => row.id)).toEqual([
      "sch_new",
    ])
    expect(inboxFilteredSchedules(rows, "paused", "").map((row) => row.id)).toEqual([
      "sch_paused",
    ])
    expect(inboxFilteredSchedules(rows, "completed", "").map((row) => row.id)).toEqual([
      "sch_old",
      "sch_cancel",
    ])
    expect(inboxFilteredSchedules(rows, "all", "").map((row) => row.id)).toEqual([
      "sch_paused",
      "sch_new",
      "sch_old",
      "sch_cancel",
    ])
  })

  it("filters the inbox by title or prompt without treating the query as a rule", () => {
    const named = wait({
      id: "sch_named",
      status: "active",
      title: "wake",
      prompt: "Continue the wait.",
    })
    const other = wait({
      id: "sch_other",
      status: "active",
      title: "other",
      prompt: "Stay parked.",
    })
    expect(inboxSearchMatch(named, "WAKE")).toBe(true)
    expect(inboxSearchMatch(named, "parked")).toBe(false)
    expect(inboxFilteredSchedules([named, other], "all", "wait").map((row) => row.id)).toEqual([
      "sch_named",
    ])
    expect(JSON.stringify(inboxFilteredSchedules([named, other], "all", "wait"))).not.toMatch(
      /CI|deploy|GitHub/,
    )
  })

  it("names cadence in whole minutes or hours when the seconds divide evenly", () => {
    expect(cadenceAmount(1)).toEqual({ n: 1, unit: "second" })
    expect(cadenceAmount(45)).toEqual({ n: 45, unit: "second" })
    expect(cadenceAmount(60)).toEqual({ n: 1, unit: "minute" })
    expect(cadenceAmount(1800)).toEqual({ n: 30, unit: "minute" })
    expect(cadenceAmount(3600)).toEqual({ n: 1, unit: "hour" })
    expect(cadenceAmount(0)).toEqual({ n: 0, unit: "second" })
    expect(scheduleCadenceSpec(wait({ every_s: 60, delay_s: 0, cron: "" }))).toEqual({
      kind: "every",
      seconds: 60,
    })
    expect(scheduleCadenceSpec(wait({ every_s: 0, delay_s: 15, cron: "" }))).toEqual({
      kind: "delay",
      seconds: 15,
    })
    expect(scheduleCadenceSpec(wait({ every_s: 0, delay_s: 0, cron: "0 * * * *" }))).toEqual({
      kind: "cron",
      expr: "0 * * * *",
    })
    expect(scheduleCadenceSpec(wait({ every_s: 0, delay_s: 0, cron: "" }))).toEqual({
      kind: "none",
    })
    expect(JSON.stringify(scheduleCadenceSpec(wait({ cron: "0 * * * *" })))).not.toMatch(
      /CI|deploy|GitHub/,
    )
  })

  it("treats a due slot as now", () => {
    const now = Date.parse("2026-09-21T05:00:00.000Z")
    expect(isScheduleDue("2026-09-21T04:59:00.000Z", now)).toBe(true)
    expect(isScheduleDue("2026-09-21T05:00:00.000Z", now)).toBe(true)
    expect(isScheduleDue("2026-09-21T05:01:00.000Z", now)).toBe(false)
    expect(isScheduleDue("not-a-time", now)).toBe(false)
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
