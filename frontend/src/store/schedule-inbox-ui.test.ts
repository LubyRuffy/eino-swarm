import { describe, expect, it } from "vitest"

import type { Schedule } from "@/lib/types"
import { resetScheduleInboxUI, useScheduleInboxUI } from "./schedule-inbox-ui"

function row(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "standalone",
    origin_thread_id: "",
    thread_id: "",
    project_id: "",
    provider_id: "",
    model: "",
    title: "Periodic check",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 60,
    cron: "",
    status: "active",
    next_run_at: "2026-09-19T04:00:00.000Z",
    run_count: 0,
    max_runs: 0,
    created_by: "human",
    created_at: "2026-09-19T00:00:00.000Z",
    updated_at: "2026-09-19T00:00:00.000Z",
    ...partial,
  }
}

describe("schedule inbox ui", () => {
  it("toggles create and edit without leaving a dirty form behind", () => {
    resetScheduleInboxUI()
    const ui = useScheduleInboxUI.getState()
    ui.openCreate()
    expect(useScheduleInboxUI.getState().drawer).toEqual({ mode: "create" })
    ui.openCreate()
    expect(useScheduleInboxUI.getState().drawer).toBeNull()

    ui.openEdit(row())
    expect(useScheduleInboxUI.getState().form.title).toBe("Periodic check")
    expect(useScheduleInboxUI.getState().form.cadence).toBe("every")
    ui.setExpanded(true)
    ui.openEdit(row())
    expect(useScheduleInboxUI.getState().drawer).toBeNull()
    expect(useScheduleInboxUI.getState().expanded).toBe(false)

    ui.openEdit(row())
    ui.patchForm({ title: "Mine" })
    ui.reset()
    expect(useScheduleInboxUI.getState().form.title).toBe("")
    expect(useScheduleInboxUI.getState().filter).toBe("active")
  })
})
