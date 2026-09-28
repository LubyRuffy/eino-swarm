import { describe, expect, it } from "vitest"

import { t, type MessageKey, type Vars } from "@/lib/i18n"
import type { Schedule } from "@/lib/types"
import { scheduleMetaLine } from "./schedule-inbox-row"

function wait(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "standalone",
    origin_thread_id: "",
    thread_id: "",
    project_id: "",
    provider_id: "",
    model: "",
    title: "wake",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 60,
    cron: "",
    status: "active",
    next_run_at: "2026-09-21T06:00:00.000Z",
    run_count: 1,
    max_runs: 0,
    created_by: "human",
    created_at: "2026-09-19T00:00:00.000Z",
    updated_at: "2026-09-19T00:00:00.000Z",
    ...partial,
  }
}

function tr(key: MessageKey, vars?: Vars) {
  return t("en", key, vars)
}

describe("scheduleMetaLine", () => {
  const now = Date.parse("2026-09-21T05:00:00.000Z")

  it("puts the clock ahead of the cadence the way the list row paints them", () => {
    expect(scheduleMetaLine(wait(), tr, now)).toMatch(/^Today .+ · Every 1 minute$/)
    expect(scheduleMetaLine(wait({ every_s: 45 }), tr, now)).toMatch(/^Today .+ · Every 45 seconds$/)
    const tomorrow = new Date(now)
    tomorrow.setDate(tomorrow.getDate() + 1)
    expect(
      scheduleMetaLine(wait({ next_run_at: tomorrow.toISOString() }), tr, now),
    ).toMatch(/^Tomorrow .+ · Every 1 minute$/)
    expect(
      scheduleMetaLine(
        wait({ every_s: 0, delay_s: 120, next_run_at: "2026-09-21T04:00:00.000Z" }),
        tr,
        now,
      ),
    ).toBe("Next run now · In 2 minutes")
    expect(
      scheduleMetaLine(wait({ every_s: 0, delay_s: 0, cron: "0 * * * *" }), tr, now),
    ).toMatch(/^Today .+ · 0 \* \* \* \*$/)
    expect(scheduleMetaLine(wait({ next_run_at: "" }), tr, now)).toBe("Every 1 minute")
    expect(JSON.stringify(scheduleMetaLine(wait(), tr, now))).not.toMatch(/CI|deploy|GitHub/)
  })
})
