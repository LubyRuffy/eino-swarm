import { Button } from "@/components/ui/button"
import { type MessageKey, type Vars } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import type { Schedule, ScheduleRun } from "@/lib/types"
import {
  cadenceAmount,
  isScheduleDue,
  scheduleCadenceSpec,
  scheduleHeadline,
  unreadFindings,
  type CadenceUnit,
} from "@/lib/schedule-view"

function dayKey(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

/** Compact clock for a list row: today, tomorrow, or a short date. */
export function formatScheduleWhen(iso: string, t: TFn, nowMs = Date.now()): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  const time = at.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })
  const day = dayKey(at.getTime())
  if (day === dayKey(nowMs)) return t("schedule.metaToday", { time })
  const next = new Date(nowMs)
  next.setDate(next.getDate() + 1)
  if (day === dayKey(next.getTime())) return t("schedule.metaTomorrow", { time })
  const date = at.toLocaleDateString(undefined, { month: "short", day: "numeric" })
  return t("schedule.metaOn", { date, time })
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
  let cadence = ""
  if (spec.kind === "cron") cadence = spec.expr
  else if (spec.kind === "every" || spec.kind === "delay") {
    const amt = cadenceAmount(spec.seconds)
    const unit = unitWord(amt.n, amt.unit, t)
    cadence =
      spec.kind === "every"
        ? t("schedule.metaEvery", { n: amt.n, unit })
        : t("schedule.metaDelay", { n: amt.n, unit })
  }
  const parts: string[] = []
  if (row.next_run_at) {
    parts.push(
      isScheduleDue(row.next_run_at, nowMs)
        ? t("schedule.nextRunNow")
        : formatScheduleWhen(row.next_run_at, t, nowMs),
    )
  }
  if (cadence) parts.push(cadence)
  return parts.join(" · ")
}

export function ScheduleInboxRow({
  row,
  runs,
  selected = false,
  onSelect,
  onOpenFindings,
}: {
  row: Schedule
  runs: ScheduleRun[]
  selected?: boolean
  onSelect: () => void
  onOpenFindings: (run: ScheduleRun) => void
}) {
  const t = useT()
  const headline = scheduleHeadline(row) || row.id
  const findings = unreadFindings(runs)
  const prompt = (row.prompt ?? "").trim()
  const blurb = (row.title ?? "").trim() && prompt && prompt !== (row.title ?? "").trim() ? prompt : ""
  return (
    <li
      data-testid="schedule-row"
      data-selected={selected ? "true" : undefined}
      className={cn("min-w-0 shrink-0 rounded-md", selected && "bg-sidebar-accent")}
    >
      <div className="min-w-0 px-2 py-2">
        <button
          type="button"
          data-testid="schedule-row-toggle"
          aria-current={selected ? "true" : undefined}
          className="block w-full min-w-0 text-left"
          onClick={onSelect}
        >
          <p className="truncate text-sm font-medium text-sidebar-foreground">{headline}</p>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {scheduleMetaLine(row, t)}
          </p>
          {blurb ? (
            <p
              data-testid="schedule-row-blurb"
              className="mt-1 line-clamp-2 text-xs leading-snug text-muted-foreground"
            >
              {blurb}
            </p>
          ) : null}
        </button>
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
      className="mt-1 h-auto px-0 text-xs text-muted-foreground"
      onClick={() => onOpen(latest)}
    >
      {label}
    </Button>
  )
}
