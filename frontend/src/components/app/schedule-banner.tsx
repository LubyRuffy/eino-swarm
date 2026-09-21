import { ScheduleWaitActions } from "@/components/app/schedule-wait-actions"
import { WaitMark } from "@/components/app/wait-mark"
import { composerPinClass } from "@/lib/chrome-type"
import { scheduleHeadline } from "@/lib/schedule-view"
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
  const headline = scheduleHeadline(wake)

  return (
    <div data-testid="schedule-banner" className={composerPinClass}>
      <div className="flex items-center gap-2">
        <WaitMark className="size-3.5" />
        <span className="shrink-0 text-muted-foreground">{t("schedule.waiting")}</span>
        {headline ? (
          <p data-testid="schedule-prompt" className="min-w-0 flex-1 truncate">
            {headline}
          </p>
        ) : (
          <span className="min-w-0 flex-1" />
        )}
        {when ? (
          <span
            data-testid="schedule-next"
            className="max-w-[9rem] shrink-0 truncate text-muted-foreground"
            title={t("schedule.nextCheck", { time: when })}
          >
            {when}
          </span>
        ) : (
          <span data-testid="schedule-next" className="sr-only">
            {headline}
          </span>
        )}
        <ScheduleWaitActions compact onRunNow={runNow} onCancel={cancel} />
      </div>
    </div>
  )
}
