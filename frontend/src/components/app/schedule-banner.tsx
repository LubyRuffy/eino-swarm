import { Clock } from "lucide-react"

import { ScheduleWaitActions } from "@/components/app/schedule-wait-actions"
import { Badge } from "@/components/ui/badge"
import { useT } from "@/lib/use-t"
import type { Schedule } from "@/lib/types"
import { activeWake } from "@/store/app-schedule"
import { useApp } from "@/store/app"

function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}

/** Same density as GoalBanner: next check + Run now / Cancel wait. */
export function ScheduleBanner({
  wake: wakeProp,
  onCancel,
  onRunNow,
}: {
  wake?: Schedule
  onCancel?: () => void
  onRunNow?: () => void
}) {
  const t = useT()
  const schedules = useApp((s) => s.schedules)
  const activeId = useApp((s) => s.activeId)
  const running = useApp((s) => s.status.running)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const runScheduleNow = useApp((s) => s.runScheduleNow)
  const wake = wakeProp ?? activeWake(schedules, activeId)
  // Working is not waiting. Run now used to leave this chip up with the
  // old due time, so it looked like the click did nothing.
  if (!wake || wake.kind !== "thread" || wake.status !== "active" || running) return null
  const cancel = onCancel ?? (() => void deleteSchedule(wake.id))
  const runNow = onRunNow ?? (() => void runScheduleNow(wake.id))
  const when = wake.next_run_at ? formatWhen(wake.next_run_at) : ""

  return (
    <div data-testid="schedule-banner" className="mb-2 rounded-xl border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <Clock className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline">{t("schedule.waiting")}</Badge>
            {when ? (
              <span data-testid="schedule-next" className="text-xs text-muted-foreground">
                {t("schedule.nextCheck", { time: when })}
              </span>
            ) : (
              <span data-testid="schedule-next" className="text-xs text-muted-foreground">
                {wake.title}
              </span>
            )}
          </div>
          {wake.title ? (
            <p className="mt-1 truncate text-sm">{wake.title}</p>
          ) : null}
        </div>
        <ScheduleWaitActions onRunNow={runNow} onCancel={cancel} />
      </div>
    </div>
  )
}
