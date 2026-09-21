import { useState } from "react"

import { Button } from "@/components/ui/button"
import { type MessageKey, type Vars } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import type { Schedule, ScheduleRun } from "@/lib/types"
import {
  cadenceAmount,
  isLiveSchedule,
  isScheduleDue,
  scheduleCadenceSpec,
  scheduleHeadline,
  unreadFindings,
  type CadenceUnit,
} from "@/lib/schedule-view"

export function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}

type TFn = (key: MessageKey, vars?: Vars) => string

function unitWord(n: number, unit: CadenceUnit, t: TFn): string {
  if (unit === "hour") return n === 1 ? t("schedule.unitHour") : t("schedule.unitHours")
  if (unit === "minute") {
    return n === 1 ? t("schedule.unitMinute") : t("schedule.unitMinutes")
  }
  return n === 1 ? t("schedule.unitSecond") : t("schedule.unitSeconds")
}

export function scheduleMetaLine(row: Schedule, t: TFn, nowMs?: number): string {
  const spec = scheduleCadenceSpec(row)
  const parts: string[] = []
  if (spec.kind === "cron") parts.push(spec.expr)
  else if (spec.kind === "every" || spec.kind === "delay") {
    const amt = cadenceAmount(spec.seconds)
    const unit = unitWord(amt.n, amt.unit, t)
    parts.push(
      spec.kind === "every"
        ? t("schedule.metaEvery", { n: amt.n, unit })
        : t("schedule.metaDelay", { n: amt.n, unit }),
    )
  }
  if (row.next_run_at) {
    parts.push(
      isScheduleDue(row.next_run_at, nowMs)
        ? t("schedule.nextRunNow")
        : t("schedule.nextCheck", { time: formatWhen(row.next_run_at) }),
    )
  }
  return parts.join(" · ")
}

export function ScheduleInboxRow({
  row,
  runs,
  onPause,
  onResume,
  onCancel,
  onRunNow,
  onOpenFindings,
}: {
  row: Schedule
  runs: ScheduleRun[]
  onPause: () => void
  onResume: () => void
  onCancel: () => void
  onRunNow: () => void
  onOpenFindings: (run: ScheduleRun) => void
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const headline = scheduleHeadline(row) || row.id
  const findings = unreadFindings(runs)
  const live = isLiveSchedule(row)
  const tab = row.status === "paused" ? "paused" : live ? "active" : "completed"
  return (
    <li
      data-testid="schedule-row"
      className="min-w-0 shrink-0 border-b border-border last:border-b-0"
    >
      <div className="flex min-w-0 items-start gap-3 py-2.5">
        <span
          aria-hidden="true"
          className={cn(
            "mt-1.5 size-2.5 shrink-0 rounded-full",
            tab === "active" && "bg-done",
            tab === "paused" && "bg-running",
            tab === "completed" && "bg-muted-foreground/30",
          )}
        />
        <div className="min-w-0 flex-1">
          <button
            type="button"
            data-testid="schedule-row-toggle"
            aria-expanded={open}
            className="block w-full min-w-0 text-left"
            onClick={() => setOpen((on) => !on)}
          >
            <p className="truncate text-sm font-medium">{headline}</p>
            <p className="mt-0.5 truncate text-xs text-muted-foreground">
              {scheduleMetaLine(row, t)}
            </p>
          </button>
          {open ? (
            <div className="mt-2 flex min-w-0 flex-wrap gap-1">
              {row.status === "active" ? (
                <Button type="button" variant="ghost" size="sm" onClick={onPause}>
                  {t("schedule.pause")}
                </Button>
              ) : row.status === "paused" ? (
                <Button type="button" variant="ghost" size="sm" onClick={onResume}>
                  {t("schedule.resume")}
                </Button>
              ) : null}
              {live ? (
                <>
                  <Button type="button" variant="ghost" size="sm" onClick={onRunNow}>
                    {t("schedule.runNow")}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={t("schedule.cancel")}
                    onClick={onCancel}
                  >
                    {t("schedule.cancel")}
                  </Button>
                </>
              ) : null}
            </div>
          ) : null}
        </div>
        <FindingsControl findings={findings} onOpen={onOpenFindings} />
      </div>
    </li>
  )
}

function FindingsControl({
  findings,
  onOpen,
}: {
  findings: ScheduleRun[]
  onOpen: (run: ScheduleRun) => void
}) {
  const t = useT()
  if (findings.length === 0) return null
  const latest = findings[0]
  const label =
    findings.length === 1
      ? t("schedule.openFindings")
      : t("schedule.openFindingsCount", { n: findings.length })
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="shrink-0"
      onClick={() => onOpen(latest)}
    >
      {label}
    </Button>
  )
}
