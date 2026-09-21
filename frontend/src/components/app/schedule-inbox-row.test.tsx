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

  it("joins cadence and next check the way the list row paints them", () => {
    expect(scheduleMetaLine(wait(), tr, now)).toMatch(/^Every 1 minute · /)
    expect(scheduleMetaLine(wait({ every_s: 45 }), tr, now)).toMatch(/^Every 45 seconds · /)
    expect(
      scheduleMetaLine(
        wait({ every_s: 0, delay_s: 120, next_run_at: "2026-09-21T04:00:00.000Z" }),
        tr,
        now,
      ),
    ).toBe("In 2 minutes · Next run now")
    expect(
      scheduleMetaLine(wait({ every_s: 0, delay_s: 0, cron: "0 * * * *" }), tr, now),
    ).toMatch(/^0 \* \* \* \* · /)
    expect(scheduleMetaLine(wait({ next_run_at: "" }), tr, now)).toBe("Every 1 minute")
    expect(JSON.stringify(scheduleMetaLine(wait(), tr, now))).not.toMatch(/CI|deploy|GitHub/)
  })
})
